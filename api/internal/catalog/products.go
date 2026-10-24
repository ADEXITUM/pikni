package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

var (
	ErrConflict         = errors.New("record was changed by someone else")
	ErrInvalidAbv       = errors.New("ABV must be from 0 to 100")
	ErrAbvRequired      = errors.New("alcohol needs ABV")
	ErrNotAlcoholic     = errors.New("kind and ABV are only for alcohol categories")
	ErrProductExists    = errors.New("producer already has a product with this name")
	ErrInactiveCategory = errors.New("category is archived")
	ErrLineNeedsBrand   = errors.New("product line needs a brand")
	ErrBrandMismatch    = errors.New("brand belongs to another producer")
	ErrLineMismatch     = errors.New("product line belongs to another brand")
	ErrRegionMismatch   = errors.New("origin region belongs to another country")
)

type ProductFilter struct {
	Search              string
	CategoryID, BrandID *uuid.UUID
	After               *uuid.UUID
	Limit               int32
}

// Products ищет позиции (НСИ-8). Если поисковая строка похожа на код или скан, второе значение
// говорит, как она понята: что именно искали и в каком поле.
func (s *Service) Products(ctx context.Context, f ProductFilter) ([]store.ProductCard, *Scan, error) {
	params := store.ListProductsParams{After: f.After, CategoryID: f.CategoryID, BrandID: f.BrandID, MaxRows: f.Limit}
	q := strings.TrimSpace(f.Search)
	if q == "" {
		rows, err := s.q.ListProducts(ctx, params)
		return rows, nil, err
	}

	search := parseSearch(q)
	if search.Nothing {
		return []store.ProductCard{}, search.Scan, nil
	}
	params.Searching = true
	params.Article, params.Words, params.Code = search.Article, search.Words, search.Code
	params.Alcocode, params.Gtin = search.Alcocode, search.Gtin
	rows, err := s.q.ListProducts(ctx, params)
	return rows, search.Scan, err
}

// codeSearch — условия поиска по строке: часть артикула и слова наименования либо код варианта,
// если строка похожа на скан. Общие для каталога и ассортиментной матрицы.
type codeSearch struct {
	Article, Words, Alcocode, Gtin, Code *string
	Scan                                 *Scan
	// Nothing — марка нового образца: алкокода в ней нет, товар по ней находится только по
	// документам приёмки.
	Nothing bool
}

func parseSearch(q string) codeSearch {
	scan, ok := parseScan(q)
	if !ok {
		return codeSearch{Article: likePattern(q), Words: tsquery(q)}
	}
	s := codeSearch{Scan: &scan}
	switch scan.Kind {
	case apispec.ScanKindEgaisMarkNew:
		s.Nothing = true
	case apispec.ScanKindArticle:
		s.Article = likePattern(scan.Code)
	case apispec.ScanKindEgaisMark, apispec.ScanKindAlcocode:
		s.Alcocode = &scan.Code
	case apispec.ScanKindCrptCode:
		s.Gtin = &scan.Code
	default:
		s.Code = &scan.Code
		s.Gtin, _ = normalizeGTIN(&scan.Code)
	}
	// Цифры могут оказаться и частью артикула.
	if scan.Kind == apispec.ScanKindBarcode || scan.Kind == apispec.ScanKindAlcocode {
		s.Article = likePattern(q)
	}
	return s
}

func (s *Service) Product(ctx context.Context, id uuid.UUID) (store.ProductCard, error) {
	p, err := s.q.GetProduct(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Service) CreateProduct(ctx context.Context, f apispec.ProductFields) (store.ProductCard, error) {
	var card store.ProductCard
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		p, _, err := productParams(ctx, q, f, nil)
		if err != nil {
			return err
		}
		id, err := q.CreateProduct(ctx, p)
		if err != nil {
			return productWriteError(err)
		}
		if card, err = q.GetProduct(ctx, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionProductCreated, EntityType: audit.EntityProduct, EntityID: id.String(),
			Data: map[string]any{"name": card.Name, "category": card.CategoryName},
		})
	})
	return card, err
}

// UpdateProduct сохраняет правку, начатую с версии version. Если позицию за это время
// изменил кто-то другой, правка отклоняется, чтобы не затереть чужие изменения.
func (s *Service) UpdateProduct(ctx context.Context, id uuid.UUID, f apispec.ProductFields, isActive bool, version int32) (store.ProductCard, error) {
	var card store.ProductCard
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		current, err := q.LockProductVersion(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current != version {
			return ErrConflict
		}
		before, err := q.GetProduct(ctx, id)
		if err != nil {
			return err
		}
		p, names, err := productParams(ctx, q, f, &before)
		if err != nil {
			return err
		}
		err = q.UpdateProduct(ctx, store.UpdateProductParams{
			ID: id, Name: p.Name, CategoryID: p.CategoryID, KindCode: p.KindCode, Abv: p.Abv,
			CrptMarked: p.CrptMarked, ProducerID: p.ProducerID, BrandID: p.BrandID, ProductLineID: p.ProductLineID,
			Variety: p.Variety, CountryCode: p.CountryCode, OriginRegionID: p.OriginRegionID,
			Attributes: p.Attributes, IsActive: isActive,
		})
		if err != nil {
			return productWriteError(err)
		}
		if card, err = q.GetProduct(ctx, id); err != nil {
			return err
		}
		changes := audit.Changes(productFields(before, names), productFields(card, names))
		if len(changes) == 0 {
			return nil
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionProductUpdated, EntityType: audit.EntityProduct, EntityID: id.String(),
			Data: map[string]any{"name": card.Name, "changes": changes},
		})
	})
	return card, err
}

// productParams проверяет поля карточки. old — карточка до правки, nil при создании.
// Вторым значением возвращает названия характеристик для журнала.
func productParams(ctx context.Context, q *store.Queries, f apispec.ProductFields, old *store.ProductCard) (store.CreateProductParams, map[string]string, error) {
	var p store.CreateProductParams
	n, err := name(f.Name)
	if err != nil {
		return p, nil, err
	}
	var abv *float64
	if f.Abv != nil {
		if math.IsNaN(*f.Abv) || *f.Abv < 0 || *f.Abv > 100 {
			return p, nil, ErrInvalidAbv
		}
		v := math.Round(*f.Abv*10) / 10
		abv = &v
	}
	kind := optional(f.KindCode)
	if kind != nil && abv == nil {
		return p, nil, ErrAbvRequired
	}
	if f.ProductLineID != nil && f.BrandID == nil {
		return p, nil, ErrLineNeedsBrand
	}

	// Архивная категория остаётся у тех, кто уже в ней, но новых позиций не принимает.
	if old == nil || old.CategoryID != f.CategoryID {
		active, err := q.CategoryIsActive(ctx, f.CategoryID)
		if errors.Is(err, pgx.ErrNoRows) {
			return p, nil, ErrUnknownReference
		}
		if err != nil {
			return p, nil, err
		}
		if !active {
			return p, nil, ErrInactiveCategory
		}
	}
	if kind != nil || abv != nil {
		alcohol, err := q.IsAlcoholCategory(ctx, f.CategoryID)
		if err != nil {
			return p, nil, err
		}
		if !alcohol {
			return p, nil, ErrNotAlcoholic
		}
	}
	schema, err := q.CategorySchema(ctx, f.CategoryID)
	if err != nil {
		return p, nil, err
	}
	var before map[string]any
	names := map[string]string{}
	if old != nil {
		_ = json.Unmarshal(old.Attributes, &before)
		if old.CategoryID != f.CategoryID {
			oldSchema, err := q.CategorySchema(ctx, old.CategoryID)
			if err != nil {
				return p, nil, err
			}
			for _, a := range oldSchema {
				names[a.Code] = a.Name
			}
		}
	}
	for _, a := range schema {
		names[a.Code] = a.Name
	}
	attributes, err := checkAttributes(schema, before, f.Attributes)
	if err != nil {
		return p, nil, err
	}

	return store.CreateProductParams{
		Name: n, CategoryID: f.CategoryID, KindCode: kind, Abv: abv, CrptMarked: f.CrptMarked,
		ProducerID: f.ProducerID, BrandID: f.BrandID, ProductLineID: f.ProductLineID, Variety: optional(f.Variety),
		CountryCode: f.CountryCode, OriginRegionID: f.OriginRegionID, Attributes: attributes,
	}, names, nil
}

func optional(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	v := strings.TrimSpace(*s)
	return &v
}

// Связь бренда с производителем, линейки с брендом и региона со страной проверяют составные
// внешние ключи; здесь их нарушение переводится в понятную ошибку.
func productWriteError(err error) error {
	switch {
	case db.IsUniqueViolationOn(err, "products_producer_name_key"):
		return ErrProductExists
	case db.IsForeignKeyViolationOn(err, "products_brand_fkey"):
		return ErrBrandMismatch
	case db.IsForeignKeyViolationOn(err, "products_line_fkey"):
		return ErrLineMismatch
	case db.IsForeignKeyViolationOn(err, "products_region_fkey"):
		return ErrRegionMismatch
	case db.IsForeignKeyViolation(err):
		return ErrUnknownReference
	}
	return fmt.Errorf("write product: %w", err)
}

func productFields(p store.ProductCard, names map[string]string) map[string]any {
	return map[string]any{
		"name": p.Name, "category": p.CategoryName, "kindCode": p.KindCode, "abv": p.Abv,
		"crptMarked": p.CrptMarked, "producer": p.ProducerName, "brand": p.BrandName, "line": p.LineName,
		"variety": p.Variety, "country": p.CountryName, "region": p.RegionName,
		"attributes": describeAttributes(p.Attributes, names), "isActive": p.IsActive,
	}
}

func (s *Service) ProductLines(ctx context.Context) ([]store.ProductLine, error) {
	return s.q.ListProductLines(ctx)
}

// SaveProductLine создаёт линейку или меняет её название и архив; бренд линейки не меняется.
func (s *Service) SaveProductLine(ctx context.Context, id *uuid.UUID, brandID uuid.UUID, lineName string, active bool) (store.ProductLine, error) {
	n, err := name(lineName)
	if err != nil {
		return store.ProductLine{}, err
	}
	fields := func(l store.ProductLine) map[string]any {
		return map[string]any{"name": l.Name, "brandId": l.BrandID, "isActive": l.IsActive}
	}

	var saved store.ProductLine
	err = s.write(ctx, "product_line", func(q *store.Queries) (change, error) {
		if id == nil {
			l, err := q.CreateProductLine(ctx, store.CreateProductLineParams{BrandID: brandID, Name: n})
			saved = l
			return change{id: l.ID.String(), after: fields(l)}, err
		}
		old, err := q.GetProductLine(ctx, *id)
		if err != nil {
			return change{}, err
		}
		saved, err = q.UpdateProductLine(ctx, store.UpdateProductLineParams{ID: *id, Name: n, IsActive: active})
		return change{id: id.String(), before: fields(old), after: fields(saved)}, err
	})
	return saved, err
}
