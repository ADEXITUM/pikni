package demo_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/demo"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/reports"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

func TestYear(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()
	// Тот же путь, что на стенде: точки, регионы и поставщики из dev.sql, затем каталог.
	seed, err := os.ReadFile("../../db/seed/dev.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Owner.Exec(ctx, string(seed)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../db/seed/catalog-full.csv")
	if err != nil {
		t.Fatal(err)
	}
	if report, err := catalog.NewService(db.App).Import(ctx, "catalog-full.csv", data, true, nil, nil); err != nil || len(report.Errors) > 0 {
		t.Fatalf("import: %v %+v", err, report.Errors)
	}
	var rests bytes.Buffer
	if err := demo.Generate(ctx, db.App, demo.Options{Days: 5, Small: []string{"МГ-02"}, Rests: &rests}, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	var receipts, sold, transfers int
	if err := db.App.QueryRow(ctx, `select (select count(*) from pos_receipts), (select count(*) from excise_marks where status = 'sold'),
		(select count(*) from documents where kind = 'transfer' and status = 'posted')`).
		Scan(&receipts, &sold, &transfers); err != nil || receipts < 9000 || sold == 0 || transfers == 0 {
		t.Fatalf("receipts %d, sold marks %d, transfers %d, %v", receipts, sold, transfers, err)
	}
	// Магазин у дома держит несколько сотен позиций, крупный — почти весь каталог.
	var small, large int
	if err := db.App.QueryRow(ctx, `select
		(select count(*) from assortment_matrix m join locations l on l.id = m.location_id where l.code = 'МГ-02'),
		(select count(*) from assortment_matrix m join locations l on l.id = m.location_id where l.code = 'МГ-01')`).Scan(&small, &large); err != nil || small > 700 || large < 1500 {
		t.Fatalf("matrix: small %d, large %d, %v", small, large, err)
	}
	// Имитатору ЕГАИС уходят ровно те марки, что числятся на остатке.
	var file demo.Rests
	if err := json.Unmarshal(rests.Bytes(), &file); err != nil {
		t.Fatal(err)
	}
	var inFile, onHand int
	for _, p := range file.Points {
		inFile += len(p.Marks)
	}
	if err := db.App.QueryRow(ctx, `select count(*) from excise_marks where status in ('in_stock', 'reserved')`).Scan(&onHand); err != nil || inFile != onHand || onHand == 0 {
		t.Fatalf("rests file has %d marks, stock has %d, %v", inFile, onHand, err)
	}
	problems, err := stock.NewService(db.App, marks.NewService(db.App)).Integrity(ctx)
	if err != nil || len(problems) > 0 {
		t.Fatalf("integrity: %+v %v", problems, err)
	}
	// П-30: отчёт о продажах алкоголя совпадает с чеками; оборотная ведомость сходится.
	var boss uuid.UUID
	if err := db.App.QueryRow(ctx, `insert into users (login, full_name, role, password_hash) values ('boss', 'Б', 'manager', 'x') returning id`).Scan(&boss); err != nil {
		t.Fatal(err)
	}
	manager := auth.WithSession(ctx, auth.Session{UserID: boss, Role: "manager"})
	rep, err := reports.NewService(db.App)
	if err != nil {
		t.Fatal(err)
	}
	period := reports.Params{From: reports.Today().AddDate(0, 0, -40), To: reports.Today()}
	_, sales, err := rep.Run(manager, "alcohol_sales", period)
	if err != nil {
		t.Fatal(err)
	}
	var inReport, inReceipts int64
	for _, r := range sales.Rows {
		inReport += r[5].(int64)
	}
	if err := db.App.QueryRow(ctx, `select sum(l.qty) from pos_receipt_lines l join variants v on v.id = l.variant_id
		join products p on p.id = v.product_id where p.kind_code is not null`).Scan(&inReceipts); err != nil || inReport != inReceipts {
		t.Fatalf("alcohol sales report %d, receipts %d, %v", inReport, inReceipts, err)
	}
	_, turnover, err := rep.Run(manager, "turnover", period)
	if err != nil || len(turnover.Rows) == 0 {
		t.Fatalf("turnover: %v", err)
	}
	for _, r := range turnover.Rows {
		if r[3].(int64)+r[4].(int64)-r[5].(int64) != r[6].(int64) {
			t.Fatalf("turnover does not add up: %v", r)
		}
	}

	if err := demo.Year(ctx, db.App, 21, slog.New(slog.DiscardHandler)); !errors.Is(err, demo.ErrAlreadySeeded) {
		t.Fatalf("second run: %v", err)
	}
}
