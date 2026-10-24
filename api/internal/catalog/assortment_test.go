package catalog_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func TestAssortment(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()

	store := func(code string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		err := db.App.QueryRow(ctx, `insert into locations (code, name, kind, city, timezone, subject_code)
			values ($1, 'Магазин', 'store', 'Благовещенск', 'Asia/Yakutsk', '28') returning id`, code).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	lenina, oktyabrya := store("МГ-01"), store("МГ-02")

	vodka, err := f.svc.CreateProduct(ctx, f.vodkaFields("Тайга"))
	if err != nil {
		t.Fatal(err)
	}
	variants, err := f.svc.GenerateVariants(ctx, vodka.ID, []int32{500, 700}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	half, bottle := variants[0].ID, variants[1].ID

	if err := f.svc.SetAssortmentItem(ctx, lenina, half, 0); !errors.Is(err, catalog.ErrInvalidMinQty) {
		t.Errorf("zero minimum: error = %v", err)
	}
	if err := f.svc.SetAssortmentItem(ctx, uuid.New(), half, 1); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("unknown location: error = %v", err)
	}
	for _, step := range []struct {
		variant uuid.UUID
		qty     int32
	}{{half, 6}, {bottle, 2}, {half, 10}, {half, 10}} {
		if err := f.svc.SetAssortmentItem(ctx, lenina, step.variant, step.qty); err != nil {
			t.Fatal(err)
		}
	}
	all := catalog.AssortmentFilter{Limit: 50}
	items, _, err := f.svc.Assortment(ctx, lenina, all)
	if err != nil || len(items) != 2 || items[0].MinQty != 10 || items[1].MinQty != 2 {
		t.Fatalf("assortment = %+v, %v", items, err)
	}

	// В МГ-02 уже есть 0,5 л со своим минимумом: копирование его не трогает.
	if err := f.svc.SetAssortmentItem(ctx, oktyabrya, half, 3); err != nil {
		t.Fatal(err)
	}
	added, err := f.svc.CopyAssortment(ctx, oktyabrya, lenina)
	if err != nil || added != 1 {
		t.Fatalf("copy added %d, %v", added, err)
	}
	copied, _, _ := f.svc.Assortment(ctx, oktyabrya, all)
	if len(copied) != 2 || copied[0].MinQty != 3 {
		t.Errorf("after copy = %+v", copied)
	}

	// В магазине тысячи вариантов: матрица идёт страницами, total считает все подходящие.
	page, total, err := f.svc.Assortment(ctx, oktyabrya, catalog.AssortmentFilter{Limit: 1})
	if err != nil || len(page) != 1 || total != 2 || page[0].VariantID != half {
		t.Fatalf("first page = %+v, total %d, %v", page, total, err)
	}
	next, _, _ := f.svc.Assortment(ctx, oktyabrya, catalog.AssortmentFilter{Limit: 1, After: &page[0].VariantID})
	if len(next) != 1 || next[0].VariantID != bottle {
		t.Errorf("second page = %+v", next)
	}
	byArticle, total, _ := f.svc.Assortment(ctx, oktyabrya, catalog.AssortmentFilter{Search: variants[1].Article, Limit: 50})
	if len(byArticle) != 1 || total != 1 || byArticle[0].VariantID != bottle {
		t.Errorf("search by article = %+v, total %d", byArticle, total)
	}
	none, total, _ := f.svc.Assortment(ctx, oktyabrya, catalog.AssortmentFilter{Search: "коньяк", Limit: 50})
	if len(none) != 0 || total != 0 {
		t.Errorf("search by other word = %+v, total %d", none, total)
	}
	if _, err := f.svc.CopyAssortment(ctx, lenina, lenina); !errors.Is(err, catalog.ErrSameLocation) {
		t.Errorf("copy into itself: error = %v", err)
	}

	if err := f.svc.RemoveAssortmentItem(ctx, lenina, bottle); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RemoveAssortmentItem(ctx, lenina, bottle); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("remove twice: error = %v", err)
	}

	// Повтор того же минимума — не изменение: из пяти вызовов в журнал попали четыре.
	var set int
	if err := db.App.QueryRow(ctx, `select count(*) from audit_log where action = 'assortment.set'`).Scan(&set); err != nil || set != 4 {
		t.Errorf("assortment.set entries = %d, %v", set, err)
	}
}
