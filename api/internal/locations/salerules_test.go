package locations_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/locations"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func TestSaleRuleEditions(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := locations.NewService(db.App)
	ctx := t.Context()
	amur := ptr("28")
	rule := func(from, to string, validFrom time.Time) locations.NewSaleRule {
		return locations.NewSaleRule{Subject: amur, Scope: apispec.SaleScopeStrong, ValidFrom: validFrom, From: from, To: to, Source: "Закон для теста"}
	}
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	future := time.Now().AddDate(0, 2, 0).Truncate(24 * time.Hour)

	inEffect, err := svc.CreateSaleRule(ctx, rule("11:00", "21:00", past))
	if err != nil {
		t.Fatalf("CreateSaleRule() error = %v", err)
	}
	planned, err := svc.CreateSaleRule(ctx, rule("12:00", "20:00", future))
	if err != nil {
		t.Fatalf("CreateSaleRule(future) error = %v", err)
	}

	invalid := []struct {
		name string
		r    locations.NewSaleRule
		want error
	}{
		{"та же дата начала", rule("10:00", "22:00", past), locations.ErrRuleEdition},
		{"окно наоборот", rule("21:00", "11:00", future.AddDate(0, 0, 1)), locations.ErrInvalidWindow},
		{"неизвестный регион", locations.NewSaleRule{Subject: ptr("77"), Scope: apispec.SaleScopeAll, ValidFrom: past, From: "10:00", To: "22:00", Source: "x"}, locations.ErrUnknownSubject},
		{"без закона", locations.NewSaleRule{Subject: amur, Scope: apispec.SaleScopeAll, ValidFrom: future, From: "10:00", To: "22:00", Source: " "}, locations.ErrEmptySource},
	}
	for _, tt := range invalid {
		if _, err := svc.CreateSaleRule(ctx, tt.r); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}

	if err := svc.DeleteSaleRule(ctx, inEffect.ID); !errors.Is(err, locations.ErrRuleInEffect) {
		t.Errorf("deleting rule in effect: error = %v, want ErrRuleInEffect", err)
	}
	if err := svc.DeleteSaleRule(ctx, planned.ID); err != nil {
		t.Errorf("deleting planned rule: %v", err)
	}
}

func TestCheckSale(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := locations.NewService(db.App)
	ctx := t.Context()

	card, err := svc.Create(ctx, "МГ-01", apispec.LocationKindStore, store())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.App.Exec(ctx, `
		delete from alcohol_sale_bans;
		delete from alcohol_sale_rules;
		insert into alcohol_sale_rules (subject_code, scope, valid_from, sale_from, sale_to, source) values
			(null, 'all', '2012-01-01', '08:00', '23:00', 'федеральный'),
			('28', 'all', '2016-01-01', '11:00', '21:00', 'региональный');
		insert into alcohol_sale_bans (subject_code, name, scope, month_day, source) values
			('28', 'День знаний', 'all', '09-01', 'региональный')`)
	if err != nil {
		t.Fatal(err)
	}

	// 12:30 UTC — это 21:30 в Благовещенске (UTC+9): по местному времени уже нельзя.
	check, err := svc.CheckSale(ctx, card.ID, time.Date(2026, 10, 23, 12, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CheckSale() error = %v", err)
	}
	if check.Local.Format("15:04") != "21:30" || check.Strong.Allowed || check.Other.Allowed || check.Strong.To != 21*60 {
		t.Errorf("check at 21:30 local = %+v", check)
	}

	check, err = svc.CheckSale(ctx, card.ID, time.Date(2027, 9, 1, 3, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if check.Other.Ban != "День знаний" {
		t.Errorf("check on 1 September = %+v", check)
	}
}

// Правила из миграции: Хабаровский край, окно 10–22, День молодёжи 26.06.2027.
func TestSeededKhabarovskRules(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := locations.NewService(db.App)
	ctx := t.Context()

	f := store()
	f.City, f.Timezone, f.SubjectCode, f.FSRARID = "Хабаровск", "Asia/Vladivostok", "27", nil
	card, err := svc.Create(ctx, "МГ-03", apispec.LocationKindStore, f)
	if err != nil {
		t.Fatal(err)
	}
	vladivostok, err := time.LoadLocation("Asia/Vladivostok")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		local   string
		allowed bool
		ban     string
	}{
		{"2026-10-23 09:59", false, ""},
		{"2026-10-23 10:00", true, ""},
		{"2026-10-23 21:59", true, ""},
		{"2026-10-23 22:00", false, ""},
		{"2027-06-26 15:00", false, "День молодёжи (последняя суббота июня)"},
		{"2027-09-01 15:00", false, "День знаний (если воскресенье — 2 сентября)"},
	}
	for _, tt := range tests {
		at, err := time.ParseInLocation("2006-01-02 15:04", tt.local, vladivostok)
		if err != nil {
			t.Fatal(err)
		}
		check, err := svc.CheckSale(ctx, card.ID, at)
		if err != nil {
			t.Fatal(err)
		}
		if check.Strong.Allowed != tt.allowed || check.Strong.Ban != tt.ban || check.Other != check.Strong {
			t.Errorf("%s: strong=%+v other=%+v", tt.local, check.Strong, check.Other)
		}
	}
}
