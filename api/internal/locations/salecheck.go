package locations

import (
	"time"
)

// Правило продажи: окно в местном времени точки. Subject == "" — федеральное правило,
// оно действует во всех регионах. Scope "strong" — продукция крепче 15 %, "all" — вся алкогольная.
type SaleRule struct {
	Subject   string
	Scope     string
	ValidFrom time.Time
	From, To  int // минуты от полуночи, To не включается
}

// День полного запрета: разовый (Date) или ежегодный (MonthDay в виде «ММ-ДД»).
type SaleBan struct {
	Name     string
	Scope    string
	Date     *time.Time
	MonthDay string
}

type SaleVerdict struct {
	Allowed  bool
	From, To int
	Ban      string
}

const (
	scopeAll    = "all"
	scopeStrong = "strong"
)

// evaluate решает, можно ли продавать в местное время local: строже всех действующих правил
// одновременно — федерального и регионального, общего и для крепкого. Для каждого уровня
// и вида берётся редакция с самой поздней датой начала, которая уже наступила.
func evaluate(rules []SaleRule, bans []SaleBan, local time.Time, strong bool) SaleVerdict {
	scopes := []string{scopeAll}
	if strong {
		scopes = append(scopes, scopeStrong)
	}
	applies := func(scope string) bool { return scope == scopeAll || (strong && scope == scopeStrong) }

	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	for _, b := range bans {
		if !applies(b.Scope) {
			continue
		}
		onDate := b.Date != nil && b.Date.Year() == local.Year() && b.Date.YearDay() == local.YearDay()
		if onDate || b.MonthDay == local.Format("01-02") {
			return SaleVerdict{Ban: b.Name}
		}
	}

	from, to := 0, 24*60
	for _, scope := range scopes {
		for _, subject := range uniqueSubjects(rules) {
			var current *SaleRule
			for i, r := range rules {
				if r.Scope != scope || r.Subject != subject || r.ValidFrom.After(day) {
					continue
				}
				if current == nil || r.ValidFrom.After(current.ValidFrom) {
					current = &rules[i]
				}
			}
			if current != nil {
				from, to = max(from, current.From), min(to, current.To)
			}
		}
	}

	minute := local.Hour()*60 + local.Minute()
	return SaleVerdict{Allowed: from <= minute && minute < to, From: from, To: to}
}

func uniqueSubjects(rules []SaleRule) []string {
	seen := map[string]bool{}
	var subjects []string
	for _, r := range rules {
		if !seen[r.Subject] {
			seen[r.Subject] = true
			subjects = append(subjects, r.Subject)
		}
	}
	return subjects
}
