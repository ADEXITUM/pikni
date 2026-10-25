package availability_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/availability"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func ptr[T any](v T) *T { return &v }

type fixture struct {
	db                         *dbtest.DB
	svc                        *availability.Service
	blg, khv                   uuid.UUID
	taiga, taiga07             uuid.UUID // варианты 0,5 и 0,7 л одной позиции
	taigaSilver, beluga, pricy uuid.UUID // та же линейка, аналог и слишком дорогой аналог
	taigaProduct               uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.New(t)
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	f := &fixture{db: db, svc: availability.NewService(db.App, marks.NewService(db.App))}
	store := func(code, city, tz, subject string) uuid.UUID {
		var id uuid.UUID
		must(db.App.QueryRow(ctx, `insert into locations (code, name, kind, city, timezone, subject_code, lat, lon)
			values ($1, 'Магазин', 'store', $2, $3, $4, 50.3, 127.5) returning id`, code, city, tz, subject).Scan(&id))
		return id
	}
	f.blg = store("МГ-01", "Благовещенск", "Asia/Yakutsk", "28")
	f.khv = store("МГ-03", "Хабаровск", "Asia/Vladivostok", "27")

	var admin uuid.UUID
	must(db.App.QueryRow(ctx, `insert into users (login, full_name, role, password_hash) values ('a', 'А', 'admin', 'x') returning id`).Scan(&admin))
	actx := auth.WithSession(ctx, auth.Session{UserID: admin, Role: "admin"})
	cat := catalog.NewService(db.App)
	producer, err := cat.SaveProducer(actx, nil, apispec.ProducerFields{Name: "ООО «Тайга»", CountryCode: "643"}, true)
	must(err)
	var vodka, brand, line uuid.UUID
	must(db.App.QueryRow(ctx, `select id from categories where name = 'Водка'`).Scan(&vodka))
	must(db.App.QueryRow(ctx, `insert into brands (producer_id, name) values ($1, 'Тайга') returning id`, producer.ID).Scan(&brand))
	must(db.App.QueryRow(ctx, `insert into product_lines (brand_id, name) values ($1, 'Тайга') returning id`, brand).Scan(&line))

	n := 0
	variant := func(product uuid.UUID, ml int32, rub int64) uuid.UUID {
		n++
		v, err := cat.CreateVariant(actx, product, catalog.VariantSpec{VolumeMl: ptr(ml), Alcocode: ptr(fmt.Sprintf("01772070000024636%02d", n))})
		must(err)
		_, err = cat.SetPrice(actx, v.ID, apispec.PriceKindRetail, rub*100, nil, nil)
		must(err)
		return v.ID
	}
	product := func(name string, withLine bool) uuid.UUID {
		fields := apispec.ProductFields{Name: name, CategoryID: vodka, KindCode: ptr("200"), Abv: ptr(40.0),
			ProducerID: producer.ID, CountryCode: "643"}
		if withLine {
			fields.BrandID, fields.ProductLineID = &brand, &line
			fields.Variety = ptr(name)
		}
		p, err := cat.CreateProduct(actx, fields)
		must(err)
		return p.ID
	}
	f.taigaProduct = product("Тайга классическая", true)
	f.taiga = variant(f.taigaProduct, 500, 599)
	f.taiga07 = variant(f.taigaProduct, 700, 799)
	f.taigaSilver = variant(product("Тайга серебряная", true), 500, 649)
	f.beluga = variant(product("Белуга", false), 500, 640)
	f.pricy = variant(product("Царская", false), 500, 900)

	put := func(variant, location uuid.UUID, zone string, qty int) {
		_, err := db.App.Exec(ctx, `insert into stock_balances (variant_id, zone_id, location_id, on_hand)
			select $1, z.id, $2, $4 from zones z where z.location_id = $2 and z.kind = $3`, variant, location, zone, qty)
		must(err)
	}
	put(f.taiga, f.blg, "floor", 3)
	put(f.taiga, f.blg, "backroom", 10)
	put(f.taiga, f.khv, "floor", 2)
	put(f.taiga07, f.khv, "floor", 1)
	put(f.beluga, f.blg, "floor", 4)
	put(f.pricy, f.blg, "floor", 4)
	return f
}

func (f *fixture) search(t *testing.T, flt availability.Filter) map[string][3]int32 {
	t.Helper()
	if flt.Sort == "" {
		flt.Sort = "name"
	}
	if flt.Limit == 0 {
		flt.Limit = 30
	}
	rows, err := f.svc.Search(context.Background(), flt)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][3]int32{}
	for _, r := range rows {
		out[r.Name] = [3]int32{r.ScopeQty, r.NetworkQty, r.HereQty}
	}
	return out
}

func TestSearch(t *testing.T) {
	f := newFixture(t)
	got := f.search(t, availability.Filter{Search: "тайга", Here: &f.blg})
	if got["Тайга классическая"] != [3]int32{16, 16, 13} || len(got) != 2 {
		t.Fatalf("search by name: %v", got)
	}
	got = f.search(t, availability.Filter{City: ptr("Хабаровск"), InStock: true})
	if len(got) != 1 || got["Тайга классическая"][0] != 3 {
		t.Fatalf("in stock in Khabarovsk: %v", got)
	}
	got = f.search(t, availability.Filter{VolumeMl: ptr(int32(700))})
	if len(got) != 1 {
		t.Fatalf("0,7 l only: %v", got)
	}
	got = f.search(t, availability.Filter{PriceFrom: ptr(int64(62000)), PriceTo: ptr(int64(70000))})
	if _, ok := got["Белуга"]; !ok || len(got) != 2 {
		t.Fatalf("price 620–700: %v", got)
	}
	rows, _ := f.svc.Search(t.Context(), availability.Filter{Sort: "price", Limit: 30})
	if rows[0].Name != "Тайга классическая" || rows[len(rows)-1].Name != "Царская" || rows[0].Total != 4 {
		t.Fatalf("sorted by price: %s … %s, total %d", rows[0].Name, rows[len(rows)-1].Name, rows[0].Total)
	}

	if _, err := f.db.App.Exec(t.Context(), `update products set attributes = '{"filtration": "серебро", "age": 5}'
		where id = (select product_id from variants where id = $1)`, f.beluga); err != nil {
		t.Fatal(err)
	}
	got = f.search(t, availability.Filter{Attrs: `{"filtration": "серебро", "age": {"min": 3}}`})
	if len(got) != 1 || got["Белуга"] == ([3]int32{}) {
		t.Fatalf("attribute filters: %v", got)
	}
	if len(f.search(t, availability.Filter{Attrs: `{"age": {"min": 6}}`})) != 0 {
		t.Fatal("age range excludes")
	}
	if _, err := f.svc.Search(t.Context(), availability.Filter{Sort: "name", Limit: 30, Attrs: `{"age": [1]}`}); !errors.Is(err, availability.ErrBadFilters) {
		t.Fatalf("bad filter: %v", err)
	}
}

func TestCardAndSimilar(t *testing.T) {
	f := newFixture(t)
	c, err := f.svc.Card(t.Context(), f.taigaProduct)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Variants) != 2 || len(c.Locations) != 2 || len(c.Cells) != 3 {
		t.Fatalf("card: %d variants, %d locations, %d cells", len(c.Variants), len(c.Locations), len(c.Cells))
	}
	for _, cell := range c.Cells {
		if cell.VariantID == f.taiga && cell.LocationID == f.blg && (cell.Floor != 3 || cell.Storage != 10) {
			t.Fatalf("floor and storage split: %+v", cell)
		}
	}

	pack, line, analogs, err := f.svc.Similar(t.Context(), f.taiga, &f.blg)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack) != 1 || pack[0].VariantID != f.taiga07 {
		t.Fatalf("other volume: %+v", pack)
	}
	if len(line) != 1 || line[0].VariantID != f.taigaSilver {
		t.Fatalf("same line: %+v", line)
	}
	// Белуга: 640 ₽ в пределах ±15 % от 599 ₽ и есть в точке; Царская дороже, серебряная — в линейке.
	if len(analogs) != 1 || analogs[0].VariantID != f.beluga || analogs[0].HereQty != 4 {
		t.Fatalf("analogs: %+v", analogs)
	}
	_, _, elsewhere, _ := f.svc.Similar(t.Context(), f.taiga, &f.khv)
	if len(elsewhere) != 0 {
		t.Fatalf("no analogs in stock in Khabarovsk: %+v", elsewhere)
	}
}
