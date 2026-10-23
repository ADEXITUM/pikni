package catalog

import (
	"context"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

func toMinPrice(r MinPriceRule) apispec.MinPrice {
	return apispec.MinPrice{
		ID: r.ID, GroupCode: r.GroupCode, GroupName: r.GroupName, ValidFrom: openapi_types.Date{Time: r.ValidFrom},
		AbvFrom: r.AbvFrom, AbvTo: r.AbvTo, Price: *toRubles(&r.Kopecks), VolumeMl: r.VolumeMl,
		SamePriceOverMl: r.SamePriceOverMl, Source: r.Source,
	}
}

func (h *Handler) ListMinPrices(ctx context.Context, _ apispec.ListMinPricesRequestObject) (apispec.ListMinPricesResponseObject, error) {
	rules, groups, err := h.svc.MinPriceRules(ctx)
	items, err := list(rules, err, toMinPrice)
	names, err := list(groups, err, func(g store.MinPriceGroup) apispec.MinPriceGroup {
		return apispec.MinPriceGroup{Code: g.Code, Name: g.Name}
	})
	if err != nil {
		return nil, err
	}
	return apispec.ListMinPrices200JSONResponse{Items: items, Groups: names}, nil
}

func (h *Handler) CreateMinPrice(ctx context.Context, req apispec.CreateMinPriceRequestObject) (apispec.CreateMinPriceResponseObject, error) {
	b := req.Body
	r, err := h.svc.CreateMinPriceRule(ctx, MinPriceRuleSpec{
		GroupCode: b.GroupCode, ValidFrom: b.ValidFrom.Time, AbvFrom: b.AbvFrom, AbvTo: b.AbvTo,
		Kopecks: toKopecks(b.Price), VolumeMl: b.VolumeMl, SamePriceOverMl: b.SamePriceOverMl, Source: b.Source,
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
