package catalog_test

import (
	"errors"
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func TestImportUpdatesExisting(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := catalog.NewService(db.App)
	ctx := t.Context()

	first := header +
		"Водка «Север»;Крепкий алкоголь / Водка;200;40;ООО «Север»;Север;643;Сырьё: рожь;0,5;стекло;4006381333931;449\n" +
		"Водка «Север»;Крепкий алкоголь / Водка;200;40;ООО «Север»;Север;643;Сырьё: рожь;0,7;стекло;;650\n"
	if r, err := svc.Import(ctx, "товары.csv", []byte(first), true, nil, nil); err != nil || !r.Committed {
		t.Fatalf("first import: %+v, %v", r, err)
	}

	// Тот же файл ещё раз — ни дублей, ни расхождений.
	again, err := svc.Import(ctx, "товары.csv", []byte(first), false, nil, nil)
	if err != nil || again.Products != 0 || again.Existing != 1 || again.Variants != 0 || len(again.Changes) != 0 || len(again.Errors) != 0 {
		t.Fatalf("same file again: %+v, %v", again, err)
	}

	// В новом прайсе другая цена 0,5 л и крепость, плюс новый литровый вариант.
	next := header +
		"Водка «Север»;Крепкий алкоголь / Водка;200;41;ООО «Север»;Север;643;Сырьё: рожь;0,5;стекло;;479\n" +
		"Водка «Север»;Крепкий алкоголь / Водка;200;41;ООО «Север»;Север;643;Сырьё: рожь;0,7;стекло;;650\n" +
		"Водка «Север»;Крепкий алкоголь / Водка;200;41;ООО «Север»;Север;643;Сырьё: рожь;1;стекло;;900\n"
	preview, err := svc.Import(ctx, "товары.csv", []byte(next), false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	changes := map[string]catalog.ImportChange{}
	for _, c := range preview.Changes {
		changes[c.Key] = c
	}
	abv, price := changes["2:abv"], changes["2:price"]
	if len(changes) != 2 || abv.Old != "40 %" || abv.New != "41 %" || price.Old != "449,00 ₽" || price.New != "479,00 ₽" ||
		price.Variant != "0,5 л, стекло" || preview.Variants != 1 {
		t.Fatalf("preview = %+v", preview)
	}

	// Цену берём из файла, крепость оставляем как в каталоге.
	done, err := svc.Import(ctx, "товары.csv", []byte(next), true, nil, map[string]bool{"2:price": true})
	if err != nil || !done.Committed {
		t.Fatalf("update: %+v, %v", done, err)
	}
	var abvNow float64
	var priceNow, variants int
	err = db.App.QueryRow(ctx, `select p.abv,
	           (select (pr.price * 100)::int from prices pr join variants v on v.id = pr.variant_id
	            where v.product_id = p.id and v.volume_ml = 500 and pr.kind = 'retail' order by pr.valid_from desc limit 1),
	           (select count(*) from variants v where v.product_id = p.id)
	    from products p`).Scan(&abvNow, &priceNow, &variants)
	if err != nil || abvNow != 40 || priceNow != 47900 || variants != 3 {
		t.Errorf("after update: abv %v, price %d, variants %d, %v", abvNow, priceNow, variants, err)
	}
}

func TestProductIsUnique(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()

	if _, err := f.svc.CreateProduct(ctx, f.vodkaFields("Тайга")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateProduct(ctx, f.vodkaFields("ТАЙГА")); !errors.Is(err, catalog.ErrProductExists) {
		t.Errorf("same name, same producer: error = %v", err)
	}
}
