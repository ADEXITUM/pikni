// Package demo — демонстрационные данные за год (раздел 4.1.4, решения 0062 и 0063): поставки
// дистрибьюторов на склады, перемещения со складов в магазины, прямые поставки пива, передачи в
// зал, продажи через кассы и бой по каждой точке сети. Документы проводятся тем же регистром
// движений и учётом марок, что и настоящие, поэтому остатки, средняя себестоимость и помарочный
// учёт сходятся (П-35), а отчёты и дашборд показывают правдоподобный год.
package demo

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	mrand "math/rand/v2"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

var ErrAlreadySeeded = errors.New("demo year is already generated")

// Options — размер истории и формат магазинов. Small — коды магазинов «у дома»: узкая матрица и
// одна касса; остальные магазины — крупные алкомаркеты.
type Options struct {
	Days  int
	Small []string
	// Rests — куда записать остатки точек для имитатора ЕГАИС (govsim seed-rests); nil — не писать.
	Rests io.Writer
}

// Year — год истории с форматами по умолчанию: МГ-02 — магазин у дома.
func Year(ctx context.Context, pool *pgxpool.Pool, days int, log *slog.Logger) error {
	return Generate(ctx, pool, Options{Days: days, Small: []string{"МГ-02"}}, log)
}

type item struct {
	id         uuid.UUID
	priceKop   int64
	costE4     int64
	marked     bool
	quantity   bool // пиво, сидр: в ЕГАИС количеством, возят пивзаводы напрямую
	alcohol    bool
	category   string
	popularity float64
}

type point struct {
	id, zone, floor uuid.UUID // zone — склад точки (склад магазина или склад сети)
	code, kind      string
	tz              *time.Location
	lat, lon        float64
	small           bool
	wh              *point
	stores          []*point
	devices         []uuid.UUID
	cashiers        []uuid.UUID
	shifts          []int32
	assort          []int // индексы позиций матрицы
	in              map[int]bool
	demand          map[int]float64 // продажи позиции в день
	back, shelf     map[int]int32
	backMarks       map[int][]string
	shelfMarks      map[int][]string
	traffic         float64
}

type gen struct {
	pool      *pgxpool.Pool
	author    uuid.UUID
	reason    uuid.UUID
	suppliers []uuid.UUID // [0] — крепкий алкоголь и вино на СК-01, [1] — на СК-02, [2] — пиво
	items     []item
	seq       int
	rnd       *mrand.Rand
	log       *slog.Logger
}

// Generate пишет opts.Days дней истории, заканчивая вчерашним днём. Повторный запуск отклоняется.
func Generate(ctx context.Context, pool *pgxpool.Pool, opts Options, log *slog.Logger) error {
	var done bool
	if err := pool.QueryRow(ctx, `select exists (select 1 from app_settings where key = 'demo_year')`).Scan(&done); err != nil {
		return err
	}
	if done {
		return ErrAlreadySeeded
	}
	g := &gen{pool: pool, rnd: mrand.New(mrand.NewPCG(2026, 10)), log: log}
	if err := g.setup(ctx); err != nil {
		return err
	}
	stores, whs, err := g.points(ctx, opts.Small)
	if err != nil {
		return err
	}
	g.assortment(ctx, stores)
	if err := g.saveMatrix(ctx, stores); err != nil {
		return err
	}
	today := time.Now()
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -opts.Days)
	began := time.Now()
	for d := 0; d < opts.Days; d++ {
		day := start.AddDate(0, 0, d)
		for _, w := range whs {
			if err := g.warehouseDay(ctx, w, day, d); err != nil {
				return fmt.Errorf("%s %s: %w", w.code, day.Format("2006-01-02"), err)
			}
		}
		for _, s := range stores {
			if err := g.storeDay(ctx, s, day, d); err != nil {
				return fmt.Errorf("%s %s: %w", s.code, day.Format("2006-01-02"), err)
			}
		}
		if d%30 == 0 || d == opts.Days-1 {
			log.Info("demo year", "day", day.Format("2006-01-02"), "documents", g.seq, "elapsed", time.Since(began).Round(time.Second))
		}
	}
	if opts.Rests != nil {
		if err := g.writeRests(ctx, opts.Rests); err != nil {
			return err
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
	rows, err := g.pool.Query(ctx, `select id from suppliers order by inn`)
	if err != nil {
		return err
	}
	if g.suppliers, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
		return err
	}
	// Алкоголь и сопутствующие товары с розничной ценой; табак — без генерации (коды пачек ГИС МТ).
	rows, err = g.pool.Query(ctx, `
		select v.id,
		       (select (pr.price * 100)::bigint from prices pr where pr.variant_id = v.id and pr.kind = 'retail' order by pr.valid_from desc limit 1),
		       coalesce((select (pr.price * 10000)::bigint from prices pr where pr.variant_id = v.id and pr.kind = 'purchase' order by pr.valid_from desc limit 1), 0),
		       coalesce(k.egais_accounting, '') = 'marked', coalesce(k.egais_accounting, '') = 'quantity', c.alcohol,
		       coalesce(pc.name, c.name) || ' / ' || c.name
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
		err := r.Scan(&it.id, &it.priceKop, &it.costE4, &it.marked, &it.quantity, &it.alcohol, &it.category)
		return it, err
	})
	if err != nil {
		return err
	}
	if len(g.items) == 0 {
		return errors.New("no products with retail prices: load the catalog first")
	}
	// Спрос по Ципфу (немногие позиции продаются часто) с поправкой на цену: дешёвое берут чаще.
	for i := range g.items {
		it := &g.items[i]
		if it.costE4 == 0 {
			it.costE4 = it.priceKop * 60
		}
		price := max(float64(it.priceKop)/100, 50)
		it.popularity = 1 / math.Pow(float64(1+g.rnd.IntN(len(g.items))), 0.9) * math.Pow(500/price, 0.45)
		if it.quantity {
			it.popularity *= 2.5
		}
	}
	return nil
}

func (g *gen) points(ctx context.Context, small []string) ([]*point, []*point, error) {
	stores, whs, err := g.existing(ctx, small)
	if err != nil {
		return nil, nil, err
	}
	if len(whs) == 0 {
		return nil, nil, errors.New("no warehouses: load dev.sql first")
	}
	for _, s := range stores {
		// Магазин снабжает ближайший склад.
		s.wh = nearest(s, whs)
		s.wh.stores = append(s.wh.stores, s)
		if err := g.staff(ctx, s); err != nil {
			return nil, nil, err
		}
	}
	return stores, whs, nil
}

// existing — действующие магазины и склады из базы; small — коды магазинов у дома.
func (g *gen) existing(ctx context.Context, small []string) ([]*point, []*point, error) {
	rows, err := g.pool.Query(ctx, `select l.id, l.code, l.kind, l.timezone, coalesce(l.lat, 0)::float8, coalesce(l.lon, 0)::float8,
		(select id from zones where location_id = l.id and kind in ('backroom', 'warehouse') order by kind limit 1),
		coalesce((select id from zones where location_id = l.id and kind = 'floor'), '00000000-0000-0000-0000-000000000000')
		from locations l where l.kind in ('store', 'warehouse') and l.is_active order by l.code`)
	if err != nil {
		return nil, nil, err
	}
	type raw struct {
		ID         uuid.UUID
		Code, Kind string
		TZ         string
		Lat, Lon   float64
		Zone       uuid.UUID
		Floor      uuid.UUID
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[raw])
	if err != nil {
		return nil, nil, err
	}
	var stores, whs []*point
	for _, r := range list {
		tz, err := time.LoadLocation(r.TZ)
		if err != nil {
			tz = time.UTC
		}
		p := &point{id: r.ID, code: r.Code, kind: r.Kind, tz: tz, lat: r.Lat, lon: r.Lon, zone: r.Zone, floor: r.Floor,
			back: map[int]int32{}, shelf: map[int]int32{}, backMarks: map[int][]string{}, shelfMarks: map[int][]string{},
			in: map[int]bool{}, demand: map[int]float64{}, small: slices.Contains(small, r.Code), traffic: 0.85 + 0.3*g.rnd.Float64()}
		if r.Kind == "warehouse" {
			whs = append(whs, p)
			continue
		}
		stores = append(stores, p)
	}
	return stores, whs, nil
}

func nearest(p *point, list []*point) *point {
	return slices.MinFunc(list, func(a, b *point) int { return cmp.Compare(dist(p, a), dist(p, b)) })
}

func dist(a, b *point) float64 { return math.Hypot(a.lat-b.lat, a.lon-b.lon) }

// staff — кассы и кассиры демо-истории: у крупного магазина три кассы и шесть кассиров, у малого —
// одна касса и двое. Учётные записи заблокированы: под ними не входят, они только в истории.
func (g *gen) staff(ctx context.Context, s *point) error {
	tills, people := 3, 6
	if s.small {
		tills, people = 1, 2
	}
	for i := range tills {
		var id uuid.UUID
		prefix := strings.ToLower(rand.Text()[:8])
		if err := g.pool.QueryRow(ctx, `insert into pos_devices (location_id, name, key_prefix, key_hash)
			values ($1, $2, $3, $4) returning id`, s.id, fmt.Sprintf("Касса %d (история)", i+1), prefix, []byte(rand.Text())).Scan(&id); err != nil {
			return err
		}
		s.devices = append(s.devices, id)
		s.shifts = append(s.shifts, 0)
	}
	names := []string{"Иванова", "Петрова", "Сидоренко", "Ким", "Лебедева", "Орлов"}
	for j := range people {
		var id uuid.UUID
		login := fmt.Sprintf("hist-%s-%d", strings.ToLower(strings.ReplaceAll(s.code, "-", "")), j+1)
		if err := g.pool.QueryRow(ctx, `insert into users (login, full_name, role, password_hash, must_change_password, is_blocked)
			values ($1, $2, 'cashier', 'x', false, true)
			on conflict ((lower(login))) do update set full_name = excluded.full_name returning id`,
			login, "Кассир "+names[j]+" ("+s.code+")").Scan(&id); err != nil {
			return err
		}
		if _, err := g.pool.Exec(ctx, `insert into user_locations (user_id, location_id) values ($1, $2) on conflict do nothing`, id, s.id); err != nil {
			return err
		}
		s.cashiers = append(s.cashiers, id)
	}
	return nil
}

// smallQuota — матрица магазина у дома: сколько самых ходовых позиций каждой категории.
var smallQuota = map[string]int{
	"Крепкий алкоголь / Водка": 60, "Пиво и сидр / Пиво": 110, "Вино / Тихое": 110, "Вино / Игристое": 20,
	"Крепкий алкоголь / Коньяк и бренди": 30, "Крепкий алкоголь / Виски": 15, "Крепкий алкоголь / Ром": 5,
	"Крепкий алкоголь / Джин": 5, "Крепкий алкоголь / Текила": 3, "Крепкий алкоголь / Ликёры и настойки": 15,
	"Вино / Креплёное": 8, "Пиво и сидр / Сидр и пуаре": 10, "Слабоалкогольные напитки": 10,
	"Безалкогольное и снеки / Напитки": 50, "Безалкогольное и снеки / Снеки": 50,
}

// Средний чек — 1,8 позиции; крупный алкомаркет — около тысячи чеков в день, у дома — около 260.
const linesPerReceipt = 1.8

func receiptsPerDay(s *point) float64 {
	if s.small {
		return 260 * s.traffic
	}
	return 1000 * s.traffic
}

func (g *gen) assortment(_ context.Context, stores []*point) {
	byCat := map[string][]int{}
	for i, it := range g.items {
		byCat[it.category] = append(byCat[it.category], i)
	}
	for _, list := range byCat {
		sort.Slice(list, func(a, b int) bool { return g.items[list[a]].popularity > g.items[list[b]].popularity })
	}
	for _, s := range stores {
		if s.small {
			for cat, list := range byCat {
				// В небольшом магазине — недорогое: из ходового выбирается то, что дешевле 1 500 ₽.
				n := smallQuota[cat]
				for _, i := range list {
					if n == 0 {
						break
					}
					if g.items[i].priceKop <= 150000 {
						s.assort = append(s.assort, i)
						n--
					}
				}
			}
		} else {
			// Крупный магазин держит почти весь каталог; 6 % позиций — не в его матрице.
			for i := range g.items {
				if g.rnd.Float64() > 0.06 {
					s.assort = append(s.assort, i)
				}
			}
		}
		sort.Ints(s.assort)
		var total float64
		for _, i := range s.assort {
			s.in[i] = true
			total += g.items[i].popularity
		}
		units := receiptsPerDay(s) * linesPerReceipt
		for _, i := range s.assort {
			s.demand[i] = units * g.items[i].popularity / total
		}
	}
}

func (g *gen) saveMatrix(ctx context.Context, stores []*point) error {
	return pgx.BeginFunc(ctx, g.pool, func(tx pgx.Tx) error {
		for _, s := range stores {
			if _, err := tx.Exec(ctx, `delete from assortment_matrix where location_id = $1`, s.id); err != nil {
				return err
			}
			var rows [][]any
			for _, i := range s.assort {
				rows = append(rows, []any{s.id, g.items[i].id, int32(max(2, math.Ceil(s.demand[i]*2)))})
			}
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{"assortment_matrix"}, []string{"location_id", "variant_id", "min_qty"}, pgx.CopyFromRows(rows)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (g *gen) number(prefix string, day time.Time) string {
	g.seq++
	return fmt.Sprintf("%s-%d-Д%06d", prefix, day.Year(), g.seq)
}

type docSpec struct {
	kind, prefix string
	p            *point
	zone         uuid.UUID
	destLocation *uuid.UUID
	destZone     *uuid.UUID
	supplier     *uuid.UUID
	transfer     *uuid.UUID
	at           time.Time
	ttn          string
	status       string
}

func (g *gen) document(ctx context.Context, tx pgx.Tx, d docSpec) (uuid.UUID, string, error) {
	var mode, reason any
	if d.kind == "receipt" {
		mode = "document"
		if d.transfer != nil {
			mode = "piece"
		}
	}
	if d.kind == "writeoff" {
		reason = g.reason
	}
	status := "posted"
	if d.status != "" {
		status = d.status
	}
	number := g.number(d.prefix, d.at)
	var ttn any
	var ttnDate any
	if d.ttn != "" {
		ttn, ttnDate = d.ttn, d.at.In(d.p.tz).Format("2006-01-02")
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `insert into documents (kind, number, status, doc_date, location_id, zone_id, dest_zone_id,
			dest_location_id, supplier_id, transfer_id, ttn_number, ttn_date, author_id, posted_by, posted_at, created_at,
			receiving_mode, reason_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13, $14, $14, $15, $16) returning id`,
		d.kind, number, status, d.at.In(d.p.tz).Format("2006-01-02"), d.p.id, d.zone, d.destZone, d.destLocation,
		d.supplier, d.transfer, ttn, ttnDate, g.author, d.at, mode, reason).Scan(&id)
	return id, number, err
}

func (g *gen) line(ctx context.Context, tx pgx.Tx, doc uuid.UUID, n int, variant uuid.UUID, qty int32) error {
	_, err := tx.Exec(ctx, `insert into document_lines (document_id, line_no, variant_id, qty, qty_plan) values ($1, $2, $3, $4, $4)`,
		doc, n, variant, qty)
	return err
}

func (g *gen) message(ctx context.Context, tx pgx.Tx, p *point, direction, docType string, doc uuid.UUID, at time.Time) error {
	status := "accepted"
	if direction == "in" {
		status = "processed"
	}
	_, err := tx.Exec(ctx, `insert into egais_messages (location_id, direction, doc_type, document_id, payload, status, created_at, updated_at)
		values ($1, $2, $3, $4, '{}', $5, $6, $6)`, p.id, direction, docType, doc, status, at)
	return err
}

func newMark() string { return "PKN" + strings.ToUpper(rand.Text()[:13]) }

// warehouseDay — склад: по понедельникам дистрибьютор довозит крепкий алкоголь, вино и сопутствующие
// товары до запаса на три недели продаж магазинов склада.
func (g *gen) warehouseDay(ctx context.Context, w *point, day time.Time, n int) error {
	local := time.Date(day.Year(), day.Month(), day.Day(), 7, 0, 0, 0, w.tz)
	if n != 0 && local.Weekday() != time.Monday {
		return nil
	}
	need := map[int]float64{}
	for _, s := range w.stores {
		for _, i := range s.assort {
			if !g.items[i].quantity {
				need[i] += s.demand[i]
			}
		}
	}
	var lines []int
	for i := range need {
		lines = append(lines, i)
	}
	sort.Ints(lines)
	supplier := g.suppliers[0]
	if len(g.suppliers) > 1 && w.code != "СК-01" {
		supplier = g.suppliers[1]
	}
	return pgx.BeginFunc(ctx, g.pool, func(tx pgx.Tx) error {
		qtys := map[int]int32{}
		for _, i := range lines {
			target := int32(math.Ceil(need[i]*21)) + 24
			if have := w.back[i]; have < target*2/3 {
				qtys[i] = target - have
			}
		}
		return g.receive(ctx, tx, w, &supplier, qtys, local)
	})
}

// receive — поступление от поставщика по ТТН на склад точки, с марками и сообщениями ЕГАИС.
func (g *gen) receive(ctx context.Context, tx pgx.Tx, p *point, supplier *uuid.UUID, qtys map[int]int32, at time.Time) error {
	if len(qtys) == 0 {
		return nil
	}
	doc, _, err := g.document(ctx, tx, docSpec{kind: "receipt", prefix: "ПТ", p: p, zone: p.zone, supplier: supplier, at: at,
		ttn: fmt.Sprintf("ТТН-%07d", g.rnd.IntN(10_000_000))})
	if err != nil {
		return err
	}
	var moves []stock.Move
	var codes []string
	var variants []uuid.UUID
	keys := sortedKeys(qtys)
	for n, i := range keys {
		it, qty := g.items[i], qtys[i]
		moves = append(moves, stock.Move{VariantID: it.id, LocationID: p.id, ZoneID: p.zone, Qty: qty, External: true, UnitCostE4: it.costE4})
		p.back[i] += qty
		if it.marked {
			for range qty {
				c := newMark()
				codes, variants = append(codes, c), append(variants, it.id)
				p.backMarks[i] = append(p.backMarks[i], c)
			}
		}
		if err := g.line(ctx, tx, doc, n+1, it.id, qty); err != nil {
			return err
		}
	}
	if len(codes) > 0 {
		if err := marks.Register(ctx, tx, marks.Event{DocumentID: doc, UserID: g.author}, p.id, p.zone, codes, variants); err != nil {
			return err
		}
	}
	if _, err := stock.Apply(ctx, tx, stock.Doc{ID: doc, Date: at}, moves); err != nil {
		return err
	}
	if err := g.message(ctx, tx, p, "in", "WayBill", doc, at.Add(-2*time.Hour)); err != nil {
		return err
	}
	return g.message(ctx, tx, p, "out", "WayBillAct", doc, at.Add(time.Hour))
}

func sortedKeys(m map[int]int32) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// season — спрос по месяцам: декабрь и майские праздники выше, январь ниже; пиво растёт летом.
func season(m time.Month, beer bool) float64 {
	k := map[time.Month]float64{1: 0.75, 2: 0.85, 3: 0.95, 4: 1, 5: 1.15, 6: 1.05, 7: 1.05, 8: 1.05, 9: 0.95, 10: 1, 11: 1.05, 12: 1.6}[m]
	if beer && m >= 5 && m <= 8 {
		k *= 1.4
	}
	return k
}

// storeDay — один день магазина: пиво от пивзавода по понедельникам и четвергам, товар со склада
// сети по вторникам и пятницам, утром — пополнение зала, днём — продажи, изредка — бой.
func (g *gen) storeDay(ctx context.Context, s *point, day time.Time, n int) error {
	local := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, s.tz)
	wd := local.Weekday()
	return pgx.BeginFunc(ctx, g.pool, func(tx pgx.Tx) error {
		if n == 0 || wd == time.Monday || wd == time.Thursday {
			qtys := map[int]int32{}
			for _, i := range s.assort {
				if g.items[i].quantity {
					target := int32(math.Ceil(s.demand[i]*5)) + 12
					if have := s.back[i] + s.shelf[i]; have < target*2/3 {
						qtys[i] = target - have
					}
				}
			}
			supplier := g.suppliers[len(g.suppliers)-1]
			if err := g.receive(ctx, tx, s, &supplier, qtys, local.Add(8*time.Hour)); err != nil {
				return err
			}
		}
		if n == 0 || wd == time.Tuesday || wd == time.Friday {
			if err := g.transfer(ctx, tx, s, local.Add(7*time.Hour)); err != nil {
				return err
			}
		}
		if err := g.toFloor(ctx, tx, s, local.Add(9*time.Hour)); err != nil {
			return err
		}
		if err := g.sales(ctx, tx, s, local); err != nil {
			return err
		}
		rate := 30.0 / 4
		if s.small {
			rate = 30
		}
		if g.rnd.Float64() < 1/rate {
			return g.breakage(ctx, tx, s, local.Add(15*time.Hour))
		}
		return nil
	})
}

// transfer — перемещение со склада сети в магазин: отгрузка ставит товар «в пути», приёмка в тот же
// день снимает его на склад магазина; марки идут следом, в ЕГАИС — ТТН и акт.
func (g *gen) transfer(ctx context.Context, tx pgx.Tx, s *point, at time.Time) error {
	w := s.wh
	qtys := map[int]int32{}
	for _, i := range s.assort {
		if g.items[i].quantity {
			continue
		}
		// Неделя продаж и выкладка: у крупного магазина не меньше 8 штук позиции, у малого — 4.
		target := int32(math.Ceil(s.demand[i]*7)) + 8
		if s.small {
			target = int32(math.Ceil(s.demand[i]*7)) + 4
		}
		if have := s.back[i] + s.shelf[i]; have < target*3/4 {
			if q := min(target-have, w.back[i]); q > 0 {
				qtys[i] = q
			}
		}
	}
	if len(qtys) == 0 {
		return nil
	}
	tr, number, err := g.document(ctx, tx, docSpec{kind: "transfer", prefix: "ПМ", p: w, zone: w.zone,
		destLocation: &s.id, destZone: &s.zone, at: at})
	if err != nil {
		return err
	}
	rc, _, err := g.document(ctx, tx, docSpec{kind: "receipt", prefix: "ПТ", p: s, zone: s.zone, transfer: &tr,
		ttn: number, at: at.Add(3 * time.Hour)})
	if err != nil {
		return err
	}
	var out, in []stock.Move
	var codes []string
	for n, i := range sortedKeys(qtys) {
		it, q := g.items[i], qtys[i]
		out = append(out,
			stock.Move{VariantID: it.id, LocationID: w.id, ZoneID: w.zone, Qty: -q},
			stock.Move{VariantID: it.id, LocationID: s.id, ZoneID: s.zone, Qty: q, InTransit: true})
		in = append(in,
			stock.Move{VariantID: it.id, LocationID: s.id, ZoneID: s.zone, Qty: -q, InTransit: true},
			stock.Move{VariantID: it.id, LocationID: s.id, ZoneID: s.zone, Qty: q})
		w.back[i] -= q
		s.back[i] += q
		if it.marked {
			m := w.backMarks[i][:q]
			codes = append(codes, m...)
			s.backMarks[i] = append(s.backMarks[i], m...)
			w.backMarks[i] = slices.Clone(w.backMarks[i][q:])
		}
		if err := g.line(ctx, tx, tr, n+1, it.id, q); err != nil {
			return err
		}
		if err := g.line(ctx, tx, rc, n+1, it.id, q); err != nil {
			return err
		}
	}
	if len(codes) > 0 {
		if err := marks.Ship(ctx, tx, marks.Event{DocumentID: tr, UserID: g.author}, w.zone, s.id, codes); err != nil {
			return err
		}
	}
	if _, err := stock.Apply(ctx, tx, stock.Doc{ID: tr, Date: at}, out); err != nil {
		return err
	}
	if len(codes) > 0 {
		if err := marks.Arrive(ctx, tx, marks.Event{DocumentID: rc, UserID: g.author}, s.id, s.zone, codes); err != nil {
			return err
		}
	}
	if _, err := stock.Apply(ctx, tx, stock.Doc{ID: rc, Date: at}, in); err != nil {
		return err
	}
	if err := g.message(ctx, tx, w, "out", "WayBill", tr, at); err != nil {
		return err
	}
	return g.message(ctx, tx, s, "out", "WayBillAct", rc, at.Add(3*time.Hour))
}

func (g *gen) toFloor(ctx context.Context, tx pgx.Tx, s *point, at time.Time) error {
	var moves []stock.Move
	var codes []string
	for _, i := range s.assort {
		it := g.items[i]
		target := int32(max(4, math.Ceil(s.demand[i]*2)))
		if s.small {
			target = int32(max(3, math.Ceil(s.demand[i]*2)))
		}
		qty := min(target-s.shelf[i], s.back[i])
		if qty <= 0 {
			continue
		}
		moves = append(moves,
			stock.Move{VariantID: it.id, LocationID: s.id, ZoneID: s.zone, Qty: -qty},
			stock.Move{VariantID: it.id, LocationID: s.id, ZoneID: s.floor, Qty: qty})
		s.back[i] -= qty
		s.shelf[i] += qty
		if it.marked {
			m := s.backMarks[i][:qty]
			codes = append(codes, m...)
			s.shelfMarks[i] = append(s.shelfMarks[i], m...)
			s.backMarks[i] = slices.Clone(s.backMarks[i][qty:])
		}
	}
	if len(moves) == 0 {
		return nil
	}
	doc, _, err := g.document(ctx, tx, docSpec{kind: "floor_transfer", prefix: "ПЗ", p: s, zone: s.zone, destZone: &s.floor, at: at})
	if err != nil {
		return err
	}
	if len(codes) > 0 {
		if err := marks.MoveWithin(ctx, tx, marks.Event{DocumentID: doc, UserID: g.author}, s.zone, s.id, s.floor, codes); err != nil {
			return err
		}
	}
	_, err = stock.Apply(ctx, tx, stock.Doc{ID: doc, Date: at}, moves)
	return err
}

// hourWeights — когда приходят покупатели: днём ровно, вечером после работы — пик.
var hourWeights = []float64{1, 1.2, 1.4, 1.5, 1.6, 1.8, 2.2, 2.8, 3.4, 3.6, 3.2, 2.2}

func (g *gen) sales(ctx context.Context, tx pgx.Tx, s *point, local time.Time) error {
	week := map[time.Weekday]float64{0: 1.15, 1: 0.8, 2: 0.85, 3: 0.9, 4: 1, 5: 1.35, 6: 1.4}[local.Weekday()]
	total := int(receiptsPerDay(s) * week * season(local.Month(), false) * (0.92 + 0.16*g.rnd.Float64()))
	// Выбор позиции — бинарный поиск по накопленным весам матрицы магазина.
	cum := make([]float64, len(s.assort))
	var acc float64
	for k, i := range s.assort {
		acc += g.items[i].popularity * season(local.Month(), g.items[i].quantity)
		cum[k] = acc
	}
	var hourAcc []float64
	var h float64
	for _, w := range hourWeights {
		h += w
		hourAcc = append(hourAcc, h)
	}
	opened, closed := local.Add(10*time.Hour), local.Add(22*time.Hour)
	sold := map[int]int32{}
	for d, device := range s.devices {
		count := total / len(s.devices)
		cashier := s.cashiers[(local.YearDay()*len(s.devices)+d)%len(s.cashiers)]
		doc, _, err := g.document(ctx, tx, docSpec{kind: "sale", prefix: "ПР", p: s, zone: s.floor, at: closed})
		if err != nil {
			return err
		}
		s.shifts[d]++
		var shift uuid.UUID
		if err := tx.QueryRow(ctx, `insert into pos_shifts (device_id, cashier_id, number, sale_document_id, opened_at, closed_at, status)
			values ($1, $2, $3, $4, $5, $6, 'closed') returning id`, device, cashier, s.shifts[d], doc, opened, closed).Scan(&shift); err != nil {
			return err
		}
		docSold := map[int]int32{}
		var codes []string
		var receipts, lines [][]any
		for r := range count {
			hour := sort.SearchFloat64s(hourAcc, g.rnd.Float64()*h)
			at := opened.Add(time.Duration(hour)*time.Hour + time.Duration(g.rnd.Int64N(int64(time.Hour))))
			n := 1
			for n < 5 && g.rnd.Float64() < 0.45 {
				n++
			}
			id := uuid.New()
			var sum int64
			var marked bool
			var rl [][]any
			for range n {
				k := sort.SearchFloat64s(cum, g.rnd.Float64()*acc)
				i := s.assort[min(k, len(s.assort)-1)]
				it := g.items[i]
				left := s.shelf[i] - sold[i]
				if left <= 0 {
					continue
				}
				qty := int32(1)
				if it.quantity {
					qty = min(int32(1+g.rnd.IntN(4)), left)
				}
				var mark any
				if it.marked {
					mark = s.shelfMarks[i][0]
					s.shelfMarks[i] = s.shelfMarks[i][1:]
					codes = append(codes, mark.(string))
					marked = true
				}
				sold[i] += qty
				docSold[i] += qty
				sum += it.priceKop * int64(qty)
				rl = append(rl, []any{id, int32(len(rl) + 1), it.id, qty, kop(it.priceKop), mark})
			}
			if len(rl) == 0 {
				continue
			}
			payment := "card"
			if g.rnd.IntN(10) < 3 {
				payment = "cash"
			}
			var qr any
			if marked {
				qr = "https://check.egais.ru/?id=" + id.String()
			}
			receipts = append(receipts, []any{id, shift, int32(r + 1), "sale", at, at, kop(sum), payment, true, qr})
			lines = append(lines, rl...)
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"pos_receipts"},
			[]string{"id", "shift_id", "number", "kind", "created_on_device_at", "received_at", "total", "payment", "age_confirmed", "egais_qr"},
			pgx.CopyFromRows(receipts)); err != nil {
			return err
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"pos_receipt_lines"},
			[]string{"receipt_id", "line_no", "variant_id", "qty", "price", "mark_code"}, pgx.CopyFromRows(lines)); err != nil {
			return err
		}
		var moves []stock.Move
		var beer bool
		for n, i := range sortedKeys(docSold) {
			moves = append(moves, stock.Move{VariantID: g.items[i].id, LocationID: s.id, ZoneID: s.floor, Qty: -docSold[i], External: true})
			beer = beer || g.items[i].quantity
			if err := g.line(ctx, tx, doc, n+1, g.items[i].id, docSold[i]); err != nil {
				return err
			}
		}
		if len(codes) > 0 {
			if _, err := marks.Sell(ctx, tx, marks.Event{DocumentID: doc, UserID: cashier}, s.id, codes); err != nil {
				return err
			}
		}
		if len(moves) > 0 {
			if _, err := stock.Apply(ctx, tx, stock.Doc{ID: doc, Date: closed}, moves); err != nil {
				return err
			}
		}
		// Пиво смены списывается в ЕГАИС актом «Реализация».
		if beer {
			if err := g.message(ctx, tx, s, "out", "ActWriteOffShop", doc, closed); err != nil {
				return err
			}
		}
	}
	for i, q := range sold {
		s.shelf[i] -= q
	}
	return nil
}

func kop(v int64) pgtype.Numeric { return pgtype.Numeric{Int: big.NewInt(v), Exp: -2, Valid: true} }

func (g *gen) breakage(ctx context.Context, tx pgx.Tx, s *point, at time.Time) error {
	start := g.rnd.IntN(len(s.assort))
	for k := range s.assort {
		i := s.assort[(start+k)%len(s.assort)]
		it := g.items[i]
		if !it.marked || s.shelf[i] == 0 {
			continue
		}
		doc, _, err := g.document(ctx, tx, docSpec{kind: "writeoff", prefix: "СП", p: s, zone: s.floor, at: at})
		if err != nil {
			return err
		}
		code := s.shelfMarks[i][0]
		s.shelfMarks[i] = s.shelfMarks[i][1:]
		s.shelf[i]--
		if err := g.line(ctx, tx, doc, 1, it.id, 1); err != nil {
			return err
		}
		z := s.floor
		if err := marks.WriteOff(ctx, tx, marks.Event{DocumentID: doc, UserID: g.author}, s.id, &z, []string{code}); err != nil {
			return err
		}
		if _, err := stock.Apply(ctx, tx, stock.Doc{ID: doc, Date: at}, []stock.Move{
			{VariantID: it.id, LocationID: s.id, ZoneID: s.floor, Qty: -1, External: true}}); err != nil {
			return err
		}
		return g.message(ctx, tx, s, "out", "ActWriteOff", doc, at.Add(10*time.Minute))
	}
	return nil
}

// Rests — остатки точки так, как их видит ЕГАИС: марки на балансе и количество продукции без марок
// по регистрам (1 — склад, 2 — торговый зал). Читает govsim seed-rests.
type Rests struct {
	Points []PointRests `json:"points"`
}

type PointRests struct {
	FSRARID string      `json:"fsrar_id"`
	Marks   [][2]string `json:"marks"`
	Rests   []Rest      `json:"rests"`
}

type Rest struct {
	Alcocode string `json:"alcocode"`
	Register int    `json:"register"`
	Qty      int    `json:"qty"`
}

func (g *gen) writeRests(ctx context.Context, w io.Writer) error {
	out := Rests{}
	rows, err := g.pool.Query(ctx, `select l.fsrar_id, coalesce(
			(select json_agg(json_build_array(e.code, v.alcocode)) from excise_marks e join variants v on v.id = e.variant_id
			 where e.location_id = l.id and e.status in ('in_stock', 'reserved') and v.alcocode is not null), '[]'),
		coalesce(
			(select json_agg(json_build_object('alcocode', r.alcocode, 'register', r.register, 'qty', r.qty))
			 from (select v.alcocode, case when z.kind = 'floor' then 2 else 1 end register, sum(b.on_hand)::int qty
			       from stock_balances b join zones z on z.id = b.zone_id join variants v on v.id = b.variant_id
			       join products p on p.id = v.product_id join product_kinds k on k.code = p.kind_code
			       where b.location_id = l.id and k.egais_accounting = 'quantity' and v.alcocode is not null and b.on_hand > 0
			       group by 1, 2) r), '[]')
		from locations l where l.fsrar_id is not null order by l.code`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p PointRests
		var m, r []byte
		if err := rows.Scan(&p.FSRARID, &m, &r); err != nil {
			return err
		}
		if err := json.Unmarshal(m, &p.Marks); err != nil {
			return err
		}
		if err := json.Unmarshal(r, &p.Rests); err != nil {
			return err
		}
		out.Points = append(out.Points, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(out)
}
