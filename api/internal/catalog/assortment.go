package catalog

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

var (
	ErrInvalidMinQty = errors.New("minimum quantity must be from 1 to 9999")
	ErrSameLocation  = errors.New("cannot copy assortment from the same location")
)

func locationCode(ctx context.Context, q *store.Queries, id uuid.UUID) (string, error) {
	code, err := q.GetLocationCode(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return code, err
}

func (s *Service) Assortment(ctx context.Context, locationID uuid.UUID) ([]store.ListAssortmentRow, error) {
	if _, err := locationCode(ctx, s.q, locationID); err != nil {
		return nil, err
	}
	return s.q.ListAssortment(ctx, locationID)
}

// SetAssortmentItem добавляет вариант в матрицу точки или меняет его минимальный остаток.
func (s *Service) SetAssortmentItem(ctx context.Context, locationID, variantID uuid.UUID, minQty int32) error {
	if minQty < 1 || minQty > 9999 {
		return ErrInvalidMinQty
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		code, err := locationCode(ctx, q, locationID)
		if err != nil {
			return err
		}
		v, p, err := lockVariant(ctx, q, variantID)
		if err != nil {
			return err
		}
		var before *int32
		if old, err := q.GetAssortmentItem(ctx, store.GetAssortmentItemParams{LocationID: locationID, VariantID: variantID}); err == nil {
			if old == minQty {
				return nil
			}
			before = &old
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		err = q.UpsertAssortmentItem(ctx, store.UpsertAssortmentItemParams{LocationID: locationID, VariantID: variantID, MinQty: minQty})
		if err != nil {
			return err
		}
		data := assortmentAuditData(code, p, v)
		if before != nil {
			data["changes"] = map[string]any{"minQty": []any{*before, minQty}}
		} else {
			data["minQty"] = minQty
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionAssortmentSet, EntityType: audit.EntityAssortment, EntityID: locationID.String(), Data: data,
		})
	})
}

func assortmentAuditData(code string, p store.GetVariantProductRow, v store.Variant) map[string]any {
	return map[string]any{"location": code, "article": p.Article, "name": p.Name, "variant": variantLabel(v)}
}

func (s *Service) RemoveAssortmentItem(ctx context.Context, locationID, variantID uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		code, err := locationCode(ctx, q, locationID)
		if err != nil {
			return err
		}
		if _, err := q.DeleteAssortmentItem(ctx, store.DeleteAssortmentItemParams{LocationID: locationID, VariantID: variantID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		v, p, err := lockVariant(ctx, q, variantID)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionAssortmentRemoved, EntityType: audit.EntityAssortment, EntityID: locationID.String(),
			Data: assortmentAuditData(code, p, v),
		})
	})
}

// CopyAssortment добавляет в матрицу точки варианты из матрицы другой точки; уже имеющиеся не меняются.
func (s *Service) CopyAssortment(ctx context.Context, to, from uuid.UUID) (int64, error) {
	if to == from {
		return 0, ErrSameLocation
	}
	var added int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		code, err := locationCode(ctx, q, to)
		if err != nil {
			return err
		}
		fromCode, err := locationCode(ctx, q, from)
		if err != nil {
			return err
		}
		if added, err = q.CopyAssortment(ctx, store.CopyAssortmentParams{ToLocationID: to, FromLocationID: from}); err != nil {
			return err
		}
		if added == 0 {
			return nil
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionAssortmentCopied, EntityType: audit.EntityAssortment, EntityID: to.String(),
			Data: map[string]any{"location": code, "from": fromCode, "added": added},
		})
	})
	return added, err
}
