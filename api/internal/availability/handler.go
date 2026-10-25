package availability

import (
	"context"
	"errors"
	"math"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/availability/store"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func problem(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "Позиция не найдена или в архиве")
	case errors.Is(err, ErrBadFilters):
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "Некорректный фильтр характеристик")
	}
	return err
}

func rubles(kopecks int64) *float64 {
	if kopecks <= 0 {
		return nil
	}
	r := float64(kopecks) / 100
	return &r
}

func positive(v float64) *float64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func int32p(v *int) *int32 {
	if v == nil {
		return nil
	}
	n := int32(min(max(*v, math.MinInt32), math.MaxInt32))
	return &n
}

func accounting(s *string) *apispec.EgaisAccounting {
	if s == nil {
		return nil
	}
	a := apispec.EgaisAccounting(*s)
	return &a
}

func (h *Handler) SearchAvailability(ctx context.Context, req apispec.SearchAvailabilityRequestObject) (apispec.SearchAvailabilityResponseObject, error) {
	p := req.Params
	f := Filter{
		CategoryID: p.CategoryID, BrandID: p.BrandID, LocationID: p.LocationID, Here: p.Here,
		KindCode: p.KindCode, CountryCode: p.CountryCode, City: p.City,
		VolumeMl: int32p(p.VolumeMl), AbvFrom: p.AbvFrom, AbvTo: p.AbvTo,
		PriceFrom: Kopecks(p.PriceFrom), PriceTo: Kopecks(p.PriceTo),
		InStock: p.InStock != nil && *p.InStock, Sort: "name", Limit: 30,
	}
	if p.Q != nil {
		f.Search = *p.Q
	}
	if p.Attrs != nil {
		f.Attrs = *p.Attrs
	}
	if p.LocationKind != nil {
		k := string(*p.LocationKind)
		f.LocationKd = &k
	}
	if p.Sort != nil {
		f.Sort = string(*p.Sort)
	}
	if p.Offset != nil {
		f.Offset = *int32p(p.Offset)
	}
	if p.Limit != nil {
		f.Limit = *int32p(p.Limit)
	}
	rows, err := h.svc.Search(ctx, f)
	if err != nil {
		return nil, problem(err)
	}
	out := apispec.SearchAvailability200JSONResponse{Items: make([]apispec.AvailabilityItem, len(rows))}
	for i, r := range rows {
		out.Total = int(r.Total)
		out.Items[i] = apispec.AvailabilityItem{
			ProductID: r.ID, Name: r.Name, CategoryName: r.CategoryName, KindCode: r.KindCode,
			Accounting: accounting(r.EgaisAccounting), Abv: positive(r.Abv), BrandName: r.BrandName,
			CountryName: r.CountryName, LineName: r.LineName, Variety: r.Variety, Variants: int(r.Variants),
			MinPrice: rubles(r.MinPrice), MaxPrice: rubles(r.MaxPrice),
			ScopeQty: int(r.ScopeQty), NetworkQty: int(r.NetworkQty), HereQty: int(r.HereQty),
		}
	}
	return out, nil
}

func (h *Handler) GetAvailability(ctx context.Context, req apispec.GetAvailabilityRequestObject) (apispec.GetAvailabilityResponseObject, error) {
	c, err := h.svc.Card(ctx, req.ProductID)
	if err != nil {
		return nil, problem(err)
	}
	p := c.Product
	out := apispec.GetAvailability200JSONResponse{
		ProductID: p.ID, Name: p.Name, CategoryName: p.CategoryName, KindCode: p.KindCode,
		Accounting: accounting(p.EgaisAccounting), Abv: positive(p.Abv), BrandName: p.BrandName,
		CountryName: p.CountryName, LineName: p.LineName, Variety: p.Variety, CrptMarked: p.CrptMarked,
		Attributes: make([]apispec.AvailabilityAttribute, len(c.Attributes)),
		Variants:   make([]apispec.AvailabilityVariant, len(c.Variants)),
		Locations:  make([]apispec.AvailabilityLocation, len(c.Locations)),
		Cells:      make([]apispec.AvailabilityCell, len(c.Cells)),
	}
	for i, a := range c.Attributes {
		out.Attributes[i] = apispec.AvailabilityAttribute{Code: a.Code, Name: a.Name, Value: a.Value}
	}
	for i, v := range c.Variants {
		out.Variants[i] = apispec.AvailabilityVariant{
			ID: v.ID, Article: v.Article, Label: catalog.PackLabel(v.VolumeMl, v.NetWeightG, v.Container, v.GiftPack),
			Price: rubles(v.PriceKopecks),
		}
		if v.VolumeMl != nil {
			ml := int(*v.VolumeMl)
			out.Variants[i].VolumeMl = &ml
		}
	}
	for i, l := range c.Locations {
		out.Locations[i] = apispec.AvailabilityLocation{
			ID: l.ID, Code: l.Code, Name: l.Name, City: l.City, Kind: apispec.LocationKind(l.Kind), Lat: l.Lat, Lon: l.Lon,
		}
	}
	for i, cell := range c.Cells {
		out.Cells[i] = apispec.AvailabilityCell{
			VariantID: cell.VariantID, LocationID: cell.LocationID, Floor: int(cell.Floor), Storage: int(cell.Storage),
			Reserved: int(cell.Reserved), InTransit: int(cell.InTransit),
		}
	}
	return out, nil
}

func similar(rows []store.SimilarProductsRow) []apispec.SimilarItem {
	out := make([]apispec.SimilarItem, len(rows))
	for i, r := range rows {
		out[i] = apispec.SimilarItem{
			VariantID: r.VariantID, ProductID: r.ProductID, Name: r.Name,
			Label: catalog.PackLabel(r.VolumeMl, r.NetWeightG, r.Container, r.GiftPack),
			Price: rubles(r.PriceKopecks), HereQty: int(r.HereQty), NetworkQty: int(r.NetworkQty),
		}
	}
	return out
}

func (h *Handler) GetSimilar(ctx context.Context, req apispec.GetSimilarRequestObject) (apispec.GetSimilarResponseObject, error) {
	pack, line, analogs, err := h.svc.Similar(ctx, req.VariantID, req.Params.Here)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.GetSimilar200JSONResponse{Pack: similar(pack), Line: similar(line), Analogs: similar(analogs)}, nil
}
