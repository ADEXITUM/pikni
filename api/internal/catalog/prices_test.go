package catalog_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func TestPrices(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()

	// Свои правила вместо начальных данных: тест не зависит от того, какие МРЦ сейчас действуют.
	if _, err := db.App.Exec(ctx, `delete from min_retail_prices`); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.CreateMinPriceRule(ctx, catalog.MinPriceRuleSpec{
		KindCode: "200", ValidFrom: time.Now().AddDate(-1, 0, 0), Kopecks: 34900, VolumeMl: 500,
		Abv: ptr(40.0), AbvOver: ptr(28.0), Source: "приказ для теста",
	})
	if err != nil {
		t.Fatal(err)
	}

	vodka, err := f.svc.CreateProduct(ctx, f.vodkaFields("Тайга"))
	if err != nil {
		t.Fatal(err)
	}
	variants, err := f.svc.GenerateVariants(ctx, vodka.ID, []int32{700}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	bottle := variants[0].ID

	var below *catalog.BelowMinPriceError
	if _, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindRetail, 45000, nil, nil); !errors.As(err, &below) || below.Min != 48860 {
		t.Errorf("retail below the minimum for 0,7 l: error = %v", err)
	}
	if _, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindRetail, 0, nil, nil); !errors.Is(err, catalog.ErrInvalidPrice) {
		t.Errorf("zero price: error = %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if _, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindRetail, 49990, &past, nil); !errors.Is(err, catalog.ErrPriceInPast) {
		t.Errorf("price in the past: error = %v", err)
	}

	if _, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindRetail, 49990, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindPurchase, 31000, nil, nil); err != nil {
		t.Fatal(err)
	}
	tomorrow := time.Now().Add(24 * time.Hour)
	planned, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindRetail, 52990, &tomorrow, nil)
	if err != nil {
		t.Fatal(err)
	}

	list, err := f.svc.Variants(ctx, vodka.ID)
	if err != nil {
		t.Fatal(err)
	}
	v := list[0]
	if *v.RetailPrice != 49990 || *v.PurchasePrice != 31000 || *v.MinPrice != 48860 {
		t.Errorf("current prices: retail %d, purchase %d, min %d", *v.RetailPrice, *v.PurchasePrice, *v.MinPrice)
	}

	history, err := f.svc.Prices(ctx, bottle, apispec.PriceKindRetail)
	if err != nil || len(history) != 2 || history[0].Kopecks != 52990 {
		t.Errorf("retail history = %+v, %v", history, err)
	}
	if err := f.svc.DeletePrice(ctx, history[1].ID); !errors.Is(err, catalog.ErrPriceInEffect) {
		t.Errorf("delete current price: error = %v", err)
	}
	if err := f.svc.DeletePrice(ctx, planned.ID); err != nil {
		t.Fatalf("delete planned price: %v", err)
	}

	rules, err := f.svc.MinPriceRules(ctx)
	if err != nil || len(rules) != 1 {
		t.Fatalf("rules = %+v, %v", rules, err)
	}
	if err := f.svc.DeleteMinPriceRule(ctx, rules[0].ID); !errors.Is(err, catalog.ErrMinPriceInEffect) {
		t.Errorf("delete rule in effect: error = %v", err)
	}
	next, err := f.svc.CreateMinPriceRule(ctx, catalog.MinPriceRuleSpec{
		KindCode: "200", ValidFrom: time.Now().AddDate(0, 1, 0), Kopecks: 39900, VolumeMl: 500, Source: "следующий приказ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteMinPriceRule(ctx, next.ID); err != nil {
		t.Errorf("delete planned rule: %v", err)
	}
	if _, err := f.svc.CreateMinPriceRule(ctx, catalog.MinPriceRuleSpec{KindCode: "999", ValidFrom: time.Now(), Kopecks: 100, VolumeMl: 500, Source: "x"}); !errors.Is(err, catalog.ErrUnknownReference) {
		t.Errorf("unknown kind: error = %v", err)
	}
}
