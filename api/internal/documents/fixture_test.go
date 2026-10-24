package documents_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

// Марка старого образца из серверных тестов разбора: алкокод 0177207000002463655.
const oldMark = "20N00001CGUMZYCB99J1NKN31105001000056NQQMS5VP4HTF5SB46ZSQQJD8BNJP891"

type product struct {
	variant uuid.UUID
	ean     string
}

type fixture struct {
	db       *dbtest.DB
	cat      *catalog.Service
	marks    *marks.Service
	stock    *stock.Service
	svc      *documents.Service
	store    uuid.UUID
	backroom uuid.UUID
	floor    uuid.UUID

	keeper, manager, accountant context.Context

	vodka, beer, snack product
}

// ean дополняет 12 цифр контрольной цифрой GS1.
func ean(body string) string {
	sum := 0
	for i := len(body) - 1; i >= 0; i-- {
		d := int(body[i] - '0')
		if (len(body)-1-i)%2 == 0 {
			d *= 3
		}
		sum += d
	}
	return fmt.Sprintf("%s%d", body, (10-sum%10)%10)
}

func ptr[T any](v T) *T { return &v }

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
	f := &fixture{db: db, cat: catalog.NewService(db.App), marks: marks.NewService(db.App)}
	f.stock = stock.NewService(db.App, f.marks)
	f.svc = documents.NewService(db.App, f.cat, f.marks)

	f.store = f.newStore(t, "МГ-01")
	must(db.App.QueryRow(ctx, `select id from zones where location_id = $1 and kind = 'backroom'`, f.store).Scan(&f.backroom))
	must(db.App.QueryRow(ctx, `select id from zones where location_id = $1 and kind = 'floor'`, f.store).Scan(&f.floor))

	f.keeper = f.session(t, "storekeeper", f.store)
	f.manager = f.session(t, "manager")
	f.accountant = f.session(t, "accountant")

	producer, err := f.cat.SaveProducer(ctx, nil, apispec.ProducerFields{Name: "ООО «Тайга»", CountryCode: "643"}, true)
	must(err)
	category := func(name string) uuid.UUID {
		var id uuid.UUID
		must(db.App.QueryRow(ctx, `select id from categories where name = $1`, name).Scan(&id))
		return id
	}
	add := func(fields apispec.ProductFields, spec catalog.VariantSpec, code string, kopecks int64) product {
		t.Helper()
		fields.ProducerID, fields.CountryCode = producer.ID, "643"
		p, err := f.cat.CreateProduct(ctx, fields)
		must(err)
		v, err := f.cat.CreateVariant(ctx, p.ID, spec)
		must(err)
		_, err = f.cat.AddBarcode(ctx, v.ID, &code)
		must(err)
		if kopecks > 0 {
			_, err = f.cat.SetPrice(ctx, v.ID, apispec.PriceKindPurchase, kopecks, nil, nil)
			must(err)
		}
		return product{variant: v.ID, ean: code}
	}
	f.vodka = add(apispec.ProductFields{Name: "Тайга", CategoryID: category("Водка"), KindCode: ptr("200"), Abv: ptr(40.0)},
		catalog.VariantSpec{VolumeMl: ptr(int32(500)), Alcocode: ptr("0177207000002463655")}, ean("460000000001"), 35000)
	f.beer = add(apispec.ProductFields{Name: "Амурское", CategoryID: category("Пиво и сидр"), KindCode: ptr("500"), Abv: ptr(4.5)},
		catalog.VariantSpec{VolumeMl: ptr(int32(500))}, ean("460000000002"), 6000)
	// У снеков закупочной цены нет: на них проверяется запрет проводить ввод без оценки.
	f.snack = add(apispec.ProductFields{Name: "Сухарики", CategoryID: category("Снеки")},
		catalog.VariantSpec{NetWeightG: ptr(int32(70))}, ean("460000000003"), 0)
	return f
}

func (f *fixture) newStore(t *testing.T, code string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := f.db.App.QueryRow(t.Context(), `insert into locations (code, name, kind, city, timezone, subject_code)
		values ($1, 'Магазин', 'store', 'Благовещенск', 'Asia/Yakutsk', '28') returning id`, code).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) session(t *testing.T, role string, locations ...uuid.UUID) context.Context {
	t.Helper()
	var id uuid.UUID
	err := f.db.App.QueryRow(t.Context(), `insert into users (login, full_name, role, password_hash)
		values ($1, 'Сотрудник', $2, 'x') returning id`, role+"-"+uuid.NewString()[:8], role).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return auth.WithSession(t.Context(), auth.Session{UserID: id, Role: role, LocationIDs: locations})
}

func (f *fixture) scan(t *testing.T, ctx context.Context, doc uuid.UUID, raw string, hint *uuid.UUID) documents.ScanResult {
	t.Helper()
	res, err := f.svc.AddScan(ctx, doc, documents.NewScan{
		ID: uuid.New(), Raw: raw, Source: "manual", ScannedAt: time.Now(), VariantID: hint,
	})
	if err != nil {
		t.Fatalf("scan %s: %v", raw, err)
	}
	return res
}

func (f *fixture) onHand(t *testing.T, variant, zone uuid.UUID) int32 {
	t.Helper()
	var n int32
	err := f.db.App.QueryRow(t.Context(),
		`select coalesce((select on_hand from stock_balances where variant_id = $1 and zone_id = $2), 0)`, variant, zone).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) version(t *testing.T, doc uuid.UUID) int32 {
	t.Helper()
	d, err := f.svc.Get(f.manager, doc)
	if err != nil {
		t.Fatal(err)
	}
	return d.Doc.Version
}

func (f *fixture) integrity(t *testing.T) {
	t.Helper()
	problems, err := f.stock.Integrity(t.Context())
	if err != nil || len(problems) > 0 {
		t.Fatalf("integrity: %+v, %v", problems, err)
	}
}

// opening вводит начальный остаток склада магазина: две бутылки водки по маркам и пиво.
func (f *fixture) opening(t *testing.T, marksCodes []string, beer int32) uuid.UUID {
	t.Helper()
	doc, err := f.svc.CreateOpening(f.keeper, f.backroom, "")
	if err != nil {
		t.Fatal(err)
	}
	f.scan(t, f.keeper, doc, f.vodka.ean, nil)
	for _, m := range marksCodes {
		if res := f.scan(t, f.keeper, doc, m, &f.vodka.variant); res.Scan.Status != documents.ScanAccepted {
			t.Fatalf("mark %s: %+v", m, res.Scan)
		}
	}
	if beer > 0 {
		if err := f.svc.SetLine(f.keeper, doc, f.beer.variant, beer); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.svc.Post(f.manager, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}
	return doc
}
