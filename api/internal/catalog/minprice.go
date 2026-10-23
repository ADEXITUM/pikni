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

// MinPriceRule — редакция МРЦ для вида продукции. Запросы возвращают одинаковые по полям
// строки, поэтому они приводятся к одному типу.
type MinPriceRule = store.ListMinPriceRulesRow

// Даты приказов Минфина — по московскому времени; Москва живёт в UTC+3 без перехода на летнее.
var moscow = time.FixedZone("MSK", 3*60*60)

// moscowDay — календарный день в Москве в том же виде, в каком база отдаёт даты: полночь UTC.
func moscowDay(t time.Time) time.Time {
	local := t.In(moscow)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// minPriceAt выбирает редакцию, действующую на момент at, и пересчитывает её цену на объём
// и крепость варианта. Считается в целых копейках, остаток округляется вверх: продать хотя бы
// на копейку дешевле МРЦ нельзя. rules — от новых редакций к старым.
func minPriceAt(rules []MinPriceRule, volumeMl int32, abv float64, at time.Time) *int64 {
	day := moscowDay(at)
	for _, r := range rules {
		if r.ValidFrom.After(day) {
			continue
		}
		if r.AbvOver != nil && tenths(abv) <= tenths(*r.AbvOver) {
			return nil
		}
		num, den := r.Kopecks*int64(volumeMl), int64(r.VolumeMl)
		if r.Abv != nil {
			num, den = num*tenths(abv), den*tenths(*r.Abv)
		}
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

func (s *Service) MinPriceRules(ctx context.Context) ([]MinPriceRule, error) {
	return s.q.ListMinPriceRules(ctx)
}

// MinPriceRuleSpec — новая редакция МРЦ.
type MinPriceRuleSpec struct {
	KindCode     string
	ValidFrom    time.Time
	Kopecks      int64
	VolumeMl     int32
	Abv, AbvOver *float64
	Source       string
}

func (s *Service) CreateMinPriceRule(ctx context.Context, f MinPriceRuleSpec) (MinPriceRule, error) {
	source := strings.TrimSpace(f.Source)
	if f.Kopecks <= 0 || f.VolumeMl <= 0 || source == "" ||
		(f.Abv != nil && (*f.Abv <= 0 || *f.Abv > 100)) || (f.AbvOver != nil && (*f.AbvOver < 0 || *f.AbvOver >= 100)) {
		return MinPriceRule{}, ErrInvalidMinPrice
	}
	var saved MinPriceRule
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		row, err := s.q.WithTx(tx).CreateMinPriceRule(ctx, store.CreateMinPriceRuleParams{
			KindCode: f.KindCode, ValidFrom: f.ValidFrom, Kopecks: f.Kopecks, VolumeMl: f.VolumeMl,
			Abv: f.Abv, AbvOver: f.AbvOver, Source: source,
		})
		switch {
		case db.IsUniqueViolation(err):
			return ErrDuplicate
		case db.IsForeignKeyViolation(err):
			return ErrUnknownReference
		case err != nil:
			return err
		}
		saved = MinPriceRule(row)
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionMinPriceCreated, EntityType: audit.EntityMinPrice, EntityID: row.ID.String(),
			Data: map[string]any{"kindCode": row.KindCode, "price": rubles(row.Kopecks), "validFrom": row.ValidFrom.Format(time.DateOnly), "source": source},
		})
	})
	return saved, err
}

// DeleteMinPriceRule удаляет редакцию, которая ещё не вступила в силу: по прошлым уже продавали.
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
			Data: map[string]any{"kindCode": row.KindCode, "price": rubles(row.Kopecks), "validFrom": row.ValidFrom.Format(time.DateOnly)},
		})
	})
}
