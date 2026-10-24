package catalog

import (
	"context"
	"errors"
	"strings"
	"time"

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
// «Честного знака» — GTIN, штрихкод ищется среди штрихкодов и GTIN, артикул — среди артикулов
// вариантов. Код, который разбор не узнал, всё равно ищется как штрихкод: у своих этикеток
// бывают короткие номера.
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
		params.Article = &scan.Code
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
	product, err := s.q.GetProduct(ctx, found.ProductID)
	if err != nil {
		return r, err
	}
	variants, err := variantsOf(ctx, s.q, found.ProductID)
	if err != nil {
		return r, err
	}
	r.Product = &product
	for i := range variants {
		if variants[i].ID == found.ID {
			r.Variant = &variants[i]
		}
	}
	return r, nil
}

// FindVariant находит вариант по разобранному коду: по алкокоду марки, GTIN кода «Честного знака»,
// штрихкоду или артикулу. Марка нового образца алкокода не содержит — для неё результат пустой.
func (s *Service) FindVariant(ctx context.Context, scan Scan) (*uuid.UUID, error) {
	var params store.FindVariantByCodeParams
	switch scan.Kind {
	case apispec.ScanKindEgaisMarkNew:
		return nil, nil
	case apispec.ScanKindArticle:
		params.Article = &scan.Code
	case apispec.ScanKindEgaisMark, apispec.ScanKindAlcocode:
		params.Alcocode = &scan.Code
	case apispec.ScanKindCrptCode:
		params.Gtin = &scan.Code
	default:
		params.Code = &scan.Code
		params.Gtin, _ = normalizeGTIN(&scan.Code)
	}
	found, err := s.q.FindVariantByCode(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &found.ID, nil
}

// PurchasePrices — справочные закупочные цены вариантов на момент at, в копейках.
// Вариантов без закупочной цены в ответе нет.
func (s *Service) PurchasePrices(ctx context.Context, variantIDs []uuid.UUID, at time.Time) (map[uuid.UUID]int64, error) {
	rows, err := s.q.CurrentPurchasePrices(ctx, store.CurrentPurchasePricesParams{VariantIds: variantIDs, At: at})
	if err != nil {
		return nil, err
	}
	prices := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		prices[r.VariantID] = r.Kopecks
	}
	return prices, nil
}
