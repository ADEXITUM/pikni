package catalog_test

import (
	"errors"
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func TestNetWeight(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()

	chips, err := f.svc.CreateProduct(ctx, apispec.ProductFields{
		Name: "Чипсы", CategoryID: f.categories["Снеки"].ID, ProducerID: f.other.ID, CountryCode: "643",
		Attributes: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	// У снеков фасовка — масса: 70 и 150 г — разные варианты одной позиции.
	for _, g := range []int32{70, 150} {
		if _, err := f.svc.CreateVariant(ctx, chips.ID, catalog.VariantSpec{NetWeightG: ptr(g)}); err != nil {
			t.Fatalf("%d g: %v", g, err)
		}
	}
	if _, err := f.svc.CreateVariant(ctx, chips.ID, catalog.VariantSpec{VolumeMl: ptr(int32(500)), NetWeightG: ptr(int32(70))}); !errors.Is(err, catalog.ErrVolumeAndWeight) {
		t.Errorf("volume and weight: error = %v", err)
	}

	vodka, err := f.svc.CreateProduct(ctx, f.vodkaFields("Тайга"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateVariant(ctx, vodka.ID, catalog.VariantSpec{NetWeightG: ptr(int32(500))}); !errors.Is(err, catalog.ErrWeightForEgais) {
		t.Errorf("weight for EGAIS product: error = %v", err)
	}

	var labels []string
	rows, err := db.App.Query(ctx, `select data->>'variant' from audit_log where action = 'variant.created' order by id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatal(err)
		}
		labels = append(labels, l)
	}
	if len(labels) != 2 || labels[0] != "70 г" || labels[1] != "150 г" {
		t.Errorf("labels = %v", labels)
	}
}

func TestAlcoholCategories(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()

	// Вид продукции и крепость — только в алкогольных категориях.
	snack := f.vodkaFields("Арахис")
	snack.CategoryID, snack.Attributes = f.categories["Снеки"].ID, map[string]any{}
	if _, err := f.svc.CreateProduct(ctx, snack); !errors.Is(err, catalog.ErrNotAlcoholic) {
		t.Errorf("kind in snacks: error = %v", err)
	}

	// Подкатегория наследует признак, а у неё самой его не поменять.
	tobacco := f.categories["Табачная продукция"]
	child, err := f.svc.CreateCategory(ctx, apispec.NewCategory{Name: "Сигариллы", ParentID: &tobacco.ID, Alcohol: ptr(true)})
	if err != nil || child.Alcohol {
		t.Fatalf("child = %+v, %v", child, err)
	}
	_, err = f.svc.UpdateCategory(ctx, child.ID, apispec.CategoryUpdate{Name: "Сигариллы", IsActive: true, Alcohol: ptr(true)})
	if !errors.Is(err, catalog.ErrAlcoholInherited) {
		t.Errorf("flag on a subcategory: error = %v", err)
	}

	// Снять признак с ветки, где есть водка, нельзя.
	if _, err := f.svc.CreateProduct(ctx, f.vodkaFields("Тайга")); err != nil {
		t.Fatal(err)
	}
	strong := f.categories["Крепкий алкоголь"]
	_, err = f.svc.UpdateCategory(ctx, strong.ID, apispec.CategoryUpdate{Name: strong.Name, Sort: strong.Sort, IsActive: true, Alcohol: ptr(false)})
	if !errors.Is(err, catalog.ErrAlcoholProducts) {
		t.Errorf("flag off with vodka inside: error = %v", err)
	}

	// Признак верхнего уровня переходит на всю ветку.
	if _, err := f.svc.UpdateCategory(ctx, tobacco.ID, apispec.CategoryUpdate{Name: tobacco.Name, Sort: tobacco.Sort, IsActive: true, Alcohol: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if !categoriesByName(t, f.svc)["Сигариллы"].Alcohol {
		t.Error("subcategory did not inherit the flag")
	}
}
