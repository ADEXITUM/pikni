package locations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/locations/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

var (
	ErrRuleNotFound   = errors.New("sale rule not found")
	ErrRuleInEffect   = errors.New("sale rule is already in effect")
	ErrRuleEdition    = errors.New("an edition with this start date already exists")
	ErrBanNotFound    = errors.New("sale ban not found")
	ErrInvalidScope   = errors.New("unknown sale scope")
	ErrInvalidWindow  = errors.New("invalid sale window")
	ErrInvalidBanDate = errors.New("ban needs exactly one of date and month-day")
	ErrEmptySource    = errors.New("name and source are required")
)

type (
	Rulebook struct {
		Rules []store.ListSaleRulesRow
		Bans  []store.ListSaleBansRow
	}
	NewSaleRule struct {
		Subject   *string
		Scope     apispec.SaleScope
		ValidFrom time.Time
		From, To  string
		Source    string
	}
	NewSaleBan struct {
		Subject, Name, Source string
		Scope                 apispec.SaleScope
		Date                  *time.Time
		MonthDay              *string
	}
	SaleCheck struct {
		Local         time.Time
		Strong, Other SaleVerdict
	}
)

func (s *Service) Rulebook(ctx context.Context) (Rulebook, error) {
	rules, err := s.q.ListSaleRules(ctx)
	if err != nil {
		return Rulebook{}, fmt.Errorf("list sale rules: %w", err)
	}
	bans, err := s.q.ListSaleBans(ctx)
	if err != nil {
		return Rulebook{}, fmt.Errorf("list sale bans: %w", err)
	}
	return Rulebook{Rules: rules, Bans: bans}, nil
}

func (s *Service) CreateSaleRule(ctx context.Context, r NewSaleRule) (store.CreateSaleRuleRow, error) {
	from, okFrom := minutes(r.From)
	to, okTo := minutes(r.To)
	r.Source = strings.TrimSpace(r.Source)
	switch {
	case !r.Scope.Valid():
		return store.CreateSaleRuleRow{}, ErrInvalidScope
	case !okFrom || !okTo || from >= to || to > 24*60-1:
		return store.CreateSaleRuleRow{}, ErrInvalidWindow
	case r.Source == "":
		return store.CreateSaleRuleRow{}, ErrEmptySource
	}

	var row store.CreateSaleRuleRow
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		row, err = s.q.WithTx(tx).CreateSaleRule(ctx, store.CreateSaleRuleParams{
			SubjectCode: r.Subject, Scope: string(r.Scope), ValidFrom: r.ValidFrom,
			SaleFrom: timeOfDay(from), SaleTo: timeOfDay(to), Source: r.Source,
		})
		switch {
		case db.IsUniqueViolation(err):
			return ErrRuleEdition
		case db.IsForeignKeyViolation(err):
			return ErrUnknownSubject
		case err != nil:
			return fmt.Errorf("create sale rule: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{
			Action:     apispec.AuditActionSaleRuleCreated,
			EntityType: audit.EntitySaleRule,
			EntityID:   row.ID.String(),
			Data: map[string]any{
				"subjectCode": r.Subject, "scope": r.Scope, "validFrom": r.ValidFrom.Format(time.DateOnly),
				"from": r.From, "to": r.To,
			},
		})
	})
	return row, err
}

func (s *Service) DeleteSaleRule(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.DeleteFutureSaleRule(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			exists, err := q.SaleRuleExists(ctx, id)
			if err != nil {
				return fmt.Errorf("check sale rule: %w", err)
			}
			if exists {
				return ErrRuleInEffect
			}
			return ErrRuleNotFound
		}
		if err != nil {
			return fmt.Errorf("delete sale rule: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{
			Action:     apispec.AuditActionSaleRuleDeleted,
			EntityType: audit.EntitySaleRule,
			EntityID:   id.String(),
			Data:       map[string]any{"subjectCode": row.SubjectCode, "scope": row.Scope, "validFrom": row.ValidFrom.Format(time.DateOnly)},
		})
	})
}

func (s *Service) CreateSaleBan(ctx context.Context, b NewSaleBan) (store.CreateSaleBanRow, error) {
	b.Name, b.Source = strings.TrimSpace(b.Name), strings.TrimSpace(b.Source)
	switch {
	case !b.Scope.Valid():
		return store.CreateSaleBanRow{}, ErrInvalidScope
	case b.Name == "" || b.Source == "":
		return store.CreateSaleBanRow{}, ErrEmptySource
	case (b.Date == nil) == (b.MonthDay == nil), b.MonthDay != nil && !validMonthDay(*b.MonthDay):
		return store.CreateSaleBanRow{}, ErrInvalidBanDate
	}

	var row store.CreateSaleBanRow
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		row, err = s.q.WithTx(tx).CreateSaleBan(ctx, store.CreateSaleBanParams{
			SubjectCode: b.Subject, Name: b.Name, Scope: string(b.Scope), Date: b.Date,
			MonthDay: b.MonthDay, Source: b.Source,
		})
		if db.IsForeignKeyViolation(err) {
			return ErrUnknownSubject
		}
		if err != nil {
			return fmt.Errorf("create sale ban: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{
			Action:     apispec.AuditActionSaleBanCreated,
			EntityType: audit.EntitySaleBan,
			EntityID:   row.ID.String(),
			Data:       map[string]any{"subjectCode": b.Subject, "name": b.Name},
		})
	})
	return row, err
}

func (s *Service) DeleteSaleBan(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		row, err := s.q.WithTx(tx).DeleteSaleBan(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBanNotFound
		}
		if err != nil {
			return fmt.Errorf("delete sale ban: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{
			Action:     apispec.AuditActionSaleBanDeleted,
			EntityType: audit.EntitySaleBan,
			EntityID:   id.String(),
			Data:       map[string]any{"subjectCode": row.SubjectCode, "name": row.Name},
		})
	})
}

// CheckSale проверяет продажу в точке на момент at по её местному времени.
func (s *Service) CheckSale(ctx context.Context, locationID uuid.UUID, at time.Time) (SaleCheck, error) {
	loc, err := s.q.GetLocation(ctx, locationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SaleCheck{}, ErrNotFound
	}
	if err != nil {
		return SaleCheck{}, fmt.Errorf("get location: %w", err)
	}
	tz, err := time.LoadLocation(loc.Timezone)
	if err != nil {
		return SaleCheck{}, fmt.Errorf("location %s timezone: %w", loc.Code, err)
	}

	ruleRows, err := s.q.ListSubjectSaleRules(ctx, &loc.SubjectCode)
	if err != nil {
		return SaleCheck{}, fmt.Errorf("list sale rules: %w", err)
	}
	banRows, err := s.q.ListSubjectSaleBans(ctx, loc.SubjectCode)
	if err != nil {
		return SaleCheck{}, fmt.Errorf("list sale bans: %w", err)
	}

	rules := make([]SaleRule, len(ruleRows))
	for i, r := range ruleRows {
		rules[i] = SaleRule{Scope: r.Scope, ValidFrom: r.ValidFrom, From: minutesOf(r.SaleFrom), To: minutesOf(r.SaleTo)}
		if r.SubjectCode != nil {
			rules[i].Subject = *r.SubjectCode
		}
	}
	bans := make([]SaleBan, len(banRows))
	for i, b := range banRows {
		bans[i] = SaleBan{Name: b.Name, Scope: b.Scope, Date: b.Date}
		if b.MonthDay != nil {
			bans[i].MonthDay = *b.MonthDay
		}
	}

	local := at.In(tz)
	return SaleCheck{
		Local:  local,
		Strong: evaluate(rules, bans, local, true),
		Other:  evaluate(rules, bans, local, false),
	}, nil
}

func validMonthDay(md string) bool {
	_, err := time.Parse("2006-01-02", "2024-"+md)
	return err == nil
}

func minutesOf(t pgtype.Time) int {
	return int(t.Microseconds / int64(time.Minute/time.Microsecond))
}

func timeOfDay(minutes int) pgtype.Time {
	return pgtype.Time{Microseconds: int64(minutes) * int64(time.Minute/time.Microsecond), Valid: true}
}

func formatMinutes(m int) string {
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}
