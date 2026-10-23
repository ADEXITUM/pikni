package catalog_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func ptr[T any](v T) *T { return &v }

func TestCountriesAreLoaded(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	countries, err := catalog.NewService(db.App).Countries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byCode := map[string]string{}
	for _, c := range countries {
		byCode[c.Code] = c.Name
	}
	if len(countries) != 250 || byCode["643"] != "Российская Федерация" || byCode["250"] != "Франция" {
		t.Errorf("countries: %d, 643=%q, 250=%q", len(countries), byCode["643"], byCode["250"])
	}
}

func TestProducersAndBrands(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := catalog.NewService(db.App)
	ctx := t.Context()

	taiga, err := svc.SaveProducer(ctx, nil, apispec.ProducerFields{Name: " ООО «Тайга» ", INN: ptr("2801000006"), CountryCode: "643"}, true)
	if err != nil {
		t.Fatalf("SaveProducer() error = %v", err)
	}
	if taiga.Name != "ООО «Тайга»" || !taiga.IsActive {
		t.Errorf("producer = %+v", taiga)
	}

	invalid := []struct {
		name string
		f    apispec.ProducerFields
		want error
	}{
		{"короткий ИНН", apispec.ProducerFields{Name: "А", INN: ptr("123"), CountryCode: "643"}, catalog.ErrInvalidINN},
		{"без названия", apispec.ProducerFields{Name: " ", CountryCode: "643"}, catalog.ErrEmptyName},
		{"то же название", apispec.ProducerFields{Name: "ООО «Тайга»", CountryCode: "643"}, catalog.ErrDuplicate},
		{"нет такой страны", apispec.ProducerFields{Name: "Б", CountryCode: "999"}, catalog.ErrUnknownReference},
	}
	for _, tt := range invalid {
		if _, err := svc.SaveProducer(ctx, nil, tt.f, true); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}

	if _, err := svc.SaveBrand(ctx, nil, apispec.BrandFields{ProducerID: uuid.New(), Name: "Тайга"}, true); !errors.Is(err, catalog.ErrUnknownReference) {
		t.Errorf("brand of unknown producer: error = %v", err)
	}
	brand, err := svc.SaveBrand(ctx, nil, apispec.BrandFields{ProducerID: taiga.ID, Name: "Тайга"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveBrand(ctx, &brand.ID, apispec.BrandFields{ProducerID: taiga.ID, Name: "Тайга Премиум"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveBrand(ctx, ptr(uuid.New()), apispec.BrandFields{ProducerID: taiga.ID, Name: "X"}, true); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("updating unknown brand: error = %v", err)
	}

	var changes string
	err = db.App.QueryRow(ctx, `select data->'changes' from audit_log where action = 'ref.updated' and entity_type = 'brand'`).Scan(&changes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(changes, "Тайга Премиум") || !strings.Contains(changes, "isActive") || strings.Contains(changes, "producerId") {
		t.Errorf("brand changes = %s", changes)
	}
}

func TestVolumesAndWriteoffReasons(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := catalog.NewService(db.App)
	ctx := t.Context()

	if _, err := svc.CreateVolume(ctx, 500); !errors.Is(err, catalog.ErrDuplicate) {
		t.Errorf("seeded 500 ml again: error = %v", err)
	}
	if _, err := svc.CreateVolume(ctx, 5); !errors.Is(err, catalog.ErrInvalidVolume) {
		t.Errorf("5 ml: error = %v", err)
	}
	if v, err := svc.SetVolumeActive(ctx, 3000, false); err != nil || v.IsActive {
		t.Errorf("deactivate 3 l: %+v, %v", v, err)
	}
	if _, err := svc.SetVolumeActive(ctx, 3000, false); err != nil {
		t.Fatal(err)
	}

	var volumeChanges int
	if err := db.App.QueryRow(ctx, `select count(*) from audit_log where entity_type = 'volume'`).Scan(&volumeChanges); err != nil || volumeChanges != 1 {
		t.Errorf("volume audit entries = %d (%v), want 1: repeating the same state is not a change", volumeChanges, err)
	}

	if _, err := svc.SaveWriteoffReason(ctx, nil, apispec.WriteoffReasonFields{Name: "Бой", EgaisType: "Разбили"}, true); !errors.Is(err, catalog.ErrInvalidEgaisType) {
		t.Errorf("unknown EGAIS type: error = %v", err)
	}
}

func TestSeededClassifiers(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := catalog.NewService(db.App)

	kinds, err := svc.ProductKinds(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	accounting := map[string]string{}
	for _, k := range kinds {
		accounting[k.Code] = k.EgaisAccounting
	}
	for code, want := range map[string]string{"200": "marked", "2291": "marked", "500": "quantity", "261": "quantity", "4321": "marked"} {
		if accounting[code] != want {
			t.Errorf("product kind %s accounting = %q, want %q", code, accounting[code], want)
		}
	}

	reasons, err := svc.WriteoffReasons(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reasons {
		if !apispec.EgaisWriteoffType(r.EgaisType).Valid() {
			t.Errorf("seeded reason %q has unknown EGAIS type %q", r.Name, r.EgaisType)
		}
	}
}
