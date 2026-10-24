package sim

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ShipmentRequest — демо-поставка (ИС-4): состав и расхождения, которые нужно заложить в короба.
type ShipmentRequest struct {
	Supplier  string `json:"supplier"`
	Recipient string `json:"recipient"`
	Positions []Item `json:"positions"`
	BoxSize   int    `json:"box_size"`
	// Расхождения меняют то, что лежит в коробах, а ТТН остаётся «правильной».
	Shortage    int    `json:"shortage"`
	Surplus     int    `json:"surplus"`
	ForeignMark bool   `json:"foreign_mark"`
	Mixup       *Mixup `json:"mixup,omitempty"`
}

// Mixup — пересорт: вместо части единиц позиции From в короб положена продукция To.
type Mixup struct {
	From string `json:"from"`
	To   string `json:"to"`
	Qty  int    `json:"qty"`
}

// PrintBox — короб на печатной странице: этикетка и то, что в нём лежит на самом деле.
type PrintBox struct {
	Code  string      `json:"code"`
	Units []PrintUnit `json:"units"`
}

type PrintUnit struct {
	Name    string `json:"name"`
	Barcode string `json:"barcode,omitempty"`
	Mark    string `json:"mark,omitempty"`
	Crpt    string `json:"crpt,omitempty"`
}

type Shipment struct {
	ID     uuid.UUID
	TTNID  uuid.UUID
	Number string
}

const (
	alnumUpper = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	alnum      = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	gs         = "\x1d"
)

func randomString(alphabet string, n int) string {
	b := make([]byte, n)
	for i := range b {
		k, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b[i] = alphabet[k.Int64()]
	}
	return string(b)
}

// newMark — учебная марка: префикс PKN не совпадает ни с одной настоящей маркой.
func newMark() string { return "PKN" + randomString(alnumUpper, 13) }

func gs1Check(body string) byte {
	sum := 0
	for i := len(body) - 1; i >= 0; i-- {
		d := int(body[i] - '0')
		if (len(body)-1-i)%2 == 0 {
			d *= 3
		}
		sum += d
	}
	return byte('0' + (10-sum%10)%10)
}

// sscc — номер короба: цифра расширения, префикс компании 4600000 и порядковый номер.
func sscc(ttn int64, box int) string {
	body := fmt.Sprintf("0%s%09d", "4600000", ttn*1000+int64(box))
	return body + string(gs1Check(body))
}

type product struct {
	Alcocode, Name, Accounting string
	Barcode, GTIN              *string
	Crpt                       bool
	Price                      float64
	VolumeMl                   *int
}

func (p product) marked() bool { return p.Accounting == "marked" }

// crptCode — код «Честного знака» по правилам GS1: (01) GTIN, (21) серийный номер из 7 символов
// (первый — код страны, 5 — Россия), разделитель GS, (93) код проверки из четырёх символов.
func (p product) crptCode() (full, unit string, ok bool) {
	gtin := ""
	switch {
	case p.GTIN != nil:
		gtin = *p.GTIN
	case p.Barcode != nil && len(*p.Barcode) == 13:
		gtin = "0" + *p.Barcode
	default:
		return "", "", false
	}
	serial := "5" + randomString(alnum, 6)
	return "01" + gtin + "21" + serial + gs + "93" + randomString(alnum, 4), gtin + serial, true
}

type unit struct {
	product
	mark       string
	crpt, code string // полный код «Честного знака» и его единица (GTIN + серийный номер)
	inTTN      bool
}

func (u unit) print() PrintUnit {
	pu := PrintUnit{Name: u.Name, Mark: u.mark, Crpt: u.crpt}
	if u.Barcode != nil {
		pu.Barcode = *u.Barcode
	}
	return pu
}

func newUnit(p product, inTTN bool) unit {
	u := unit{product: p, inTTN: inTTN}
	if p.marked() {
		u.mark = newMark()
	}
	if p.Crpt {
		u.crpt, u.code, _ = p.crptCode()
	}
	return u
}

// CreateShipment генерирует поставку: марки, коды «Честного знака», короба, ТТН во «входящие»
// получателя и печатную страницу с тем, что лежит в коробах на самом деле.
func (s *Sim) CreateShipment(ctx context.Context, req ShipmentRequest) (Shipment, error) {
	var sh Shipment
	if req.BoxSize <= 0 {
		req.BoxSize = 6
	}
	req.BoxSize = min(req.BoxSize, 48)
	if len(req.Positions) == 0 {
		return sh, errors.New("в поставке нет позиций")
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		supplier, err := s.Participant(ctx, req.Supplier)
		if err != nil || supplier.Kind != "supplier" {
			return fmt.Errorf("поставщик %s не найден", req.Supplier)
		}
		if p, err := s.Participant(ctx, req.Recipient); err != nil || p.Kind == "supplier" {
			return fmt.Errorf("получатель %s не найден", req.Recipient)
		}
		load := func(alcocode string) (product, error) {
			var p product
			err := tx.QueryRow(ctx, `select alcocode, name, egais_accounting, barcode, gtin, crpt, price::float8, volume_ml
				from products where alcocode = $1`, alcocode).
				Scan(&p.Alcocode, &p.Name, &p.Accounting, &p.Barcode, &p.GTIN, &p.Crpt, &p.Price, &p.VolumeMl)
			if errors.Is(err, pgx.ErrNoRows) {
				return p, fmt.Errorf("продукции с алкокодом %s нет в справочнике", alcocode)
			}
			return p, err
		}

		var n int64
		if err := tx.QueryRow(ctx, `select nextval('ttn_number_seq')`).Scan(&n); err != nil {
			return err
		}
		sh.TTNID, _ = uuid.NewV7()
		sh.Number = fmt.Sprintf("ТТН-%06d", n)
		ttn := TTN{
			ID: sh.TTNID, Number: sh.Number, Date: time.Now().Format("2006-01-02"), Consignee: req.Recipient,
			Shipper: Party{FSRARID: supplier.FSRARID, Name: supplier.Name, INN: supplier.INN, KPP: supplier.KPP},
		}

		// Короба одной позиции; physical — то, что окажется в коробах после расхождений.
		type box struct {
			code     string
			identity int
			units    []unit
		}
		var boxes []*box
		var all []unit
		for i, it := range req.Positions {
			if it.Qty <= 0 {
				continue
			}
			p, err := load(it.Alcocode)
			if err != nil {
				return err
			}
			pos := TTNPosition{Identity: i + 1, Alcocode: p.Alcocode, Name: p.Name, VolumeMl: p.VolumeMl, Qty: it.Qty,
				Price: p.Price, Marked: p.marked()}
			for k := 0; k < it.Qty; k++ {
				if k%req.BoxSize == 0 {
					boxes = append(boxes, &box{code: sscc(n, len(boxes)+1), identity: pos.Identity})
				}
				u := newUnit(p, true)
				b := boxes[len(boxes)-1]
				b.units = append(b.units, u)
				all = append(all, u)
				if u.mark != "" {
					pos.Marks = append(pos.Marks, u.mark)
				}
			}
			ttn.Positions = append(ttn.Positions, pos)
		}
		if len(ttn.Positions) == 0 {
			return errors.New("в поставке нет позиций")
		}
		for _, b := range boxes {
			pkg := TTNPackage{Code: b.code, Identity: b.identity, Qty: len(b.units)}
			for _, u := range b.units {
				if u.mark != "" {
					pkg.Marks = append(pkg.Marks, u.mark)
				}
			}
			ttn.Packages = append(ttn.Packages, pkg)
		}

		// Расхождения: ТТН не меняется, меняется содержимое коробов.
		var notes []string
		var extra []unit
		boxesOf := func(identity int) []*box {
			var out []*box
			for _, b := range boxes {
				if b.identity == identity {
					out = append(out, b)
				}
			}
			return out
		}
		first := boxesOf(1)
		if req.Shortage > 0 && len(first) > 0 {
			last := first[len(first)-1]
			k := min(req.Shortage, len(last.units))
			last.units = last.units[:len(last.units)-k]
			notes = append(notes, fmt.Sprintf("недостача %d ед. в коробе %s", k, last.code))
		}
		if req.Surplus > 0 && len(first) > 0 {
			last := first[len(first)-1]
			for range req.Surplus {
				if len(last.units) == 0 {
					break
				}
				u := newUnit(last.units[0].product, false)
				last.units = append(last.units, u)
				extra = append(extra, u)
			}
			notes = append(notes, fmt.Sprintf("излишек %d ед. в коробе %s", req.Surplus, last.code))
		}
		if req.ForeignMark {
			for _, b := range boxes {
				if len(b.units) > 0 && b.units[0].mark != "" {
					u := b.units[0]
					u.mark, u.inTTN = newMark(), false
					b.units[0] = u
					extra = append(extra, u)
					notes = append(notes, fmt.Sprintf("чужая марка %s в коробе %s", u.mark, b.code))
					break
				}
			}
		}
		if m := req.Mixup; m != nil && m.Qty > 0 {
			to, err := load(m.To)
			if err != nil {
				return err
			}
			for _, pos := range ttn.Positions {
				if pos.Alcocode != m.From {
					continue
				}
				if bs := boxesOf(pos.Identity); len(bs) > 0 {
					b := bs[0]
					k := min(m.Qty, len(b.units))
					for j := range k {
						u := newUnit(to, false)
						b.units[j] = u
						extra = append(extra, u)
					}
					notes = append(notes, fmt.Sprintf("пересорт: %d ед. «%s» вместо «%s» в коробе %s", k, to.Name, pos.Name, b.code))
				}
				break
			}
		}

		// Марки ТТН едут к получателю, лишние марки числятся у поставщика: на приёмке они «чужие».
		for _, u := range all {
			if u.mark == "" {
				continue
			}
			pkg := ""
			for _, b := range ttn.Packages {
				for _, m := range b.Marks {
					if m == u.mark {
						pkg = b.Code
					}
				}
			}
			if _, err := tx.Exec(ctx, `insert into marks (code, alcocode, owner_fsrar, status, package_code, ttn_id)
				values ($1, $2, $3, 'in_transit', $4, $5)`, u.mark, u.Alcocode, supplier.FSRARID, pkg, sh.TTNID); err != nil {
				return err
			}
			if err := markEvent(ctx, tx, []string{u.mark}, "Отгружена по "+sh.Number, supplier.FSRARID, sh.TTNID); err != nil {
				return err
			}
		}
		for _, u := range extra {
			if u.mark != "" {
				if _, err := tx.Exec(ctx, `insert into marks (code, alcocode, owner_fsrar, status)
					values ($1, $2, $3, 'on_balance') on conflict do nothing`, u.mark, u.Alcocode, supplier.FSRARID); err != nil {
					return err
				}
				if err := markEvent(ctx, tx, []string{u.mark}, "На балансе поставщика", supplier.FSRARID, sh.TTNID); err != nil {
					return err
				}
			}
		}
		batch := fmt.Sprintf("П%s-%06d", time.Now().Format("060102"), n)
		insertCode := func(u unit, ttnID *uuid.UUID) error {
			if u.code == "" {
				return nil
			}
			_, err := tx.Exec(ctx, `insert into crpt_codes (code, gtin, serial, alcocode, owner_fsrar, status, produced_at,
				expires_at, batch, ttn_id) values ($1, $2, $3, $4, $5, 'introduced', current_date, current_date + 180, $6, $7)
				on conflict do nothing`, u.code, u.code[:14], u.code[14:], u.Alcocode, supplier.FSRARID, batch, ttnID)
			return err
		}
		for _, u := range all {
			if err := insertCode(u, &sh.TTNID); err != nil {
				return err
			}
		}
		for _, u := range extra {
			if err := insertCode(u, nil); err != nil {
				return err
			}
		}

		var print []PrintBox
		for _, b := range boxes {
			pb := PrintBox{Code: b.code}
			for _, u := range b.units {
				pb.Units = append(pb.Units, u.print())
			}
			print = append(print, pb)
		}
		if _, err := tx.Exec(ctx, `insert into documents (id, fsrar_from, fsrar_to, type, payload, status)
			values ($1, $2, $3, 'WayBill', $4, 'accepted')`, sh.TTNID, supplier.FSRARID, req.Recipient, ttn); err != nil {
			return err
		}
		if err := put(ctx, tx, req.Recipient, "WayBill", ttn, &sh.TTNID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `insert into shipments (ttn_id, supplier, recipient, number, print, notes)
			values ($1, $2, $3, $4, $5, $6) returning id`,
			sh.TTNID, supplier.FSRARID, req.Recipient, sh.Number, print, strings.Join(notes, "; ")).Scan(&sh.ID)
	})
	return sh, err
}

// ShipmentInfo — поставка для списка и печатной страницы.
type ShipmentInfo struct {
	ID        uuid.UUID
	Number    string
	Supplier  string
	Recipient string
	Notes     string
	CreatedAt time.Time
	Print     []PrintBox
}

func (s *Sim) Shipments(ctx context.Context) ([]ShipmentInfo, error) {
	rows, err := s.pool.Query(ctx, `select s.id, s.number, sp.name, rp.name, s.notes, s.created_at, s.print
		from shipments s join participants sp on sp.fsrar_id = s.supplier join participants rp on rp.fsrar_id = s.recipient
		order by s.created_at desc limit 50`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ShipmentInfo])
}

func (s *Sim) Shipment(ctx context.Context, id uuid.UUID) (ShipmentInfo, error) {
	rows, err := s.pool.Query(ctx, `select s.id, s.number, sp.name, rp.name, s.notes, s.created_at, s.print
		from shipments s join participants sp on sp.fsrar_id = s.supplier join participants rp on rp.fsrar_id = s.recipient
		where s.id = $1`, id)
	if err != nil {
		return ShipmentInfo{}, err
	}
	info, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[ShipmentInfo])
	if errors.Is(err, pgx.ErrNoRows) {
		return info, ErrNotFound
	}
	return info, err
}
