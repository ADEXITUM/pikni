package catalog

import (
	"context"
	"math"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

// В API цена — рубли с копейками, в сервисе — целые копейки.
func toRubles(kopecks *int64) *float64 {
	if kopecks == nil {
		return nil
	}
	r := float64(*kopecks) / 100
	return &r
}

func toKopecks(rubles float64) int64 {
	if math.IsNaN(rubles) || math.IsInf(rubles, 0) {
		return 0
	}
	return int64(math.Round(rubles * 100))
}

func toPriceList(rows []store.ListVariantPricesRow) apispec.PriceList {
	items := make([]apispec.Price, len(rows))
	for i, p := range rows {
		items[i] = apispec.Price{
			ID: p.ID, Kind: apispec.PriceKind(p.Kind), Price: *toRubles(&p.Kopecks), ValidFrom: p.ValidFrom, SetBy: p.SetBy,
		}
	}
	return apispec.PriceList{Items: items}
}

func toPrice(p store.CreatePriceRow) apispec.Price {
	return apispec.Price{ID: p.ID, Kind: apispec.PriceKind(p.Kind), Price: *toRubles(&p.Kopecks), ValidFrom: p.ValidFrom}
}

func (h *Handler) setPrice(ctx context.Context, variantID uuid.UUID, kind apispec.PriceKind, b *apispec.NewPrice) (apispec.Price, error) {
	var setBy *uuid.UUID
	if sess, ok := auth.FromContext(ctx); ok {
		setBy = &sess.UserID
	}
	p, err := h.svc.SetPrice(ctx, variantID, kind, toKopecks(b.Price), b.ValidFrom, setBy)
	if err != nil {
		return apispec.Price{}, problem(err)
	}
	return toPrice(p), nil
}

func (h *Handler) ListRetailPrices(ctx context.Context, req apispec.ListRetailPricesRequestObject) (apispec.ListRetailPricesResponseObject, error) {
	rows, err := h.svc.Prices(ctx, req.ID, apispec.PriceKindRetail)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.ListRetailPrices200JSONResponse(toPriceList(rows)), nil
}

func (h *Handler) ListPurchasePrices(ctx context.Context, req apispec.ListPurchasePricesRequestObject) (apispec.ListPurchasePricesResponseObject, error) {
	rows, err := h.svc.Prices(ctx, req.ID, apispec.PriceKindPurchase)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.ListPurchasePrices200JSONResponse(toPriceList(rows)), nil
}

func (h *Handler) SetRetailPrice(ctx context.Context, req apispec.SetRetailPriceRequestObject) (apispec.SetRetailPriceResponseObject, error) {
	p, err := h.setPrice(ctx, req.ID, apispec.PriceKindRetail, req.Body)
	if err != nil {
		return nil, err
	}
	return apispec.SetRetailPrice201JSONResponse(p), nil
}

func (h *Handler) SetPurchasePrice(ctx context.Context, req apispec.SetPurchasePriceRequestObject) (apispec.SetPurchasePriceResponseObject, error) {
	p, err := h.setPrice(ctx, req.ID, apispec.PriceKindPurchase, req.Body)
	if err != nil {
		return nil, err
	}
	return apispec.SetPurchasePrice201JSONResponse(p), nil
}

func (h *Handler) DeletePrice(ctx context.Context, req apispec.DeletePriceRequestObject) (apispec.DeletePriceResponseObject, error) {
	if err := h.svc.DeletePrice(ctx, req.ID); err != nil {
		return nil, problem(err)
	}
	return apispec.DeletePrice204Response{}, nil
}
