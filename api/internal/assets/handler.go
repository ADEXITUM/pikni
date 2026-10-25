package assets

import (
	"bytes"
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/assets/store"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/forms"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

var problems = map[error]*echo.HTTPError{
	ErrNotFound:          echo.NewHTTPError(http.StatusNotFound, "Объект имущества не найден"),
	ErrConflict:          echo.NewHTTPError(http.StatusConflict, "Карточку уже изменил другой сотрудник — обновите страницу"),
	ErrInvalid:           echo.NewHTTPError(http.StatusUnprocessableEntity, "Проверьте наименование, категорию, дату принятия к учёту и стоимость"),
	ErrWrittenOff:        echo.NewHTTPError(http.StatusConflict, "Объект списан — изменить его нельзя"),
	ErrPendingWriteoff:   echo.NewHTTPError(http.StatusConflict, "Списание уже ждёт утверждения"),
	ErrNoPendingWriteoff: echo.NewHTTPError(http.StatusConflict, "Списание не оформлялось или уже рассмотрено"),
	ErrSameApprover:      echo.NewHTTPError(http.StatusForbidden, "Списание утверждает другой сотрудник — не тот, кто его оформил"),
	ErrBadResponsible:    echo.NewHTTPError(http.StatusUnprocessableEntity, "Сотрудник заблокирован или не найден"),
	ErrAuditOpen:         echo.NewHTTPError(http.StatusConflict, "В точке уже идёт инвентаризация имущества"),
	ErrAuditClosed:       echo.NewHTTPError(http.StatusConflict, "Инвентаризация уже завершена"),
	ErrNotAsset:          echo.NewHTTPError(http.StatusUnprocessableEntity, "Это не инвентарная наклейка или объект не найден"),
	auth.ErrForbidden:    echo.NewHTTPError(http.StatusForbidden, "Недостаточно прав для этой точки"),
}

func problem(err error) error {
	for target, he := range problems {
		if errors.Is(err, target) {
			return he
		}
	}
	return err
}

func toAPI(a store.GetAssetRow) apispec.Asset {
	return apispec.Asset{
		ID: a.ID, Number: a.Number, Name: a.Name, CategoryCode: a.CategoryCode, CategoryName: a.CategoryName,
		Serial: a.Serial, AcceptedAt: openapi_types.Date{Time: a.AcceptedAt}, CostKopecks: a.CostKopecks, LocationID: a.LocationID,
		LocationCode: a.LocationCode, LocationName: a.LocationName, ResponsibleID: a.ResponsibleID,
		ResponsibleName: a.ResponsibleName, Status: apispec.AssetStatus(a.Status), HasPhoto: a.HasPhoto, Version: int(a.Version),
	}
}

func (h *Handler) card(ctx context.Context, id uuid.UUID) (apispec.AssetCard, error) {
	c, err := h.svc.Get(ctx, id)
	if err != nil {
		return apispec.AssetCard{}, problem(err)
	}
	out := apispec.AssetCard{
		Asset:   toAPI(c.Asset),
		Events:  make([]apispec.AssetEvent, len(c.Events)),
		CanEdit: c.Asset.Status != StatusWrittenOff && auth.CheckLocation(ctx, apispec.PermissionAssetsEdit, c.Asset.LocationID) == nil,
	}
	for i, e := range c.Events {
		out.Events[i] = apispec.AssetEvent{
			ID: e.ID, Kind: apispec.AssetEventKind(e.Kind), At: e.At, Comment: e.Comment, FromStatus: e.FromStatus, ToStatus: e.ToStatus,
			FromLocationName: e.FromLocationName, ToLocationName: e.ToLocationName, FromUserName: e.FromUserName,
			ToUserName: e.ToUserName, UserName: e.UserName,
		}
	}
	if c.Last != nil {
		out.Writeoff = &apispec.AssetWriteoff{
			Reason: c.Last.Reason, Status: apispec.AssetWriteoffStatus(c.Last.Status), RequestedAt: c.Last.RequestedAt,
			RequestedByName: c.Last.RequestedByName, DecidedAt: c.Last.DecidedAt, DecidedByName: c.Last.DecidedByName,
			DecisionComment: c.Last.DecisionComment,
		}
	}
	sess, _ := auth.FromContext(ctx)
	out.CanApprove = c.Pending != nil && c.Pending.RequestedBy != sess.UserID &&
		auth.CheckLocation(ctx, apispec.PermissionDocsApprove, c.Asset.LocationID) == nil
	return out, nil
}

func (h *Handler) ListAssetCategories(ctx context.Context, _ apispec.ListAssetCategoriesRequestObject) (apispec.ListAssetCategoriesResponseObject, error) {
	rows, err := h.svc.Categories(ctx)
	if err != nil {
		return nil, err
	}
	out := apispec.ListAssetCategories200JSONResponse{Items: make([]apispec.AssetCategory, len(rows))}
	for i, r := range rows {
		out.Items[i] = apispec.AssetCategory{Code: r.Code, Name: r.Name}
	}
	return out, nil
}

func filter(location, responsible *uuid.UUID, category *string, status *apispec.AssetStatus, q *string) Filter {
	f := Filter{LocationID: location, ResponsibleID: responsible, Category: category, Q: q}
	if status != nil {
		st := string(*status)
		f.Status = &st
	}
	return f
}

func (h *Handler) ListAssets(ctx context.Context, req apispec.ListAssetsRequestObject) (apispec.ListAssetsResponseObject, error) {
	p := req.Params
	f := filter(p.LocationID, p.ResponsibleID, p.Category, p.Status, p.Q)
	f.Limit = 50
	if p.Limit != nil {
		f.Limit = int32(*p.Limit)
	}
	if p.Offset != nil {
		f.Offset = int32(*p.Offset)
	}
	rows, err := h.svc.List(ctx, f)
	if err != nil {
		return nil, problem(err)
	}
	totals, err := h.svc.Totals(ctx, f)
	if err != nil {
		return nil, problem(err)
	}
	out := apispec.ListAssets200JSONResponse{Items: make([]apispec.Asset, len(rows)), Total: int(totals.Total),
		TotalCostKopecks: totals.CostKopecks}
	for i, r := range rows {
		out.Items[i] = toAPI(store.GetAssetRow(r))
	}
	return out, nil
}

func (h *Handler) GetAssetRegister(ctx context.Context, req apispec.GetAssetRegisterRequestObject) (apispec.GetAssetRegisterResponseObject, error) {
	p := req.Params
	data, err := h.svc.Register(ctx, filter(p.LocationID, p.ResponsibleID, p.Category, p.Status, p.Q))
	if err != nil {
		return nil, problem(err)
	}
	return apispec.GetAssetRegister200ApplicationVndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		Body: bytes.NewReader(data), ContentLength: int64(len(data)),
	}, nil
}

func fields(name, category string, serial *string, accepted openapi_types.Date, cost int64) Fields {
	f := Fields{Name: name, Category: category, AcceptedAt: accepted.Time, CostKopeck: cost}
	if serial != nil {
		f.Serial = *serial
	}
	return f
}

func (h *Handler) CreateAsset(ctx context.Context, req apispec.CreateAssetRequestObject) (apispec.CreateAssetResponseObject, error) {
	b := req.Body
	id, err := h.svc.Create(ctx, b.LocationID, b.ResponsibleID, fields(b.Name, b.CategoryCode, b.Serial, b.AcceptedAt, b.CostKopecks))
	if err != nil {
		return nil, problem(err)
	}
	c, err := h.card(ctx, id)
	if err != nil {
		return nil, err
	}
	return apispec.CreateAsset201JSONResponse(c), nil
}

func (h *Handler) GetAsset(ctx context.Context, req apispec.GetAssetRequestObject) (apispec.GetAssetResponseObject, error) {
	c, err := h.card(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.GetAsset200JSONResponse(c), nil
}

func (h *Handler) UpdateAsset(ctx context.Context, req apispec.UpdateAssetRequestObject) (apispec.UpdateAssetResponseObject, error) {
	b := req.Body
	if err := h.svc.Update(ctx, req.ID, int32(b.Version), fields(b.Name, b.CategoryCode, b.Serial, b.AcceptedAt, b.CostKopecks)); err != nil {
		return nil, problem(err)
	}
	c, err := h.card(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.UpdateAsset200JSONResponse(c), nil
}

func comment(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h *Handler) MoveAsset(ctx context.Context, req apispec.MoveAssetRequestObject) (apispec.MoveAssetResponseObject, error) {
	b := req.Body
	if err := h.svc.Move(ctx, req.ID, int32(b.Version), b.LocationID, b.ResponsibleID, comment(b.Comment)); err != nil {
		return nil, problem(err)
	}
	c, err := h.card(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.MoveAsset200JSONResponse(c), nil
}

func (h *Handler) SetAssetStatus(ctx context.Context, req apispec.SetAssetStatusRequestObject) (apispec.SetAssetStatusResponseObject, error) {
	b := req.Body
	if err := h.svc.SetStatus(ctx, req.ID, int32(b.Version), string(b.Status), comment(b.Comment)); err != nil {
		return nil, problem(err)
	}
	c, err := h.card(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.SetAssetStatus200JSONResponse(c), nil
}

const maxPhoto = 3 << 20

func (h *Handler) SetAssetPhoto(ctx context.Context, req apispec.SetAssetPhotoRequestObject) (apispec.SetAssetPhotoResponseObject, error) {
	data := req.Body.Data
	if len(data) == 0 || len(data) > maxPhoto || http.DetectContentType(data) != "image/jpeg" {
		return nil, echo.NewHTTPError(http.StatusUnprocessableEntity, "Нужна фотография JPEG до 3 МБ")
	}
	if err := h.svc.SetPhoto(ctx, req.ID, data, "image/jpeg"); err != nil {
		return nil, problem(err)
	}
	return apispec.SetAssetPhoto204Response{}, nil
}

func (h *Handler) DeleteAssetPhoto(ctx context.Context, req apispec.DeleteAssetPhotoRequestObject) (apispec.DeleteAssetPhotoResponseObject, error) {
	if err := h.svc.SetPhoto(ctx, req.ID, nil, ""); err != nil {
		return nil, problem(err)
	}
	return apispec.DeleteAssetPhoto204Response{}, nil
}

func (h *Handler) GetAssetPhoto(ctx context.Context, req apispec.GetAssetPhotoRequestObject) (apispec.GetAssetPhotoResponseObject, error) {
	data, _, err := h.svc.Photo(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.GetAssetPhoto200ImageJpegResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}

func (h *Handler) ListAssetResponsibles(ctx context.Context, req apispec.ListAssetResponsiblesRequestObject) (apispec.ListAssetResponsiblesResponseObject, error) {
	rows, err := h.svc.Responsibles(ctx, req.Params.LocationID)
	if err != nil {
		return nil, problem(err)
	}
	out := apispec.ListAssetResponsibles200JSONResponse{Items: make([]apispec.AssetResponsible, len(rows))}
	for i, r := range rows {
		out.Items[i] = apispec.AssetResponsible{ID: r.ID, FullName: r.FullName, Role: apispec.Role(r.Role)}
	}
	return out, nil
}

func (h *Handler) RequestAssetWriteoff(ctx context.Context, req apispec.RequestAssetWriteoffRequestObject) (apispec.RequestAssetWriteoffResponseObject, error) {
	if err := h.svc.RequestWriteoff(ctx, req.ID, int32(req.Body.Version), req.Body.Reason); err != nil {
		return nil, problem(err)
	}
	c, err := h.card(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.RequestAssetWriteoff200JSONResponse(c), nil
}

func (h *Handler) DecideAssetWriteoff(ctx context.Context, req apispec.DecideAssetWriteoffRequestObject) (apispec.DecideAssetWriteoffResponseObject, error) {
	var text string
	if req.Body != nil {
		text = comment(req.Body.Comment)
	}
	if err := h.svc.DecideWriteoff(ctx, req.ID, req.Decision == apispec.DecideAssetWriteoffParamsDecisionApprove, text); err != nil {
		return nil, problem(err)
	}
	c, err := h.card(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.DecideAssetWriteoff200JSONResponse(c), nil
}

func (h *Handler) GetAssetWriteoffAct(ctx context.Context, req apispec.GetAssetWriteoffActRequestObject) (apispec.GetAssetWriteoffActResponseObject, error) {
	data, err := h.svc.WriteoffAct(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.GetAssetWriteoffAct200ApplicationPdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}

func (h *Handler) PrintAssetLabels(ctx context.Context, req apispec.PrintAssetLabelsRequestObject) (apispec.PrintAssetLabelsResponseObject, error) {
	data, err := h.svc.Labels(ctx, req.Body.AssetIds, forms.Format(req.Body.Format))
	if err != nil {
		return nil, problem(err)
	}
	return apispec.PrintAssetLabels200ApplicationPdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}

func auditToAPI(a store.GetAuditRow) apispec.AssetAudit {
	return apispec.AssetAudit{
		ID: a.ID, LocationID: a.LocationID, LocationName: a.LocationName, Status: apispec.AssetAuditStatus(a.Status),
		StartedAt: a.StartedAt, StartedByName: a.StartedByName, FinishedAt: a.FinishedAt, FinishedByName: a.FinishedByName,
	}
}

func (h *Handler) auditState(ctx context.Context, id uuid.UUID) (apispec.AssetAuditState, error) {
	st, err := h.svc.Audit(ctx, id)
	if err != nil {
		return apispec.AssetAuditState{}, problem(err)
	}
	out := apispec.AssetAuditState{Audit: auditToAPI(st.Audit), Results: make([]apispec.AssetAuditResult, len(st.Results))}
	for i, r := range st.Results {
		result := apispec.AssetAuditResultResultNotFound
		switch {
		case r.Found && r.Elsewhere:
			result = apispec.AssetAuditResultResultElsewhere
		case r.Found:
			result = apispec.AssetAuditResultResultFound
		}
		out.Results[i] = apispec.AssetAuditResult{
			AssetID: r.ID, Number: r.Number, Name: r.Name, CategoryName: r.CategoryName, LocationName: r.LocationName,
			ResponsibleName: r.ResponsibleName, Result: result, ScannedAt: r.ScannedAt,
		}
	}
	return out, nil
}

func (h *Handler) ListAssetAudits(ctx context.Context, _ apispec.ListAssetAuditsRequestObject) (apispec.ListAssetAuditsResponseObject, error) {
	rows, err := h.svc.Audits(ctx)
	if err != nil {
		return nil, problem(err)
	}
	out := apispec.ListAssetAudits200JSONResponse{Items: make([]apispec.AssetAudit, len(rows))}
	for i, r := range rows {
		out.Items[i] = apispec.AssetAudit{
			ID: r.ID, LocationID: r.LocationID, LocationName: r.LocationName, Status: apispec.AssetAuditStatus(r.Status),
			StartedAt: r.StartedAt, StartedByName: r.StartedByName, FinishedAt: r.FinishedAt,
		}
	}
	return out, nil
}

func (h *Handler) StartAssetAudit(ctx context.Context, req apispec.StartAssetAuditRequestObject) (apispec.StartAssetAuditResponseObject, error) {
	id, err := h.svc.StartAudit(ctx, req.Body.LocationID)
	if err != nil {
		return nil, problem(err)
	}
	st, err := h.auditState(ctx, id)
	if err != nil {
		return nil, err
	}
	return apispec.StartAssetAudit201JSONResponse(st), nil
}

func (h *Handler) GetAssetAudit(ctx context.Context, req apispec.GetAssetAuditRequestObject) (apispec.GetAssetAuditResponseObject, error) {
	st, err := h.auditState(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.GetAssetAudit200JSONResponse(st), nil
}

func (h *Handler) ScanAssetLabel(ctx context.Context, req apispec.ScanAssetLabelRequestObject) (apispec.ScanAssetLabelResponseObject, error) {
	res, err := h.svc.AuditScan(ctx, req.ID, req.Body.Raw)
	if err != nil {
		return nil, problem(err)
	}
	out := apispec.ScanAssetLabel200JSONResponse{AssetID: res.AssetID, Result: apispec.AssetAuditScanResultFound, Duplicate: res.Duplicate}
	if res.Elsewhere {
		out.Result = apispec.AssetAuditScanResultElsewhere
	}
	return out, nil
}

func (h *Handler) FinishAssetAudit(ctx context.Context, req apispec.FinishAssetAuditRequestObject) (apispec.FinishAssetAuditResponseObject, error) {
	relocate := req.Params.Relocate != nil && *req.Params.Relocate
	if err := h.svc.FinishAudit(ctx, req.ID, relocate, req.Action == apispec.FinishAssetAuditParamsActionCancel); err != nil {
		return nil, problem(err)
	}
	st, err := h.auditState(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.FinishAssetAudit200JSONResponse(st), nil
}
