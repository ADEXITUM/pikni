package catalog

import (
	"testing"
	"time"
)

func date(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

// band — полоса «свыше from до to % включительно»; to = 0 — без верхней границы.
func band(validFrom string, from, to float64, kopecks int64, volume int32, sameOver int32) MinPriceRule {
	r := MinPriceRule{ValidFrom: date(validFrom), AbvFrom: from, Kopecks: kopecks, VolumeMl: volume}
	if to > 0 {
		r.AbvTo = &to
	}
	if sameOver > 0 {
		r.SamePriceOverMl = &sameOver
	}
	return r
}

func TestMinPriceAt(t *testing.T) {
	t.Parallel()
	// Фрагменты приказов 181н (2026) и 191н (2025), от новых редакций к старым.
	vodka := []MinPriceRule{
		band("2026-01-01", 37, 38, 40900, 500, 375),
		band("2026-01-01", 39, 40, 40900, 500, 375),
		band("2026-01-01", 40, 41, 42500, 500, 375),
		band("2025-01-01", 39, 40, 34900, 500, 375),
	}
	spirits := []MinPriceRule{
		band("2026-01-01", 28, 29, 32500, 500, 0),
		band("2026-01-01", 37, 38, 39300, 500, 0),
		band("2026-01-01", 61, 0, 59100, 500, 0),
	}
	cognac := []MinPriceRule{band("2026-01-01", 0, 0, 75500, 500, 0)}
	sparkling := []MinPriceRule{band("2023-01-07", 0, 0, 23900, 750, 0)}
	now := date("2026-10-23")

	tests := []struct {
		name  string
		rules []MinPriceRule
		ml    int32
		abv   float64
		at    time.Time
		want  int64
	}{
		{"водка 0,5 л 40 %", vodka, 500, 40, now, 40900},
		{"водка 0,7 л — пропорционально объёму", vodka, 700, 40, now, 57260},
		{"38 % — та же полоса цен, не пропорционально", vodka, 500, 38, now, 40900},
		{"40,5 % — следующая полоса", vodka, 500, 40.5, now, 42500},
		{"водка 0,45 л стоит как 0,5 л", vodka, 450, 40, now, 40900},
		{"ровно 0,375 л — уже пропорционально", vodka, 375, 40, now, 30675},
		{"до 2026 года — прежняя редакция", vodka, 500, 40, date("2025-06-01"), 34900},
		{"полночь по Москве — уже новая", vodka, 500, 40, time.Date(2025, 12, 31, 21, 30, 0, 0, time.UTC), 40900},
		{"ликёр 38 %", spirits, 500, 38, now, 39300},
		{"крепче 61 % — открытая полоса", spirits, 500, 70, now, 59100},
		{"коньяк 0,7 л", cognac, 700, 40, now, 105700},
		{"игристое 0,2 л — копейки вверх", sparkling, 200, 12, now, 6374},
	}
	for _, tt := range tests {
		got := minPriceAt(tt.rules, tt.ml, tt.abv, tt.at)
		if got == nil || *got != tt.want {
			t.Errorf("%s: got %v, want %d", tt.name, got, tt.want)
		}
	}

	none := []struct {
		name  string
		rules []MinPriceRule
		abv   float64
		at    time.Time
	}{
		{"ровно 28 % — не «свыше 28»", spirits, 28, now},
		{"водка без своей полосы", vodka, 36, now},
		{"до первой редакции", vodka, 40, date("2024-06-01")},
	}
	for _, tt := range none {
		if got := minPriceAt(tt.rules, 500, tt.abv, tt.at); got != nil {
			t.Errorf("%s: got %d, want no minimum", tt.name, *got)
		}
	}
}
