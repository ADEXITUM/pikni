package catalog

import (
	"context"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

func toSupplier(sp store.Supplier) apispec.Supplier {
	out := apispec.Supplier{
		ID: sp.ID, Name: sp.Name, INN: sp.INN, KPP: sp.KPP, FSRARID: sp.FSRARID, LicenseNo: sp.LicenseNo,
		DefaultReceivingMode: apispec.ReceivingMode(sp.DefaultReceivingMode), Contacts: supplierContacts(sp.Contacts),
		IsActive: sp.IsActive,
	}
	if sp.LicenseValidTo != nil {
		out.LicenseValidTo = &openapi_types.Date{Time: *sp.LicenseValidTo}
	}
	return out
}

func (h *Handler) ListSuppliers(ctx context.Context, _ apispec.ListSuppliersRequestObject) (apispec.ListSuppliersResponseObject, error) {
	rows, err := h.svc.Suppliers(ctx)
	items, err := list(rows, err, toSupplier)
	if err != nil {
		return nil, err
	}
	return apispec.ListSuppliers200JSONResponse{Items: items}, nil
}

func (h *Handler) CreateSupplier(ctx context.Context, req apispec.CreateSupplierRequestObject) (apispec.CreateSupplierResponseObject, error) {
	sp, err := h.svc.SaveSupplier(ctx, nil, *req.Body, true)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateSupplier201JSONResponse(toSupplier(sp)), nil
}

func (h *Handler) UpdateSupplier(ctx context.Context, req apispec.UpdateSupplierRequestObject) (apispec.UpdateSupplierResponseObject, error) {
	b := req.Body
	f := apispec.SupplierFields{
		Name: b.Name, INN: b.INN, KPP: b.KPP, FSRARID: b.FSRARID, LicenseNo: b.LicenseNo, LicenseValidTo: b.LicenseValidTo,
		DefaultReceivingMode: b.DefaultReceivingMode, Contacts: b.Contacts,
	}
	sp, err := h.svc.SaveSupplier(ctx, &req.ID, f, b.IsActive)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateSupplier200JSONResponse(toSupplier(sp)), nil
}
