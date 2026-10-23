package locations

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/locations/store"
)

var saleProblems = map[error]*echo.HTTPError{
	ErrRuleNotFound:   echo.NewHTTPError(http.StatusNotFound, "Редакция правила не найдена"),
	ErrBanNotFound:    echo.NewHTTPError(http.StatusNotFound, "День запрета не найден"),
	ErrRuleInEffect:   echo.NewHTTPError(http.StatusConflict, "Редакция уже действует, удалить её нельзя. Добавьте новую с нужной датой начала"),
	ErrRuleEdition:    echo.NewHTTPError(http.StatusConflict, "Редакция с этой датой начала уже есть"),
	ErrUnknownSubject: echo.NewHTTPError(http.StatusUnprocessableEntity, "Такого субъекта РФ нет в справочнике"),
	ErrInvalidScope:   echo.NewHTTPError(http.StatusUnprocessableEntity, "Неизвестный вид продукции"),
	ErrInvalidWindow:  echo.NewHTTPError(http.StatusUnprocessableEntity, "Окно продажи: время в формате ЧЧ:ММ, начало раньше конца"),
	ErrInvalidBanDate: echo.NewHTTPError(http.StatusUnprocessableEntity, "Укажите либо конкретную дату, либо ежегодную в формате ММ-ДД"),
	ErrEmptySource:    echo.NewHTTPError(http.StatusUnprocessableEntity, "Укажите название и закон-основание"),
}

func saleProblem(err error) error {
	for target, he := range saleProblems {
		if errors.Is(err, target) {
			return he
		}
	}
	return problem(err)
}

func (h *Handler) ListSaleRules(ctx context.Context, _ apispec.ListSaleRulesRequestObject) (apispec.ListSaleRulesResponseObject, error) {
	book, err := h.svc.Rulebook(ctx)
	if err != nil {
		return nil, err
	}
	resp := apispec.SaleRulebook{
		Rules: make([]apispec.SaleRule, len(book.Rules)),
		Bans:  make([]apispec.SaleBan, len(book.Bans)),
	}
	for i, r := range book.Rules {
		resp.Rules[i] = toAPIRule(store.CreateSaleRuleRow(r))
	}
	for i, b := range book.Bans {
		resp.Bans[i] = toAPIBan(store.CreateSaleBanRow(b))
	}
	return apispec.ListSaleRules200JSONResponse(resp), nil
}

func (h *Handler) CreateSaleRule(ctx context.Context, req apispec.CreateSaleRuleRequestObject) (apispec.CreateSaleRuleResponseObject, error) {
	b := req.Body
	row, err := h.svc.CreateSaleRule(ctx, NewSaleRule{
		Subject: b.SubjectCode, Scope: b.Scope, ValidFrom: b.ValidFrom.Time, From: b.From, To: b.To, Source: b.Source,
	})
	if err != nil {
		return nil, saleProblem(err)
	}
	return apispec.CreateSaleRule201JSONResponse(toAPIRule(row)), nil
}

func (h *Handler) DeleteSaleRule(ctx context.Context, req apispec.DeleteSaleRuleRequestObject) (apispec.DeleteSaleRuleResponseObject, error) {
	if err := h.svc.DeleteSaleRule(ctx, req.ID); err != nil {
		return nil, saleProblem(err)
	}
	return apispec.DeleteSaleRule204Response{}, nil
}

func (h *Handler) CreateSaleBan(ctx context.Context, req apispec.CreateSaleBanRequestObject) (apispec.CreateSaleBanResponseObject, error) {
	b := req.Body
	row, err := h.svc.CreateSaleBan(ctx, NewSaleBan{
		Subject: b.SubjectCode, Name: b.Name, Scope: b.Scope, Date: fromDate(b.Date), MonthDay: b.MonthDay, Source: b.Source,
	})
	if err != nil {
		return nil, saleProblem(err)
	}
	return apispec.CreateSaleBan201JSONResponse(toAPIBan(row)), nil
}

func (h *Handler) DeleteSaleBan(ctx context.Context, req apispec.DeleteSaleBanRequestObject) (apispec.DeleteSaleBanResponseObject, error) {
	if err := h.svc.DeleteSaleBan(ctx, req.ID); err != nil {
		return nil, saleProblem(err)
	}
	return apispec.DeleteSaleBan204Response{}, nil
}

func (h *Handler) CheckSale(ctx context.Context, req apispec.CheckSaleRequestObject) (apispec.CheckSaleResponseObject, error) {
	at := time.Now()
	if req.Params.At != nil {
		at = *req.Params.At
	}
	check, err := h.svc.CheckSale(ctx, req.ID, at)
	if err != nil {
		return nil, saleProblem(err)
	}
	return apispec.CheckSale200JSONResponse{
		LocalTime: check.Local.Format(time.RFC3339),
		Strong:    toAPIVerdict(check.Strong),
		Other:     toAPIVerdict(check.Other),
	}, nil
}

func toAPIRule(r store.CreateSaleRuleRow) apispec.SaleRule {
	return apispec.SaleRule{
		ID: r.ID, SubjectCode: r.SubjectCode, Scope: apispec.SaleScope(r.Scope),
		ValidFrom: openapi_types.Date{Time: r.ValidFrom},
		From:      formatMinutes(minutesOf(r.SaleFrom)), To: formatMinutes(minutesOf(r.SaleTo)),
		Source: r.Source,
	}
}

func toAPIBan(b store.CreateSaleBanRow) apispec.SaleBan {
	ban := apispec.SaleBan{
		ID: b.ID, SubjectCode: b.SubjectCode, Name: b.Name, Scope: apispec.SaleScope(b.Scope),
		MonthDay: b.MonthDay, Source: b.Source,
	}
	if b.Date != nil {
		ban.Date = &openapi_types.Date{Time: *b.Date}
	}
	return ban
}

func toAPIVerdict(v SaleVerdict) apispec.SaleVerdict {
	if v.Ban != "" {
		return apispec.SaleVerdict{Allowed: false, BanDay: &v.Ban}
	}
	from, to := formatMinutes(v.From), formatMinutes(v.To)
	return apispec.SaleVerdict{Allowed: v.Allowed, From: &from, To: &to}
}
