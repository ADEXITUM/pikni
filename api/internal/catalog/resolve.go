package catalog

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

// Resolution — как понят считанный код и какой вариант ему соответствует (СК-9).
type Resolution struct {
	Scan    *Scan
	Product *store.ProductCard
	Variant *Variant
}

// Resolve находит вариант по считанному коду: марка старого образца даёт алкокод, код
// «Честного знака» — GTIN, штрихкод ищется среди штрихкодов и GTIN. Код, который разбор не
// узнал, всё равно ищется как штрихкод: у своих этикеток бывают короткие номера. Артикул,
// набранный вручную, находит позицию; вариант известен, только если он у позиции один.
func (s *Service) Resolve(ctx context.Context, raw string) (Resolution, error) {
	var r Resolution
	var params store.FindVariantByCodeParams
	scan, ok := parseScan(raw)
	switch {
	case !ok:
		code := strings.TrimSpace(raw)
		if code == "" {
			return r, nil
		}
		params.Code = &code
	case scan.Kind == apispec.ScanKindEgaisMarkNew:
		// Алкокода в марке нового образца нет: найти товар можно только по учёту марок.
		return Resolution{Scan: &scan}, nil
	case scan.Kind == apispec.ScanKindArticle:
		id, err := s.q.FindProductByArticle(ctx, scan.Code)
		if errors.Is(err, pgx.ErrNoRows) {
			return Resolution{Scan: &scan}, nil
		}
		if err != nil {
			return r, err
		}
		return s.resolved(ctx, &scan, id, nil)
	case scan.Kind == apispec.ScanKindEgaisMark, scan.Kind == apispec.ScanKindAlcocode:
		params.Alcocode = &scan.Code
	case scan.Kind == apispec.ScanKindCrptCode:
		params.Gtin = &scan.Code
	default:
		params.Code = &scan.Code
		params.Gtin, _ = normalizeGTIN(&scan.Code)
	}
	if ok {
		r.Scan = &scan
	}

	found, err := s.q.FindVariantByCode(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	return s.resolved(ctx, r.Scan, found.ProductID, &found.ID)
}

// resolved дополняет найденную позицию карточкой и вариантом. Без variantID вариант берётся,
// только если он у позиции единственный.
func (s *Service) resolved(ctx context.Context, scan *Scan, productID uuid.UUID, variantID *uuid.UUID) (Resolution, error) {
	r := Resolution{Scan: scan}
	product, err := s.q.GetProduct(ctx, productID)
	if err != nil {
		return r, err
	}
	variants, err := variantsOf(ctx, s.q, productID)
	if err != nil {
		return r, err
	}
	r.Product = &product
	for i := range variants {
		if variantID != nil && variants[i].ID == *variantID || variantID == nil && len(variants) == 1 {
			r.Variant = &variants[i]
		}
	}
	return r, nil
}
