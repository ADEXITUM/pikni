package labels

import (
	"bytes"
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/forms"
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
		return echo.NewHTTPError(http.StatusNotFound, "Документ не найден в этой точке")
	case errors.Is(err, ErrEmpty):
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "Нечего печатать: нет товаров с розничной ценой")
	case errors.Is(err, auth.ErrForbidden):
		return echo.NewHTTPError(http.StatusForbidden, "Недостаточно прав для этой точки")
	}
	return err
}

func (h *Handler) ListStalePriceTags(ctx context.Context, req apispec.ListStalePriceTagsRequestObject) (apispec.ListStalePriceTagsResponseObject, error) {
	items, err := h.svc.Stale(ctx, req.Params.LocationID)
	if err != nil {
		return nil, problem(err)
	}
	out := apispec.ListStalePriceTags200JSONResponse{Items: make([]apispec.PriceTagItem, len(items))}
	for i, it := range items {
		out.Items[i] = apispec.PriceTagItem{
			VariantID: it.ID, Article: it.Article, Name: it.Name, Abv: it.Abv, PriceKopecks: it.PriceKopecks,
			Label:   catalog.PackLabel(it.VolumeMl, it.NetWeightG, it.Container, it.GiftPack),
			Barcode: &it.Barcode, PrintedAt: it.PrintedAt,
		}
		if it.PrintedAt != nil {
			out.Items[i].PrintedKopecks = &it.PrintedKopecks
		}
	}
	return out, nil
}

func (h *Handler) PrintPriceTags(ctx context.Context, req apispec.PrintPriceTagsRequestObject) (apispec.PrintPriceTagsResponseObject, error) {
	b := req.Body
	src := Source{Document: b.DocumentID}
	if b.VariantIds != nil {
		src.Variants = *b.VariantIds
	}
	data, err := h.svc.Print(ctx, b.LocationID, src, forms.Format(b.Format))
	if err != nil {
		return nil, problem(err)
	}
	return apispec.PrintPriceTags200ApplicationPdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}
