package locations

import (
	"testing"
	"time"
)

func TestEvaluateSale(t *testing.T) {
	date := func(s string) time.Time {
		d, err := time.Parse(time.DateOnly, s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	hm := func(h, m int) int { return h*60 + m }
	yakutsk, err := time.LoadLocation("Asia/Yakutsk")
	if err != nil {
		t.Fatal(err)
	}

	// Учебный набор: федеральное окно 8–23, в регионе общее 10–22, а для крепкого
	// с 01.11.2026 — 11–21; ежегодный запрет 1 сентября и разовый «последний звонок».
	rules := []SaleRule{
		{Subject: "", Scope: scopeAll, ValidFrom: date("2012-01-01"), From: hm(8, 0), To: hm(23, 0)},
		{Subject: "28", Scope: scopeAll, ValidFrom: date("2020-01-01"), From: hm(10, 0), To: hm(22, 0)},
		{Subject: "28", Scope: scopeStrong, ValidFrom: date("2026-11-01"), From: hm(11, 0), To: hm(21, 0)},
	}
	lastBell := date("2027-05-25")
	bans := []SaleBan{
		{Name: "День знаний", Scope: scopeAll, MonthDay: "09-01"},
		{Name: "Последний звонок", Scope: scopeStrong, Date: &lastBell},
	}

	tests := []struct {
		name   string
		local  string
		strong bool
		want   SaleVerdict
	}{
		{"слабый днём", "2026-10-23 12:00", false, SaleVerdict{Allowed: true, From: hm(10, 0), To: hm(22, 0)}},
		{"ровно в момент открытия", "2026-10-23 10:00", false, SaleVerdict{Allowed: true, From: hm(10, 0), To: hm(22, 0)}},
		{"ровно в момент закрытия уже нельзя", "2026-10-23 22:00", false, SaleVerdict{From: hm(10, 0), To: hm(22, 0)}},
		{"крепкий до новой редакции", "2026-10-31 21:30", true, SaleVerdict{Allowed: true, From: hm(10, 0), To: hm(22, 0)}},
		{"крепкий после новой редакции", "2026-11-01 21:30", true, SaleVerdict{From: hm(11, 0), To: hm(21, 0)}},
		{"слабый новая редакция не трогает", "2026-11-01 21:30", false, SaleVerdict{Allowed: true, From: hm(10, 0), To: hm(22, 0)}},
		{"ежегодный запрет", "2027-09-01 12:00", false, SaleVerdict{Ban: "День знаний"}},
		{"разовый запрет крепкого", "2027-05-25 12:00", true, SaleVerdict{Ban: "Последний звонок"}},
		{"разовый запрет крепкого не касается пива", "2027-05-25 12:00", false, SaleVerdict{Allowed: true, From: hm(10, 0), To: hm(22, 0)}},
	}
	for _, tt := range tests {
		local, err := time.ParseInLocation("2006-01-02 15:04", tt.local, yakutsk)
		if err != nil {
			t.Fatal(err)
		}
		if got := evaluate(rules, bans, local, tt.strong); got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestEvaluateWithoutRegionalRules(t *testing.T) {
	federal := []SaleRule{{Scope: scopeAll, From: 8 * 60, To: 23 * 60}}
	local := time.Date(2026, 10, 23, 7, 59, 0, 0, time.UTC)
	if got := evaluate(federal, nil, local, true); got.Allowed || got.From != 8*60 || got.To != 23*60 {
		t.Errorf("before federal window: %+v", got)
	}
}
