package catalog

import (
	"context"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

func toCategory(c store.Category) apispec.Category {
	return apispec.Category{ID: c.ID, ParentID: c.ParentID, Name: c.Name, Depth: int32(c.Depth), Sort: c.Sort, IsActive: c.IsActive}
}

func toCategoryAttribute(a store.CategoryAttribute) apispec.CategoryAttribute {
	return apispec.CategoryAttribute{
		ID: a.ID, CategoryID: a.CategoryID, Code: a.Code, Name: a.Name, Type: apispec.AttributeType(a.Type),
		Unit: a.Unit, Options: options(a.Options), Filterable: a.Filterable, UsedForAnalogs: a.UsedForAnalogs,
		Sort: a.Sort, IsActive: a.IsActive,
	}
}

func (h *Handler) ListCategories(ctx context.Context, _ apispec.ListCategoriesRequestObject) (apispec.ListCategoriesResponseObject, error) {
	tree, err := h.svc.Categories(ctx)
	categories, err := list(tree.Categories, err, toCategory)
	attributes, err := list(tree.Attributes, err, toCategoryAttribute)
	if err != nil {
		return nil, err
	}
	return apispec.ListCategories200JSONResponse{Categories: categories, Attributes: attributes}, nil
}

func (h *Handler) CreateCategory(ctx context.Context, req apispec.CreateCategoryRequestObject) (apispec.CreateCategoryResponseObject, error) {
	c, err := h.svc.CreateCategory(ctx, *req.Body)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateCategory201JSONResponse(toCategory(c)), nil
}

func (h *Handler) UpdateCategory(ctx context.Context, req apispec.UpdateCategoryRequestObject) (apispec.UpdateCategoryResponseObject, error) {
	c, err := h.svc.UpdateCategory(ctx, req.ID, *req.Body)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateCategory200JSONResponse(toCategory(c)), nil
}

func (h *Handler) CreateCategoryAttribute(ctx context.Context, req apispec.CreateCategoryAttributeRequestObject) (apispec.CreateCategoryAttributeResponseObject, error) {
	a, err := h.svc.CreateAttribute(ctx, req.ID, *req.Body)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateCategoryAttribute201JSONResponse(toCategoryAttribute(a)), nil
}

func (h *Handler) UpdateCategoryAttribute(ctx context.Context, req apispec.UpdateCategoryAttributeRequestObject) (apispec.UpdateCategoryAttributeResponseObject, error) {
	a, err := h.svc.UpdateAttribute(ctx, req.ID, *req.Body)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateCategoryAttribute200JSONResponse(toCategoryAttribute(a)), nil
}
