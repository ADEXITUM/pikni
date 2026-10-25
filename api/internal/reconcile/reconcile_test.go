package reconcile_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/reconcile"
)

func ptr[T any](v T) *T { return &v }

func TestReconciliation(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var store, admin uuid.UUID
	must(db.App.QueryRow(ctx, `insert into locations (code, name, kind, city, timezone, subject_code)
		values ('МГ-01', 'Магазин', 'store', 'Благовещенск', 'Asia/Yakutsk', '28') returning id`).Scan(&store))
	must(db.App.QueryRow(ctx, `insert into users (login, full_name, role, password_hash) values ('a', 'А', 'manager', 'x') returning id`).Scan(&admin))
	actx := auth.WithSession(ctx, auth.Session{UserID: admin, Role: "manager"})
	svc := reconcile.NewService(db.App)

	if err := svc.Request(actx, store); !errors.Is(err, reconcile.ErrNoFSRAR) {
		t.Fatalf("request without FSRAR ID = %v", err)
	}
	_, err := db.App.Exec(ctx, `update locations set fsrar_id = '030000000001' where id = $1`, store)
	must(err)
	must(svc.Request(actx, store))
	r, err := svc.Report(actx, store)
	must(err)
	if r.RequestedAt == nil || r.TakenAt != nil {
		t.Fatalf("before reply: requested %v, taken %v", r.RequestedAt, r.TakenAt)
	}

	cat := catalog.NewService(db.App)
	producer, err := cat.SaveProducer(ctx, nil, apispec.ProducerFields{Name: "ООО «Амур»", CountryCode: "643"}, true)
	must(err)
	var beerCat, vodkaCat uuid.UUID
	must(db.App.QueryRow(ctx, `select id from categories where name = 'Пиво и сидр'`).Scan(&beerCat))
	must(db.App.QueryRow(ctx, `select id from categories where name = 'Водка'`).Scan(&vodkaCat))
	variant := func(name string, category uuid.UUID, kind, alcocode string) uuid.UUID {
		p, err := cat.CreateProduct(ctx, apispec.ProductFields{Name: name, CategoryID: category, KindCode: ptr(kind), Abv: ptr(5.0),
			ProducerID: producer.ID, CountryCode: "643"})
		must(err)
		v, err := cat.CreateVariant(ctx, p.ID, catalog.VariantSpec{VolumeMl: ptr(int32(500)), Alcocode: ptr(alcocode)})
		must(err)
		return v.ID
	}
	beer := variant("Амурское", beerCat, "500", "0000000000000000001")
	cider := variant("Сидр яблочный", beerCat, "261", "0000000000000000002")
	vodka := variant("Тайга", vodkaCat, "200", "0177207000002463655")
	put := func(v uuid.UUID, zone string, qty int) {
		_, err := db.App.Exec(ctx, `insert into stock_balances (variant_id, zone_id, location_id, on_hand)
			select $1, z.id, $2, $4 from zones z where z.location_id = $2 and z.kind = $3`, v, store, zone, qty)
		must(err)
	}
	put(beer, "backroom", 10)
	put(beer, "floor", 6)
	put(cider, "floor", 3)
	put(vodka, "floor", 2)
	for _, code := range []string{"MARKOURS000001", "MARKBOTH000001"} {
		_, err := db.App.Exec(ctx, `insert into excise_marks (code, variant_id, location_id, zone_id, status)
			select $1, $2, $3, z.id, 'in_stock' from zones z where z.location_id = $3 and z.kind = 'floor'`, code, vodka, store)
		must(err)
	}

	payload, _ := json.Marshal(map[string]any{
		"rests": []map[string]any{
			{"alcocode": "0000000000000000001", "name": "Амурское", "register": 1, "qty": 10},
			{"alcocode": "0000000000000000001", "name": "Амурское", "register": 2, "qty": 4},
			{"alcocode": "0000000000000000009", "name": "Чужое пиво", "register": 2, "qty": 1},
		},
		"marks": []map[string]any{
			{"code": "MARKBOTH000001", "alcocode": "0177207000002463655", "name": "Тайга"},
			{"code": "MARKEGAIS00001", "alcocode": "0177207000002463655", "name": "Тайга"},
		},
	})
	_, err = db.App.Exec(ctx, `insert into egais_rests_snapshots (location_id, payload) values ($1, $2)`, store, payload)
	must(err)

	r, err = svc.Report(actx, store)
	must(err)
	if r.TakenAt == nil {
		t.Fatal("snapshot not taken into account")
	}
	type diff struct {
		alcocode    string
		register    int
		ours, egais int
	}
	var got []diff
	for _, p := range r.Products {
		got = append(got, diff{p.Alcocode, p.Register, p.Ours, p.Egais})
	}
	want := []diff{
		{"0000000000000000001", 2, 6, 4},
		{"0000000000000000002", 2, 3, 0},
		{"0000000000000000009", 2, 0, 1},
	}
	if len(got) != len(want) {
		t.Fatalf("products = %+v, want %+v", got, want)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g == w
		}
		if !found {
			t.Fatalf("products = %+v, missing %+v", got, w)
		}
	}
	if len(r.Marks) != 2 || r.Marks[0].Code != "MARKEGAIS00001" || r.Marks[0].OnlyOurs ||
		r.Marks[1].Code != "MARKOURS000001" || !r.Marks[1].OnlyOurs {
		t.Fatalf("marks = %+v", r.Marks)
	}

	var keeper uuid.UUID
	must(db.App.QueryRow(ctx, `insert into users (login, full_name, role, password_hash) values ('k', 'К', 'cashier', 'x') returning id`).Scan(&keeper))
	kctx := auth.WithSession(ctx, auth.Session{UserID: keeper, Role: "cashier", LocationIDs: []uuid.UUID{store}})
	if _, err := svc.Report(kctx, store); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cashier report = %v, want forbidden", err)
	}
}
