package catalog_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func TestSuppliers(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := catalog.NewService(db.App)
	ctx := t.Context()

	fields := func(change func(*apispec.SupplierFields)) apispec.SupplierFields {
		f := apispec.SupplierFields{
			Name: "ООО «Амур-Дистрибуция»", INN: "7707083893", KPP: ptr("280101001"), FSRARID: ptr("030000123456"),
			DefaultReceivingMode: apispec.ReceivingModeSample,
			Contacts:             apispec.SupplierContacts{Person: ptr(" Иванова "), Email: ptr("sales@example.ru")},
		}
		if change != nil {
			change(&f)
		}
		return f
	}

	amur, err := svc.SaveSupplier(ctx, nil, fields(nil), true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(amur.Contacts), `"person": "Иванова"`) {
		t.Errorf("contacts = %s", amur.Contacts)
	}
	// Филиал той же организации — другой КПП, другая запись.
	if _, err := svc.SaveSupplier(ctx, nil, fields(func(f *apispec.SupplierFields) {
		f.KPP, f.FSRARID = ptr("272101001"), nil
	}), true); err != nil {
		t.Errorf("branch with another KPP: %v", err)
	}

	invalid := []struct {
		name   string
		change func(*apispec.SupplierFields)
		want   error
	}{
		{"контрольная цифра ИНН", func(f *apispec.SupplierFields) { f.INN = "7707083894" }, catalog.ErrInvalidINN},
		{"КПП у предпринимателя", func(f *apispec.SupplierFields) { f.INN = "500100732259" }, catalog.ErrInvalidKPP},
		{"ФСРАР ID короткий", func(f *apispec.SupplierFields) { f.KPP, f.FSRARID = ptr("280201001"), ptr("123") }, catalog.ErrInvalidFSRARID},
		{"тот же ИНН и КПП", func(f *apispec.SupplierFields) { f.FSRARID = nil }, catalog.ErrSupplierExists},
		{"ФСРАР ID занят", func(f *apispec.SupplierFields) { f.KPP = ptr("280301001") }, catalog.ErrFSRARIDTaken},
		{"режим приёмки", func(f *apispec.SupplierFields) { f.KPP, f.FSRARID, f.DefaultReceivingMode = nil, nil, "fast" }, catalog.ErrInvalidReceivingMode},
		{"почта", func(f *apispec.SupplierFields) { f.KPP, f.FSRARID, f.Contacts.Email = nil, nil, ptr("нет") }, catalog.ErrInvalidEmail},
	}
	for _, tt := range invalid {
		if _, err := svc.SaveSupplier(ctx, nil, fields(tt.change), true); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}

	entrepreneur, err := svc.SaveSupplier(ctx, nil, fields(func(f *apispec.SupplierFields) {
		f.Name, f.INN, f.KPP, f.FSRARID = "ИП Петров", "500100732259", nil, nil
	}), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveSupplier(ctx, &entrepreneur.ID, fields(func(f *apispec.SupplierFields) {
		f.Name, f.INN, f.KPP, f.FSRARID, f.DefaultReceivingMode = "ИП Петров", "500100732259", nil, nil, apispec.ReceivingModeDocument
	}), false); err != nil {
		t.Fatal(err)
	}
	var changes string
	err = db.App.QueryRow(ctx, `select data->'changes' from audit_log where entity_type = 'supplier' and action = 'ref.updated'`).Scan(&changes)
	if err != nil || !strings.Contains(changes, `"receivingMode": ["sample", "document"]`) {
		t.Errorf("supplier changes = %s, %v", changes, err)
	}
}
