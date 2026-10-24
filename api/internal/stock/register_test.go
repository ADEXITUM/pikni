package stock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

type env struct {
	db       *dbtest.DB
	location uuid.UUID
	backroom uuid.UUID
	floor    uuid.UUID
	variant  uuid.UUID
	user     uuid.UUID
	n        int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := t.Context()
	e := &env{db: dbtest.New(t)}
	q := func(sql string, dst any, args ...any) {
		t.Helper()
		if err := e.db.App.QueryRow(ctx, sql, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	q(`insert into locations (code, name, kind, city, timezone, subject_code)
		values ('МГ-01', 'Магазин', 'store', 'Благовещенск', 'Asia/Yakutsk', '28') returning id`, &e.location)
	q(`select id from zones where location_id = $1 and kind = 'backroom'`, &e.backroom, e.location)
	q(`select id from zones where location_id = $1 and kind = 'floor'`, &e.floor, e.location)
	q(`insert into users (login, full_name, role, password_hash) values ('keeper', 'Кладовщик', 'storekeeper', 'x') returning id`, &e.user)
	var producer, product uuid.UUID
	q(`insert into producers (name, country_code) values ('ООО «Тайга»', '643') returning id`, &producer)
	q(`insert into products (name, category_id, producer_id, country_code)
		select 'Сухарики', id, $1, '643' from categories where name = 'Снеки' returning id`, &product, producer)
	q(`insert into variants (product_id, net_weight_g, article) values ($1, 70, next_article()) returning id`, &e.variant, product)
	return e
}

// apply проводит движения отдельным документом, как это делает модуль документов.
func (e *env) apply(t *testing.T, moves ...stock.Move) (map[uuid.UUID]int64, error) {
	t.Helper()
	ctx := t.Context()
	e.n++
	var doc uuid.UUID
	err := e.db.App.QueryRow(ctx, `insert into documents (kind, number, doc_date, location_id, zone_id, dest_zone_id, author_id)
		values ('floor_transfer', $1, current_date, $2, $3, $4, $5) returning id`,
		uuid.NewString(), e.location, e.backroom, e.floor, e.user).Scan(&doc)
	if err != nil {
		t.Fatal(err)
	}
	lines := map[uuid.UUID]bool{}
	for _, m := range moves {
		if lines[m.LineID] {
			continue
		}
		lines[m.LineID] = true
		if _, err := e.db.App.Exec(ctx, `insert into document_lines (id, document_id, line_no, variant_id, qty)
			values ($1, $2, $3, $4, 0)`, m.LineID, doc, len(lines), m.VariantID); err != nil {
			t.Fatal(err)
		}
	}
	var costs map[uuid.UUID]int64
	err = pgx.BeginFunc(ctx, e.db.App, func(tx pgx.Tx) error {
		costs, err = stock.Apply(ctx, tx, stock.Doc{ID: doc, Date: time.Now()}, moves)
		return err
	})
	return costs, err
}

func (e *env) move(zone uuid.UUID, qty int32, external bool, priceRub int64) stock.Move {
	return stock.Move{
		LineID: uuid.New(), VariantID: e.variant, LocationID: e.location, ZoneID: zone,
		Qty: qty, External: external, UnitCostE4: priceRub * 10000,
	}
}

// transfer — передача со склада в зал одной строкой документа, как её проводит модуль документов.
func (e *env) transfer(qty int32) []stock.Move {
	from, to := e.move(e.backroom, -qty, false, 0), e.move(e.floor, qty, false, 0)
	to.LineID = from.LineID
	return []stock.Move{from, to}
}

func (e *env) state(t *testing.T) (onHandBack, onHandFloor, deficitFloor, qtyTotal int32, avgE4 int64) {
	t.Helper()
	ctx := t.Context()
	row := func(zone uuid.UUID) (int32, int32) {
		var on, def int32
		_ = e.db.App.QueryRow(ctx, `select on_hand, deficit from stock_balances where variant_id = $1 and zone_id = $2`,
			e.variant, zone).Scan(&on, &def)
		return on, def
	}
	onHandBack, _ = row(e.backroom)
	onHandFloor, deficitFloor = row(e.floor)
	_ = e.db.App.QueryRow(ctx, `select qty_total, (avg_cost * 10000)::bigint from variant_costs where variant_id = $1`,
		e.variant).Scan(&qtyTotal, &avgE4)
	return
}

func integrity(t *testing.T, ctx context.Context, svc *stock.Service) {
	t.Helper()
	problems, err := svc.Integrity(ctx)
	if err != nil || len(problems) > 0 {
		t.Fatalf("integrity: %+v, %v", problems, err)
	}
}

func TestAverageCost(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	svc := stock.NewService(e.db.App, nil)

	if _, err := e.apply(t, e.move(e.backroom, 10, true, 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.apply(t, e.move(e.backroom, 10, true, 120)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, total, avg := e.state(t); total != 20 || avg != 1100000 {
		t.Fatalf("after two receipts: total %d, avg %d", total, avg)
	}

	// Передача в зал переносит запас по текущей средней и среднюю не меняет.
	transfer := e.transfer(5)
	costs, err := e.apply(t, transfer...)
	if err != nil || costs[transfer[0].LineID] != 1100000 {
		t.Fatalf("transfer: %v, %v", costs, err)
	}
	back, floor, _, total, avg := e.state(t)
	if back != 15 || floor != 5 || total != 20 || avg != 1100000 {
		t.Errorf("after transfer: %d/%d, total %d, avg %d", back, floor, total, avg)
	}

	// Внешний расход уменьшает запас организации, средняя остаётся.
	if _, err := e.apply(t, e.move(e.floor, -5, true, 0)); err != nil {
		t.Fatal(err)
	}
	if _, floor, _, total, avg := e.state(t); floor != 0 || total != 15 || avg != 1100000 {
		t.Errorf("after issue: floor %d, total %d, avg %d", floor, total, avg)
	}

	var short *stock.ShortageError
	if _, err := e.apply(t, e.move(e.floor, -1, false, 0)); !errors.As(err, &short) || short.Available != 0 || short.Need != 1 {
		t.Errorf("shortage: %v", err)
	}

	var sum int64
	if err := e.db.App.QueryRow(t.Context(), `select (sum(cost_amount) * 100)::bigint from stock_moves where variant_id = $1`,
		e.variant).Scan(&sum); err != nil || sum != 15*11000 {
		t.Errorf("register value = %d kopecks, want %d, %v", sum, 15*11000, err)
	}
	integrity(t, t.Context(), svc)
}

// Продажа сверх учёта оставляет недостачу к разбору (решение 0013). Приход в зону сначала
// гасит её, на остаток встаёт только разница.
func TestDeficitIsSettledFirst(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	svc := stock.NewService(e.db.App, nil)

	// Кассы ещё нет (итерация 6): недостачу записываем так, как её запишет закрытие смены.
	if _, err := e.apply(t, e.move(e.backroom, 1, true, 50)); err != nil {
		t.Fatal(err)
	}
	var doc uuid.UUID
	_ = e.db.App.QueryRow(ctx, `select document_id from stock_moves limit 1`).Scan(&doc)
	if _, err := e.db.App.Exec(ctx, `insert into stock_moves (document_id, variant_id, location_id, zone_id, bucket, qty, cost_amount, doc_date)
		values ($1, $2, $3, $4, 'deficit', 2, 0, current_date)`, doc, e.variant, e.location, e.floor); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.App.Exec(ctx, `insert into stock_balances (variant_id, zone_id, location_id, deficit) values ($1, $2, $3, 2)`,
		e.variant, e.floor, e.location); err != nil {
		t.Fatal(err)
	}
	integrity(t, ctx, svc)

	if _, err := e.apply(t, e.move(e.backroom, 4, true, 50)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.apply(t, e.transfer(5)...); err != nil {
		t.Fatal(err)
	}
	if back, floor, deficit, _, _ := e.state(t); back != 0 || floor != 3 || deficit != 0 {
		t.Errorf("after transfer to floor: back %d, floor %d, deficit %d", back, floor, deficit)
	}
	integrity(t, ctx, svc)
}

func TestRegisterIsAppendOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if _, err := e.apply(t, e.move(e.backroom, 1, true, 50)); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{`update stock_moves set qty = 100`, `delete from stock_moves`} {
		_, err := e.db.App.Exec(t.Context(), stmt)
		dbtest.WantCode(t, err, "42501")
		_, err = e.db.Owner.Exec(t.Context(), stmt)
		dbtest.WantCode(t, err, "P0001")
	}
	// Остаток не может стать отрицательным даже в обход сервиса.
	_, err := e.db.App.Exec(t.Context(), `update stock_balances set on_hand = -1`)
	dbtest.WantCode(t, err, "23514")
}
