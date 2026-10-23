package catalog

import (
	"testing"
	"time"
)

func date(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

func TestMinPriceAt(t *testing.T) {
	t.Parallel()
	forty, over := 40.0, 28.0
	vodka := []MinPriceRule{
		{ValidFrom: date("2025-01-01"), Kopecks: 34900, VolumeMl: 500, Abv: &forty, AbvOver: &over},
		{ValidFrom: date("2024-01-01"), Kopecks: 29900, VolumeMl: 500, Abv: &forty, AbvOver: &over},
	}
	cognac := []MinPriceRule{{ValidFrom: date("2025-01-01"), Kopecks: 61900, VolumeMl: 500}}
	summer := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		rules []MinPriceRule
		ml    int32
		abv   float64
		at    time.Time
		want  int64
	}{
		{"0,5 л 40 %", vodka, 500, 40, summer, 34900},
		{"0,7 л — пропорционально объёму", vodka, 700, 40, summer, 48860},
		{"38 % — пропорционально крепости", vodka, 500, 38, summer, 33155},
		{"копейки округляются вверх", vodka, 500, 37.5, summer, 32719},
		{"до новой редакции — прежняя", vodka, 500, 40, date("2024-06-01"), 29900},
		{"полночь по Москве — уже новая", vodka, 500, 40, time.Date(2024, 12, 31, 21, 30, 0, 0, time.UTC), 34900},
		{"коньяк — без пересчёта по крепости", cognac, 700, 42, summer, 86660},
	}
	for _, tt := range tests {
		got := minPriceAt(tt.rules, tt.ml, tt.abv, tt.at)
		if got == nil || *got != tt.want {
			t.Errorf("%s: got %v, want %d", tt.name, got, tt.want)
		}
	}

	if got := minPriceAt(vodka, 500, 25, summer); got != nil {
		t.Errorf("liqueur under 28 %%: got %d, want no minimum", *got)
	}
	if got := minPriceAt(vodka, 500, 40, date("2023-06-01")); got != nil {
		t.Errorf("before the first rule: got %d, want no minimum", *got)
	}
}
