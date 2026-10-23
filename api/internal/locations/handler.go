package locations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

var problems = map[error]*echo.HTTPError{
	ErrNotFound:        echo.NewHTTPError(http.StatusNotFound, "Точка не найдена"),
	ErrConflict:        echo.NewHTTPError(http.StatusConflict, "Карточку точки уже изменил другой пользователь. Обновите страницу и повторите"),
	ErrCodeTaken:       echo.NewHTTPError(http.StatusConflict, "Точка с таким кодом уже есть"),
	ErrFSRARIDTaken:    echo.NewHTTPError(http.StatusConflict, "Этот ФСРАР ID уже указан у другой точки"),
	ErrInvalidCode:     echo.NewHTTPError(http.StatusUnprocessableEntity, "Код: от 2 до 16 символов — заглавные буквы, цифры и дефис, например МГ-05"),
	ErrInvalidKind:     echo.NewHTTPError(http.StatusUnprocessableEntity, "Неизвестный тип точки"),
	ErrEmptyField:      echo.NewHTTPError(http.StatusUnprocessableEntity, "Укажите название и город"),
	ErrInvalidTimezone: echo.NewHTTPError(http.StatusUnprocessableEntity, "Неизвестный часовой пояс"),
	ErrUnknownSubject:  echo.NewHTTPError(http.StatusUnprocessableEntity, "Такого субъекта РФ нет в справочнике"),
	ErrInvalidFSRARID:  echo.NewHTTPError(http.StatusUnprocessableEntity, "ФСРАР ID — 12 цифр"),
	ErrInvalidKPP:      echo.NewHTTPError(http.StatusUnprocessableEntity, "КПП — 9 цифр"),
	ErrCoordinates:     echo.NewHTTPError(http.StatusUnprocessableEntity, "Укажите обе координаты: широту от −90 до 90 и долготу от −180 до 180"),
	ErrOpeningHours:    echo.NewHTTPError(http.StatusUnprocessableEntity, "Часы работы: время в формате ЧЧ:ММ, открытие раньше закрытия"),
}

func problem(err error) error {
	for target, he := range problems {
		if errors.Is(err, target) {
			return he
		}
	}
	return err
}

func (h *Handler) ListLocations(ctx context.Context, _ apispec.ListLocationsRequestObject) (apispec.ListLocationsResponseObject, error) {
	cards, err := h.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	// Не nil: пустой список должен уйти клиенту как [], а не null.
	items := make([]apispec.Location, 0, len(cards))
	for _, c := range cards {
		items = append(items, toAPI(c))
	}
	return apispec.ListLocations200JSONResponse{Items: items}, nil
}

func (h *Handler) GetLocation(ctx context.Context, req apispec.GetLocationRequestObject) (apispec.GetLocationResponseObject, error) {
	card, err := h.svc.Get(ctx, req.ID)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.GetLocation200JSONResponse(toAPI(card)), nil
}

func (h *Handler) CreateLocation(ctx context.Context, req apispec.CreateLocationRequestObject) (apispec.CreateLocationResponseObject, error) {
	b := req.Body
	f := Fields{
		Name: b.Name, City: b.City, Timezone: b.Timezone, SubjectCode: b.SubjectCode,
		Address: b.Address, Lat: b.Lat, Lon: b.Lon, FSRARID: b.FSRARID, KPP: b.KPP,
		LicenseNo: b.LicenseNo, LicenseValidTo: fromDate(b.LicenseValidTo),
		OpeningHours: b.OpeningHours, IsPublic: b.IsPublic,
	}
	card, err := h.svc.Create(ctx, b.Code, b.Kind, f)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateLocation201JSONResponse(toAPI(card)), nil
}

func (h *Handler) UpdateLocation(ctx context.Context, req apispec.UpdateLocationRequestObject) (apispec.UpdateLocationResponseObject, error) {
	b := req.Body
	f := Fields{
		Name: b.Name, City: b.City, Timezone: b.Timezone, SubjectCode: b.SubjectCode,
		Address: b.Address, Lat: b.Lat, Lon: b.Lon, FSRARID: b.FSRARID, KPP: b.KPP,
		LicenseNo: b.LicenseNo, LicenseValidTo: fromDate(b.LicenseValidTo),
		OpeningHours: b.OpeningHours, IsPublic: b.IsPublic,
	}
	card, err := h.svc.Update(ctx, req.ID, f, b.IsActive, b.Version)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateLocation200JSONResponse(toAPI(card)), nil
}

func (h *Handler) ListRuSubjects(ctx context.Context, _ apispec.ListRuSubjectsRequestObject) (apispec.ListRuSubjectsResponseObject, error) {
	rows, err := h.svc.Subjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}
	items := make([]apispec.RuSubject, len(rows))
	for i, r := range rows {
		items[i] = apispec.RuSubject{Code: r.Code, Name: r.Name}
	}
	return apispec.ListRuSubjects200JSONResponse{Items: items}, nil
}

func toAPI(c Card) apispec.Location {
	var hours apispec.OpeningHours
	_ = json.Unmarshal(c.OpeningHours, &hours)

	zones := make([]apispec.Zone, len(c.Zones))
	for i, z := range c.Zones {
		zones[i] = apispec.Zone{ID: z.ID, Kind: apispec.ZoneKind(z.Kind), Name: z.Name}
	}

	l := apispec.Location{
		ID: c.ID, Code: c.Code, Name: c.Name, Kind: apispec.LocationKind(c.Kind), City: c.City,
		Timezone: c.Timezone, SubjectCode: c.SubjectCode, Address: c.Address, Lat: c.Lat, Lon: c.Lon,
		FSRARID: c.FSRARID, KPP: c.KPP, LicenseNo: c.LicenseNo, OpeningHours: hours,
		IsPublic: c.IsPublic, IsActive: c.IsActive, Version: c.Version, Zones: zones,
	}
	if c.LicenseValidTo != nil {
		l.LicenseValidTo = &openapi_types.Date{Time: *c.LicenseValidTo}
	}
	return l
}

func fromDate(d *openapi_types.Date) *time.Time {
	if d == nil {
		return nil
	}
	return &d.Time
}
