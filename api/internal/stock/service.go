package stock

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/stock/store"
)

var ErrNotFound = errors.New("not found")

// MarkFinder находит вариант по марке нового образца: в ней нет алкокода, товар известен только по учёту марок.
type MarkFinder interface {
	MarkVariant(ctx context.Context, code string) (*uuid.UUID, error)
}

type Service struct {
	pool  *pgxpool.Pool
	q     *store.Queries
	marks MarkFinder
}

func NewService(pool *pgxpool.Pool, marks MarkFinder) *Service {
	return &Service{pool: pool, q: store.New(pool), marks: marks}
}

// BalanceFilter — страница остатков точки: зона, поиск и вариант, после которого начинается страница.
type BalanceFilter struct {
	LocationID uuid.UUID
	ZoneID     *uuid.UUID
	Search     string
	After      *uuid.UUID
	Limit      int32
}

func (s *Service) Balances(ctx context.Context, f BalanceFilter) ([]store.ListBalancesRow, error) {
	params := store.ListBalancesParams{LocationID: f.LocationID, ZoneID: f.ZoneID, After: f.After, MaxRows: f.Limit}
	if q := strings.TrimSpace(f.Search); q != "" {
		search := catalog.ParseSearch(q)
		params.Searching = true
		params.Article, params.Words, params.Code = search.Article, search.Words, search.Code
		params.Alcocode, params.Gtin = search.Alcocode, search.Gtin
		if search.Nothing {
			// Марка нового образца: товар находится по учёту марок.
			variant, err := s.marks.MarkVariant(ctx, search.Scan.Mark)
			if err != nil {
				return nil, err
			}
			if variant == nil {
				return []store.ListBalancesRow{}, nil
			}
			params.VariantID = variant
		}
	}
	return s.q.ListBalances(ctx, params)
}

// Moves — карточка движения варианта по зоне (СО-8), страница до движения before.
func (s *Service) Moves(ctx context.Context, variantID, zoneID uuid.UUID, before *int64, limit int32) (store.MoveCardHeaderRow, []store.ListMovesRow, error) {
	header, err := s.q.MoveCardHeader(ctx, store.MoveCardHeaderParams{VariantID: variantID, ZoneID: zoneID})
	if errors.Is(err, pgx.ErrNoRows) {
		return header, nil, ErrNotFound
	}
	if err != nil {
		return header, nil, err
	}
	rows, err := s.q.ListMoves(ctx, store.ListMovesParams{VariantID: variantID, ZoneID: zoneID, Before: before, MaxRows: limit})
	return header, rows, err
}

// Problem — расхождение, найденное проверкой целостности.
type Problem struct {
	VariantID, ZoneID uuid.UUID
	Kind              string // on_hand, in_transit, deficit — остаток с суммой движений; marks — марки с остатком
	Expected, Actual  int64
}

// Integrity сверяет остатки с суммой движений и помарочный остаток с числом марок на месте (СО-7).
func (s *Service) Integrity(ctx context.Context) ([]Problem, error) {
	rows, err := s.pool.Query(ctx, `select variant_id, zone_id, problem, expected, actual from stock_integrity()`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Problem, error) {
		var p Problem
		err := r.Scan(&p.VariantID, &p.ZoneID, &p.Kind, &p.Expected, &p.Actual)
		return p, err
	})
}
