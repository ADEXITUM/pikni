package documents

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/documents/store"
	"github.com/ADEXITUM/pikni/api/internal/marks"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

var problems = map[error]*echo.HTTPError{
	ErrNotFound:         echo.NewHTTPError(http.StatusNotFound, "Документ не найден"),
	ErrNotDraft:         echo.NewHTTPError(http.StatusConflict, "Документ уже проведён или отменён"),
	ErrConflict:         echo.NewHTTPError(http.StatusConflict, "Документ уже изменил другой пользователь. Обновите страницу и повторите"),
	ErrEmpty:            echo.NewHTTPError(http.StatusUnprocessableEntity, "В документе нет ни одной строки"),
	ErrOpeningExists:    echo.NewHTTPError(http.StatusConflict, "Начальные остатки по этой зоне уже вводились. Исправить их можно ревизией"),
	ErrNotStore:         echo.NewHTTPError(http.StatusUnprocessableEntity, "Передача в торговый зал есть только в магазине"),
	ErrInactiveLocation: echo.NewHTTPError(http.StatusUnprocessableEntity, "Точка в архиве"),
	ErrMarkedQty:        echo.NewHTTPError(http.StatusUnprocessableEntity, "Количество товара с марками считается по отсканированным маркам"),
	ErrUnknownVariant:   echo.NewHTTPError(http.StatusUnprocessableEntity, "Такого товара нет в каталоге"),
	ErrInactiveVariant:  echo.NewHTTPError(http.StatusUnprocessableEntity, "Вариант в архиве"),
	ErrForeignScan:      echo.NewHTTPError(http.StatusConflict, "Скан с этим идентификатором относится к другому документу"),
	auth.ErrForbidden:   echo.NewHTTPError(http.StatusForbidden, "Недостаточно прав для этой точки"),
	ErrModeTooWeak:      echo.NewHTTPError(http.StatusUnprocessableEntity, "Продукцию с марками принимают выборочно или поштучно"),
	ErrUnknownMode:      echo.NewHTTPError(http.StatusUnprocessableEntity, "Режим приёмки есть только у поступления"),
	ErrSameLocation:     echo.NewHTTPError(http.StatusUnprocessableEntity, "Точка-отправитель и получатель совпадают"),
	ErrNoStorage:        echo.NewHTTPError(http.StatusUnprocessableEntity, "У точки нет склада для перемещения"),
	ErrNotRequested:     echo.NewHTTPError(http.StatusConflict, "Заявка уже согласована или отклонена"),
	ErrNoFSRAR:          echo.NewHTTPError(http.StatusUnprocessableEntity, "У точки не указан ФСРАР ID — ТТН в ЕГАИС не отправить"),
	ErrTTNPending:       echo.NewHTTPError(http.StatusConflict, "ТТН ещё не пришла из ЕГАИС — подождите и проведите приёмку позже"),
}

func problem(err error) error {
	if e, ok := errors.AsType[*MissingCostError](err); ok {
		return echo.NewHTTPError(http.StatusUnprocessableEntity,
			"Не задана закупочная цена — начальный остаток нечем оценить: "+strings.Join(e.Articles, ", "))
	}
	if e, ok := errors.AsType[*ShortageError](err); ok {
		return echo.NewHTTPError(http.StatusConflict, fmt.Sprintf(
			"Не хватает товара %s «%s» в зоне «%s»: свободно %d, нужно %d", e.Article, e.Name, e.Zone, e.Available, e.Need))
	}
	if e, ok := errors.AsType[*UnmappedError](err); ok {
		return echo.NewHTTPError(http.StatusUnprocessableEntity,
			"В ТТН есть продукция, которой нет в каталоге — укажите алкокод у варианта: "+strings.Join(e.Names, "; "))
	}
	if e, ok := errors.AsType[*marks.TakenError](err); ok {
		return echo.NewHTTPError(http.StatusConflict, "Марка уже учтена в системе: "+e.Code)
	}
	if e, ok := errors.AsType[*marks.MisplacedError](err); ok {
		return echo.NewHTTPError(http.StatusConflict, "Марка больше не числится в зоне отправления: "+e.Code)
	}
	for target, he := range problems {
		if errors.Is(err, target) {
			return he
		}
	}
	return err
}

func kopecksE4ToRubles(e4 int64) float64 {
	return float64(e4) / 10000
}

func zoneKind(s *string) *apispec.ZoneKind {
	if s == nil {
		return nil
	}
	k := apispec.ZoneKind(*s)
	return &k
}

func toLine(l store.ListLinesRow, showCost bool) apispec.DocumentLine {
	line := apispec.DocumentLine{
		VariantID: l.VariantID, ProductID: l.ProductID, Article: l.Article, Name: l.Name,
		VolumeMl: l.VolumeMl, NetWeightG: l.NetWeightG, GiftPack: l.GiftPack, Qty: l.Qty, QtyPlan: l.QtyPlan,
	}
	if l.QtyPlan != nil {
		line.QtyFact, line.QtyAccepted = &l.Qty, &l.Qty
	}
	if l.Container != nil {
		c := apispec.Container(*l.Container)
		line.Container = &c
	}
	if l.EgaisAccounting != nil {
		a := apispec.EgaisAccounting(*l.EgaisAccounting)
		line.EgaisAccounting = &a
	}
	if showCost && l.Costed {
		cost := kopecksE4ToRubles(l.UnitCostE4)
		line.UnitCost = &cost
	}
	return line
}

func toScan(s store.ListScansRow) apispec.DocumentScan {
	return apispec.DocumentScan{
		ID: s.ID, Raw: s.Raw, Status: apispec.DocumentScanStatus(s.Status), Reason: s.Reason,
		VariantID: s.VariantID, Article: s.Article, Name: s.Name, MarkCode: s.MarkCode, Qty: s.Qty,
		ScannedAt: s.ScannedAt, UndoneAt: s.UndoneAt,
	}
}

func toSummary(r store.ListDocumentsRow) apispec.DocumentSummary {
	var mode *apispec.ReceivingMode
	if r.ReceivingMode != nil {
		m := apispec.ReceivingMode(*r.ReceivingMode)
		mode = &m
	}
	return apispec.DocumentSummary{
		SupplierName: r.SupplierName, TtnNumber: r.TtnNumber, ReceivingMode: mode,
		ID: r.ID, Kind: apispec.DocumentKind(r.Kind), Number: r.Number, Status: apispec.DocumentStatus(r.Status),
		DocDate: openapi_types.Date{Time: r.DocDate}, LocationID: r.LocationID, LocationName: r.LocationName,
		ZoneID: r.ZoneID, ZoneKind: apispec.ZoneKind(r.ZoneKind), DestZoneID: r.DestZoneID,
		DestZoneKind: zoneKind(r.DestZoneKind), AuthorName: r.AuthorName, PostedByName: r.PostedByName,
		LineCount: r.LineCount, TotalQty: r.TotalQty, CreatedAt: r.CreatedAt, PostedAt: r.PostedAt, Version: r.Version,
		DestLocationID: r.DestLocationID, DestLocationName: r.DestLocationName,
	}
}

func toDocument(d Detail) apispec.Document {
	r := d.Doc
	doc := apispec.Document{
		ID: r.ID, Kind: apispec.DocumentKind(r.Kind), Number: r.Number, Status: apispec.DocumentStatus(r.Status),
		DocDate: openapi_types.Date{Time: r.DocDate}, LocationID: r.LocationID, LocationName: r.LocationName,
		ZoneID: r.ZoneID, ZoneKind: apispec.ZoneKind(r.ZoneKind), DestZoneID: r.DestZoneID,
		DestZoneKind: zoneKind(r.DestZoneKind), AuthorName: r.AuthorName, PostedByName: r.PostedByName,
		LineCount: r.LineCount, TotalQty: r.TotalQty, CreatedAt: r.CreatedAt, PostedAt: r.PostedAt, Version: r.Version,
		Comment: r.Comment, CanEdit: d.CanEdit, CanPost: d.CanPost, CanApprove: &d.CanApprove,
		DestLocationID: r.DestLocationID, DestLocationName: r.DestLocationName,
		TransferID: r.TransferID, TransferNumber: r.TransferNumber, ReceiptID: r.ReceiptID, ReceiptNumber: r.ReceiptNumber,
		Lines: make([]apispec.DocumentLine, len(d.Lines)), Scans: make([]apispec.DocumentScan, len(d.Scans)),
	}
	for i, l := range d.Lines {
		doc.Lines[i] = toLine(l, d.ShowCost)
	}
	doc.SupplierName, doc.TtnNumber = r.SupplierName, r.TtnNumber
	if r.ReceivingMode != nil {
		m := apispec.ReceivingMode(*r.ReceivingMode)
		doc.ReceivingMode = &m
	}
	if st := d.Receipt; st != nil {
		doc.Lines = make([]apispec.DocumentLine, len(st.Lines))
		for i, l := range st.Lines {
			line := toLine(l.ListLinesRow, d.ShowCost)
			line.QtyFact, line.QtyAccepted = &l.Fact, &l.Accepted
			line.Qty = l.Fact
			doc.Lines[i] = line
		}
		info := apispec.ReceiptInfo{
			Mode: apispec.ReceivingMode(st.Mode), Shortage: st.Shortage, Surplus: st.Surplus, Foreign: st.Foreign,
			Differences: st.Differences(), Packages: []apispec.ReceiptPackage{}, Unmapped: []apispec.UnmappedPosition{},
		}
		for _, p := range st.Packages {
			info.Packages = append(info.Packages, apispec.ReceiptPackage{
				Code: p.Code, Qty: p.Qty, ToOpen: p.ToOpen, Scanned: p.Scanned, Counted: p.Counted,
			})
		}
		for _, u := range st.Unmapped {
			info.Unmapped = append(info.Unmapped, apispec.UnmappedPosition{Identity: u.Identity, Alcocode: u.Alcocode, Name: u.Name, Qty: u.Qty})
		}
		doc.Receipt = &info
	}
	exchange := make([]apispec.ExchangeMessage, len(d.Exchange))
	for i, m := range d.Exchange {
		exchange[i] = apispec.ExchangeMessage{
			ID: m.ID, Direction: apispec.ExchangeMessageDirection(m.Direction), DocType: m.DocType,
			Status: apispec.ExchangeMessageStatus(m.Status), Attempts: m.Attempts, LastError: m.LastError,
			CreatedAt: m.CreatedAt,
		}
		if m.Reason != "" {
			exchange[i].Reason = &m.Reason
		}
		if m.Status == "queued" {
			exchange[i].NextAttemptAt = &m.NextAttemptAt
		}
	}
	doc.Exchange = &exchange
	for i, s := range d.Scans {
		doc.Scans[i] = toScan(s)
	}
	return doc
}

func toScanResult(r ScanResult, showCost bool) apispec.DocumentScanResult {
	s := r.Scan
	res := apispec.DocumentScanResult{Scan: toScan(store.ListScansRow{
		ID: s.ID, Raw: s.Raw, Status: s.Status, Reason: s.Reason, VariantID: s.VariantID, Article: s.Article,
		Name: s.Name, MarkCode: s.MarkCode, Qty: s.Qty, ScannedAt: s.ScannedAt, UndoneAt: s.UndoneAt,
	})}
	if r.Line != nil {
		line := toLine(*r.Line, showCost)
		res.Line = &line
	}
	return res
}

func (h *Handler) detail(ctx context.Context, id uuid.UUID) (apispec.Document, error) {
	d, err := h.svc.Get(ctx, id)
	if err != nil {
		return apispec.Document{}, problem(err)
	}
	return toDocument(d), nil
}

func (h *Handler) ListDocuments(ctx context.Context, req apispec.ListDocumentsRequestObject) (apispec.ListDocumentsResponseObject, error) {
	p := req.Params
	limit := int32(50)
	if p.Limit != nil {
		limit = int32(*p.Limit)
	}
	f := ListFilter{LocationID: p.LocationID, After: p.After, Limit: limit + 1}
	if p.Kind != nil {
		k := string(*p.Kind)
		f.Kind = &k
	}
	if p.Status != nil {
		st := string(*p.Status)
		f.Status = &st
	}
	rows, err := h.svc.List(ctx, f)
	if err != nil {
		return nil, problem(err)
	}
	out := apispec.DocumentList{Items: []apispec.DocumentSummary{}}
	if len(rows) > int(limit) {
		rows = rows[:limit]
		out.NextAfter = &rows[len(rows)-1].ID
	}
	for _, r := range rows {
		out.Items = append(out.Items, toSummary(r))
	}
	return apispec.ListDocuments200JSONResponse(out), nil
}

func comment(c *string) string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(*c)
}

func (h *Handler) CreateOpening(ctx context.Context, req apispec.CreateOpeningRequestObject) (apispec.CreateOpeningResponseObject, error) {
	id, err := h.svc.CreateOpening(ctx, req.Body.ZoneID, comment(req.Body.Comment))
	if err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, id)
	if err != nil {
		return nil, err
	}
	return apispec.CreateOpening201JSONResponse(doc), nil
}

func (h *Handler) CreateFloorTransfer(ctx context.Context, req apispec.CreateFloorTransferRequestObject) (apispec.CreateFloorTransferResponseObject, error) {
	id, err := h.svc.CreateFloorTransfer(ctx, req.Body.LocationID, req.Body.Direction, comment(req.Body.Comment))
	if err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, id)
	if err != nil {
		return nil, err
	}
	return apispec.CreateFloorTransfer201JSONResponse(doc), nil
}

func (h *Handler) CreateTransfer(ctx context.Context, req apispec.CreateTransferRequestObject) (apispec.CreateTransferResponseObject, error) {
	lines := make([]TransferLine, len(req.Body.Lines))
	for i, l := range req.Body.Lines {
		lines[i] = TransferLine{VariantID: l.VariantID, Qty: l.Qty}
	}
	id, err := h.svc.CreateTransfer(ctx, req.Body.FromLocationID, req.Body.ToLocationID, lines, comment(req.Body.Comment))
	if err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, id)
	if err != nil {
		return nil, err
	}
	return apispec.CreateTransfer201JSONResponse(doc), nil
}

func (h *Handler) ApproveTransfer(ctx context.Context, req apispec.ApproveTransferRequestObject) (apispec.ApproveTransferResponseObject, error) {
	if err := h.svc.DecideTransfer(ctx, req.ID, req.Body.Version, true, ""); err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.ApproveTransfer200JSONResponse(doc), nil
}

func (h *Handler) RejectTransfer(ctx context.Context, req apispec.RejectTransferRequestObject) (apispec.RejectTransferResponseObject, error) {
	if err := h.svc.DecideTransfer(ctx, req.ID, req.Body.Version, false, req.Body.Reason); err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.RejectTransfer200JSONResponse(doc), nil
}

func (h *Handler) GetDocument(ctx context.Context, req apispec.GetDocumentRequestObject) (apispec.GetDocumentResponseObject, error) {
	doc, err := h.detail(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.GetDocument200JSONResponse(doc), nil
}

func (h *Handler) AddDocumentScan(ctx context.Context, req apispec.AddDocumentScanRequestObject) (apispec.AddDocumentScanResponseObject, error) {
	b := req.Body
	scannedAt := b.ScannedAt
	// Часы устройства могут убегать вперёд; будущее время скана не принимаем.
	if now := time.Now(); scannedAt.After(now) {
		scannedAt = now
	}
	res, err := h.svc.AddScan(ctx, req.ID, NewScan{
		ID: b.ID, Raw: b.Raw, Source: string(b.Source), ScannedAt: scannedAt, VariantID: b.VariantID,
	})
	if err != nil {
		return nil, problem(err)
	}
	return apispec.AddDocumentScan200JSONResponse(toScanResult(res, auth.Has(ctx, apispec.PermissionCostView))), nil
}

func (h *Handler) UndoDocumentScan(ctx context.Context, req apispec.UndoDocumentScanRequestObject) (apispec.UndoDocumentScanResponseObject, error) {
	res, err := h.svc.UndoScan(ctx, req.ID, req.ScanID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UndoDocumentScan200JSONResponse(toScanResult(res, auth.Has(ctx, apispec.PermissionCostView))), nil
}

func (h *Handler) SetDocumentLine(ctx context.Context, req apispec.SetDocumentLineRequestObject) (apispec.SetDocumentLineResponseObject, error) {
	if err := h.svc.SetLine(ctx, req.ID, req.VariantID, req.Body.Qty); err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.SetDocumentLine200JSONResponse(doc), nil
}

func (h *Handler) PostDocument(ctx context.Context, req apispec.PostDocumentRequestObject) (apispec.PostDocumentResponseObject, error) {
	if err := h.svc.Post(ctx, req.ID, req.Body.Version); err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.PostDocument200JSONResponse(doc), nil
}

func (h *Handler) CancelDocument(ctx context.Context, req apispec.CancelDocumentRequestObject) (apispec.CancelDocumentResponseObject, error) {
	if err := h.svc.Cancel(ctx, req.ID, req.Body.Version); err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.CancelDocument200JSONResponse(doc), nil
}

func (h *Handler) SetReceivingMode(ctx context.Context, req apispec.SetReceivingModeRequestObject) (apispec.SetReceivingModeResponseObject, error) {
	if err := h.svc.SetMode(ctx, req.ID, string(req.Body.Mode)); err != nil {
		return nil, problem(err)
	}
	doc, err := h.detail(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return apispec.SetReceivingMode200JSONResponse(doc), nil
}
