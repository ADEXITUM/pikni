// Package labels — ценники (ЭТ-2, ЭТ-3): из документа поступления, выборочно или по списку
// товаров, у которых цена изменилась после печати ценника. Напечатанное запоминается по точке.
package labels

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/forms"
	"github.com/ADEXITUM/pikni/api/internal/labels/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

var (
	ErrNotFound = errors.New("document not found")
	ErrEmpty    = errors.New("nothing to print")
)

type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

// Item — товар в списке печати: текущая цена и цена на последнем напечатанном ценнике.
type Item = store.PriceTagItemsRow

// Stale — товары торгового зала точки, ценники которых устарели или ещё не печатались.
func (s *Service) Stale(ctx context.Context, location uuid.UUID) ([]Item, error) {
	if err := auth.CheckLocation(ctx, apispec.PermissionFloorView, location); err != nil {
		return nil, err
	}
	ids, err := s.q.StaleTags(ctx, location)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return s.q.PriceTagItems(ctx, store.PriceTagItemsParams{LocationID: location, VariantIds: ids})
}

// Source — что печатать: выбранные варианты или все строки документа.
type Source struct {
	Variants []uuid.UUID
	Document *uuid.UUID
}

func (s *Service) Print(ctx context.Context, location uuid.UUID, src Source, format forms.Format) ([]byte, error) {
	if err := auth.CheckLocation(ctx, apispec.PermissionFloorView, location); err != nil {
		return nil, err
	}
	ids := src.Variants
	if src.Document != nil {
		d, err := s.q.DocumentLocation(ctx, *src.Document)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if d.LocationID != location {
			return nil, ErrNotFound
		}
		if ids, err = s.q.DocumentVariants(ctx, *src.Document); err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 {
		return nil, ErrEmpty
	}
	items, err := s.q.PriceTagItems(ctx, store.PriceTagItemsParams{LocationID: location, VariantIds: ids})
	if err != nil {
		return nil, err
	}
	tags := make([]forms.PriceTag, 0, len(items))
	printed := make([]uuid.UUID, 0, len(items))
	now := time.Now()
	for _, it := range items {
		// Без розничной цены ценник не печатается: на нём нечего показать покупателю.
		if it.PriceKopecks == 0 {
			continue
		}
		tags = append(tags, Tag(it, now))
		printed = append(printed, it.ID)
	}
	if len(tags) == 0 {
		return nil, ErrEmpty
	}
	pdf, err := forms.PriceTagsPDF(format, tags)
	if err != nil {
		return nil, err
	}
	sess, _ := auth.FromContext(ctx)
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.q.WithTx(tx).RecordPrint(ctx, store.RecordPrintParams{LocationID: location, VariantIds: printed, PrintedBy: &sess.UserID})
	})
	return pdf, err
}

// Tag собирает ценник: цена за литр — для фасованных по объёму товаров.
func Tag(it Item, date time.Time) forms.PriceTag {
	t := forms.PriceTag{
		Name: it.Name, Pack: catalog.PackLabel(it.VolumeMl, it.NetWeightG, it.Container, it.GiftPack),
		PriceKopeck: it.PriceKopecks, Barcode: it.Barcode, Article: it.Article, Date: date,
	}
	if it.Abv != nil {
		t.Abv = strings.Replace(fmt.Sprintf("%g %%", *it.Abv), ".", ",", 1)
	}
	if it.VolumeMl != nil && *it.VolumeMl > 0 {
		t.PerLiterKopeck = (it.PriceKopecks*1000 + int64(*it.VolumeMl)/2) / int64(*it.VolumeMl)
	}
	return t
}
