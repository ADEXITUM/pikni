package catalog

import (
	"context"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
)

func toMinPrice(r MinPriceRule) apispec.MinPrice {
	return apispec.MinPrice{
		ID: r.ID, KindCode: r.KindCode, ValidFrom: openapi_types.Date{Time: r.ValidFrom}, Price: *toRubles(&r.Kopecks),
		VolumeMl: r.VolumeMl, Abv: r.Abv, AbvOver: r.AbvOver, Source: r.Source,
	}
}

func (h *Handler) ListMinPrices(ctx context.Context, _ apispec.ListMinPricesRequestObject) (apispec.ListMinPricesResponseObject, error) {
	rows, err := h.svc.MinPriceRules(ctx)
	items, err := list(rows, err, toMinPrice)
	if err != nil {
		return nil, err
	}
	return apispec.ListMinPrices200JSONResponse{Items: items}, nil
}

func (h *Handler) CreateMinPrice(ctx context.Context, req apispec.CreateMinPriceRequestObject) (apispec.CreateMinPriceResponseObject, error) {
	b := req.Body
	r, err := h.svc.CreateMinPriceRule(ctx, MinPriceRuleSpec{
		KindCode: b.KindCode, ValidFrom: b.ValidFrom.Time, Kopecks: toKopecks(b.Price), VolumeMl: b.VolumeMl,
		Abv: b.Abv, AbvOver: b.AbvOver, Source: b.Source,
	})
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateMinPrice201JSONResponse(toMinPrice(r)), nil
}

func (h *Handler) DeleteMinPrice(ctx context.Context, req apispec.DeleteMinPriceRequestObject) (apispec.DeleteMinPriceResponseObject, error) {
	if err := h.svc.DeleteMinPriceRule(ctx, req.ID); err != nil {
		return nil, problem(err)
	}
	return apispec.DeleteMinPrice204Response{}, nil
}
