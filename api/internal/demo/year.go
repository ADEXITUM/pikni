// Package demo — демонстрационные данные за год (раздел 4.1.4): поставки, передачи в зал, продажи
// через кассы и редкий бой по каждому магазину сети. Документы проводятся тем же регистром движений
// и учётом марок, что и настоящие, поэтому остатки, средняя себестоимость и помарочный учёт
// сходятся (П-35), а отчёты и дашборд показывают правдоподобный год.
package demo

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math"
	mrand "math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

var ErrAlreadySeeded = errors.New("demo year is already generated")

type item struct {
	id         uuid.UUID
	priceKop   int64
	costE4     int64
	marked     bool
	beer       bool
	alcohol    bool
	popularity float64
}

type store struct {
	id, backroom, floor, device uuid.UUID
	code                        string
	tz                          *time.Location
	cashiers                    []uuid.UUID
	// Остатки, которые сгенерировал сам генератор: продаётся только то, что он завёз.
	back, shelf         map[uuid.UUID]int32
	backMarks, shelfMks map[uuid.UUID][]string
	traffic             float64
	shift               int32
}

type gen struct {
	pool   *pgxpool.Pool
	author uuid.UUID
	items  []item
	reason uuid.UUID
	seq    int
	rnd    *mrand.Rand
}

// Year генерирует days дней истории, заканчивая вчерашним днём. Повторный запуск отклоняется.
func Year(ctx context.Context, pool *pgxpool.Pool, days int, log *slog.Logger) error {
	var done bool
	if err := pool.QueryRow(ctx, `select exists (select 1 from app_settings where key = 'demo_year')`).Scan(&done); err != nil {
		return err
	}
	if done {
		return ErrAlreadySeeded
	}
	g := &gen{pool: pool, rnd: mrand.New(mrand.NewPCG(2026, 10))}
	if err := g.setup(ctx); err != nil {
		return err
	}
	stores, err := g.stores(ctx)
	if err != nil {
		return err
	}
	today := time.Now()
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -days)
	for d := 0; d < days; d++ {
		day := start.AddDate(0, 0, d)
		for _, s := range stores {
			if err := g.day(ctx, s, day, d); err != nil {
				return fmt.Errorf("%s %s: %w", s.code, day.Format("2006-01-02"), err)
			}
		}
		if d%30 == 0 {
			log.Info("demo year", "day", day.Format("2006-01-02"), "documents", g.seq)
		}
	}
	_, err = pool.Exec(ctx, `insert into app_settings (key, value) values ('demo_year', $1)`, today.Format(time.RFC3339))
	return err
}

func (g *gen) setup(ctx context.Context) error {
	err := g.pool.QueryRow(ctx, `insert into users (login, full_name, role, password_hash, must_change_password, is_blocked)
		values ('demo-generator', 'Демо-данные', 'manager', 'x', false, true)
		on conflict ((lower(login))) do update set full_name = excluded.full_name returning id`).Scan(&g.author)
	if err != nil {
		return err
	}
	if err := g.pool.QueryRow(ctx, `select id from writeoff_reasons where name = 'Бой'`).Scan(&g.reason); err != nil {
		return fmt.Errorf("writeoff reason «Бой»: %w", err)
	}
	// Алкоголь и сопутствующие товары с розничной ценой; табак — без генерации (коды пачек ГИС МТ).
	rows, err := g.pool.Query(ctx, `
		select v.id,
		       (select (pr.price * 100)::bigint from prices pr where pr.variant_id = v.id and pr.kind = 'retail' order by pr.valid_from desc limit 1),
		       coalesce((select (pr.price * 10000)::bigint from prices pr where pr.variant_id = v.id and pr.kind = 'purchase' order by pr.valid_from desc limit 1), 0),
		       coalesce(k.egais_accounting, '') = 'marked', coalesce(k.egais_accounting, '') = 'quantity', c.alcohol
		from variants v
		join products p on p.id = v.product_id
		join categories c on c.id = p.category_id
		left join categories pc on pc.id = c.parent_id
		left join product_kinds k on k.code = p.kind_code
		where v.is_active and p.is_active and coalesce(pc.name, c.name) <> 'Табачная продукция'
		  and exists (select 1 from prices pr where pr.variant_id = v.id and pr.kind = 'retail')
		order by v.article`)
	if err != nil {
		return err
	}
	g.items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (item, error) {
		var it item
		err := r.Scan(&it.id, &it.priceKop, &it.costE4, &it.marked, &it.beer, &it.alcohol)
		return it, err
	})
	if err != nil {
		return err
	}
	if len(g.items) == 0 {
		return errors.New("no products with retail prices: load the catalog first")
	}
	// Спрос по Ципфу: немногие позиции продаются часто, большинство — редко.
	for i := range g.items {
		it := &g.items[i]
		if it.costE4 == 0 {
			it.costE4 = it.priceKop * 60 // закупка — 60 % розничной цены
		}
		it.popularity = 1 / math.Pow(float64(1+g.rnd.IntN(len(g.items))), 0.8)
		if it.beer {
			it.popularity *= 2.5
		}
	}
	return nil
}

func (g *gen) stores(ctx context.Context) ([]*store, error) {
	rows, err := g.pool.Query(ctx, `select l.id, l.code, l.timezone,
		(select id from zones where location_id = l.id and kind = 'backroom'),
		(select id from zones where location_id = l.id and kind = 'floor')
		from locations l where l.kind = 'store' and l.is_active order by l.code`)
	if err != nil {
		return nil, err
	}
	type raw struct {
		ID              uuid.UUID
		Code, TZ        string
		Backroom, Floor uuid.UUID
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[raw])
	if err != nil {
		return nil, err
	}
	var out []*store
	for i, r := range list {
		tz, err := time.LoadLocation(r.TZ)
		if err != nil {
			tz = time.UTC
		}
		s := &store{id: r.ID, code: r.Code, tz: tz, backroom: r.Backroom, floor: r.Floor,
			back: map[uuid.UUID]int32{}, shelf: map[uuid.UUID]int32{},
			backMarks: map[uuid.UUID][]string{}, shelfMks: map[uuid.UUID][]string{},
			traffic: 0.8 + 0.15*float64(i)}
		prefix := strings.ToLower(rand.Text()[:8])
		if err := g.pool.QueryRow(ctx, `insert into pos_devices (location_id, name, key_prefix, key_hash)
			values ($1, 'Касса 1 (демо)', $2, $3) returning id`, s.id, prefix, []byte(rand.Text())).Scan(&s.device); err != nil {
			return nil, err
		}
		for j, name := range []string{"Иванова", "Петрова"} {
			var id uuid.UUID
			login := fmt.Sprintf("demo-%s-%d", strings.ToLower(strings.ReplaceAll(r.Code, "-", "")), j+1)
			if err := g.pool.QueryRow(ctx, `insert into users (login, full_name, role, password_hash, must_change_password, is_blocked)
				values ($1, $2, 'cashier', 'x', false, true)
				on conflict ((lower(login))) do update set full_name = excluded.full_name returning id`,
				login, "Кассир "+name+" ("+r.Code+")").Scan(&id); err != nil {
				return nil, err
			}
			if _, err := g.pool.Exec(ctx, `insert into user_locations (user_id, location_id) values ($1, $2) on conflict do nothing`, id, s.id); err != nil {
				return nil, err
			}
			s.cashiers = append(s.cashiers, id)
		}
		out = append(out, s)
	}
	return out, nil
}

func (g *gen) number(prefix string, day time.Time) string {
	g.seq++
	return fmt.Sprintf("%s-%d-Д%05d", prefix, day.Year(), g.seq)
}

func (g *gen) document(ctx context.Context, tx pgx.Tx, kind, prefix string, s *store, zone uuid.UUID, dest *uuid.UUID, at time.Time) (uuid.UUID, error) {
	var mode, reason any
	if kind == "receipt" {
		mode = "document"
	}
	if kind == "writeoff" {
		reason = g.reason
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `insert into documents (kind, number, status, doc_date, location_id, zone_id, dest_zone_id, author_id,
			posted_by, posted_at, created_at, receiving_mode, reason_id)
		values ($1, $2, 'posted', $3, $4, $5, $6, $7, $7, $8, $8, $9, $10) returning id`,
		kind, g.number(prefix, at), at.In(s.tz).Format("2006-01-02"), s.id, zone, dest, g.author, at, mode, reason).Scan(&id)
	return id, err
}

func newMark() string { return "DEMO" + strings.ToUpper(rand.Text()[:16]) }

// day — один день магазина: поставка по понедельникам, утром — товар в зал, днём — продажи.
func (g *gen) day(ctx context.Context, s *store, day time.Time, n int) error {
	local := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, s.tz)
	return pgx.BeginFunc(ctx, g.pool, func(tx pgx.Tx) error {
		if n == 0 || local.Weekday() == time.Monday {
			if err := g.delivery(ctx, tx, s, local.Add(8*time.Hour)); err != nil {
				return err
			}
		}
		if err := g.toFloor(ctx, tx, s, local.Add(9*time.Hour)); err != nil {
			return err
		}
		if err := g.sales(ctx, tx, s, local); err != nil {
			return err
		}
		// Бой — примерно раз в месяц.
		if g.rnd.IntN(30) == 0 {
			return g.breakage(ctx, tx, s, local.Add(15*time.Hour))
		}
		return nil
	})
}

func (g *gen) delivery(ctx context.Context, tx pgx.Tx, s *store, at time.Time) error {
	doc, err := g.document(ctx, tx, "receipt", "ПТ", s, s.backroom, nil, at)
	if err != nil {
		return err
	}
	var moves []stock.Move
	var codes []string
	var variants []uuid.UUID
	for _, it := range g.items {
		target := int32(6 + 30*it.popularity)
		have := s.back[it.id] + s.shelf[it.id]
		if have >= target/2 {
			continue
		}
		qty := target - have
		moves = append(moves, stock.Move{VariantID: it.id, LocationID: s.id, ZoneID: s.backroom, Qty: qty, External: true, UnitCostE4: it.costE4})
		s.back[it.id] += qty
		if it.marked {
			for range qty {
				c := newMark()
				codes, variants = append(codes, c), append(variants, it.id)
				s.backMarks[it.id] = append(s.backMarks[it.id], c)
			}
		}
		if _, err := tx.Exec(ctx, `insert into document_lines (document_id, line_no, variant_id, qty) values ($1, (select count(*) + 1 from document_lines where document_id = $1), $2, $3)`, doc, it.id, qty); err != nil {
			return err
		}
	}
	if len(codes) > 0 {
		if err := marks.Register(ctx, tx, marks.Event{DocumentID: doc, UserID: g.author}, s.id, s.backroom, codes, variants); err != nil {
			return err
		}
	}
	_, err = stock.Apply(ctx, tx, stock.Doc{ID: doc, Date: at}, moves)
	return err
}

func (g *gen) toFloor(ctx context.Context, tx pgx.Tx, s *store, at time.Time) error {
	var moves []stock.Move
	var codes []string
	for _, it := range g.items {
		target := int32(3 + 10*it.popularity)
		qty := min(target-s.shelf[it.id], s.back[it.id])
		if qty <= 0 {
			continue
		}
		moves = append(moves,
			stock.Move{VariantID: it.id, LocationID: s.id, ZoneID: s.backroom, Qty: -qty},
			stock.Move{VariantID: it.id, LocationID: s.id, ZoneID: s.floor, Qty: qty})
		s.back[it.id] -= qty
		s.shelf[it.id] += qty
		if it.marked {
			m := s.backMarks[it.id][:qty]
			codes = append(codes, m...)
			s.shelfMks[it.id] = append(s.shelfMks[it.id], m...)
			s.backMarks[it.id] = s.backMarks[it.id][qty:]
		}
	}
	if len(moves) == 0 {
		return nil
	}
	doc, err := g.document(ctx, tx, "floor_transfer", "ПЗ", s, s.backroom, &s.floor, at)
	if err != nil {
		return err
	}
	if len(codes) > 0 {
		if err := marks.MoveWithin(ctx, tx, marks.Event{DocumentID: doc, UserID: g.author}, s.backroom, s.id, s.floor, codes); err != nil {
			return err
		}
	}
	_, err = stock.Apply(ctx, tx, stock.Doc{ID: doc, Date: at}, moves)
	return err
}

// season — спрос по месяцам: декабрь и майские праздники выше, январь ниже; пиво растёт летом.
func season(m time.Month, beer bool) float64 {
	k := map[time.Month]float64{1: 0.75, 2: 0.85, 3: 0.95, 4: 1, 5: 1.15, 6: 1.05, 7: 1.05, 8: 1.05, 9: 0.95, 10: 1, 11: 1.05, 12: 1.6}[m]
	if beer && m >= 5 && m <= 8 {
		k *= 1.4
	}
	return k
}

func (g *gen) sales(ctx context.Context, tx pgx.Tx, s *store, local time.Time) error {
	week := map[time.Weekday]float64{0: 1.15, 1: 0.8, 2: 0.85, 3: 0.9, 4: 1, 5: 1.35, 6: 1.4}[local.Weekday()]
	receipts := int(float64(55+g.rnd.IntN(20)) * s.traffic * week * season(local.Month(), false))
	cashier := s.cashiers[local.YearDay()%len(s.cashiers)]
	opened, closed := local.Add(10*time.Hour), local.Add(22*time.Hour)
	doc, err := g.document(ctx, tx, "sale", "ПР", s, s.floor, nil, closed)
	if err != nil {
		return err
	}
	s.shift++
	var shift uuid.UUID
	if err := tx.QueryRow(ctx, `insert into pos_shifts (device_id, cashier_id, number, sale_document_id, opened_at, closed_at, status)
		values ($1, $2, $3, $4, $5, $6, 'closed') returning id`, s.device, cashier, s.shift, doc, opened, closed).Scan(&shift); err != nil {
		return err
	}
	var total float64
	for _, it := range g.items {
		total += it.popularity * season(local.Month(), it.beer)
	}
	sold := map[uuid.UUID]int32{}
	var soldCodes []string
	for r := range receipts {
		at := opened.Add(time.Duration(g.rnd.Int64N(int64(12 * time.Hour))))
		lines := 1 + g.rnd.IntN(3)
		var rows [][]any
		var sum int64
		for range lines {
			x := g.rnd.Float64() * total
			var it item
			for _, c := range g.items {
				x -= c.popularity * season(local.Month(), c.beer)
				if x <= 0 {
					it = c
					break
				}
			}
			if it.id == uuid.Nil || s.shelf[it.id]-sold[it.id] <= 0 {
				continue
			}
			qty := int32(1)
			if it.beer {
				qty = min(int32(1+g.rnd.IntN(4)), s.shelf[it.id]-sold[it.id])
			}
			mark := ""
			if it.marked {
				mark = s.shelfMks[it.id][0]
				s.shelfMks[it.id] = s.shelfMks[it.id][1:]
				soldCodes = append(soldCodes, mark)
			}
			sold[it.id] += qty
			sum += it.priceKop * int64(qty)
			rows = append(rows, []any{it.id, qty, it.priceKop, mark})
		}
		if len(rows) == 0 {
			continue
		}
		id := uuid.New()
		payment := "card"
		if g.rnd.IntN(3) == 0 {
			payment = "cash"
		}
		var qr any
		if slices.ContainsFunc(rows, func(r []any) bool { return r[3] != "" }) {
			qr = "https://check.egais.ru/?id=" + id.String()
		}
		if _, err := tx.Exec(ctx, `insert into pos_receipts (id, shift_id, number, kind, created_on_device_at, received_at, total, payment, age_confirmed, egais_qr)
			values ($1, $2, $3, 'sale', $4, $4, $5::bigint / 100.0, $6, true, $7)`, id, shift, r+1, at, sum, payment, qr); err != nil {
			return err
		}
		for i, row := range rows {
			if _, err := tx.Exec(ctx, `insert into pos_receipt_lines (receipt_id, line_no, variant_id, qty, price, mark_code)
				values ($1, $2, $3, $4, $5::bigint / 100.0, nullif($6, ''))`, id, i+1, row[0], row[1], row[2], row[3]); err != nil {
				return err
			}
		}
	}
	var moves []stock.Move
	for v, qty := range sold {
		moves = append(moves, stock.Move{VariantID: v, LocationID: s.id, ZoneID: s.floor, Qty: -qty, External: true})
		s.shelf[v] -= qty
		if _, err := tx.Exec(ctx, `insert into document_lines (document_id, line_no, variant_id, qty) values ($1, (select count(*) + 1 from document_lines where document_id = $1), $2, $3)`, doc, v, qty); err != nil {
			return err
		}
	}
	if len(soldCodes) > 0 {
		if _, err := marks.Sell(ctx, tx, marks.Event{DocumentID: doc, UserID: cashier}, s.id, soldCodes); err != nil {
			return err
		}
	}
	if len(moves) > 0 {
		if _, err := stock.Apply(ctx, tx, stock.Doc{ID: doc, Date: closed}, moves); err != nil {
			return err
		}
	}
	// Пиво смены списано в ЕГАИС актом «Реализация»; квитанция принята.
	_, err = tx.Exec(ctx, `insert into egais_messages (location_id, direction, doc_type, document_id, payload, status, created_at, updated_at)
		values ($1, 'out', 'ActWriteOffShop', $2, '{}', 'accepted', $3, $3)`, s.id, doc, closed)
	return err
}

func (g *gen) breakage(ctx context.Context, tx pgx.Tx, s *store, at time.Time) error {
	for _, it := range g.items {
		if !it.marked || s.shelf[it.id] == 0 {
			continue
		}
		doc, err := g.document(ctx, tx, "writeoff", "СП", s, s.floor, nil, at)
		if err != nil {
			return err
		}
		code := s.shelfMks[it.id][0]
		s.shelfMks[it.id] = s.shelfMks[it.id][1:]
		s.shelf[it.id]--
		if _, err := tx.Exec(ctx, `insert into document_lines (document_id, line_no, variant_id, qty) values ($1, 1, $2, 1)`, doc, it.id); err != nil {
			return err
		}
		z := s.floor
		if err := marks.WriteOff(ctx, tx, marks.Event{DocumentID: doc, UserID: g.author}, s.id, &z, []string{code}); err != nil {
			return err
		}
		_, err = stock.Apply(ctx, tx, stock.Doc{ID: doc, Date: at}, []stock.Move{
			{VariantID: it.id, LocationID: s.id, ZoneID: s.floor, Qty: -1, External: true}})
		return err
	}
	return nil
}
