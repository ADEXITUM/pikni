package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

var problems = map[error]*echo.HTTPError{
	ErrNotFound:         echo.NewHTTPError(http.StatusNotFound, "Запись не найдена"),
	ErrDuplicate:        echo.NewHTTPError(http.StatusConflict, "Такая запись уже есть"),
	ErrUnknownReference: echo.NewHTTPError(http.StatusUnprocessableEntity, "Выбранной записи нет в справочнике"),
	ErrEmptyName:        echo.NewHTTPError(http.StatusUnprocessableEntity, "Укажите название"),
	ErrInvalidINN:       echo.NewHTTPError(http.StatusUnprocessableEntity, "ИНН — 10 или 12 цифр"),
	ErrInvalidVolume:    echo.NewHTTPError(http.StatusUnprocessableEntity, "Объём — от 10 до 30 000 мл"),
	ErrInvalidEgaisType: echo.NewHTTPError(http.StatusUnprocessableEntity, "Неизвестный тип списания ЕГАИС"),

	ErrCategoryTooDeep:      echo.NewHTTPError(http.StatusUnprocessableEntity, "Категории — не больше трёх уровней"),
	ErrInactiveParent:       echo.NewHTTPError(http.StatusUnprocessableEntity, "Родительская категория в архиве"),
	ErrActiveSubcategories:  echo.NewHTTPError(http.StatusConflict, "Сначала отправьте в архив подкатегории"),
	ErrInvalidCode:          echo.NewHTTPError(http.StatusUnprocessableEntity, "Код — от 2 до 32 символов: латиница в нижнем регистре, цифры и _, первая — буква"),
	ErrInvalidAttributeType: echo.NewHTTPError(http.StatusUnprocessableEntity, "Неизвестный тип характеристики"),
	ErrCodeTaken:            echo.NewHTTPError(http.StatusConflict, "Характеристика с таким кодом уже есть в этой ветке категорий"),
	ErrInvalidOptions:       echo.NewHTTPError(http.StatusUnprocessableEntity, "Значения списка не должны быть пустыми или повторяться"),
	ErrOptionRemoved:        echo.NewHTTPError(http.StatusUnprocessableEntity, "Значение списка нельзя удалить — отправьте его в архив"),
	ErrNoActiveOptions:      echo.NewHTTPError(http.StatusUnprocessableEntity, "В списке должно остаться хотя бы одно действующее значение"),

	ErrConflict:         echo.NewHTTPError(http.StatusConflict, "Позицию уже изменил другой пользователь. Обновите страницу и повторите"),
	ErrInvalidAbv:       echo.NewHTTPError(http.StatusUnprocessableEntity, "Крепость — от 0 до 100 %"),
	ErrAbvRequired:      echo.NewHTTPError(http.StatusUnprocessableEntity, "Для алкогольной продукции укажите крепость"),
	ErrInactiveCategory: echo.NewHTTPError(http.StatusUnprocessableEntity, "Категория в архиве"),
	ErrLineNeedsBrand:   echo.NewHTTPError(http.StatusUnprocessableEntity, "Линейку можно выбрать только вместе с брендом"),
	ErrBrandMismatch:    echo.NewHTTPError(http.StatusUnprocessableEntity, "Бренд принадлежит другому производителю"),
	ErrLineMismatch:     echo.NewHTTPError(http.StatusUnprocessableEntity, "Линейка относится к другому бренду"),
	ErrRegionMismatch:   echo.NewHTTPError(http.StatusUnprocessableEntity, "Регион относится к другой стране"),
}

var attributeReasons = map[error]string{
	ErrUnknownAttribute:  "её нет у этой категории",
	ErrArchivedAttribute: "она в архиве, новое значение не принимается",
	ErrWrongValueType:    "значение не того типа",
	ErrUnknownOption:     "такого значения нет в списке",
	ErrArchivedOption:    "значение в архиве",
}

func problem(err error) error {
	if ae, ok := errors.AsType[*AttributeError](err); ok {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, fmt.Sprintf("Характеристика «%s»: %s", ae.Attribute, attributeReasons[ae.Err]))
	}
	for target, he := range problems {
		if errors.Is(err, target) {
			return he
		}
	}
	return err
}

// list переводит строки справочника в ответ API; пустой список уходит как [], а не null.
func list[R, A any](rows []R, err error, convert func(R) A) ([]A, error) {
	if err != nil {
		return nil, err
	}
	items := make([]A, len(rows))
	for i, r := range rows {
		items[i] = convert(r)
	}
	return items, nil
}

func (h *Handler) ListCountries(ctx context.Context, _ apispec.ListCountriesRequestObject) (apispec.ListCountriesResponseObject, error) {
	rows, err := h.svc.Countries(ctx)
	items, err := list(rows, err, func(c store.Country) apispec.Country {
		return apispec.Country{Code: c.Code, Alpha2: c.Alpha2, Name: c.Name}
	})
	if err != nil {
		return nil, err
	}
	return apispec.ListCountries200JSONResponse{Items: items}, nil
}

func (h *Handler) ListProductKinds(ctx context.Context, _ apispec.ListProductKindsRequestObject) (apispec.ListProductKindsResponseObject, error) {
	rows, err := h.svc.ProductKinds(ctx)
	items, err := list(rows, err, func(k store.ProductKind) apispec.ProductKind {
		return apispec.ProductKind{Code: k.Code, Name: k.Name, EgaisAccounting: apispec.EgaisAccounting(k.EgaisAccounting)}
	})
	if err != nil {
		return nil, err
	}
	return apispec.ListProductKinds200JSONResponse{Items: items}, nil
}

func toOriginRegion(r store.OriginRegion) apispec.OriginRegion {
	return apispec.OriginRegion{ID: r.ID, CountryCode: r.CountryCode, Name: r.Name, IsActive: r.IsActive}
}

func (h *Handler) ListOriginRegions(ctx context.Context, _ apispec.ListOriginRegionsRequestObject) (apispec.ListOriginRegionsResponseObject, error) {
	rows, err := h.svc.OriginRegions(ctx)
	items, err := list(rows, err, toOriginRegion)
	if err != nil {
		return nil, err
	}
	return apispec.ListOriginRegions200JSONResponse{Items: items}, nil
}

func (h *Handler) CreateOriginRegion(ctx context.Context, req apispec.CreateOriginRegionRequestObject) (apispec.CreateOriginRegionResponseObject, error) {
	r, err := h.svc.SaveOriginRegion(ctx, nil, *req.Body, true)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateOriginRegion201JSONResponse(toOriginRegion(r)), nil
}

func (h *Handler) UpdateOriginRegion(ctx context.Context, req apispec.UpdateOriginRegionRequestObject) (apispec.UpdateOriginRegionResponseObject, error) {
	b := req.Body
	r, err := h.svc.SaveOriginRegion(ctx, &req.ID, apispec.OriginRegionFields{CountryCode: b.CountryCode, Name: b.Name}, b.IsActive)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateOriginRegion200JSONResponse(toOriginRegion(r)), nil
}

func toProducer(p store.Producer) apispec.Producer {
	return apispec.Producer{ID: p.ID, Name: p.Name, INN: p.INN, CountryCode: p.CountryCode, IsActive: p.IsActive}
}

func (h *Handler) ListProducers(ctx context.Context, _ apispec.ListProducersRequestObject) (apispec.ListProducersResponseObject, error) {
	rows, err := h.svc.Producers(ctx)
	items, err := list(rows, err, toProducer)
	if err != nil {
		return nil, err
	}
	return apispec.ListProducers200JSONResponse{Items: items}, nil
}

func (h *Handler) CreateProducer(ctx context.Context, req apispec.CreateProducerRequestObject) (apispec.CreateProducerResponseObject, error) {
	p, err := h.svc.SaveProducer(ctx, nil, *req.Body, true)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateProducer201JSONResponse(toProducer(p)), nil
}

func (h *Handler) UpdateProducer(ctx context.Context, req apispec.UpdateProducerRequestObject) (apispec.UpdateProducerResponseObject, error) {
	b := req.Body
	p, err := h.svc.SaveProducer(ctx, &req.ID, apispec.ProducerFields{Name: b.Name, INN: b.INN, CountryCode: b.CountryCode}, b.IsActive)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateProducer200JSONResponse(toProducer(p)), nil
}

func toBrand(b store.Brand) apispec.Brand {
	return apispec.Brand{ID: b.ID, ProducerID: b.ProducerID, Name: b.Name, IsActive: b.IsActive}
}

func (h *Handler) ListBrands(ctx context.Context, _ apispec.ListBrandsRequestObject) (apispec.ListBrandsResponseObject, error) {
	rows, err := h.svc.Brands(ctx)
	items, err := list(rows, err, toBrand)
	if err != nil {
		return nil, err
	}
	return apispec.ListBrands200JSONResponse{Items: items}, nil
}

func (h *Handler) CreateBrand(ctx context.Context, req apispec.CreateBrandRequestObject) (apispec.CreateBrandResponseObject, error) {
	b, err := h.svc.SaveBrand(ctx, nil, *req.Body, true)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateBrand201JSONResponse(toBrand(b)), nil
}

func (h *Handler) UpdateBrand(ctx context.Context, req apispec.UpdateBrandRequestObject) (apispec.UpdateBrandResponseObject, error) {
	b := req.Body
	saved, err := h.svc.SaveBrand(ctx, &req.ID, apispec.BrandFields{ProducerID: b.ProducerID, Name: b.Name}, b.IsActive)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateBrand200JSONResponse(toBrand(saved)), nil
}

func toVolume(v store.Volume) apispec.Volume {
	return apispec.Volume{Ml: v.Ml, IsActive: v.IsActive}
}

func (h *Handler) ListVolumes(ctx context.Context, _ apispec.ListVolumesRequestObject) (apispec.ListVolumesResponseObject, error) {
	rows, err := h.svc.Volumes(ctx)
	items, err := list(rows, err, toVolume)
	if err != nil {
		return nil, err
	}
	return apispec.ListVolumes200JSONResponse{Items: items}, nil
}

func (h *Handler) CreateVolume(ctx context.Context, req apispec.CreateVolumeRequestObject) (apispec.CreateVolumeResponseObject, error) {
	v, err := h.svc.CreateVolume(ctx, req.Body.Ml)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateVolume201JSONResponse(toVolume(v)), nil
}

func (h *Handler) UpdateVolume(ctx context.Context, req apispec.UpdateVolumeRequestObject) (apispec.UpdateVolumeResponseObject, error) {
	v, err := h.svc.SetVolumeActive(ctx, req.Ml, req.Body.IsActive)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateVolume200JSONResponse(toVolume(v)), nil
}

func toWriteoffReason(r store.WriteoffReason) apispec.WriteoffReason {
	return apispec.WriteoffReason{ID: r.ID, Name: r.Name, EgaisType: apispec.EgaisWriteoffType(r.EgaisType), IsActive: r.IsActive}
}

func (h *Handler) ListWriteoffReasons(ctx context.Context, _ apispec.ListWriteoffReasonsRequestObject) (apispec.ListWriteoffReasonsResponseObject, error) {
	rows, err := h.svc.WriteoffReasons(ctx)
	items, err := list(rows, err, toWriteoffReason)
	if err != nil {
		return nil, err
	}
	return apispec.ListWriteoffReasons200JSONResponse{Items: items}, nil
}

func (h *Handler) CreateWriteoffReason(ctx context.Context, req apispec.CreateWriteoffReasonRequestObject) (apispec.CreateWriteoffReasonResponseObject, error) {
	r, err := h.svc.SaveWriteoffReason(ctx, nil, *req.Body, true)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.CreateWriteoffReason201JSONResponse(toWriteoffReason(r)), nil
}

func (h *Handler) UpdateWriteoffReason(ctx context.Context, req apispec.UpdateWriteoffReasonRequestObject) (apispec.UpdateWriteoffReasonResponseObject, error) {
	b := req.Body
	r, err := h.svc.SaveWriteoffReason(ctx, &req.ID, apispec.WriteoffReasonFields{Name: b.Name, EgaisType: b.EgaisType}, b.IsActive)
	if err != nil {
		return nil, problem(err)
	}
	return apispec.UpdateWriteoffReason200JSONResponse(toWriteoffReason(r)), nil
}
