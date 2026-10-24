package catalog_test

import (
	"os"
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

// Демо-данные проходят через те же проверки, что и ручной ввод: если правила изменятся
// (например, вырастет МРЦ), этот тест покажет, что каталог стенда пора поправить.
func TestDemoSeed(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := t.Context()
	exec := func(path string) {
		t.Helper()
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Owner.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}

	exec("../../db/seed/dev.sql")
	data, err := os.ReadFile("../../db/seed/catalog.csv")
	if err != nil {
		t.Fatal(err)
	}
	report, err := catalog.NewService(db.App).Import(ctx, "catalog.csv", data, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Committed || report.Products != 36 || report.Variants != 48 {
		t.Fatalf("demo catalog: %+v", report)
	}
	exec("../../db/seed/matrix.sql")
	exec("../../db/seed/matrix.sql")

	var stores, items int
	err = db.App.QueryRow(ctx, `select count(distinct location_id), count(*) from assortment_matrix`).Scan(&stores, &items)
	if err != nil || stores != 4 || items < 4*30 {
		t.Errorf("matrix: %d stores, %d items, %v", stores, items, err)
	}
}
