package catalog_test

import (
	"strings"
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func TestSearchProducts(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()

	wheat, err := f.svc.CreateProduct(ctx, f.vodkaFields("Водка «Тайга» на пшенице"))
	if err != nil {
		t.Fatal(err)
	}
	beluga := f.vodkaFields("Водка «Белуга» 100%")
	beluga.Variety = ptr("классическая")
	if _, err := f.svc.CreateProduct(ctx, beluga); err != nil {
		t.Fatal(err)
	}
	glass := "glass"
	v, err := f.svc.CreateVariant(ctx, wheat.ID, catalog.VariantSpec{
		VolumeMl: ptr(int32(500)), Container: &glass, Alcocode: ptr("0177207000002463655"), GTIN: ptr("04602124002104"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddBarcode(ctx, v.ID, ptr("4006381333931")); err != nil {
		t.Fatal(err)
	}

	search := func(q string) (string, *catalog.Scan) {
		t.Helper()
		rows, scan, err := f.svc.Products(ctx, catalog.ProductFilter{Search: q, Limit: 50})
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		names := make([]string, len(rows))
		for i, r := range rows {
			names[i] = r.Name
		}
		return strings.Join(names, ","), scan
	}

	const taiga = "Водка «Тайга» на пшенице"
	tests := []struct {
		name, q, want string
		kind          apispec.ScanKind
	}{
		{"морфология", "водки пшеница", taiga, ""},
		{"начало слова", "тайг", taiga, ""},
		{"процент не шаблон", "%", "", ""},
		{"часть артикула варианта", "000001", taiga, ""},
		{"штрихкод", "4006381333931", taiga, apispec.ScanKindBarcode},
		{"GTIN как EAN-13", "4602124002104", taiga, apispec.ScanKindBarcode},
		{"алкокод", "0177207000002463655", taiga, apispec.ScanKindAlcocode},
		{"скан старой марки", "20N00001CGUMZYCB99J1NKN31105001000056NQQMS5VP4HTF5SB46ZSQQJD8BNJP891", taiga, apispec.ScanKindEgaisMark},
		{"код «Честного знака»", "0104602124002104215i.7D(U\x1d9333kC", taiga, apispec.ScanKindCrptCode},
		{"марка нового образца", "10110026167968" + "0118001" + strings.Repeat("A", 129), "", apispec.ScanKindEgaisMarkNew},
	}
	for _, tt := range tests {
		got, scan := search(tt.q)
		kind := apispec.ScanKind("")
		if scan != nil {
			kind = scan.Kind
		}
		if got != tt.want || kind != tt.kind {
			t.Errorf("%s: found %q as %q, want %q as %q", tt.name, got, kind, tt.want, tt.kind)
		}
	}
}
