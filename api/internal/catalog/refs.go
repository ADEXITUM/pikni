// Package catalog — нормативно-справочная информация: справочники, позиции, варианты,
// штрихкоды и цены.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

var (
	ErrNotFound         = errors.New("record not found")
	ErrDuplicate        = errors.New("record already exists")
	ErrUnknownReference = errors.New("referenced record does not exist")
	ErrEmptyName        = errors.New("name is required")
	ErrInvalidINN       = errors.New("INN must be 10 or 12 digits with valid check digits")
	ErrInvalidVolume    = errors.New("volume must be 10 to 30000 ml")
	ErrInvalidEgaisType = errors.New("unknown EGAIS write-off type")
)

// DB — пул соединений или уже открытая транзакция. Begin у транзакции создаёт точку
// сохранения, поэтому методы сервиса работают и внутри чужой транзакции (импорт).
type DB interface {
	store.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct {
	pool DB
	q    *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

func (s *Service) withTx(tx pgx.Tx) *Service {
	return &Service{pool: tx, q: store.New(tx)}
}

// change — изменение справочника: before == nil при создании записи.
type change struct {
	id            string
	before, after map[string]any
}

// write выполняет изменение и запись в журнал в одной транзакции.
func (s *Service) write(ctx context.Context, entity string, fn func(q *store.Queries) (change, error)) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		c, err := fn(s.q.WithTx(tx))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrNotFound
		case db.IsUniqueViolation(err):
			return ErrDuplicate
		case db.IsForeignKeyViolation(err):
			return ErrUnknownReference
		case err != nil:
			return fmt.Errorf("write %s: %w", entity, err)
		}

		e := audit.Event{EntityType: entity, EntityID: c.id, Action: apispec.AuditActionRefCreated, Data: c.after}
		if c.before != nil {
			changes := audit.Changes(c.before, c.after)
			if len(changes) == 0 {
				return nil
			}
			e.Action, e.Data = apispec.AuditActionRefUpdated, map[string]any{"name": c.after["name"], "changes": changes}
		}
		return audit.Record(ctx, tx, e)
	})
}

func name(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ErrEmptyName
	}
	return s, nil
}

func (s *Service) Countries(ctx context.Context) ([]store.Country, error) {
	return s.q.ListCountries(ctx)
}

func (s *Service) ProductKinds(ctx context.Context) ([]store.ListProductKindsRow, error) {
	return s.q.ListProductKinds(ctx)
}

func (s *Service) OriginRegions(ctx context.Context) ([]store.OriginRegion, error) {
	return s.q.ListOriginRegions(ctx)
}

func (s *Service) SaveOriginRegion(ctx context.Context, id *uuid.UUID, f apispec.OriginRegionFields, active bool) (store.OriginRegion, error) {
	n, err := name(f.Name)
	if err != nil {
		return store.OriginRegion{}, err
	}
	fields := func(r store.OriginRegion) map[string]any {
		return map[string]any{"name": r.Name, "countryCode": r.CountryCode, "isActive": r.IsActive}
	}

	var saved store.OriginRegion
	err = s.write(ctx, "origin_region", func(q *store.Queries) (change, error) {
		if id == nil {
			r, err := q.CreateOriginRegion(ctx, store.CreateOriginRegionParams{CountryCode: f.CountryCode, Name: n})
			saved = r
			return change{id: r.ID.String(), after: fields(r)}, err
		}
		old, err := q.GetOriginRegion(ctx, *id)
		if err != nil {
			return change{}, err
		}
		saved, err = q.UpdateOriginRegion(ctx, store.UpdateOriginRegionParams{ID: *id, CountryCode: f.CountryCode, Name: n, IsActive: active})
		return change{id: id.String(), before: fields(old), after: fields(saved)}, err
	})
	return saved, err
}

func (s *Service) Producers(ctx context.Context) ([]store.Producer, error) {
	return s.q.ListProducers(ctx)
}

func (s *Service) SaveProducer(ctx context.Context, id *uuid.UUID, f apispec.ProducerFields, active bool) (store.Producer, error) {
	n, err := name(f.Name)
	if err != nil {
		return store.Producer{}, err
	}
	var inn *string
	if f.INN != nil && strings.TrimSpace(*f.INN) != "" {
		v := strings.TrimSpace(*f.INN)
		if !innValid(v) {
			return store.Producer{}, ErrInvalidINN
		}
		inn = &v
	}
	fields := func(p store.Producer) map[string]any {
		return map[string]any{"name": p.Name, "inn": p.INN, "countryCode": p.CountryCode, "isActive": p.IsActive}
	}

	var saved store.Producer
	err = s.write(ctx, "producer", func(q *store.Queries) (change, error) {
		if id == nil {
			p, err := q.CreateProducer(ctx, store.CreateProducerParams{Name: n, INN: inn, CountryCode: f.CountryCode})
			saved = p
			return change{id: p.ID.String(), after: fields(p)}, err
		}
		old, err := q.GetProducer(ctx, *id)
		if err != nil {
			return change{}, err
		}
		saved, err = q.UpdateProducer(ctx, store.UpdateProducerParams{ID: *id, Name: n, INN: inn, CountryCode: f.CountryCode, IsActive: active})
		return change{id: id.String(), before: fields(old), after: fields(saved)}, err
	})
	return saved, err
}

func (s *Service) Brands(ctx context.Context) ([]store.Brand, error) {
	return s.q.ListBrands(ctx)
}

func (s *Service) SaveBrand(ctx context.Context, id *uuid.UUID, f apispec.BrandFields, active bool) (store.Brand, error) {
	n, err := name(f.Name)
	if err != nil {
		return store.Brand{}, err
	}
	fields := func(b store.Brand) map[string]any {
		return map[string]any{"name": b.Name, "producerId": b.ProducerID, "isActive": b.IsActive}
	}

	var saved store.Brand
	err = s.write(ctx, "brand", func(q *store.Queries) (change, error) {
		if id == nil {
			b, err := q.CreateBrand(ctx, store.CreateBrandParams{ProducerID: f.ProducerID, Name: n})
			saved = b
			return change{id: b.ID.String(), after: fields(b)}, err
		}
		old, err := q.GetBrand(ctx, *id)
		if err != nil {
			return change{}, err
		}
		saved, err = q.UpdateBrand(ctx, store.UpdateBrandParams{ID: *id, ProducerID: f.ProducerID, Name: n, IsActive: active})
		return change{id: id.String(), before: fields(old), after: fields(saved)}, err
	})
	return saved, err
}

func (s *Service) Volumes(ctx context.Context) ([]store.Volume, error) {
	return s.q.ListVolumes(ctx)
}

func (s *Service) CreateVolume(ctx context.Context, ml int32) (store.Volume, error) {
	if ml < 10 || ml > 30000 {
		return store.Volume{}, ErrInvalidVolume
	}
	var saved store.Volume
	err := s.write(ctx, "volume", func(q *store.Queries) (change, error) {
		v, err := q.CreateVolume(ctx, ml)
		saved = v
		return change{id: fmt.Sprint(ml), after: map[string]any{"name": fmt.Sprint(ml, " мл")}}, err
	})
	return saved, err
}

func (s *Service) SetVolumeActive(ctx context.Context, ml int32, active bool) (store.Volume, error) {
	var saved store.Volume
	fields := func(v store.Volume) map[string]any {
		return map[string]any{"name": fmt.Sprint(v.Ml, " мл"), "isActive": v.IsActive}
	}
	err := s.write(ctx, "volume", func(q *store.Queries) (change, error) {
		old, err := q.GetVolume(ctx, ml)
		if err != nil {
			return change{}, err
		}
		saved, err = q.SetVolumeActive(ctx, store.SetVolumeActiveParams{Ml: ml, IsActive: active})
		return change{id: fmt.Sprint(ml), before: fields(old), after: fields(saved)}, err
	})
	return saved, err
}

func (s *Service) WriteoffReasons(ctx context.Context) ([]store.WriteoffReason, error) {
	return s.q.ListWriteoffReasons(ctx)
}

func (s *Service) SaveWriteoffReason(ctx context.Context, id *uuid.UUID, f apispec.WriteoffReasonFields, active bool) (store.WriteoffReason, error) {
	n, err := name(f.Name)
	if err != nil {
		return store.WriteoffReason{}, err
	}
	if !f.EgaisType.Valid() {
		return store.WriteoffReason{}, ErrInvalidEgaisType
	}
	fields := func(r store.WriteoffReason) map[string]any {
		return map[string]any{"name": r.Name, "egaisType": r.EgaisType, "isActive": r.IsActive, "photoRequired": r.PhotoRequired}
	}

	var saved store.WriteoffReason
	err = s.write(ctx, "writeoff_reason", func(q *store.Queries) (change, error) {
		if id == nil {
			r, err := q.CreateWriteoffReason(ctx, store.CreateWriteoffReasonParams{Name: n, EgaisType: string(f.EgaisType), PhotoRequired: f.PhotoRequired != nil && *f.PhotoRequired})
			saved = r
			return change{id: r.ID.String(), after: fields(r)}, err
		}
		old, err := q.GetWriteoffReason(ctx, *id)
		if err != nil {
			return change{}, err
		}
		saved, err = q.UpdateWriteoffReason(ctx, store.UpdateWriteoffReasonParams{ID: *id, Name: n, EgaisType: string(f.EgaisType), IsActive: active, PhotoRequired: f.PhotoRequired != nil && *f.PhotoRequired})
		return change{id: id.String(), before: fields(old), after: fields(saved)}, err
	})
	return saved, err
}
