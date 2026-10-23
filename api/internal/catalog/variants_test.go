package catalog_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func TestGenerateVariants(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()
	vodka, err := f.svc.CreateProduct(ctx, f.vodkaFields("Тайга"))
	if err != nil {
		t.Fatal(err)
	}

	created, err := f.svc.GenerateVariants(ctx, vodka.ID, []int32{500, 700}, []string{"glass"}, false)
	if err != nil || len(created) != 2 {
		t.Fatalf("first generation: %d variants, %v", len(created), err)
	}
	created, err = f.svc.GenerateVariants(ctx, vodka.ID, []int32{500, 1000}, []string{"glass", "pet"}, false)
	if err != nil || len(created) != 3 {
		t.Fatalf("second generation creates only missing 0,5 ПЭТ, 1 стекло, 1 ПЭТ: %d, %v", len(created), err)
	}
	variants, err := f.svc.Variants(ctx, vodka.ID)
	if err != nil || len(variants) != 5 {
		t.Errorf("variants = %d, %v", len(variants), err)
	}

	var audited int
	if err := db.App.QueryRow(ctx, `select count(*) from audit_log where action = 'variant.created'`).Scan(&audited); err != nil || audited != 5 {
		t.Errorf("variant.created entries = %d, %v", audited, err)
	}
}

func TestVariantRules(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()
	vodka, err := f.svc.CreateProduct(ctx, f.vodkaFields("Тайга"))
	if err != nil {
		t.Fatal(err)
	}
	chips, err := f.svc.CreateProduct(ctx, apispec.ProductFields{
		Name: "Чипсы", CategoryID: f.categories["Снеки"].ID, ProducerID: f.other.ID, CountryCode: "643", Attributes: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}

	glass := "glass"
	half, err := f.svc.CreateVariant(ctx, vodka.ID, catalog.VariantSpec{
		VolumeMl: ptr(int32(500)), Container: &glass, Alcocode: ptr("0177207000002463655"), GTIN: ptr("4006381333931"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if *half.Gtin != "04006381333931" {
		t.Errorf("gtin stored as %s", *half.Gtin)
	}
	if _, err := f.svc.CreateVariant(ctx, chips.ID, catalog.VariantSpec{}); err != nil {
		t.Errorf("snack without volume: %v", err)
	}

	invalid := []struct {
		name    string
		product uuid.UUID
		spec    catalog.VariantSpec
		want    error
	}{
		{"алкоголь без объёма", vodka.ID, catalog.VariantSpec{Container: &glass}, catalog.ErrVolumeRequired},
		{"такой уже есть", vodka.ID, catalog.VariantSpec{VolumeMl: ptr(int32(500)), Container: &glass}, catalog.ErrVariantExists},
		{"объёма нет в справочнике", vodka.ID, catalog.VariantSpec{VolumeMl: ptr(int32(333))}, catalog.ErrUnknownVolume},
		{"неизвестная тара", vodka.ID, catalog.VariantSpec{VolumeMl: ptr(int32(700)), Container: ptr("box")}, catalog.ErrInvalidContainer},
		{"алкокод у снеков", chips.ID, catalog.VariantSpec{GiftPack: true, Alcocode: ptr("0177207000002463656")}, catalog.ErrAlcocodeNotEgais},
		{"алкокод не 19 цифр", vodka.ID, catalog.VariantSpec{VolumeMl: ptr(int32(700)), Alcocode: ptr("123")}, catalog.ErrInvalidAlcocode},
		{"алкокод занят", vodka.ID, catalog.VariantSpec{VolumeMl: ptr(int32(700)), Alcocode: ptr("0177207000002463655")}, catalog.ErrAlcocodeTaken},
		{"GTIN занят", vodka.ID, catalog.VariantSpec{VolumeMl: ptr(int32(700)), GTIN: ptr("04006381333931")}, catalog.ErrGTINTaken},
		{"нет позиции", uuid.New(), catalog.VariantSpec{}, catalog.ErrNotFound},
	}
	for _, tt := range invalid {
		if _, err := f.svc.CreateVariant(ctx, tt.product, tt.spec); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}

	updated, err := f.svc.UpdateVariant(ctx, half.ID, nil, ptr("73513537"), false)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Alcocode != nil || *updated.Gtin != "00000073513537" || updated.IsActive {
		t.Errorf("updated = %+v", updated.Variant)
	}
}

func TestBarcodes(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	f := newCatalogFixture(t, catalog.NewService(db.App))
	ctx := t.Context()
	vodka, err := f.svc.CreateProduct(ctx, f.vodkaFields("Тайга"))
	if err != nil {
		t.Fatal(err)
	}
	variants, err := f.svc.GenerateVariants(ctx, vodka.ID, []int32{500, 700}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	half, bottle := variants[0].ID, variants[1].ID

	if b, err := f.svc.AddBarcode(ctx, half, ptr(" 4006381333931 ")); err != nil || b.Kind != "ean13" {
		t.Fatalf("add EAN-13: %+v, %v", b, err)
	}
	if _, err := f.svc.AddBarcode(ctx, half, ptr("4006381333932")); !errors.Is(err, catalog.ErrInvalidBarcode) {
		t.Errorf("wrong check digit: error = %v", err)
	}
	if _, err := f.svc.AddBarcode(ctx, bottle, ptr("4006381333931")); !errors.Is(err, catalog.ErrBarcodeTaken) {
		t.Errorf("barcode of another variant: error = %v", err)
	}

	// Внутренний номер 2 уже ввели вручную: генератор пропускает его и берёт следующий.
	first, err := f.svc.AddBarcode(ctx, bottle, nil)
	if err != nil || first.Code != "2000000000015" || first.Kind != "internal" {
		t.Fatalf("first internal = %+v, %v", first, err)
	}
	if _, err := f.svc.AddBarcode(ctx, half, ptr("2000000000022")); err != nil {
		t.Fatal(err)
	}
	next, err := f.svc.AddBarcode(ctx, bottle, nil)
	if err != nil || next.Code != "2000000000039" {
		t.Errorf("next internal = %+v, %v", next, err)
	}

	list, err := f.svc.Variants(ctx, vodka.ID)
	if err != nil || len(list[0].Barcodes) != 2 || len(list[1].Barcodes) != 2 {
		t.Errorf("barcodes per variant: %+v, %v", list, err)
	}

	if err := f.svc.DeleteBarcode(ctx, "4006381333931"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteBarcode(ctx, "4006381333931"); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("delete twice: error = %v", err)
	}
}
