package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

var (
	ErrInvalidPrice  = errors.New("price must be positive")
	ErrPriceInPast   = errors.New("price cannot start in the past")
	ErrPriceInEffect = errors.New("price is already in effect")
)

// BelowMinPriceError — розничная цена ниже МРЦ; Min — МРЦ в копейках на дату начала цены.
type BelowMinPriceError struct{ Min int64 }

func (e *BelowMinPriceError) Error() string {
	return fmt.Sprintf("retail price is below the minimum of %s", rubles(e.Min))
}

// Часы клиента и сервера расходятся, поэтому «сейчас» с клиента может оказаться чуть в прошлом.
const pastTolerance = time.Minute

func rubles(kopecks int64) string {
	return fmt.Sprintf("%d,%02d ₽", kopecks/100, kopecks%100)
}

func (s *Service) Prices(ctx context.Context, variantID uuid.UUID, kind apispec.PriceKind) ([]store.ListVariantPricesRow, error) {
	if _, _, err := lockVariant(ctx, s.q, variantID); err != nil {
		return nil, err
	}
	return s.q.ListVariantPrices(ctx, store.ListVariantPricesParams{VariantID: variantID, Kind: string(kind)})
}

// SetPrice назначает цену с момента validFrom (nil — сразу). Розничная цена сверяется с МРЦ
// на эту дату: продавать дешевле минимальной цены Минфина нельзя.
func (s *Service) SetPrice(ctx context.Context, variantID uuid.UUID, kind apispec.PriceKind, kopecks int64, validFrom *time.Time, setBy *uuid.UUID) (store.CreatePriceRow, error) {
	if kopecks <= 0 {
		return store.CreatePriceRow{}, ErrInvalidPrice
	}
	from := time.Now()
	if validFrom != nil {
		if validFrom.Before(from.Add(-pastTolerance)) {
			return store.CreatePriceRow{}, ErrPriceInPast
		}
		from = *validFrom
	}

	var saved store.CreatePriceRow
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		v, p, err := lockVariant(ctx, q, variantID)
		if err != nil {
			return err
		}
		if kind == apispec.PriceKindRetail {
			minimum, err := minRetailPrice(ctx, q, p, v, from)
			if err != nil {
				return err
			}
			if minimum != nil && kopecks < *minimum {
				return &BelowMinPriceError{Min: *minimum}
			}
		}
		saved, err = q.CreatePrice(ctx, store.CreatePriceParams{
			VariantID: variantID, Kind: string(kind), Kopecks: kopecks, ValidFrom: from, SetBy: setBy,
		})
		if db.IsUniqueViolation(err) {
			return ErrDuplicate
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionPriceCreated, EntityType: audit.EntityVariant, EntityID: variantID.String(),
			Data: map[string]any{
				"article": v.Article, "name": p.Name, "variant": variantLabel(v),
				"kind": kind, "price": rubles(kopecks), "validFrom": from,
			},
		})
	})
	return saved, err
}

// DeletePrice отменяет цену, назначенную на будущее. Действующая и прошлые — история, их не трогают.
func (s *Service) DeletePrice(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		price, err := q.GetPrice(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !price.ValidFrom.After(time.Now()) {
			return ErrPriceInEffect
		}
		v, p, err := lockVariant(ctx, q, price.VariantID)
		if err != nil {
			return err
		}
		if err := q.DeletePrice(ctx, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionPriceDeleted, EntityType: audit.EntityVariant, EntityID: v.ID.String(),
			Data: map[string]any{
				"article": v.Article, "name": p.Name, "variant": variantLabel(v),
				"kind": price.Kind, "price": rubles(price.Kopecks), "validFrom": price.ValidFrom,
			},
		})
	})
}
