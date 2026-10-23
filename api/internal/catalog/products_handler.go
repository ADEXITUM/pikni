package catalog

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

const (
	defaultProductPage = 50
	maxProductPage     = 200
)

func toProduct(p store.ProductCard) apispec.Product {
	attributes := map[string]any{}
	_ = json.Unmarshal(p.Attributes, &attributes)
	var accounting *apispec.EgaisAccounting
	if p.EgaisAccounting != nil {
		a := apispec.EgaisAccounting(*p.EgaisAccounting)
		accounting = &a
	}
	return apispec.Product{
		ID: p.ID, Article: p.Article, Name: p.Name, CategoryID: p.CategoryID, CategoryName: p.CategoryName,
		KindCode: p.KindCode, EgaisAccounting: accounting, Abv: p.Abv, CrptMarked: p.CrptMarked,
		ProducerID: p.ProducerID, ProducerName: p.ProducerName, BrandID: p.BrandID, BrandName: p.BrandName,
		ProductLineID: p.ProductLineID, LineName: p.LineName, Variety: p.Variety, CountryCode: p.CountryCode,
		CountryName: p.CountryName, OriginRegionID: p.OriginRegionID, RegionName: p.RegionName,
		Attributes: attributes, IsActive: p.IsActive, Version: p.Version,
	}
}

func (h *Handler) ListProducts(ctx context.Context, req apispec.ListProductsRequestObject) (apispec.ListProductsResponseObject, error) {
	p := req.Params
	limit := defaultProductPage
	if p.Limit != nil {
		limit = min(max(*p.Limit, 1), maxProductPage)
	}
	f := ProductFilter{CategoryID: p.CategoryID, BrandID: p.BrandID, After: p.After, Limit: int32(limit + 1)}
	if p.Q != nil {
		f.Search = *p.Q
	}
	rows, scan, err := h.svc.Products(ctx, f)
	if err != nil {
		return nil, err
	}

	// Запрошена лишняя строка: если она пришла, есть следующая страница.
	page := apispec.ListProducts200JSONResponse{Items: make([]apispec.Product, 0, min(len(rows), limit))}
	if scan != nil {
		page.Scan = &apispec.ScanResult{Kind: scan.Kind, Code: scan.Code}
	}
	for i, r := range rows {
		if i == limit {
			page.NextAfter = &page.Items[limit-1].ID
			break
		}
		page.Items = append(page.Items, toProduct(r))
	}
	return page, nil
}

func (h *Handler) GetProduct(ctx context.Context, req apispec.GetProductRequestObject) (apispec.GetProductResponseObject, error) {
	p, err := h.svc.Product(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.GetProduct200JSONResponse(toProduct(p)), nil
}

func (h *Handler) CreateProduct(ctx context.Context, req apispec.CreateProductRequestObject) (apispec.CreateProductResponseObject, error) {
	p, err := h.svc.CreateProduct(ctx, *req.Body)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateProduct201JSONResponse(toProduct(p)), nil
}

func (h *Handler) UpdateProduct(ctx context.Context, req apispec.UpdateProductRequestObject) (apispec.UpdateProductResponseObject, error) {
	b := req.Body
	f := apispec.ProductFields{
		Name: b.Name, CategoryID: b.CategoryID, KindCode: b.KindCode, Abv: b.Abv, CrptMarked: b.CrptMarked,
		ProducerID: b.ProducerID, BrandID: b.BrandID, ProductLineID: b.ProductLineID, Variety: b.Variety,
		CountryCode: b.CountryCode, OriginRegionID: b.OriginRegionID, Attributes: b.Attributes,
	}
	p, err := h.svc.UpdateProduct(ctx, req.ID, f, b.IsActive, b.Version)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateProduct200JSONResponse(toProduct(p)), nil
}

func toProductLine(l store.ProductLine) apispec.ProductLine {
	return apispec.ProductLine{ID: l.ID, BrandID: l.BrandID, Name: l.Name, IsActive: l.IsActive}
}

func (h *Handler) ListProductLines(ctx context.Context, _ apispec.ListProductLinesRequestObject) (apispec.ListProductLinesResponseObject, error) {
	rows, err := h.svc.ProductLines(ctx)
	items, err := list(rows, err, toProductLine)
	if err != nil {
		return nil, err
	}
	return apispec.ListProductLines200JSONResponse{Items: items}, nil
}

func (h *Handler) CreateProductLine(ctx context.Context, req apispec.CreateProductLineRequestObject) (apispec.CreateProductLineResponseObject, error) {
	l, err := h.svc.SaveProductLine(ctx, nil, req.Body.BrandID, req.Body.Name, true)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateProductLine201JSONResponse(toProductLine(l)), nil
}

func (h *Handler) UpdateProductLine(ctx context.Context, req apispec.UpdateProductLineRequestObject) (apispec.UpdateProductLineResponseObject, error) {
	l, err := h.svc.SaveProductLine(ctx, &req.ID, uuid.Nil, req.Body.Name, req.Body.IsActive)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateProductLine200JSONResponse(toProductLine(l)), nil
}
