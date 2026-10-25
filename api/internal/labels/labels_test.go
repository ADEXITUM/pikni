package labels_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/forms"
	"github.com/ADEXITUM/pikni/api/internal/labels"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func ptr[T any](v T) *T { return &v }

func TestPriceTags(t *testing.T) {
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
	must(db.App.QueryRow(ctx, `insert into users (login, full_name, role, password_hash) values ('m', 'М', 'manager', 'x') returning id`).Scan(&admin))
	mctx := auth.WithSession(ctx, auth.Session{UserID: admin, Role: "manager"})

	cat := catalog.NewService(db.App)
	producer, err := cat.SaveProducer(ctx, nil, apispec.ProducerFields{Name: "ООО «Тайга»", CountryCode: "643"}, true)
	must(err)
	var vodkaCat uuid.UUID
	must(db.App.QueryRow(ctx, `select id from categories where name = 'Водка'`).Scan(&vodkaCat))
	p, err := cat.CreateProduct(ctx, apispec.ProductFields{Name: "Тайга классическая", CategoryID: vodkaCat, KindCode: ptr("200"),
		Abv: ptr(40.0), ProducerID: producer.ID, CountryCode: "643"})
	must(err)
	v, err := cat.CreateVariant(ctx, p.ID, catalog.VariantSpec{VolumeMl: ptr(int32(500)), Alcocode: ptr("0177207000002463655")})
	must(err)
	_, err = cat.AddBarcode(ctx, v.ID, ptr("4600000000015"))
	must(err)
	_, err = cat.SetPrice(ctx, v.ID, apispec.PriceKindRetail, 59900, nil, nil)
	must(err)
	_, err = db.App.Exec(ctx, `insert into stock_balances (variant_id, zone_id, location_id, on_hand)
		select $1, z.id, $2, 6 from zones z where z.location_id = $2 and z.kind = 'floor'`, v.ID, store)
	must(err)

	svc := labels.NewService(db.App)
	stale, err := svc.Stale(mctx, store)
	must(err)
	if len(stale) != 1 || stale[0].PrintedAt != nil || stale[0].PriceKopecks != 59900 || stale[0].Barcode != "4600000000015" {
		t.Fatalf("never printed: %+v", stale)
	}
	for _, format := range []forms.Format{forms.A4, forms.Thermal} {
		pdf, err := svc.Print(mctx, store, labels.Source{Variants: []uuid.UUID{v.ID}}, format)
		if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) {
			t.Fatalf("print %s: %v", format, err)
		}
	}
	if stale, _ = svc.Stale(mctx, store); len(stale) != 0 {
		t.Fatalf("after printing: %+v", stale)
	}
	_, err = cat.SetPrice(ctx, v.ID, apispec.PriceKindRetail, 64900, nil, nil)
	must(err)
	stale, _ = svc.Stale(mctx, store)
	if len(stale) != 1 || stale[0].PrintedKopecks != 59900 || stale[0].PriceKopecks != 64900 {
		t.Fatalf("after price change: %+v", stale)
	}
	tag := labels.Tag(stale[0], time.Now())
	if tag.PerLiterKopeck != 129800 || tag.Abv != "40 %" || tag.Pack == "" {
		t.Fatalf("tag: %+v", tag)
	}
}
