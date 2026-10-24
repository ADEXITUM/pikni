package stock

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func problem(err error) error {
	if errors.Is(err, ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "Вариант или зона не найдены")
	}
	return err
}

func limitOf(l *int) int32 {
	if l == nil {
		return 50
	}
	return int32(*l)
}

// Суммы в базе — копейки и десятитысячные доли рубля, в API — рубли.
func rubles(units int64, scale float64) *float64 {
	v := float64(units) / scale
	return &v
}

func (h *Handler) ListStockBalances(ctx context.Context, req apispec.ListStockBalancesRequestObject) (apispec.ListStockBalancesResponseObject, error) {
	p := req.Params
	limit := limitOf(p.Limit)
	search := ""
	if p.Q != nil {
		search = *p.Q
	}
	rows, err := h.svc.Balances(ctx, BalanceFilter{
		// В точке не больше двух зон: строк на страницу нужно вдвое больше, чем вариантов.
		LocationID: p.LocationID, ZoneID: p.ZoneID, Search: search, After: p.After, Limit: 2 * (limit + 1),
	})
	if err != nil {
		return nil, problem(err)
	}
	showCost := auth.Has(ctx, apispec.PermissionCostView)
	out := apispec.StockBalanceList{Items: []apispec.StockBalance{}}
	// Страница — по вариантам: строки одного варианта в разных зонах не разрываются между страницами.
	variants := 0
	for i, r := range rows {
		if i == 0 || rows[i-1].VariantID != r.VariantID {
			variants++
			if variants > int(limit) {
				out.NextAfter = &rows[i-1].VariantID
				break
			}
		}
		b := apispec.StockBalance{
			VariantID: r.VariantID, ProductID: r.ProductID, Article: r.Article, Name: r.Name,
			VolumeMl: r.VolumeMl, NetWeightG: r.NetWeightG, GiftPack: r.GiftPack, ZoneID: r.ZoneID,
			ZoneKind: apispec.ZoneKind(r.ZoneKind), OnHand: r.OnHand, Reserved: r.Reserved,
			InTransit: r.InTransit, Deficit: r.Deficit,
		}
		if r.Container != nil {
			c := apispec.Container(*r.Container)
			b.Container = &c
		}
		if showCost && r.AvgCostE4 > 0 {
			b.AvgCost = rubles(r.AvgCostE4, 10000)
		}
		out.Items = append(out.Items, b)
	}
	return apispec.ListStockBalances200JSONResponse(out), nil
}

func (h *Handler) ListStockMoves(ctx context.Context, req apispec.ListStockMovesRequestObject) (apispec.ListStockMovesResponseObject, error) {
	p := req.Params
	limit := limitOf(p.Limit)
	header, rows, err := h.svc.Moves(ctx, p.VariantID, p.ZoneID, p.Before, limit+1)
	if err != nil {
		return nil, problem(err)
	}
	showCost := auth.Has(ctx, apispec.PermissionCostView)
	out := apispec.StockMoveList{
		Items: []apispec.StockMove{}, Article: header.Article, Name: header.Name, VolumeMl: header.VolumeMl,
		NetWeightG: header.NetWeightG, GiftPack: header.GiftPack, LocationName: header.LocationName,
		ZoneKind: apispec.ZoneKind(header.ZoneKind),
	}
	if header.Container != nil {
		c := apispec.Container(*header.Container)
		out.Container = &c
	}
	if len(rows) > int(limit) {
		rows = rows[:limit]
		out.NextBefore = &rows[len(rows)-1].ID
	}
	for _, r := range rows {
		m := apispec.StockMove{
			ID: r.ID, PostedAt: r.PostedAt, DocDate: openapi_types.Date{Time: r.DocDate}, DocumentID: r.DocumentID,
			DocumentNumber: r.DocumentNumber, DocumentKind: apispec.DocumentKind(r.DocumentKind),
			Bucket: apispec.StockBucket(r.Bucket), Qty: r.Qty, OnHandAfter: r.OnHandAfter,
		}
		if showCost {
			m.CostAmount = rubles(r.CostKopecks, 100)
		}
		out.Items = append(out.Items, m)
	}
	return apispec.ListStockMoves200JSONResponse(out), nil
}
