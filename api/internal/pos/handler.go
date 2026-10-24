package pos

import (
	"context"
	"errors"
	"math"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/locations"
	"github.com/ADEXITUM/pikni/api/internal/posspec"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

var _ posspec.StrictServerInterface = (*Handler)(nil)

type ctxKey int

const (
	deviceKey ctxKey = iota
	cashierKey
)

// Middleware проверяет ключ кассы у каждого запроса и сессию кассира у всех, кроме входа.
func (s *Service) Middleware() posspec.StrictMiddlewareFunc {
	return func(next posspec.StrictHandlerFunc, operationID string) posspec.StrictHandlerFunc {
		return func(c *echo.Context, request any) (any, error) {
			req := c.Request()
			d, err := s.Device(req.Context(), req.Header.Get("Authorization"))
			if err != nil {
				return nil, problem(err)
			}
			ctx := context.WithValue(req.Context(), deviceKey, d)
			if operationID != "PosLogin" {
				pc, err := s.Authenticate(ctx, d, req.Header.Get("X-Cashier-Session"))
				if err != nil {
					return nil, problem(err)
				}
				ctx = context.WithValue(ctx, cashierKey, pc)
			}
			c.SetRequest(req.WithContext(ctx))
			return next(c, request)
		}
	}
}

func cashier(ctx context.Context) Context {
	pc, _ := ctx.Value(cashierKey).(Context)
	return pc
}

var problems = map[error]*echo.HTTPError{
	ErrNotFound:      echo.NewHTTPError(http.StatusNotFound, "Не найдено"),
	ErrDeviceKey:     echo.NewHTTPError(http.StatusUnauthorized, "Касса не зарегистрирована или отключена. Введите новый ключ кассы"),
	ErrBadPIN:        echo.NewHTTPError(http.StatusUnauthorized, "Неверный логин или PIN"),
	ErrLocked:        echo.NewHTTPError(http.StatusTooManyRequests, "Пять неверных PIN подряд — вход заблокирован на 15 минут"),
	ErrNotCashier:    echo.NewHTTPError(http.StatusForbidden, "У вас нет права продавать в этой точке"),
	ErrSession:       echo.NewHTTPError(http.StatusUnauthorized, "Сессия кассира истекла. Войдите заново"),
	ErrNoShift:       echo.NewHTTPError(http.StatusConflict, "Смена не открыта"),
	ErrShiftOpen:     echo.NewHTTPError(http.StatusConflict, "Смена уже открыта"),
	ErrInactivePoint: echo.NewHTTPError(http.StatusForbidden, "Точка кассы в архиве"),
}

func problem(err error) error {
	for target, he := range problems {
		if errors.Is(err, target) {
			return he
		}
	}
	return err
}

func rubles(kopecks int64) float64 { return float64(kopecks) / 100 }

func kopecks(rub float64) int64 { return int64(math.Round(rub * 100)) }

func posItem(it Item) posspec.PosItem {
	out := posspec.PosItem{
		VariantID: it.ID, Article: it.Article, Name: it.Name, Barcodes: it.Barcodes, Gtin: it.Gtin, Alcocode: it.Alcocode,
		Label:   catalog.PackLabel(it.VolumeMl, it.NetWeightG, it.Container, it.GiftPack),
		Alcohol: it.Alcohol, CrptMarked: it.CrptMarked, Abv: it.Abv, Price: rubles(it.PriceKopecks),
	}
	if it.EgaisAccounting != nil {
		a := posspec.Accounting(*it.EgaisAccounting)
		out.Accounting = &a
	}
	if it.Tobacco {
		out.Tobacco = &it.Tobacco
	}
	if it.MinPrice != nil {
		p := rubles(*it.MinPrice)
		out.MinPrice = &p
	}
	return out
}

func refusals(rs []Refusal) []posspec.PosRefusal {
	out := make([]posspec.PosRefusal, len(rs))
	for i, r := range rs {
		out[i] = posspec.PosRefusal{Code: r.Code, Message: r.Message}
	}
	return out
}

func posShift(sh *Shift) posspec.PosShift {
	out := posspec.PosShift{
		ID: sh.ID, Number: int(sh.Number), OpenedAt: sh.OpenedAt, ClosedAt: sh.ClosedAt,
		Receipts: int(sh.Totals.Receipts), Returns: int(sh.Totals.Returns),
		Cash: rubles(sh.Totals.CashKopecks), Card: rubles(sh.Totals.CardKopecks),
	}
	if sh.ClosedAt != nil {
		out.EgaisAct = &sh.EgaisAct
	}
	return out
}

func posReceipt(r Receipt) posspec.PosReceipt {
	out := posspec.PosReceipt{
		ID: r.ID, Number: int(r.Number), Kind: posspec.PosReceiptKind(r.Kind), ShiftNumber: int(r.ShiftNumber),
		Total: rubles(r.TotalKopecks), Payment: posspec.PosReceiptPayment(r.Payment), EgaisQr: r.EgaisQr,
		CreatedAt: r.ReceivedAt, Lines: make([]posspec.PosReceiptLine, len(r.Lines)),
	}
	for i, l := range r.Lines {
		qty := l.Qty
		if qty < 0 {
			qty = -qty
		}
		out.Lines[i] = posspec.PosReceiptLine{
			VariantID: l.VariantID, Name: l.Name, Label: catalog.PackLabel(l.VolumeMl, l.NetWeightG, l.Container, l.GiftPack),
			Qty: int(qty), Price: rubles(l.PriceKopecks), MarkCode: l.MarkCode, CrptCode: l.CrptCode,
		}
	}
	return out
}

func (h *Handler) PosLogin(ctx context.Context, req posspec.PosLoginRequestObject) (posspec.PosLoginResponseObject, error) {
	d, _ := ctx.Value(deviceKey).(Device)
	sess, err := h.svc.Login(ctx, d, req.Body.Login, req.Body.Pin)
	if err != nil {
		return nil, problem(err)
	}
	return posspec.PosLogin200JSONResponse{
		Token: sess.Token, CashierName: sess.Cashier.Name, DeviceName: d.Name, LocationName: d.Location,
		LocationCode: d.LocationCode, Timezone: d.Timezone, ExpiresAt: sess.ExpiresAt,
	}, nil
}

func (h *Handler) PosLogout(ctx context.Context, _ posspec.PosLogoutRequestObject) (posspec.PosLogoutResponseObject, error) {
	if err := h.svc.Logout(ctx, cashier(ctx)); err != nil {
		return nil, err
	}
	return posspec.PosLogout204Response{}, nil
}

func (h *Handler) PosCatalog(ctx context.Context, _ posspec.PosCatalogRequestObject) (posspec.PosCatalogResponseObject, error) {
	items, err := h.svc.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	out := posspec.PosCatalog200JSONResponse{Version: h.svc.now().UTC(), Items: make([]posspec.PosItem, len(items))}
	for i, it := range items {
		out.Items[i] = posItem(it)
	}
	return out, nil
}

func window(v locations.SaleVerdict) posspec.SaleWindow {
	w := posspec.SaleWindow{Allowed: v.Allowed}
	if v.Ban != "" {
		w.Ban = &v.Ban
		return w
	}
	from, to := clock(v.From), clock(v.To)
	w.From, w.To = &from, &to
	return w
}

func (h *Handler) PosTime(ctx context.Context, _ posspec.PosTimeRequestObject) (posspec.PosTimeResponseObject, error) {
	pc := cashier(ctx)
	sc, err := h.svc.Time(ctx, pc.Device)
	if err != nil {
		return nil, err
	}
	return posspec.PosTime200JSONResponse{
		ServerTime: h.svc.now().UTC(), LocalTime: sc.Local.Format("2006-01-02T15:04:05"), Timezone: pc.Device.Timezone,
		Strong: window(sc.Strong), Weak: window(sc.Other),
	}, nil
}

func (h *Handler) PosCurrentShift(ctx context.Context, _ posspec.PosCurrentShiftRequestObject) (posspec.PosCurrentShiftResponseObject, error) {
	sh, err := h.svc.CurrentShift(ctx, cashier(ctx))
	if err != nil {
		return nil, problem(err)
	}
	if sh == nil {
		return posspec.PosCurrentShift200JSONResponse{Open: false}, nil
	}
	out := posShift(sh)
	return posspec.PosCurrentShift200JSONResponse{Open: true, Shift: &out}, nil
}

func (h *Handler) PosOpenShift(ctx context.Context, _ posspec.PosOpenShiftRequestObject) (posspec.PosOpenShiftResponseObject, error) {
	sh, err := h.svc.OpenShift(ctx, cashier(ctx))
	if err != nil {
		return nil, problem(err)
	}
	return posspec.PosOpenShift201JSONResponse(posShift(sh)), nil
}

func (h *Handler) PosCloseShift(ctx context.Context, _ posspec.PosCloseShiftRequestObject) (posspec.PosCloseShiftResponseObject, error) {
	sh, err := h.svc.CloseShift(ctx, cashier(ctx))
	if err != nil {
		return nil, problem(err)
	}
	return posspec.PosCloseShift200JSONResponse(posShift(sh)), nil
}

func (h *Handler) PosCheck(ctx context.Context, req posspec.PosCheckRequestObject) (posspec.PosCheckResponseObject, error) {
	c, err := h.svc.Check(ctx, cashier(ctx), req.Body.Raw)
	if err != nil {
		return nil, problem(err)
	}
	out := posspec.PosCheck200JSONResponse{Refusals: refusals(c.Refusals)}
	if c.Item != nil {
		it := posItem(*c.Item)
		out.Item = &it
	}
	if c.Mark != "" {
		out.MarkCode = &c.Mark
	}
	if c.Crpt != "" {
		out.CrptCode = &c.Crpt
	}
	if c.Item != nil {
		p := rubles(c.Price)
		out.Price = &p
	}
	if c.MaxPrice != nil {
		p := rubles(*c.MaxPrice)
		out.MaxPrice = &p
	}
	return out, nil
}

func (h *Handler) PosCreateReceipt(ctx context.Context, req posspec.PosCreateReceiptRequestObject) (posspec.PosCreateReceiptResponseObject, error) {
	b := req.Body
	r := NewReceipt{
		ID: b.ID, Refund: b.Kind == "return", ReturnOf: b.ReturnOf, Card: b.Payment == "card",
		AgeConfirmed: b.AgeConfirmed, CreatedAt: b.CreatedOnDeviceAt, Offline: b.Offline != nil && *b.Offline,
	}
	for _, l := range b.Lines {
		nl := NewLine{VariantID: l.VariantID, Qty: int32(min(max(l.Qty, 0), math.MaxInt32)), Price: kopecks(l.Price)}
		if l.MarkCode != nil {
			nl.Mark = *l.MarkCode
		}
		if l.CrptCode != nil {
			nl.Crpt = *l.CrptCode
		}
		if nl.Qty < 1 {
			return posspec.PosCreateReceipt422JSONResponse{Code: "receipt.qty", Message: "Количество должно быть не меньше 1"}, nil
		}
		r.Lines = append(r.Lines, nl)
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = h.svc.now()
	}
	pc := cashier(ctx)
	id, err := h.svc.CreateReceipt(ctx, pc, r)
	if re, ok := errors.AsType[*RefusalError](err); ok {
		return posspec.PosCreateReceipt422JSONResponse{Code: re.Code, Message: re.Message}, nil
	}
	if err != nil {
		return nil, problem(err)
	}
	saved, err := h.svc.Receipt(ctx, pc, id)
	if err != nil {
		return nil, problem(err)
	}
	return posspec.PosCreateReceipt201JSONResponse(posReceipt(saved)), nil
}

func (h *Handler) PosFindReceipts(ctx context.Context, req posspec.PosFindReceiptsRequestObject) (posspec.PosFindReceiptsResponseObject, error) {
	var number *int32
	if req.Params.Number != nil {
		n := int32(min(max(*req.Params.Number, 0), math.MaxInt32))
		number = &n
	}
	found, err := h.svc.FindReceipts(ctx, cashier(ctx), number, req.Params.Mark)
	if err != nil {
		return nil, problem(err)
	}
	out := posspec.PosFindReceipts200JSONResponse{Items: make([]posspec.PosReceipt, len(found))}
	for i, r := range found {
		out.Items[i] = posReceipt(r)
	}
	return out, nil
}
