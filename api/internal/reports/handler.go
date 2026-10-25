package reports

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
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
		return echo.NewHTTPError(http.StatusNotFound, "Отчёт не найден или недоступен вашей роли")
	case errors.Is(err, ErrBadParams):
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "Проверьте период: начало не позже конца, не больше трёх лет")
	case errors.Is(err, ErrNotReady):
		return echo.NewHTTPError(http.StatusConflict, "Отчёт ещё формируется")
	case errors.Is(err, auth.ErrForbidden):
		return echo.NewHTTPError(http.StatusForbidden, "Нет доступа к выбранным точкам")
	}
	return err
}

func params(p apispec.ReportParams) Params {
	out := Params{}
	if p.From != nil {
		out.From = p.From.Time
	}
	if p.To != nil {
		out.To = p.To.Time
	}
	if p.Date != nil {
		out.Date = p.Date.Time
	}
	if p.LocationIds != nil {
		out.Locations = *p.LocationIds
	}
	if p.CategoryIds != nil {
		out.Categories = *p.CategoryIds
	}
	if p.BrandIds != nil {
		out.Brands = *p.BrandIds
	}
	if p.Group != nil {
		out.Group = string(*p.Group)
	}
	if p.Days != nil {
		out.Days = *p.Days
	}
	return out
}

func columns(cols []Column) []apispec.ReportColumn {
	out := make([]apispec.ReportColumn, len(cols))
	for i, c := range cols {
		out[i] = apispec.ReportColumn{Title: c.Title, Kind: apispec.ReportColumnKind(c.Kind), Sum: c.Sum}
	}
	return out
}

func (h *Handler) ListReports(ctx context.Context, _ apispec.ListReportsRequestObject) (apispec.ListReportsResponseObject, error) {
	defs := h.svc.Catalog(ctx)
	out := apispec.ListReports200JSONResponse{Items: make([]apispec.ReportDef, len(defs))}
	for i, d := range defs {
		ps := make([]apispec.ReportDefParams, len(d.Params))
		for j, p := range d.Params {
			ps[j] = apispec.ReportDefParams(p)
		}
		out.Items[i] = apispec.ReportDef{Code: d.Code, Title: d.Title, Description: d.Description, Params: ps,
			Columns: columns(d.Columns), Heavy: d.Heavy}
	}
	return out, nil
}

func (h *Handler) RunReport(ctx context.Context, req apispec.RunReportRequestObject) (apispec.RunReportResponseObject, error) {
	d, t, err := h.svc.Run(ctx, req.Code, params(*req.Body))
	if err != nil {
		return nil, problem(err)
	}
	rows := t.Rows
	if rows == nil {
		rows = [][]any{}
	}
	return apispec.RunReport200JSONResponse{Title: d.Title, Columns: columns(d.Columns), Rows: rows, Truncated: len(rows) >= maxRows}, nil
}

func fileResponse(t File) apispec.ReportFile {
	return apispec.ReportFile{ID: t.ID, Report: t.Report, Title: t.Title, Format: apispec.ReportFileFormat(t.Format),
		Status: apispec.ReportFileStatus(t.Status), Error: t.Error, CreatedAt: t.CreatedAt, FinishedAt: t.FinishedAt, ExpiresAt: t.ExpiresAt}
}

func (h *Handler) ExportReport(ctx context.Context, req apispec.ExportReportRequestObject) (apispec.ExportReportResponseObject, error) {
	p := params(req.Body.Params)
	kind := string(req.Body.Format)
	d, ok := find(req.Code)
	// Тяжёлый отчёт за длинный период строится в фоне; за короткий — сразу.
	if ok && d.Heavy && (p.From.IsZero() || p.To.Sub(p.From) > 31*24*time.Hour) {
		f, err := h.svc.Enqueue(ctx, req.Code, p, kind)
		if err != nil {
			return nil, problem(err)
		}
		return apispec.ExportReport202JSONResponse(fileResponse(f)), nil
	}
	data, _, err := h.svc.Render(ctx, req.Code, p, kind)
	if err != nil {
		return nil, problem(err)
	}
	if kind == "xlsx" {
		return apispec.ExportReport200ApplicationVndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
			Body: bytes.NewReader(data), ContentLength: int64(len(data)),
		}, nil
	}
	return apispec.ExportReport200ApplicationPdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}

func (h *Handler) ListReportFiles(ctx context.Context, _ apispec.ListReportFilesRequestObject) (apispec.ListReportFilesResponseObject, error) {
	files, err := h.svc.Files(ctx)
	if err != nil {
		return nil, err
	}
	out := apispec.ListReportFiles200JSONResponse{Items: make([]apispec.ReportFile, len(files))}
	for i, f := range files {
		out.Items[i] = fileResponse(f)
	}
	return out, nil
}

func (h *Handler) DownloadReportFile(ctx context.Context, req apispec.DownloadReportFileRequestObject) (apispec.DownloadReportFileResponseObject, error) {
	data, kind, err := h.svc.Download(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	if kind == "xlsx" {
		return apispec.DownloadReportFile200ApplicationVndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
			Body: bytes.NewReader(data), ContentLength: int64(len(data)),
		}, nil
	}
	return apispec.DownloadReportFile200ApplicationPdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}

func points(ps []Point) []apispec.ChartPoint {
	out := make([]apispec.ChartPoint, len(ps))
	for i, p := range ps {
		out[i] = apispec.ChartPoint{Label: p.Label, Value: p.Value}
	}
	return out
}

func (h *Handler) GetDashboard(ctx context.Context, req apispec.GetDashboardRequestObject) (apispec.GetDashboardResponseObject, error) {
	to := Today()
	from := to.AddDate(0, 0, -29)
	if req.Params.From != nil {
		from = req.Params.From.Time
	}
	if req.Params.To != nil {
		to = req.Params.To.Time
	}
	d, err := h.svc.Dashboard(ctx, from, to, req.Params.LocationID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.GetDashboard200JSONResponse{
		StockCost: d.StockCost, Revenue: d.Revenue, TurnoverDays: d.TurnoverDays, DeadShare: d.DeadShare,
		EgaisMismatches: d.EgaisMismatches, UnconfirmedTtn: d.UnconfirmedTTN, Breakage: d.Breakage,
		RevenueByDay: points(d.RevenueByDay), StockByCategory: points(d.StockByCategory), StockByLocation: points(d.StockByLocation),
	}, nil
}
