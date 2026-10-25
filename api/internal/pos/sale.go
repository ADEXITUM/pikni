package pos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/crpt"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/locations"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/pos/store"
	"github.com/ADEXITUM/pikni/api/internal/reservations"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

// strongAbv — порог крепкого алкоголя для региональных часов продажи (scope strong, НСИ-5).
const strongAbv = 15

// autoClose — незакрытая смена закрывается сама через сутки (КС-5).
const autoClose = 24 * time.Hour

// Item — товар в каталоге кассы.
type Item struct {
	store.PosCatalogRow
	MinPrice *int64
}

func (i Item) alcohol() bool { return i.Alcohol && i.Abv != nil }
func (i Item) marked() bool  { return i.EgaisAccounting != nil && *i.EgaisAccounting == "marked" }

// Refusal — почему позицию нельзя продать: машинный код и текст для кассира.
type Refusal struct {
	Code, Message string
}

// RefusalError — продажа невозможна; чек не принимается.
type RefusalError struct{ Refusal }

func (e *RefusalError) Error() string { return e.Code + ": " + e.Message }

// Catalog — каталог для кассы с МРЦ для алкоголя. Касса хранит его для автономной работы.
func (s *Service) Catalog(ctx context.Context) ([]Item, error) {
	rows, err := s.q.PosCatalog(ctx, nil)
	if err != nil {
		return nil, err
	}
	items := make([]Item, len(rows))
	now := s.now()
	for i, r := range rows {
		items[i] = Item{PosCatalogRow: r}
		if items[i].MinPrice, err = s.minPrice(ctx, items[i], now); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) item(ctx context.Context, variant uuid.UUID) (*Item, error) {
	rows, err := s.q.PosCatalog(ctx, &variant)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	it := Item{PosCatalogRow: rows[0]}
	it.MinPrice, err = s.minPrice(ctx, it, s.now())
	return &it, err
}

// minPrice — нижняя граница цены: МРЦ Минфина для крепкого алкоголя, ЕМЦ для сигарет.
func (s *Service) minPrice(ctx context.Context, it Item, at time.Time) (*int64, error) {
	switch {
	case it.alcohol():
		return s.catalog.MinRetailPrice(ctx, it.ID, at)
	case it.Cigarettes:
		p, err := s.q.TobaccoMinPrice(ctx, at)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return &p, err
	}
	return nil, nil
}

// crptUnit — единица «Честного знака» (GTIN и серийный номер) из кода, который прислала касса,
// и МРЦ, если это код сигаретной пачки. Уже выделенная единица возвращается как есть.
func crptUnit(code string) (string, *int64) {
	if sc, ok := catalog.ParseScan(code); ok && sc.Unit != "" {
		return sc.Unit, sc.MaxPrice
	}
	return code, nil
}

// salePrice — цена строки: пачку нельзя продать дороже МРЦ с пачки, поэтому цена каталога выше
// неё (или её отсутствие) заменяется МРЦ (НК РФ, ст. 187.1, п. 2).
func salePrice(price int64, maxPrice *int64) int64 {
	if maxPrice != nil && (price <= 0 || price > *maxPrice) {
		return *maxPrice
	}
	return price
}

// Time — серверное время и окна продажи алкоголя в точке кассы сегодня. Касса не доверяет своим часам.
func (s *Service) Time(ctx context.Context, d Device) (locations.SaleCheck, error) {
	return s.locations.CheckSale(ctx, d.LocationID, s.now())
}

func clock(min int) string { return fmt.Sprintf("%02d:%02d", min/60, min%60) }

// Check — позиция для чека и всё, что мешает её продать (КС-2): время продажи алкоголя, марка,
// код «Честного знака» в разрешительном режиме, цена.
type Check struct {
	Item     *Item
	Mark     string
	Crpt     string
	Price    int64
	MaxPrice *int64
	Refusals []Refusal
}

func (s *Service) Check(ctx context.Context, pc Context, raw string) (Check, error) {
	var c Check
	scan, parsed := catalog.ParseScan(raw)
	var variant *uuid.UUID
	switch {
	case parsed && scan.Mark != "":
		c.Mark = scan.Mark
		m, err := s.marks.Find(ctx, scan.Mark)
		if err != nil {
			return c, err
		}
		if m == nil {
			c.Refusals = append(c.Refusals, Refusal{"mark.unknown", "Марки нет в учёте сети. Отложите бутылку и сообщите управляющему"})
			return c, nil
		}
		variant = &m.VariantID
	case parsed:
		v, err := s.catalog.FindVariant(ctx, scan)
		if err != nil {
			return c, err
		}
		variant, c.MaxPrice = v, scan.MaxPrice
		// Касса возвращает код целиком: из него сервер снова возьмёт МРЦ при приёме чека.
		if scan.Unit != "" {
			c.Crpt = strings.TrimSpace(raw)
		}
	default:
		v, err := s.catalog.FindVariant(ctx, catalog.Scan{Code: raw})
		if err != nil {
			return c, err
		}
		variant = v
	}
	if variant == nil {
		c.Refusals = append(c.Refusals, Refusal{"item.unknown", "Товара с таким кодом нет в каталоге"})
		return c, nil
	}
	it, err := s.item(ctx, *variant)
	if err != nil {
		return c, err
	}
	if it == nil {
		c.Refusals = append(c.Refusals, Refusal{"item.unknown", "Товар в архиве — продавать его нельзя"})
		return c, nil
	}
	c.Item = it
	c.Price = salePrice(it.PriceKopecks, c.MaxPrice)
	c.Refusals, err = s.refusals(ctx, pc, *it, c.Mark, c.Crpt, c.Price, s.now(), false, nil)
	return c, err
}

// refusals — проверки позиции. Их повторяет и приём чека: касса могла проверить устаревшие данные.
// reserved — марки брони, которую выдаёт этот чек: их отложенный статус продаже не мешает.
func (s *Service) refusals(ctx context.Context, pc Context, it Item, mark, code string, price int64, at time.Time, offline bool, reserved map[string]bool) ([]Refusal, error) {
	var out []Refusal
	code, maxPrice := crptUnit(code)
	if it.alcohol() {
		sc, err := s.locations.CheckSale(ctx, pc.Device.LocationID, at)
		if err != nil {
			return nil, err
		}
		v := sc.Other
		if *it.Abv > strongAbv {
			v = sc.Strong
		}
		if !v.Allowed {
			msg := fmt.Sprintf("Продажа алкоголя сейчас запрещена: в регионе точки — с %s до %s", clock(v.From), clock(v.To))
			if v.Ban != "" {
				msg = "Сегодня продажа алкоголя запрещена весь день: " + v.Ban
			}
			out = append(out, Refusal{"sale.alcohol_time", msg})
		}
	}
	if it.marked() {
		switch {
		case mark == "":
			out = append(out, Refusal{"mark.required", "Отсканируйте акцизную марку на бутылке, а не штрихкод"})
		case offline:
			out = append(out, Refusal{"offline.marked", "Нет связи — продажа товара с маркой невозможна"})
		default:
			m, err := s.marks.Find(ctx, mark)
			if err != nil {
				return nil, err
			}
			switch {
			case m == nil:
				out = append(out, Refusal{"mark.unknown", "Марки нет в учёте сети. Отложите бутылку и сообщите управляющему"})
			case m.VariantID != it.ID:
				out = append(out, Refusal{"mark.unknown", "Марка относится к другому товару"})
			case m.Status == marks.Reserved && reserved[mark] && m.LocationID == pc.Device.LocationID:
			case m.Status == marks.Reserved:
				out = append(out, Refusal{"mark.reserved", "Бутылка отложена для покупателя по брони — выдайте её через «Брони»"})
			case m.Status == marks.Sold:
				out = append(out, Refusal{"mark.sold", "Бутылка с этой маркой уже продана. Отложите её и сообщите управляющему"})
			case m.Status != marks.InStock || m.LocationID != pc.Device.LocationID:
				out = append(out, Refusal{"mark.not_here", "Марка не числится на балансе магазина. Отложите бутылку и сообщите управляющему"})
			}
		}
	}
	if it.CrptMarked {
		switch {
		case code == "":
			out = append(out, Refusal{"crpt.required", "Отсканируйте код «Честного знака» на упаковке"})
		case offline:
			out = append(out, Refusal{"offline.crpt", "Нет связи — продажа товара с маркировкой невозможна"})
		case pc.Device.FSRARID != nil:
			verdicts, err := s.crpt.Check(ctx, *pc.Device.FSRARID, []string{code})
			if errors.Is(err, crpt.ErrUnavailable) {
				out = append(out, Refusal{"crpt.unavailable", "«Честный знак» не отвечает — продать маркированный товар сейчас нельзя"})
			} else if err != nil {
				return nil, err
			} else if len(verdicts) == 1 && !verdicts[0].Allowed {
				out = append(out, crptRefusal(verdicts[0].Reason))
			}
		}
	}
	// Товар идущей ревизии продаётся, если ревизия не останавливает продажи (решение 0060).
	var stocktake *string
	if err := s.pool.QueryRow(ctx, `select case when stocktake_stops_sales(b) then b end from stocktake_blocks($1, $2, $3, null) b`, pc.Device.LocationID, pc.Device.FloorZone, it.ID).Scan(&stocktake); err != nil {
		return nil, err
	}
	if stocktake != nil {
		out = append(out, Refusal{"sale.stocktake", "Товар в ревизии " + *stocktake + " — продать его можно после её окончания"})
	}
	switch {
	case price <= 0:
		out = append(out, Refusal{"price.missing", "У товара нет розничной цены. Сообщите управляющему"})
	case maxPrice != nil && price > *maxPrice:
		out = append(out, Refusal{"price.above_max", "Цена выше МРЦ на пачке (" + money(*maxPrice) + "): дороже продавать нельзя"})
	case it.MinPrice != nil && price < *it.MinPrice && it.Cigarettes:
		out = append(out, Refusal{"price.below_min", "Цена ниже единой минимальной цены табачной продукции (" + money(*it.MinPrice) + ")"})
	case it.MinPrice != nil && price < *it.MinPrice:
		out = append(out, Refusal{"price.below_min", "Цена ниже минимальной розничной (МРЦ " + money(*it.MinPrice) + ")"})
	}
	return out, nil
}

func money(kopecks int64) string { return fmt.Sprintf("%d,%02d ₽", kopecks/100, kopecks%100) }

func crptRefusal(reason string) Refusal {
	messages := map[string]string{
		"code.unknown":   "Кода нет в «Честном знаке» — товар не продавать",
		"code.sold":      "Этот код уже продан — товар не продавать",
		"code.blocked":   "Товар заблокирован: партия отозвана — не продавать",
		"code.expired":   "Срок годности истёк — не продавать",
		"code.not_owner": "Код числится за другой организацией — товар не продавать",
	}
	msg, ok := messages[reason]
	if !ok {
		msg = "«Честный знак» запретил продажу"
	}
	return Refusal{"crpt." + reason[len("code."):], msg}
}

// Shift — смена кассы и её итоги.
type Shift struct {
	ID        uuid.UUID
	Number    int32
	OpenedAt  time.Time
	ClosedAt  *time.Time
	Totals    store.ShiftTotalsRow
	EgaisAct  bool
	SaleDoc   uuid.UUID
	CashierID uuid.UUID
}

func (s *Service) totals(ctx context.Context, q *store.Queries, sh *Shift) error {
	t, err := q.ShiftTotals(ctx, sh.ID)
	sh.Totals = t
	return err
}

// CurrentShift — открытая смена кассы или nil. Смена, открытая больше суток назад, закрывается сама.
func (s *Service) CurrentShift(ctx context.Context, pc Context) (*Shift, error) {
	row, err := s.q.CurrentShift(ctx, pc.Device.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if s.now().Sub(row.OpenedAt) > autoClose {
		if _, err := s.close(ctx, pc, "auto_closed"); err != nil {
			return nil, err
		}
		return nil, nil
	}
	sh := &Shift{ID: row.ID, Number: row.Number, OpenedAt: row.OpenedAt, SaleDoc: row.SaleDocumentID, CashierID: row.CashierID}
	return sh, s.totals(ctx, s.q, sh)
}

// OpenShift открывает смену и документ «Продажа» по торговому залу точки.
func (s *Service) OpenShift(ctx context.Context, pc Context) (*Shift, error) {
	if cur, err := s.CurrentShift(ctx, pc); err != nil || cur != nil {
		if cur != nil {
			return nil, ErrShiftOpen
		}
		return nil, err
	}
	var sh Shift
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		tz, err := time.LoadLocation(pc.Device.Timezone)
		if err != nil {
			return err
		}
		local := s.now().In(tz)
		date := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
		doc, err := s.docs.OpenSale(ctx, tx, pc.Device.LocationID, pc.Device.FloorZone, pc.Cashier.ID, date)
		if err != nil {
			return err
		}
		n, err := q.NextShiftNumber(ctx, pc.Device.ID)
		if err != nil {
			return err
		}
		row, err := q.CreateShift(ctx, store.CreateShiftParams{DeviceID: pc.Device.ID, CashierID: pc.Cashier.ID, Number: n, SaleDocumentID: doc})
		if db.IsUniqueViolation(err) {
			return ErrShiftOpen
		}
		if err != nil {
			return err
		}
		sh = Shift{ID: row.ID, Number: n, OpenedAt: row.OpenedAt, SaleDoc: doc, CashierID: pc.Cashier.ID}
		return nil
	})
	return &sh, err
}

// CloseShift закрывает смену: документ «Продажа» проводится, проданное пиво уходит в ЕГАИС актом
// списания из зала «Реализация» (КС-5).
func (s *Service) CloseShift(ctx context.Context, pc Context) (*Shift, error) {
	return s.close(ctx, pc, "closed")
}

func (s *Service) close(ctx context.Context, pc Context, status string) (*Shift, error) {
	var sh Shift
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.LockCurrentShift(ctx, pc.Device.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoShift
		}
		if err != nil {
			return err
		}
		sh = Shift{ID: row.ID, Number: row.Number, OpenedAt: row.OpenedAt, SaleDoc: row.SaleDocumentID, CashierID: row.CashierID}
		sold, err := q.ShiftQuantitySales(ctx, row.ID)
		if err != nil {
			return err
		}
		var items []egais.Item
		for _, l := range sold {
			items = append(items, egais.Item{VariantID: l.VariantID, Alcocode: l.Alcocode, Qty: l.Qty})
		}
		if sh.EgaisAct, err = s.docs.CloseSale(ctx, tx, row.SaleDocumentID, pc.Device.LocationID, row.CashierID, items); err != nil {
			return err
		}
		if sh.ClosedAt, err = q.CloseShift(ctx, store.CloseShiftParams{ID: row.ID, Status: status}); err != nil {
			return err
		}
		return s.totals(ctx, q, &sh)
	})
	return &sh, err
}

// NewReceipt — чек с кассы.
type NewReceipt struct {
	ID           uuid.UUID
	Refund       bool
	ReturnOf     *uuid.UUID
	Card         bool
	AgeConfirmed bool
	CreatedAt    time.Time
	Offline      bool
	Lines        []NewLine
	// ReservationID — чек выдаёт бронь (НЛ-4).
	ReservationID *uuid.UUID
}

type NewLine struct {
	VariantID uuid.UUID
	Qty       int32
	Price     int64
	Mark      string
	Crpt      string
}

// CreateReceipt принимает чек. Повторная отправка с тем же идентификатором возвращает прежний чек.
// Все проверки повторяются на сервере; чек с марками синхронно уходит в ЕГАИС, и при отказе ЕГАИС
// продажа не проводится. Коды «Честного знака» выбывают после проведения.
func (s *Service) CreateReceipt(ctx context.Context, pc Context, r NewReceipt) (uuid.UUID, error) {
	if prev, err := s.q.GetReceipt(ctx, r.ID); err == nil {
		if prev.DeviceID != pc.Device.ID {
			return uuid.Nil, ErrNotFound
		}
		return r.ID, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	if len(r.Lines) == 0 {
		return uuid.Nil, &RefusalError{Refusal{"receipt.empty", "В чеке нет позиций"}}
	}
	var reserved map[string]bool
	if r.ReservationID != nil {
		res, err := s.reservations.Peek(ctx, *r.ReservationID)
		if err != nil || r.Refund || r.Offline || res.LocationID != pc.Device.LocationID || res.Status != reservations.StatusConfirmed {
			return uuid.Nil, &RefusalError{Refusal{"reservation.not_ready", "Бронь не найдена, не подтверждена или оформлена в другой точке"}}
		}
		reserved = map[string]bool{}
		for _, m := range res.Marks {
			reserved[m] = true
		}
	}
	// Без связи касса пробивает чек по своему времени; сервер проверяет продажу по нему.
	at := s.now()
	if r.Offline {
		at = r.CreatedAt
	}
	needAge := false
	var total int64
	var marksList, codes []string
	for i := range r.Lines {
		l := &r.Lines[i]
		raw := l.Crpt
		l.Crpt, _ = crptUnit(l.Crpt)
		it, err := s.item(ctx, l.VariantID)
		if err != nil {
			return uuid.Nil, err
		}
		if it == nil {
			return uuid.Nil, &RefusalError{Refusal{"item.unknown", "Товара из чека нет в каталоге"}}
		}
		if it.marked() && l.Qty != 1 {
			return uuid.Nil, &RefusalError{Refusal{"mark.required", "Каждую бутылку с маркой пробивают отдельно"}}
		}
		if !r.Refund {
			refusals, err := s.refusals(ctx, pc, *it, l.Mark, raw, l.Price, at, r.Offline, reserved)
			if err != nil {
				return uuid.Nil, err
			}
			if len(refusals) > 0 {
				return uuid.Nil, &RefusalError{refusals[0]}
			}
		}
		needAge = needAge || it.alcohol() || it.Tobacco
		total += l.Price * int64(l.Qty)
		if l.Mark != "" {
			marksList = append(marksList, l.Mark)
		}
		if l.Crpt != "" {
			codes = append(codes, l.Crpt)
		}
	}
	if needAge && !r.AgeConfirmed && !r.Refund {
		return uuid.Nil, &RefusalError{Refusal{"sale.age", "Подтвердите, что проверили документ покупателя: ему есть 18 лет"}}
	}

	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		shift, err := q.LockCurrentShift(ctx, pc.Device.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoShift
		}
		if err != nil {
			return err
		}
		if r.Refund {
			if err := s.checkReturn(ctx, q, pc, r); err != nil {
				return err
			}
		}
		if r.ReservationID != nil {
			taken, err := s.reservations.TakeForSale(ctx, tx, *r.ReservationID, pc.Device.LocationID, pc.Cashier.ID)
			if err != nil {
				return &RefusalError{Refusal{"reservation.not_ready", "Бронь уже выдана, снята или истекла"}}
			}
			if !covers(r.Lines, taken) {
				return &RefusalError{Refusal{"reservation.lines", "В чеке должен быть товар брони " + taken.Number + " — отсканируйте отложенные бутылки"}}
			}
		}
		lines := make([]documents.SaleLine, len(r.Lines))
		for i, l := range r.Lines {
			lines[i] = documents.SaleLine{VariantID: l.VariantID, Qty: l.Qty, Mark: l.Mark}
		}
		err = s.docs.RecordSale(ctx, tx, shift.SaleDocumentID, pc.Device.LocationID, pc.Device.FloorZone, pc.Cashier.ID, r.Refund, lines)
		if me, ok := errors.AsType[*marks.MisplacedError](err); ok {
			return &RefusalError{Refusal{"mark.not_here", "Марка " + me.Code + " не числится на балансе магазина"}}
		}
		if be, ok := errors.AsType[*stock.BlockedError](err); ok {
			return &RefusalError{Refusal{"sale.stocktake", "Товар в ревизии " + be.Stocktake + " — продать его можно после её окончания"}}
		}
		if err != nil {
			return err
		}

		var qr *string
		if len(marksList) > 0 {
			if pc.Device.FSRARID == nil {
				return &RefusalError{Refusal{"utm.unavailable", "У точки не указан ФСРАР ID — чек с марками не отправить"}}
			}
			kind := "sale"
			if r.Refund {
				kind = "return"
			}
			reply, err := s.egais.Cheque(ctx, *pc.Device.FSRARID, egais.Cheque{Kind: kind, ReceiptID: r.ID.String(), Marks: marksList})
			if errors.Is(err, egais.ErrUnavailable) {
				return &RefusalError{Refusal{"utm.unavailable", "УТМ магазина не отвечает — продать товар с маркой сейчас нельзя"}}
			}
			if err != nil {
				return err
			}
			if reply.Result != "accepted" {
				return &RefusalError{Refusal{"mark." + egaisReason(reply.Reason), "ЕГАИС отклонила чек: " + reply.Reason}}
			}
			qr = &reply.QRURL
		}

		number, err := q.NextReceiptNumber(ctx, shift.ID)
		if err != nil {
			return err
		}
		kind := "sale"
		if r.Refund {
			kind = "return"
		}
		payment := "cash"
		if r.Card {
			payment = "card"
		}
		if err := q.InsertReceipt(ctx, store.InsertReceiptParams{
			ID: r.ID, ShiftID: shift.ID, Number: number, Kind: kind, ReturnOf: r.ReturnOf, CreatedOnDeviceAt: r.CreatedAt,
			TotalKopecks: total, Payment: payment, AgeConfirmed: r.AgeConfirmed, EgaisQr: qr, Offline: r.Offline,
		}); err != nil {
			return err
		}
		ins := store.InsertReceiptLinesParams{ReceiptID: r.ID}
		for i, l := range r.Lines {
			qty := l.Qty
			if r.Refund {
				qty = -qty
			}
			ins.LineNos = append(ins.LineNos, int32(i+1))
			ins.VariantIds = append(ins.VariantIds, l.VariantID)
			ins.Qtys = append(ins.Qtys, qty)
			ins.PricesKopecks = append(ins.PricesKopecks, l.Price)
			ins.MarkCodes = append(ins.MarkCodes, l.Mark)
			ins.CrptCodes = append(ins.CrptCodes, l.Crpt)
		}
		if err := q.InsertReceiptLines(ctx, ins); err != nil {
			return err
		}
		if r.ReservationID != nil {
			return s.reservations.Issued(ctx, tx, *r.ReservationID, r.ID)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	// Выбытие кодов «Честного знака» — после проведения: продажа уже состоялась. Повтор безопасен.
	if len(codes) > 0 && pc.Device.FSRARID != nil {
		call := s.crpt.Sell
		if r.Refund {
			call = s.crpt.Return
		}
		if err := call(ctx, *pc.Device.FSRARID, codes); err != nil {
			slog.WarnContext(ctx, "crpt withdrawal failed", "receipt", r.ID, "error", err)
		}
	}
	return r.ID, nil
}

func egaisReason(reason string) string {
	switch reason {
	case "mark.already_sold":
		return "sold"
	case "mark.blocked":
		return "blocked"
	}
	return "not_here"
}

// checkReturn — вернуть можно только проданное этим чеком этой точки и не больше, чем продано.
func (s *Service) checkReturn(ctx context.Context, q *store.Queries, pc Context, r NewReceipt) error {
	if r.ReturnOf == nil {
		return &RefusalError{Refusal{"return.receipt", "Для возврата найдите чек продажи"}}
	}
	orig, err := q.GetReceipt(ctx, *r.ReturnOf)
	if errors.Is(err, pgx.ErrNoRows) {
		return &RefusalError{Refusal{"return.receipt", "Чек продажи не найден"}}
	}
	if err != nil {
		return err
	}
	if orig.Kind != "sale" {
		return &RefusalError{Refusal{"return.receipt", "Вернуть можно только по чеку продажи"}}
	}
	sold, err := q.ReceiptLines(ctx, orig.ID)
	if err != nil {
		return err
	}
	returned, err := q.ReturnedQty(ctx, &orig.ID)
	if err != nil {
		return err
	}
	type key struct {
		variant uuid.UUID
		mark    string
	}
	left := map[key]int32{}
	for _, l := range sold {
		m := ""
		if l.MarkCode != nil {
			m = *l.MarkCode
		}
		left[key{l.VariantID, m}] += l.Qty
	}
	for _, l := range returned {
		left[key{l.VariantID, l.MarkCode}] -= l.Qty
	}
	for _, l := range r.Lines {
		k := key{l.VariantID, l.Mark}
		if left[k] < l.Qty {
			return &RefusalError{Refusal{"return.qty", "Вернуть можно только то, что продано этим чеком и ещё не возвращено"}}
		}
		left[k] -= l.Qty
	}
	return nil
}

// Receipt — чек со строками для ответа кассе.
type Receipt struct {
	store.GetReceiptRow
	Lines []store.ReceiptLinesRow
}

func (s *Service) Receipt(ctx context.Context, pc Context, id uuid.UUID) (Receipt, error) {
	r, err := s.q.GetReceipt(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Receipt{}, ErrNotFound
	}
	if err != nil {
		return Receipt{}, err
	}
	lines, err := s.q.ReceiptLines(ctx, id)
	return Receipt{GetReceiptRow: r, Lines: lines}, err
}

// FindReceipts — чеки продажи точки по номеру или по марке и коду «Честного знака» из чека.
func (s *Service) FindReceipts(ctx context.Context, pc Context, number *int32, code *string) ([]Receipt, error) {
	if code != nil {
		if scan, ok := catalog.ParseScan(*code); ok {
			switch {
			case scan.Mark != "":
				code = &scan.Mark
			case scan.Unit != "":
				code = &scan.Unit
			}
		}
	}
	ids, err := s.q.FindReceipts(ctx, store.FindReceiptsParams{LocationID: pc.Device.LocationID, Number: number, Code: code})
	if err != nil {
		return nil, err
	}
	out := make([]Receipt, 0, len(ids))
	for _, id := range ids {
		r, err := s.Receipt(ctx, pc, id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// covers — в чеке весь товар брони: её марки и не меньше её количества.
func covers(lines []NewLine, t reservations.Taken) bool {
	marksIn := map[string]bool{}
	var qty int32
	for _, l := range lines {
		if l.VariantID == t.VariantID {
			qty += l.Qty
			if l.Mark != "" {
				marksIn[l.Mark] = true
			}
		}
	}
	for _, m := range t.Marks {
		if !marksIn[m] {
			return false
		}
	}
	return qty >= t.Qty
}
