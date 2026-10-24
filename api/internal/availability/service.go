// Package availability — наличие по сети (НЛ-1 – НЛ-3, НЛ-6): поиск позиций с остатками по точкам,
// карточка «вариант × точка» и похожие товары. Модуль только читает каталог и регистр остатков.
package availability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/availability/store"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/marks"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrBadFilters = errors.New("bad attribute filter")
)

type Service struct {
	q     *store.Queries
	marks *marks.Service
}

func NewService(pool *pgxpool.Pool, mk *marks.Service) *Service {
	return &Service{q: store.New(pool), marks: mk}
}

// Filter — фильтры поиска. Цены — в копейках, Attrs — JSON характеристик из адреса страницы.
type Filter struct {
	Search                                  string
	CategoryID, BrandID, LocationID, Here   *uuid.UUID
	KindCode, CountryCode, City, LocationKd *string
	VolumeMl                                *int32
	AbvFrom, AbvTo                          *float64
	PriceFrom, PriceTo                      *int64
	InStock                                 bool
	Attrs                                   string
	Sort                                    string
	Offset, Limit                           int32
}

func (s *Service) Search(ctx context.Context, f Filter) ([]store.SearchAvailabilityRow, error) {
	p := store.SearchAvailabilityParams{
		CategoryID: f.CategoryID, KindCode: f.KindCode, BrandID: f.BrandID, CountryCode: f.CountryCode,
		VolumeMl: f.VolumeMl, AbvFrom: f.AbvFrom, AbvTo: f.AbvTo, PriceFrom: f.PriceFrom, PriceTo: f.PriceTo,
		City: f.City, LocationID: f.LocationID, LocationKind: f.LocationKd, Here: f.Here,
		InStock: f.InStock, Sort: f.Sort, RowOffset: f.Offset, MaxRows: f.Limit,
	}
	var err error
	if p.Attrs, p.Ranges, err = attrFilters(f.Attrs); err != nil {
		return nil, err
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		sf := catalog.ParseSearch(q)
		p.Searching = true
		p.Article, p.Words, p.Code, p.Alcocode, p.Gtin = sf.Article, sf.Words, sf.Code, sf.Alcocode, sf.Gtin
		// Марка нового образца без алкокода: товар находится по учёту марок сети — отсканировал
		// бутылку в зале и увидел, где ещё она есть.
		if sf.Scan != nil && sf.Scan.Mark != "" {
			m, err := s.marks.Find(ctx, sf.Scan.Mark)
			if err != nil {
				return nil, err
			}
			if m != nil {
				product, err := s.q.VariantProduct(ctx, m.VariantID)
				if err != nil {
					return nil, err
				}
				p.ProductID = &product
			} else if sf.Nothing {
				return nil, nil
			}
		}
	}
	return s.q.SearchAvailability(ctx, p)
}

// attrFilters делит фильтр характеристик на точные значения и диапазоны чисел.
func attrFilters(raw string) (exact, ranges []byte, err error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil, nil
	}
	var in map[string]any
	if json.Unmarshal([]byte(raw), &in) != nil {
		return nil, nil, ErrBadFilters
	}
	ex, rg := map[string]any{}, map[string]any{}
	for k, v := range in {
		switch t := v.(type) {
		case string, bool, float64:
			ex[k] = t
		case map[string]any:
			r := map[string]float64{}
			for _, bound := range []string{"min", "max"} {
				if b, ok := t[bound]; ok {
					n, ok := b.(float64)
					if !ok {
						return nil, nil, ErrBadFilters
					}
					r[bound] = n
				}
			}
			rg[k] = r
		default:
			return nil, nil, ErrBadFilters
		}
	}
	if len(ex) > 0 {
		exact, _ = json.Marshal(ex)
	}
	if len(rg) > 0 {
		ranges, _ = json.Marshal(rg)
	}
	return exact, ranges, nil
}

type Attribute struct{ Code, Name, Value string }

type Card struct {
	Product    store.AvailabilityProductRow
	Attributes []Attribute
	Variants   []store.AvailabilityVariantsRow
	Locations  []store.AvailabilityLocationsRow
	Cells      []store.AvailabilityCellsRow
}

func (s *Service) Card(ctx context.Context, product uuid.UUID) (Card, error) {
	var c Card
	var err error
	if c.Product, err = s.q.AvailabilityProduct(ctx, product); errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	} else if err != nil {
		return c, err
	}
	schema, err := s.q.AvailabilitySchema(ctx, c.Product.CategoryID)
	if err != nil {
		return c, err
	}
	c.Attributes = describe(schema, c.Product.Attributes)
	if c.Variants, err = s.q.AvailabilityVariants(ctx, product); err != nil {
		return c, err
	}
	if c.Locations, err = s.q.AvailabilityLocations(ctx); err != nil {
		return c, err
	}
	c.Cells, err = s.q.AvailabilityCells(ctx, product)
	return c, err
}

// describe — характеристики позиции для показа в порядке схемы категории: «Выдержка: 5 лет».
func describe(schema []store.AvailabilitySchemaRow, raw []byte) []Attribute {
	var values map[string]any
	_ = json.Unmarshal(raw, &values)
	out := []Attribute{}
	for _, a := range schema {
		v, ok := values[a.Code]
		if !ok || v == nil {
			continue
		}
		var text string
		switch t := v.(type) {
		case bool:
			text = map[bool]string{true: "да", false: "нет"}[t]
		case float64:
			text = strings.Replace(strconv.FormatFloat(t, 'f', -1, 64), ".", ",", 1)
		default:
			text = fmt.Sprint(t)
		}
		if a.Unit != nil && *a.Unit != "" {
			text += " " + *a.Unit
		}
		out = append(out, Attribute{Code: a.Code, Name: a.Name, Value: text})
	}
	return out
}

// Similar — три подборки похожих товаров для варианта (НЛ-6).
func (s *Service) Similar(ctx context.Context, variant uuid.UUID, here *uuid.UUID) (pack, line, analogs []store.SimilarProductsRow, err error) {
	rows, err := s.q.SimilarProducts(ctx, store.SimilarProductsParams{VariantID: variant, Here: here})
	if err != nil {
		return nil, nil, nil, err
	}
	pack, line, analogs = []store.SimilarProductsRow{}, []store.SimilarProductsRow{}, []store.SimilarProductsRow{}
	for _, r := range rows {
		switch {
		case r.Grp == "pack":
			pack = append(pack, r)
		case r.Grp == "line" && len(line) < 12:
			line = append(line, r)
		case r.Grp == "analog" && len(analogs) < 12:
			analogs = append(analogs, r)
		}
	}
	return pack, line, analogs, nil
}

func Kopecks(rub *float64) *int64 {
	if rub == nil {
		return nil
	}
	k := int64(math.Round(*rub * 100))
	return &k
}
