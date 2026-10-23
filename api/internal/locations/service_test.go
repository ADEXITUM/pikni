package locations_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/locations"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func ptr[T any](v T) *T { return &v }

func store() locations.Fields {
	return locations.Fields{
		Name: "Магазин на ул. Ленина", City: "Благовещенск", Timezone: "Asia/Yakutsk", SubjectCode: "28",
		Address: ptr("ул. Ленина, 112"), Lat: ptr(50.2576), Lon: ptr(127.5355),
		FSRARID: ptr("030000100011"), KPP: ptr("280101002"),
		OpeningHours: apispec.OpeningHours{
			Mon: &apispec.DayHours{Open: "09:00", Close: "23:00"},
			Sun: &apispec.DayHours{Open: "10:00", Close: "24:00"},
		},
		IsPublic: true,
	}
}

func TestCreateLocation(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := locations.NewService(db.App)

	card, err := svc.Create(t.Context(), " мг-05 ", apispec.LocationKindStore, store())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if card.Code != "МГ-05" || len(card.Zones) != 2 || card.Version != 1 || !card.IsActive {
		t.Errorf("card = %+v", card)
	}
	var hours apispec.OpeningHours
	if err := json.Unmarshal(card.OpeningHours, &hours); err != nil || hours.Sun == nil || hours.Sun.Close != "24:00" || hours.Tue != nil {
		t.Errorf("opening hours = %s (%v)", card.OpeningHours, err)
	}

	var audit int
	if err := db.App.QueryRow(t.Context(), `select count(*) from audit_log where action = 'location.created'`).Scan(&audit); err != nil || audit != 1 {
		t.Errorf("location.created entries = %d (%v)", audit, err)
	}
}

func TestCreateValidatesFields(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := locations.NewService(db.App)
	if _, err := svc.Create(t.Context(), "МГ-01", apispec.LocationKindStore, store()); err != nil {
		t.Fatal(err)
	}

	with := func(change func(*locations.Fields)) locations.Fields {
		f := store()
		f.FSRARID = nil
		change(&f)
		return f
	}
	tests := []struct {
		name string
		code string
		f    locations.Fields
		want error
	}{
		{"код с пробелом", "МГ 02", with(func(*locations.Fields) {}), locations.ErrInvalidCode},
		{"занятый код", "мг-01", with(func(*locations.Fields) {}), locations.ErrCodeTaken},
		{"без названия", "МГ-02", with(func(f *locations.Fields) { f.Name = " " }), locations.ErrEmptyField},
		{"неизвестный пояс", "МГ-02", with(func(f *locations.Fields) { f.Timezone = "Asia/Blagoveshchensk" }), locations.ErrInvalidTimezone},
		{"неизвестный регион", "МГ-02", with(func(f *locations.Fields) { f.SubjectCode = "77" }), locations.ErrUnknownSubject},
		{"короткий ФСРАР ID", "МГ-02", with(func(f *locations.Fields) { f.FSRARID = ptr("12345") }), locations.ErrInvalidFSRARID},
		{"чужой ФСРАР ID", "МГ-02", with(func(f *locations.Fields) { f.FSRARID = ptr("030000100011") }), locations.ErrFSRARIDTaken},
		{"КПП с буквой", "МГ-02", with(func(f *locations.Fields) { f.KPP = ptr("28010100A") }), locations.ErrInvalidKPP},
		{"одна координата", "МГ-02", with(func(f *locations.Fields) { f.Lon = nil }), locations.ErrCoordinates},
		{"широта за пределом", "МГ-02", with(func(f *locations.Fields) { f.Lat = ptr(91.0) }), locations.ErrCoordinates},
		{"закрытие раньше открытия", "МГ-02", with(func(f *locations.Fields) {
			f.OpeningHours.Tue = &apispec.DayHours{Open: "22:00", Close: "10:00"}
		}), locations.ErrOpeningHours},
		{"время без ведущего нуля", "МГ-02", with(func(f *locations.Fields) {
			f.OpeningHours.Tue = &apispec.DayHours{Open: "9:00", Close: "18:00"}
		}), locations.ErrOpeningHours},
		{"24:30", "МГ-02", with(func(f *locations.Fields) {
			f.OpeningHours.Tue = &apispec.DayHours{Open: "09:00", Close: "24:30"}
		}), locations.ErrOpeningHours},
	}
	for _, tt := range tests {
		if _, err := svc.Create(t.Context(), tt.code, apispec.LocationKindStore, tt.f); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}
}

func TestUpdateLocation(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := locations.NewService(db.App)
	ctx := t.Context()

	card, err := svc.Create(ctx, "МГ-05", apispec.LocationKindStore, store())
	if err != nil {
		t.Fatal(err)
	}

	f := store()
	f.Address = ptr("ул. Ленина, 114")
	f.LicenseValidTo = ptr(time.Date(2029, 3, 1, 0, 0, 0, 0, time.UTC))
	updated, err := svc.Update(ctx, card.ID, f, false, card.Version)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Version != 2 || updated.IsActive || *updated.Address != "ул. Ленина, 114" || updated.Kind != "store" {
		t.Errorf("updated = %+v", updated)
	}

	if _, err := svc.Update(ctx, card.ID, f, true, card.Version); !errors.Is(err, locations.ErrConflict) {
		t.Errorf("stale update error = %v, want ErrConflict", err)
	}

	var changes string
	err = db.App.QueryRow(ctx, `select data->'changes' from audit_log where action = 'location.updated'`).Scan(&changes)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"address", "licenseValidTo", "isActive"} {
		if !strings.Contains(changes, field) {
			t.Errorf("changes %s do not mention %s", changes, field)
		}
	}
	if strings.Contains(changes, "openingHours") || strings.Contains(changes, `"name"`) {
		t.Errorf("unchanged fields are in the diff: %s", changes)
	}
}
