package catalog

import (
	"context"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

func toBarcode(b store.Barcode) apispec.Barcode {
	return apispec.Barcode{Code: b.Code, Kind: apispec.BarcodeKind(b.Kind)}
}

// Закупочная цена уходит только тем, у кого есть право cost.view.
func toVariant(ctx context.Context, v Variant) apispec.Variant {
	barcodes := make([]apispec.Barcode, len(v.Barcodes))
	for i, b := range v.Barcodes {
		barcodes[i] = toBarcode(b)
	}
	out := apispec.Variant{
		ID: v.ID, ProductID: v.ProductID, Article: v.Article, VolumeMl: v.VolumeMl, NetWeightG: v.NetWeightG, Container: (*apispec.Container)(v.Container),
		GiftPack: v.GiftPack, Alcocode: v.Alcocode, Gtin: v.Gtin, IsActive: v.IsActive, Barcodes: barcodes,
	}
	out.RetailPrice, out.MinPrice = toRubles(v.RetailPrice), toRubles(v.MinPrice)
	if auth.Has(ctx, apispec.PermissionCostView) {
		out.PurchasePrice = toRubles(v.PurchasePrice)
	}
	return out
}

func variantList(ctx context.Context, variants []Variant) apispec.VariantList {
	items := make([]apispec.Variant, len(variants))
	for i, v := range variants {
		items[i] = toVariant(ctx, v)
	}
	return apispec.VariantList{Items: items}
}

func (h *Handler) ListProductVariants(ctx context.Context, req apispec.ListProductVariantsRequestObject) (apispec.ListProductVariantsResponseObject, error) {
	variants, err := h.svc.Variants(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.ListProductVariants200JSONResponse(variantList(ctx, variants)), nil
}

func (h *Handler) CreateVariant(ctx context.Context, req apispec.CreateVariantRequestObject) (apispec.CreateVariantResponseObject, error) {
	b := req.Body
	v, err := h.svc.CreateVariant(ctx, req.ID, VariantSpec{
		VolumeMl: b.VolumeMl, NetWeightG: b.NetWeightG, Container: (*string)(b.Container), GiftPack: b.GiftPack, Alcocode: b.Alcocode, GTIN: b.Gtin,
	})
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateVariant201JSONResponse(toVariant(ctx, v)), nil
}

func (h *Handler) GenerateVariants(ctx context.Context, req apispec.GenerateVariantsRequestObject) (apispec.GenerateVariantsResponseObject, error) {
	b := req.Body
	containers := make([]string, len(b.Containers))
	for i, c := range b.Containers {
		containers[i] = string(c)
	}
	created, err := h.svc.GenerateVariants(ctx, req.ID, b.Volumes, containers, b.GiftPack)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.GenerateVariants201JSONResponse(variantList(ctx, created)), nil
}

func (h *Handler) UpdateVariant(ctx context.Context, req apispec.UpdateVariantRequestObject) (apispec.UpdateVariantResponseObject, error) {
	b := req.Body
	v, err := h.svc.UpdateVariant(ctx, req.ID, b.Alcocode, b.Gtin, b.IsActive)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateVariant200JSONResponse(toVariant(ctx, v)), nil
}

func (h *Handler) AddBarcode(ctx context.Context, req apispec.AddBarcodeRequestObject) (apispec.AddBarcodeResponseObject, error) {
	b, err := h.svc.AddBarcode(ctx, req.ID, req.Body.Code)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.AddBarcode201JSONResponse(toBarcode(b)), nil
}

func (h *Handler) DeleteBarcode(ctx context.Context, req apispec.DeleteBarcodeRequestObject) (apispec.DeleteBarcodeResponseObject, error) {
	if err := h.svc.DeleteBarcode(ctx, req.Code); err != nil {
		return nil, problem(err)
	}
	return apispec.DeleteBarcode204Response{}, nil
}
