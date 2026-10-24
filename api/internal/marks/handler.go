package marks

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func zoneKind(s *string) *apispec.ZoneKind {
	if s == nil {
		return nil
	}
	k := apispec.ZoneKind(*s)
	return &k
}

func status(s *string) *apispec.MarkStatus {
	if s == nil {
		return nil
	}
	v := apispec.MarkStatus(*s)
	return &v
}

// GetMark принимает и скан марки целиком: сканер в русской раскладке печатает кириллицу,
// разбор возвращает текст марки латиницей.
func (h *Handler) GetMark(ctx context.Context, req apispec.GetMarkRequestObject) (apispec.GetMarkResponseObject, error) {
	code := req.Code
	if scan, ok := catalog.ParseScan(code); ok && scan.Mark != "" {
		code = scan.Mark
	}
	m, events, err := h.svc.History(ctx, code)
	if errors.Is(err, ErrNotFound) {
		return nil, echo.NewHTTPError(http.StatusNotFound, "Марки нет в учёте")
	}
	if err != nil {
		return nil, err
	}
	out := apispec.Mark{
		Code: m.Code, VariantID: m.VariantID, ProductID: m.ProductID, Article: m.Article, Name: m.Name,
		Status: apispec.MarkStatus(m.Status), LocationID: m.LocationID, LocationName: m.LocationName,
		ZoneKind: zoneKind(m.ZoneKind), Events: make([]apispec.MarkEvent, len(events)),
	}
	for i, e := range events {
		ev := apispec.MarkEvent{
			ID: e.ID, At: e.At, DocumentID: e.DocumentID, DocumentNumber: e.DocumentNumber,
			FromStatus: status(e.FromStatus), ToStatus: apispec.MarkStatus(e.ToStatus),
			FromZoneKind: zoneKind(e.FromZoneKind), ToZoneKind: zoneKind(e.ToZoneKind),
			LocationName: e.LocationName, UserName: e.UserName,
		}
		if e.DocumentKind != nil {
			k := apispec.DocumentKind(*e.DocumentKind)
			ev.DocumentKind = &k
		}
		out.Events[i] = ev
	}
	return apispec.GetMark200JSONResponse(out), nil
}
