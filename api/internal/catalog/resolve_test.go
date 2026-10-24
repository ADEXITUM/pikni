package catalog_test

import (
	"strings"
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func TestResolveCode(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()

	vodka, err := f.svc.CreateProduct(ctx, f.vodkaFields("Водка «Тайга»"))
	if err != nil {
		t.Fatal(err)
	}
	glass := "glass"
	half, err := f.svc.CreateVariant(ctx, vodka.ID, catalog.VariantSpec{
		VolumeMl: ptr(int32(500)), Container: &glass, Alcocode: ptr("0177207000002463655"), GTIN: ptr("04602124002104"),
	})
	if err != nil {
		t.Fatal(err)
	}
	bottle, err := f.svc.CreateVariant(ctx, vodka.ID, catalog.VariantSpec{VolumeMl: ptr(int32(700)), Container: &glass})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddBarcode(ctx, bottle.ID, ptr("PK-12")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, raw string
		variant   *catalog.Variant
		kind      apispec.ScanKind
	}{
		{"скан старой марки", "20N00001CGUMZYCB99J1NKN31105001000056NQQMS5VP4HTF5SB46ZSQQJD8BNJP891", &half, apispec.ScanKindEgaisMark},
		{"код «Честного знака»", "0104602124002104215i.7D(U\x1d9333kC", &half, apispec.ScanKindCrptCode},
		{"EAN-13 вместо GTIN", "4602124002104", &half, apispec.ScanKindBarcode},
		{"свой штрихкод Code 128", "PK-12", &bottle, apispec.ScanKindBarcode},
		{"марка нового образца", "10110026167968" + "0118001" + strings.Repeat("A", 129), nil, apispec.ScanKindEgaisMarkNew},
		{"незнакомый код", "4006381333931", nil, apispec.ScanKindBarcode},
		{"артикул без ведущих нулей", "чк-" + strings.TrimLeft(strings.TrimPrefix(bottle.Article, "ЧК-"), "0"), &bottle, apispec.ScanKindArticle},
	}
	for _, tt := range tests {
		r, err := f.svc.Resolve(ctx, tt.raw)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		kind := apispec.ScanKind("")
		if r.Scan != nil {
			kind = r.Scan.Kind
		}
		switch {
		case kind != tt.kind:
			t.Errorf("%s: kind %q, want %q", tt.name, kind, tt.kind)
		case tt.variant == nil && r.Variant != nil:
			t.Errorf("%s: found %s, want nothing", tt.name, r.Variant.ID)
		case tt.variant != nil && (r.Variant == nil || r.Variant.ID != tt.variant.ID || r.Product.ID != vodka.ID):
			t.Errorf("%s: resolution = %+v", tt.name, r)
		}
	}
}
