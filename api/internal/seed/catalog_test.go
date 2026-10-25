package seed_test

import (
	"bytes"
	"os"
	"strconv"
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/seed"
)

func TestCatalogIsStable(t *testing.T) {
	items, err := seed.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 2000 {
		t.Fatalf("%d variants, a large store carries 2000+", len(items))
	}
	var buf bytes.Buffer
	if err := seed.WriteCSV(&buf, items); err != nil {
		t.Fatal(err)
	}
	// Файл в репозитории — результат генератора: правка описания без перегенерации видна сразу.
	committed, err := os.ReadFile("../../db/seed/catalog-full.csv")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), committed) {
		t.Fatal("db/seed/catalog-full.csv is stale: run pikni-admin gen-catalog -file db/seed/catalog-full.csv")
	}
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.Barcode] || !validEAN(it.Barcode) {
			t.Fatalf("barcode %s of %s: duplicate or bad check digit", it.Barcode, it.Name)
		}
		seen[it.Barcode] = true
		if it.Price <= it.Purchase {
			t.Fatalf("%s: price %d is not above purchase %d", it.Name, it.Price, it.Purchase)
		}
	}
}

func validEAN(code string) bool {
	if len(code) != 13 {
		return false
	}
	sum := 0
	for i := range 12 {
		d, _ := strconv.Atoi(code[i : i+1])
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return strconv.Itoa((10-sum%10)%10) == code[12:]
}

func TestCatalogImports(t *testing.T) {
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
	report, err := catalog.NewService(db.App).Import(ctx, "catalog-full.csv", data, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range report.Errors {
		if i < 20 {
			t.Errorf("row %d: %s", e.Row, e.Message)
		}
	}
	if !report.Committed {
		t.Fatalf("not committed: %d errors", len(report.Errors))
	}
	t.Logf("imported %d products, %d variants", report.Products, report.Variants)
}
