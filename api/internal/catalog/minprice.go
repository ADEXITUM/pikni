package catalog

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

var (
	ErrMinPriceInEffect = errors.New("minimum price rule is already in effect")
	ErrInvalidMinPrice  = errors.New("invalid minimum price rule")
)

// MinPriceRule — полоса крепости одной редакции МРЦ. Запросы возвращают одинаковые по полям
// строки, поэтому они приводятся к одному типу.
type MinPriceRule = store.ListMinPriceRulesRow

// Даты приказов Минфина — по московскому времени; Москва живёт в UTC+3 без перехода на летнее.
var moscow = time.FixedZone("MSK", 3*60*60)

// moscowDay — календарный день в Москве в том же виде, в каком база отдаёт даты: полночь UTC.
func moscowDay(t time.Time) time.Time {
	local := t.In(moscow)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// minPriceAt считает МРЦ варианта по приказу Минфина. Действует последняя наступившая редакция
// целиком: в ней ищется полоса «свыше X до Y % включительно», куда попадает крепость. Для другого
// объёма цена пересчитывается пропорционально, кроме тары между same_price_over_ml и эталонной.
// Считается в целых копейках с округлением вверх: продать хотя бы на копейку дешевле МРЦ нельзя.
// rules — от новых редакций к старым.
func minPriceAt(rules []MinPriceRule, volumeMl int32, abv float64, at time.Time) *int64 {
	day := moscowDay(at)
	var edition *time.Time
	for _, r := range rules {
		if !r.ValidFrom.After(day) {
			edition = &r.ValidFrom
			break
		}
	}
	if edition == nil {
		return nil
	}
	strength := tenths(abv)
	for _, r := range rules {
		if !r.ValidFrom.Equal(*edition) || strength <= tenths(r.AbvFrom) || (r.AbvTo != nil && strength > tenths(*r.AbvTo)) {
			continue
		}
		if r.SamePriceOverMl != nil && volumeMl > *r.SamePriceOverMl && volumeMl <= r.VolumeMl {
			return &r.Kopecks
		}
		num, den := r.Kopecks*int64(volumeMl), int64(r.VolumeMl)
		v := (num + den - 1) / den
		return &v
	}
	return nil
}

// Крепость хранится с одним знаком после запятой, в десятых долях она целая.
func tenths(v float64) int64 {
	return int64(math.Round(v * 10))
}

func minRetailPrice(ctx context.Context, q *store.Queries, p store.GetVariantProductRow, v store.Variant, at time.Time) (*int64, error) {
	if p.KindCode == nil || p.Abv == nil || v.VolumeMl == nil {
		return nil, nil
	}
	rows, err := q.KindMinPriceRules(ctx, *p.KindCode)
	if err != nil {
		return nil, err
	}
	rules := make([]MinPriceRule, len(rows))
	for i, r := range rows {
		rules[i] = MinPriceRule(r)
	}
	return minPriceAt(rules, *v.VolumeMl, *p.Abv, at), nil
}

func (s *Service) MinPriceRules(ctx context.Context) ([]MinPriceRule, []store.MinPriceGroup, error) {
	rules, err := s.q.ListMinPriceRules(ctx)
	if err != nil {
		return nil, nil, err
	}
	groups, err := s.q.ListMinPriceGroups(ctx)
	return rules, groups, err
}

// MinPriceRuleSpec — полоса крепости новой редакции МРЦ.
type MinPriceRuleSpec struct {
	GroupCode       string
	ValidFrom       time.Time
	AbvFrom         float64
	AbvTo           *float64
	Kopecks         int64
	VolumeMl        int32
	SamePriceOverMl *int32
	Source          string
}

func (s *Service) CreateMinPriceRule(ctx context.Context, f MinPriceRuleSpec) (MinPriceRule, error) {
	source := strings.TrimSpace(f.Source)
	if f.Kopecks <= 0 || f.VolumeMl <= 0 || source == "" || f.AbvFrom < 0 || f.AbvFrom >= 100 ||
		(f.AbvTo != nil && (*f.AbvTo <= f.AbvFrom || *f.AbvTo > 100)) || (f.SamePriceOverMl != nil && *f.SamePriceOverMl <= 0) {
		return MinPriceRule{}, ErrInvalidMinPrice
	}
	var saved MinPriceRule
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		id, err := q.CreateMinPriceRule(ctx, store.CreateMinPriceRuleParams{
			GroupCode: f.GroupCode, ValidFrom: f.ValidFrom, AbvFrom: f.AbvFrom, AbvTo: f.AbvTo, Kopecks: f.Kopecks,
			VolumeMl: f.VolumeMl, SamePriceOverMl: f.SamePriceOverMl, Source: source,
		})
		switch {
		case db.IsUniqueViolation(err):
			return ErrDuplicate
		case db.IsForeignKeyViolation(err):
			return ErrUnknownReference
		case err != nil:
			return err
		}
		row, err := q.GetMinPriceRule(ctx, id)
		if err != nil {
			return err
		}
		saved = MinPriceRule(row)
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionMinPriceCreated, EntityType: audit.EntityMinPrice, EntityID: id.String(),
			Data: minPriceAuditData(saved),
		})
	})
	return saved, err
}

func minPriceAuditData(r MinPriceRule) map[string]any {
	return map[string]any{
		"group": r.GroupName, "abvFrom": r.AbvFrom, "abvTo": r.AbvTo, "price": rubles(r.Kopecks),
		"volumeMl": r.VolumeMl, "validFrom": r.ValidFrom.Format(time.DateOnly), "source": r.Source,
	}
}

// DeleteMinPriceRule удаляет полосу редакции, которая ещё не вступила в силу: по прошлым уже продавали.
func (s *Service) DeleteMinPriceRule(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.GetMinPriceRule(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !row.ValidFrom.After(moscowDay(time.Now())) {
			return ErrMinPriceInEffect
		}
		if err := q.DeleteMinPriceRule(ctx, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionMinPriceDeleted, EntityType: audit.EntityMinPrice, EntityID: id.String(),
			Data: minPriceAuditData(MinPriceRule(row)),
		})
	})
}

// MinRetailPrice — МРЦ варианта на момент at в копейках; nil, если для него МРЦ не установлена.
func (s *Service) MinRetailPrice(ctx context.Context, variantID uuid.UUID, at time.Time) (*int64, error) {
	v, p, err := lockVariant(ctx, s.q, variantID)
	if err != nil {
		return nil, err
	}
	return minRetailPrice(ctx, s.q, p, v, at)
}
