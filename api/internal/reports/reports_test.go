package reports_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/notify"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/reports"
)

func ptr[T any](v T) *T { return &v }

func TestReports(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var store, other, backroom uuid.UUID
	for code, id := range map[string]*uuid.UUID{"МГ-01": &store, "МГ-03": &other} {
		must(db.App.QueryRow(ctx, `insert into locations (code, name, kind, city, timezone, subject_code)
			values ($1, 'Магазин', 'store', 'Благовещенск', 'Asia/Yakutsk', '28') returning id`, code).Scan(id))
	}
	must(db.App.QueryRow(ctx, `select id from zones where location_id = $1 and kind = 'backroom'`, store).Scan(&backroom))
	session := func(role string, locations ...uuid.UUID) context.Context {
		var id uuid.UUID
		must(db.App.QueryRow(ctx, `insert into users (login, full_name, role, password_hash) values ($1, 'Сотрудник', $2, 'x') returning id`,
			role+uuid.NewString()[:6], role).Scan(&id))
		for _, l := range locations {
			_, err := db.App.Exec(ctx, `insert into user_locations (user_id, location_id) values ($1, $2)`, id, l)
			must(err)
		}
		return auth.WithSession(ctx, auth.Session{UserID: id, Role: role, LocationIDs: locations})
	}
	manager, keeper, foreign := session("manager"), session("storekeeper", store), session("store_manager", other)

	cat := catalog.NewService(db.App)
	producer, err := cat.SaveProducer(ctx, nil, apispec.ProducerFields{Name: "ООО «Амур»", CountryCode: "643"}, true)
	must(err)
	var beerCat uuid.UUID
	must(db.App.QueryRow(ctx, `select id from categories where name = 'Пиво и сидр'`).Scan(&beerCat))
	p, err := cat.CreateProduct(ctx, apispec.ProductFields{Name: "Амурское", CategoryID: beerCat, KindCode: ptr("500"), Abv: ptr(4.5),
		ProducerID: producer.ID, CountryCode: "643"})
	must(err)
	v, err := cat.CreateVariant(ctx, p.ID, catalog.VariantSpec{VolumeMl: ptr(int32(500))})
	must(err)
	_, err = cat.SetPrice(ctx, v.ID, apispec.PriceKindPurchase, 6000, nil, nil)
	must(err)
	_, err = cat.SetPrice(ctx, v.ID, apispec.PriceKindRetail, 9900, nil, nil)
	must(err)

	docs := documents.NewService(db.App, cat, marks.NewService(db.App))
	doc, err := docs.CreateOpening(keeper, backroom, "")
	must(err)
	must(docs.SetLine(keeper, doc, v.ID, 10))
	d, err := docs.Get(manager, doc)
	must(err)
	must(docs.Post(manager, doc, d.Doc.Version))
	_, err = db.App.Exec(ctx, `insert into assortment_matrix (location_id, variant_id, min_qty) values ($1, $2, 20)`, store, v.ID)
	must(err)

	svc, err := reports.NewService(db.App)
	must(err)
	// Каждый запрос каталога выполняется — синтаксис и связи проверены на настоящей схеме.
	for _, def := range svc.Catalog(manager) {
		if _, _, err := svc.Run(manager, def.Code, reports.Params{}); err != nil {
			t.Fatalf("%s: %v", def.Code, err)
		}
	}
	_, stock, err := svc.Run(manager, "stock_on_date", reports.Params{})
	must(err)
	if len(stock.Rows) != 1 || stock.Rows[0][4] != int64(10) || stock.Rows[0][5] != 5.0 || stock.Rows[0][6] != 600.0 || stock.Rows[0][7] != 990.0 {
		t.Fatalf("stock on date: %v", stock.Rows)
	}
	_, turnover, err := svc.Run(manager, "turnover", reports.Params{From: reports.Today().AddDate(0, 0, -1), To: reports.Today()})
	must(err)
	if len(turnover.Rows) != 1 || turnover.Rows[0][3] != int64(0) || turnover.Rows[0][4] != int64(10) || turnover.Rows[0][6] != int64(10) {
		t.Fatalf("turnover: %v", turnover.Rows)
	}
	_, refill, err := svc.Run(keeper, "replenishment", reports.Params{})
	must(err)
	if len(refill.Rows) != 1 || refill.Rows[0][5] != int64(10) {
		t.Fatalf("replenishment: %v", refill.Rows)
	}
	// Управляющий другой точки не видит чужую точку ни по умолчанию, ни явным выбором.
	if _, rows, err := svc.Run(foreign, "replenishment", reports.Params{}); err != nil || len(rows.Rows) != 0 {
		t.Fatalf("foreign store manager: %v %v", rows.Rows, err)
	}
	if _, _, err := svc.Run(foreign, "replenishment", reports.Params{Locations: []uuid.UUID{store}}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("foreign location: %v", err)
	}
	if _, _, err := svc.Run(keeper, "abc", reports.Params{}); !errors.Is(err, reports.ErrNotFound) {
		t.Fatalf("abc for storekeeper: %v", err)
	}
	for _, kind := range []string{"pdf", "xlsx"} {
		data, _, err := svc.Render(manager, "stock_on_date", reports.Params{}, kind)
		if err != nil || len(data) < 1000 {
			t.Fatalf("render %s: %d %v", kind, len(data), err)
		}
		if kind == "pdf" && !bytes.HasPrefix(data, []byte("%PDF")) {
			t.Fatal("not a pdf")
		}
	}

	// Тяжёлый отчёт в фоне: файл готов, пользователю пришло уведомление.
	f, err := svc.Enqueue(manager, "turnover", reports.Params{From: reports.Today().AddDate(0, -3, 0), To: reports.Today()}, "xlsx")
	must(err)
	must(svc.Generate(ctx, f.ID))
	files, err := svc.Files(manager)
	must(err)
	if len(files) != 1 || files[0].Status != "done" {
		t.Fatalf("files: %+v", files)
	}
	if data, kind, err := svc.Download(manager, f.ID); err != nil || kind != "xlsx" || len(data) == 0 {
		t.Fatalf("download: %v", err)
	}
	if _, _, err := svc.Download(keeper, f.ID); !errors.Is(err, reports.ErrNotFound) {
		t.Fatalf("someone else's file: %v", err)
	}
	notes := notify.NewService(db.App)
	items, unread, err := notes.List(manager, false)
	must(err)
	if unread != 1 || items[0].Kind != "report_ready" {
		t.Fatalf("notifications: %d %+v", unread, items)
	}

	dash, err := svc.Dashboard(manager, reports.Today().AddDate(0, 0, -7), reports.Today(), nil)
	must(err)
	if dash.StockCost != 600 || len(dash.RevenueByDay) != 8 || len(dash.StockByLocation) != 1 {
		t.Fatalf("dashboard: %+v", dash)
	}
	if _, err := svc.Dashboard(keeper, time.Now(), time.Now(), nil); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("dashboard for storekeeper: %v", err)
	}

	// Ниже минимума — уведомление кладовщику точки, один раз; управляющему другой точки — нет.
	n, err := notes.Scan(ctx)
	must(err)
	if n == 0 {
		t.Fatal("no notifications created")
	}
	if again, _ := notes.Scan(ctx); again != 0 {
		t.Fatalf("second scan created %d", again)
	}
	items, _, _ = notes.List(keeper, true)
	if len(items) != 1 || items[0].Kind != "below_min" {
		t.Fatalf("keeper notifications: %+v", items)
	}
	if items, _, _ = notes.List(foreign, true); len(items) != 0 {
		t.Fatalf("foreign notifications: %+v", items)
	}
	must(notes.Read(keeper, nil))
	if _, unread, _ := notes.List(keeper, false); unread != 0 {
		t.Fatalf("unread after read all: %d", unread)
	}
}
