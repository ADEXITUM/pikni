package catalog_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

// catalogFixture — справочники для позиций: два производителя, бренд с линейкой и регион.
type catalogFixture struct {
	svc                *catalog.Service
	categories         map[string]store.Category
	taiga, other       store.Producer
	brand, otherBrand  store.Brand
	line               store.ProductLine
	cognacRegion       store.OriginRegion
	vodka, wine, still uuid.UUID
}

func newCatalogFixture(t *testing.T, svc *catalog.Service) catalogFixture {
	t.Helper()
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	f := catalogFixture{svc: svc, categories: categoriesByName(t, svc)}
	var err error
	f.taiga, err = svc.SaveProducer(ctx, nil, apispec.ProducerFields{Name: "ООО «Тайга»", CountryCode: "643"}, true)
	must(err)
	f.other, err = svc.SaveProducer(ctx, nil, apispec.ProducerFields{Name: "ООО «Другой»", CountryCode: "643"}, true)
	must(err)
	f.brand, err = svc.SaveBrand(ctx, nil, apispec.BrandFields{ProducerID: f.taiga.ID, Name: "Тайга"}, true)
	must(err)
	f.otherBrand, err = svc.SaveBrand(ctx, nil, apispec.BrandFields{ProducerID: f.other.ID, Name: "Другая"}, true)
	must(err)
	f.line, err = svc.SaveProductLine(ctx, nil, f.brand.ID, "Тайга", true)
	must(err)
	f.cognacRegion, err = svc.SaveOriginRegion(ctx, nil, apispec.OriginRegionFields{Name: "Коньяк", CountryCode: "250"}, true)
	must(err)
	f.vodka, f.wine, f.still = f.categories["Водка"].ID, f.categories["Вино"].ID, f.categories["Тихое"].ID
	return f
}

func (f catalogFixture) vodkaFields(name string) apispec.ProductFields {
	return apispec.ProductFields{
		Name: name, CategoryID: f.vodka, KindCode: ptr("200"), Abv: ptr(40.0), ProducerID: f.taiga.ID,
		BrandID: &f.brand.ID, ProductLineID: &f.line.ID, Variety: ptr("на пшенице"), CountryCode: "643",
		Attributes: map[string]any{"raw_material": "пшеница"},
	}
}

func TestCreateProduct(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()

	vodka, err := f.svc.CreateProduct(ctx, f.vodkaFields(" Тайга на пшенице "))
	if err != nil {
		t.Fatal(err)
	}
	if vodka.Article != "ЧК-000001" || vodka.Name != "Тайга на пшенице" || vodka.EgaisAccounting == nil ||
		*vodka.EgaisAccounting != "marked" || vodka.CategoryName != "Водка" || *vodka.LineName != "Тайга" {
		t.Errorf("vodka = %+v", vodka)
	}

	chips, err := f.svc.CreateProduct(ctx, apispec.ProductFields{
		Name: "Чипсы", CategoryID: f.categories["Снеки"].ID, ProducerID: f.other.ID, CountryCode: "643",
		Attributes: map[string]any{"flavor": " сыр ", "": nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	if chips.Article != "ЧК-000002" || chips.EgaisAccounting != nil || string(chips.Attributes) != `{"flavor": "сыр"}` {
		t.Errorf("chips = %+v, attributes %s", chips, chips.Attributes)
	}

	// Характеристика «Цвет» унаследована «Тихим» от «Вина».
	_, err = f.svc.CreateProduct(ctx, apispec.ProductFields{
		Name: "Каберне", CategoryID: f.still, KindCode: ptr("401"), Abv: ptr(12.5), ProducerID: f.other.ID,
		CountryCode: "643", Attributes: map[string]any{"color": "красное", "vintage": 2021.0},
	})
	if err != nil {
		t.Errorf("inherited attribute: %v", err)
	}
}

func TestProductValidation(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()

	archived, err := f.svc.CreateCategory(ctx, apispec.NewCategory{ParentID: &f.vodka, Name: "Снятая"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateCategory(ctx, archived.ID, apispec.CategoryUpdate{Name: "Снятая", IsActive: false}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		change func(*apispec.ProductFields)
		want   error
	}{
		{"алкоголь без крепости", func(p *apispec.ProductFields) { p.Abv = nil }, catalog.ErrAbvRequired},
		{"крепость больше 100", func(p *apispec.ProductFields) { p.Abv = ptr(120.0) }, catalog.ErrInvalidAbv},
		{"бренд другого производителя", func(p *apispec.ProductFields) { p.BrandID, p.ProductLineID = &f.otherBrand.ID, nil }, catalog.ErrBrandMismatch},
		{"линейка чужого бренда", func(p *apispec.ProductFields) {
			p.ProducerID, p.BrandID = f.other.ID, &f.otherBrand.ID
		}, catalog.ErrLineMismatch},
		{"линейка без бренда", func(p *apispec.ProductFields) { p.BrandID = nil }, catalog.ErrLineNeedsBrand},
		{"регион другой страны", func(p *apispec.ProductFields) { p.OriginRegionID = &f.cognacRegion.ID }, catalog.ErrRegionMismatch},
		{"нет такого вида", func(p *apispec.ProductFields) { p.KindCode = ptr("999") }, catalog.ErrUnknownReference},
		{"категория в архиве", func(p *apispec.ProductFields) { p.CategoryID = archived.ID }, catalog.ErrInactiveCategory},
		{"чужая характеристика", func(p *apispec.ProductFields) { p.Attributes = map[string]any{"color": "белое"} }, catalog.ErrUnknownAttribute},
		{"число вместо списка", func(p *apispec.ProductFields) { p.Attributes = map[string]any{"raw_material": 1.0} }, catalog.ErrWrongValueType},
		{"значения нет в списке", func(p *apispec.ProductFields) { p.Attributes = map[string]any{"raw_material": "кукуруза"} }, catalog.ErrUnknownOption},
	}
	for _, tt := range tests {
		p := f.vodkaFields("Тайга")
		tt.change(&p)
		if _, err := f.svc.CreateProduct(ctx, p); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}

	_, err = f.svc.CreateProduct(ctx, func() apispec.ProductFields {
		p := f.vodkaFields("Тайга")
		p.Attributes = map[string]any{"raw_material": "кукуруза"}
		return p
	}())
	if ae, ok := errors.AsType[*catalog.AttributeError](err); !ok || ae.Attribute != "Сырьё" {
		t.Errorf("attribute error names the attribute: %v", err)
	}
}

func TestUpdateProduct(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()

	created, err := f.svc.CreateProduct(ctx, f.vodkaFields("Тайга"))
	if err != nil {
		t.Fatal(err)
	}

	// Значение «пшеница» отправили в архив: у позиции оно остаётся, новой его не назначить.
	var rawMaterial store.CategoryAttribute
	tree, _ := f.svc.Categories(ctx)
	for _, a := range tree.Attributes {
		if a.Code == "raw_material" {
			rawMaterial = a
		}
	}
	_, err = f.svc.UpdateAttribute(ctx, rawMaterial.ID, apispec.CategoryAttributeUpdate{
		Name: "Сырьё", IsActive: true, Filterable: true, UsedForAnalogs: true,
		Options: []apispec.AttributeOption{
			{Value: "пшеница", Archived: true}, {Value: "рожь"}, {Value: "смесь зерновых"}, {Value: "картофель"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	p := f.vodkaFields("Тайга классическая")
	p.Abv = ptr(40.04)
	updated, err := f.svc.UpdateProduct(ctx, created.ID, p, true, created.Version)
	if err != nil {
		t.Fatalf("keep archived option: %v", err)
	}
	if updated.Version != created.Version+1 || *updated.Abv != 40 {
		t.Errorf("updated = version %d, abv %v", updated.Version, *updated.Abv)
	}

	if _, err := f.svc.UpdateProduct(ctx, created.ID, p, true, created.Version); !errors.Is(err, catalog.ErrConflict) {
		t.Errorf("stale version: error = %v", err)
	}
	other, err := f.svc.CreateProduct(ctx, func() apispec.ProductFields {
		p := f.vodkaFields("Тайга ржаная")
		p.Attributes = map[string]any{"raw_material": "рожь"}
		return p
	}())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateProduct(ctx, other.ID, f.vodkaFields("Тайга ржаная"), true, other.Version); !errors.Is(err, catalog.ErrArchivedOption) {
		t.Errorf("new archived option: error = %v", err)
	}

	// В «Виски» нет характеристики «Сырьё», поэтому смену категории с ней сервер не примет.
	moved := f.vodkaFields("Тайга")
	moved.CategoryID = f.categories["Виски"].ID
	if _, err := f.svc.UpdateProduct(ctx, created.ID, moved, true, updated.Version); !errors.Is(err, catalog.ErrUnknownAttribute) {
		t.Errorf("leftover attribute after category change: error = %v", err)
	}
	moved.Attributes = map[string]any{"aging": 3.0}
	if _, err := f.svc.UpdateProduct(ctx, created.ID, moved, false, updated.Version); err != nil {
		t.Fatal(err)
	}

	var changes string
	err = db.App.QueryRow(ctx, `select data->'changes' from audit_log where action = 'product.updated' order by id desc limit 1`).Scan(&changes)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"category": ["Водка", "Виски"]`, `"attributes": ["Сырьё: пшеница", "Выдержка: 3"]`, `"isActive": [true, false]`} {
		if !strings.Contains(changes, want) {
			t.Errorf("changes = %s, want %s", changes, want)
		}
	}
}

func TestListProducts(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()

	for _, n := range []string{"Тайга", "Белуга", "Арктика"} {
		if _, err := f.svc.CreateProduct(ctx, f.vodkaFields(n)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.CreateProduct(ctx, apispec.ProductFields{
		Name: "Чипсы", CategoryID: f.categories["Снеки"].ID, ProducerID: f.other.ID, CountryCode: "643", Attributes: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}

	names := func(filter catalog.ProductFilter) string {
		t.Helper()
		if filter.Limit == 0 {
			filter.Limit = 50
		}
		rows, _, err := f.svc.Products(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		list := make([]string, len(rows))
		for i, r := range rows {
			list[i] = r.Name
		}
		return strings.Join(list, ",")
	}

	strong := f.categories["Крепкий алкоголь"].ID
	if got := names(catalog.ProductFilter{CategoryID: &strong}); got != "Арктика,Белуга,Тайга" {
		t.Errorf("subtree of strong alcohol = %s", got)
	}
	if got := names(catalog.ProductFilter{Search: "тай"}); got != "Тайга" {
		t.Errorf("search by name = %s", got)
	}
	if got := names(catalog.ProductFilter{Search: "000004"}); got != "Чипсы" {
		t.Errorf("search by article = %s", got)
	}
	if got := names(catalog.ProductFilter{BrandID: &f.brand.ID}); got != "Арктика,Белуга,Тайга" {
		t.Errorf("by brand = %s", got)
	}

	first, _, err := f.svc.Products(ctx, catalog.ProductFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(catalog.ProductFilter{After: &first[1].ID}); got != "Тайга,Чипсы" {
		t.Errorf("page after %s = %s", first[1].Name, got)
	}
}
