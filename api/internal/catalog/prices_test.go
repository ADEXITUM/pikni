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

	// МРЦ — из начальных данных: приказ 181н, водка 40 % — 409 ₽ за 0,5 л, 0,7 л — 572,60 ₽.
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
	if _, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindRetail, 55000, nil, nil); !errors.As(err, &below) || below.Min != 57260 {
		t.Errorf("retail below the minimum for 0,7 l: error = %v", err)
	}
	if _, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindRetail, 0, nil, nil); !errors.Is(err, catalog.ErrInvalidPrice) {
		t.Errorf("zero price: error = %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if _, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindRetail, 59990, &past, nil); !errors.Is(err, catalog.ErrPriceInPast) {
		t.Errorf("price in the past: error = %v", err)
	}

	if _, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindRetail, 59990, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindPurchase, 31000, nil, nil); err != nil {
		t.Fatal(err)
	}
	tomorrow := time.Now().Add(24 * time.Hour)
	planned, err := f.svc.SetPrice(ctx, bottle, apispec.PriceKindRetail, 62990, &tomorrow, nil)
	if err != nil {
		t.Fatal(err)
	}

	list, err := f.svc.Variants(ctx, vodka.ID)
	if err != nil {
		t.Fatal(err)
	}
	v := list[0]
	if *v.RetailPrice != 59990 || *v.PurchasePrice != 31000 || *v.MinPrice != 57260 {
		t.Errorf("current prices: retail %d, purchase %d, min %d", *v.RetailPrice, *v.PurchasePrice, *v.MinPrice)
	}

	history, err := f.svc.Prices(ctx, bottle, apispec.PriceKindRetail)
	if err != nil || len(history) != 2 || history[0].Kopecks != 62990 {
		t.Errorf("retail history = %+v, %v", history, err)
	}
	if err := f.svc.DeletePrice(ctx, history[1].ID); !errors.Is(err, catalog.ErrPriceInEffect) {
		t.Errorf("delete current price: error = %v", err)
	}
	if err := f.svc.DeletePrice(ctx, planned.ID); err != nil {
		t.Fatalf("delete planned price: %v", err)
	}

	rules, groups, err := f.svc.MinPriceRules(ctx)
	if err != nil || len(rules) != 111 || len(groups) != 5 {
		t.Fatalf("seeded rules = %d, groups = %d, %v", len(rules), len(groups), err)
	}
	if err := f.svc.DeleteMinPriceRule(ctx, rules[0].ID); !errors.Is(err, catalog.ErrMinPriceInEffect) {
		t.Errorf("delete rule in effect: error = %v", err)
	}
	next, err := f.svc.CreateMinPriceRule(ctx, catalog.MinPriceRuleSpec{
		GroupCode: "vodka", ValidFrom: time.Now().AddDate(0, 1, 0), AbvFrom: 39, AbvTo: ptr(40.0), Kopecks: 44900,
		VolumeMl: 500, SamePriceOverMl: ptr(int32(375)), Source: "следующий приказ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteMinPriceRule(ctx, next.ID); err != nil {
		t.Errorf("delete planned rule: %v", err)
	}
	if _, err := f.svc.CreateMinPriceRule(ctx, catalog.MinPriceRuleSpec{GroupCode: "beer", ValidFrom: time.Now(), Kopecks: 100, VolumeMl: 500, Source: "x"}); !errors.Is(err, catalog.ErrUnknownReference) {
		t.Errorf("unknown group: error = %v", err)
	}
	if _, err := f.svc.CreateMinPriceRule(ctx, catalog.MinPriceRuleSpec{GroupCode: "vodka", ValidFrom: time.Now(), AbvFrom: 40, AbvTo: ptr(39.0), Kopecks: 100, VolumeMl: 500, Source: "x"}); !errors.Is(err, catalog.ErrInvalidMinPrice) {
		t.Errorf("band upside down: error = %v", err)
	}
}
