package catalog

import (
	"context"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

func toAssortmentItem(a store.ListAssortmentRow) apispec.AssortmentItem {
	return apispec.AssortmentItem{
		VariantID: a.VariantID, ProductID: a.ProductID, Article: a.Article, Name: a.Name, VolumeMl: a.VolumeMl, NetWeightG: a.NetWeightG,
		Container: (*apispec.Container)(a.Container), GiftPack: a.GiftPack, IsActive: a.IsActive, MinQty: a.MinQty,
	}
}

func (h *Handler) ListAssortment(ctx context.Context, req apispec.ListAssortmentRequestObject) (apispec.ListAssortmentResponseObject, error) {
	p := req.Params
	limit := defaultProductPage
	if p.Limit != nil {
		limit = min(max(*p.Limit, 1), maxProductPage)
	}
	f := AssortmentFilter{After: p.After, Limit: int32(limit + 1)}
	if p.Q != nil {
		f.Search = *p.Q
	}
	rows, total, err := h.svc.Assortment(ctx, req.ID, f)
	if err != nil {
		return nil, problem(err)
	}

	// Запрошена лишняя строка: если она пришла, есть следующая страница.
	page := apispec.ListAssortment200JSONResponse{
		Items: make([]apispec.AssortmentItem, 0, min(len(rows), limit)), Total: int(total),
	}
	for i, r := range rows {
		if i == limit {
			page.NextAfter = &page.Items[limit-1].VariantID
			break
		}
		page.Items = append(page.Items, toAssortmentItem(r))
	}
	return page, nil
}

func (h *Handler) SetAssortmentItem(ctx context.Context, req apispec.SetAssortmentItemRequestObject) (apispec.SetAssortmentItemResponseObject, error) {
	if err := h.svc.SetAssortmentItem(ctx, req.ID, req.VariantID, req.Body.MinQty); err != nil {
		return nil, problem(err)
	}
	return apispec.SetAssortmentItem204Response{}, nil
}

func (h *Handler) RemoveAssortmentItem(ctx context.Context, req apispec.RemoveAssortmentItemRequestObject) (apispec.RemoveAssortmentItemResponseObject, error) {
	if err := h.svc.RemoveAssortmentItem(ctx, req.ID, req.VariantID); err != nil {
		return nil, problem(err)
	}
	return apispec.RemoveAssortmentItem204Response{}, nil
}

func (h *Handler) CopyAssortment(ctx context.Context, req apispec.CopyAssortmentRequestObject) (apispec.CopyAssortmentResponseObject, error) {
	added, err := h.svc.CopyAssortment(ctx, req.ID, req.Body.FromLocationID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CopyAssortment200JSONResponse{Added: int(added)}, nil
}
