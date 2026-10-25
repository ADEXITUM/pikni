package documents

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/forms"
)

func (h *Handler) CreateStocktake(ctx context.Context, req apispec.CreateStocktakeRequestObject) (apispec.CreateStocktakeResponseObject, error) {
	b := req.Body
	scope := Scope{}
	if b.ZoneIds != nil {
		scope.Zones = *b.ZoneIds
	}
	if b.CategoryIds != nil {
		scope.Categories = *b.CategoryIds
	}
	if b.BrandIds != nil {
		scope.Brands = *b.BrandIds
	}
	scope.Strict = b.StopSales != nil && *b.StopSales
	id, err := h.svc.CreateStocktake(ctx, b.LocationID, scope, comment(b.Comment))
	if err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, id)
	if err != nil {
		return nil, err
	}
	return apispec.CreateStocktake201JSONResponse(doc), nil
}

func (h *Handler) StartStocktake(ctx context.Context, req apispec.StartStocktakeRequestObject) (apispec.StartStocktakeResponseObject, error) {
	if err := h.svc.StartStocktake(ctx, req.ID, req.Body.Version); err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.StartStocktake200JSONResponse(doc), nil
}

func (h *Handler) ReconcileStocktake(ctx context.Context, req apispec.ReconcileStocktakeRequestObject) (apispec.ReconcileStocktakeResponseObject, error) {
	if err := h.svc.FinishCounting(ctx, req.ID, req.Body.Version); err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.ReconcileStocktake200JSONResponse(doc), nil
}

func (h *Handler) GetStocktake(ctx context.Context, req apispec.GetStocktakeRequestObject) (apispec.GetStocktakeResponseObject, error) {
	sh, _, err := h.svc.Sheet(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	showCost := auth.Has(ctx, apispec.PermissionCostView)
	out := apispec.GetStocktake200JSONResponse{
		Zones: make([]apispec.StocktakeZone, len(sh.Zones)),
		Lines: make([]apispec.StocktakeLine, len(sh.Lines)),
		Marks: []apispec.StocktakeMark{},
	}
	for i, z := range sh.Zones {
		out.Zones[i] = apispec.StocktakeZone{
			ZoneID: z.ZoneID, ZoneKind: apispec.ZoneKind(z.Kind), Status: apispec.StocktakeZoneStatus(z.Status),
			BookMarks: int(z.BookMarks), FoundMarks: int(z.FoundMarks), BookQty: int(z.BookQty), CountedQty: int(z.CountedQty),
			ClosedByName: z.ClosedByName, ClosedAt: z.ClosedAt,
		}
	}
	for i, l := range sh.Lines {
		line := apispec.StocktakeLine{
			ZoneID: l.ZoneID, ZoneKind: apispec.ZoneKind(l.ZoneKind), VariantID: l.VariantID, Article: l.Article, Name: l.Name,
			Label: catalog.PackLabel(l.VolumeMl, l.NetWeightG, l.Container, l.GiftPack), BookQty: int(l.BookQty), SoldQty: int(l.SoldQty), FactQty: int(l.FactQty),
		}
		if l.EgaisAccounting != nil {
			a := apispec.EgaisAccounting(*l.EgaisAccounting)
			line.Accounting = &a
		}
		if showCost {
			c := kopecksE4ToRubles(l.CostE4)
			line.UnitCost = &c
		}
		out.Lines[i] = line
	}
	for _, m := range sh.NotFound {
		out.Marks = append(out.Marks, apispec.StocktakeMark{
			Code: m.MarkCode, ZoneKind: apispec.ZoneKind(m.ZoneKind), Kind: apispec.StocktakeMarkKindNotFound,
			Article: &m.Article, Name: &m.Name,
		})
	}
	for _, m := range sh.Found {
		known := m.Known
		out.Marks = append(out.Marks, apispec.StocktakeMark{
			Code: m.MarkCode, ZoneKind: apispec.ZoneKind(m.ZoneKind), Kind: apispec.StocktakeMarkKind(m.Kind),
			Known: &known, Article: m.Article, Name: m.Name,
		})
	}
	return out, nil
}

func (h *Handler) StocktakeZone(ctx context.Context, req apispec.StocktakeZoneRequestObject) (apispec.StocktakeZoneResponseObject, error) {
	if err := h.svc.SetZone(ctx, req.ID, req.ZoneID, string(req.Action)); err != nil {
		return nil, problem(err)
	}
	return apispec.StocktakeZone204Response{}, nil
}

func (h *Handler) SetStocktakeCount(ctx context.Context, req apispec.SetStocktakeCountRequestObject) (apispec.SetStocktakeCountResponseObject, error) {
	if err := h.svc.SetStocktakeCount(ctx, req.ID, req.ZoneID, req.VariantID, req.Body.Qty); err != nil {
		return nil, problem(err)
	}
	return apispec.SetStocktakeCount204Response{}, nil
}

// GetStocktakeForm — ИНВ-3 и ИНВ-19 по строкам ревизии в учётных ценах. Цены в форме видит
// комиссия, поэтому форму получают и без права на себестоимость — это документ для подписи.
func (h *Handler) GetStocktakeForm(ctx context.Context, req apispec.GetStocktakeFormRequestObject) (apispec.GetStocktakeFormResponseObject, error) {
	if req.Form != apispec.GetStocktakeFormParamsFormInv3 && req.Form != apispec.GetStocktakeFormParamsFormInv19 {
		data, err := h.svc.TorgForm(ctx, req.ID, string(req.Form))
		if errors.Is(err, ErrFormNotApplicable) {
			return nil, echo.NewHTTPError(http.StatusUnprocessableEntity, "Эта форма не подходит к виду документа")
		}
		if err != nil {
			return nil, problem(err)
		}
		return apispec.GetStocktakeForm200ApplicationPdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
	}
	sh, doc, err := h.svc.Sheet(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	org, err := h.svc.q.OrganizationName(ctx)
	if err != nil {
		return nil, err
	}
	d := forms.Doc{Organization: org, Location: doc.LocationName, Number: doc.Number, Date: doc.DocDate, Started: doc.CreatedAt}
	if doc.PostedAt != nil {
		d.Date = *doc.PostedAt
	}
	kinds := []string{}
	for _, z := range sh.Zones {
		kinds = append(kinds, zoneNames[z.Kind])
	}
	d.Zones = strings.Join(kinds, ", ")
	byVariant := map[string]*forms.Line{}
	var lines []*forms.Line
	for _, l := range sh.Lines {
		key := l.VariantID.String()
		line, ok := byVariant[key]
		if !ok {
			line = &forms.Line{
				Name: l.Name + ", " + catalog.PackLabel(l.VolumeMl, l.NetWeightG, l.Container, l.GiftPack),
				Code: l.Article, PriceKopeck: (l.CostE4 + 50) / 100,
			}
			byVariant[key] = line
			lines = append(lines, line)
		}
		line.Book += l.BookQty
		line.Fact += l.FactQty
	}
	flat := make([]forms.Line, len(lines))
	for i, l := range lines {
		flat[i] = *l
	}
	xlsx := req.Params.Format != nil && *req.Params.Format == apispec.GetStocktakeFormParamsFormatXlsx
	var data []byte
	switch {
	case req.Form == apispec.GetStocktakeFormParamsFormInv3 && xlsx:
		data, err = forms.Inv3XLSX(d, flat)
	case req.Form == apispec.GetStocktakeFormParamsFormInv3:
		data, err = forms.Inv3PDF(d, flat)
	case xlsx:
		data, err = forms.Inv19XLSX(d, flat)
	default:
		data, err = forms.Inv19PDF(d, flat)
	}
	if err != nil {
		return nil, err
	}
	if xlsx {
		return apispec.GetStocktakeForm200ApplicationVndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
			Body: bytes.NewReader(data), ContentLength: int64(len(data)),
		}, nil
	}
	return apispec.GetStocktakeForm200ApplicationPdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}
