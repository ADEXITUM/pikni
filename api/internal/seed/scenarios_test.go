package seed_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/demo"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/seed"
)

// Сценарии без имитатора ЕГАИС на коротком демо-наборе: каждый оставляет документ в своём
// состоянии — так их можно продолжить руками.
func TestScenarios(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()
	dev, err := os.ReadFile("../../db/seed/dev.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Owner.Exec(ctx, string(dev)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../db/seed/catalog-full.csv")
	if err != nil {
		t.Fatal(err)
	}
	if report, err := catalog.NewService(db.App).Import(ctx, "catalog-full.csv", data, true, nil, nil); err != nil || !report.Committed {
		t.Fatalf("import: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	if err := demo.Year(ctx, db.App, 2, log); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Users(ctx, db.App); err != nil {
		t.Fatal(err)
	}
	if err := seed.NewEnv(db.App, "", "", log).Run(ctx, "all"); err != nil {
		t.Fatal(err)
	}
	want := []struct{ kind, location, status string }{
		{"stocktake", "МГ-01", "draft"},
		{"stocktake", "МГ-02", "counting"},
		{"stocktake", "МГ-03", "reconciling"},
		{"stocktake", "МГ-04", "pending_approval"},
		{"transfer", "СК-01", "requested"},
		{"transfer", "СК-01", "in_transit"},
		{"transfer", "СК-02", "egais_rejected"},
		{"writeoff", "МГ-04", "pending_approval"},
	}
	for _, w := range want {
		var n int
		if err := db.App.QueryRow(ctx, `select count(*) from documents d join locations l on l.id = d.location_id
			where d.kind = $1 and l.code = $2 and d.status = $3`, w.kind, w.location, w.status).Scan(&n); err != nil || n == 0 {
			t.Errorf("no %s at %s in status %s (%v)", w.kind, w.location, w.status, err)
		}
	}
	var reserved, stuck int
	if err := db.App.QueryRow(ctx, `select (select count(*) from excise_marks where status = 'reserved'),
		(select count(*) from egais_messages where status = 'queued' and created_at < now() - interval '5 minutes')`).Scan(&reserved, &stuck); err != nil || reserved != 2 || stuck != 1 {
		t.Errorf("reserved marks %d, stuck messages %d, %v", reserved, stuck, err)
	}
	// Вторая половина зала МГ-02 не посчитана: досчитать её можно руками.
	var open int
	if err := db.App.QueryRow(ctx, `select count(*) from stocktake_zones z join documents d on d.id = z.document_id
		join locations l on l.id = d.location_id where l.code = 'МГ-02' and z.status = 'counting'`).Scan(&open); err != nil || open != 1 {
		t.Errorf("open zones at МГ-02: %d, %v", open, err)
	}
}
