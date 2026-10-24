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
	ErrInvalidINN:       echo.NewHTTPError(http.StatusUnprocessableEntity, "ИНН — 10 или 12 цифр, контрольные цифры должны сходиться"),
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
	ErrAlcoholInherited:     echo.NewHTTPError(http.StatusUnprocessableEntity, "Признак алкоголя задаётся у категории верхнего уровня"),
	ErrAlcoholProducts:      echo.NewHTTPError(http.StatusConflict, "В этой ветке есть позиции с видом продукции или крепостью — сначала уберите их"),

	ErrConflict:         echo.NewHTTPError(http.StatusConflict, "Позицию уже изменил другой пользователь. Обновите страницу и повторите"),
	ErrInvalidAbv:       echo.NewHTTPError(http.StatusUnprocessableEntity, "Крепость — от 0 до 100 %"),
	ErrAbvRequired:      echo.NewHTTPError(http.StatusUnprocessableEntity, "Для алкогольной продукции укажите крепость"),
	ErrNotAlcoholic:     echo.NewHTTPError(http.StatusUnprocessableEntity, "Вид продукции и крепость указываются только в алкогольных категориях"),
	ErrInactiveCategory: echo.NewHTTPError(http.StatusUnprocessableEntity, "Категория в архиве"),
	ErrLineNeedsBrand:   echo.NewHTTPError(http.StatusUnprocessableEntity, "Линейку можно выбрать только вместе с брендом"),
	ErrBrandMismatch:    echo.NewHTTPError(http.StatusUnprocessableEntity, "Бренд принадлежит другому производителю"),
	ErrLineMismatch:     echo.NewHTTPError(http.StatusUnprocessableEntity, "Линейка относится к другому бренду"),
	ErrRegionMismatch:   echo.NewHTTPError(http.StatusUnprocessableEntity, "Регион относится к другой стране"),

	ErrVariantExists:    echo.NewHTTPError(http.StatusConflict, "Такой вариант уже есть"),
	ErrVolumeRequired:   echo.NewHTTPError(http.StatusUnprocessableEntity, "Для продукции в ЕГАИС укажите объём"),
	ErrVolumeAndWeight:  echo.NewHTTPError(http.StatusUnprocessableEntity, "Укажите либо объём, либо массу нетто"),
	ErrWeightForEgais:   echo.NewHTTPError(http.StatusUnprocessableEntity, "Фасовка продукции в ЕГАИС — объём, а не масса"),
	ErrInvalidWeight:    echo.NewHTTPError(http.StatusUnprocessableEntity, "Масса нетто — от 1 г до 100 кг"),
	ErrUnknownVolume:    echo.NewHTTPError(http.StatusUnprocessableEntity, "Такого объёма нет в справочнике"),
	ErrInvalidContainer: echo.NewHTTPError(http.StatusUnprocessableEntity, "Неизвестный вид тары"),
	ErrInvalidAlcocode:  echo.NewHTTPError(http.StatusUnprocessableEntity, "Алкокод — 19 цифр"),
	ErrAlcocodeNotEgais: echo.NewHTTPError(http.StatusUnprocessableEntity, "Алкокод бывает только у продукции в ЕГАИС"),
	ErrAlcocodeTaken:    echo.NewHTTPError(http.StatusConflict, "Этот алкокод уже у другого варианта"),
	ErrInvalidGTIN:      echo.NewHTTPError(http.StatusUnprocessableEntity, "GTIN — 8, 12, 13 или 14 цифр с верной контрольной цифрой"),
	ErrGTINTaken:        echo.NewHTTPError(http.StatusConflict, "Этот GTIN уже у другого варианта"),
	ErrInvalidBarcode:   echo.NewHTTPError(http.StatusUnprocessableEntity, "Неверный штрихкод: у EAN и UPC не сходится контрольная цифра, другие коды — до 48 латинских символов и цифр"),
	ErrBarcodeTaken:     echo.NewHTTPError(http.StatusConflict, "Этот штрихкод уже привязан к другому варианту"),

	ErrInvalidPrice:     echo.NewHTTPError(http.StatusUnprocessableEntity, "Цена — больше нуля"),
	ErrPriceInPast:      echo.NewHTTPError(http.StatusUnprocessableEntity, "Цену нельзя назначить задним числом"),
	ErrPriceInEffect:    echo.NewHTTPError(http.StatusConflict, "Цена уже действует — это история, её не отменить. Назначьте новую"),
	ErrMinPriceInEffect: echo.NewHTTPError(http.StatusConflict, "Редакция МРЦ уже действует, удалить можно только будущую"),
	ErrInvalidMinPrice:  echo.NewHTTPError(http.StatusUnprocessableEntity, "Проверьте МРЦ: цена и объём больше нуля, крепость от 0 до 100 %, основание указано"),

	ErrInvalidKPP:           echo.NewHTTPError(http.StatusUnprocessableEntity, "КПП — 9 цифр; у предпринимателя КПП нет"),
	ErrInvalidFSRARID:       echo.NewHTTPError(http.StatusUnprocessableEntity, "ФСРАР ID — 12 цифр"),
	ErrFSRARIDTaken:         echo.NewHTTPError(http.StatusConflict, "Этот ФСРАР ID уже у другого поставщика"),
	ErrSupplierExists:       echo.NewHTTPError(http.StatusConflict, "Поставщик с таким ИНН и КПП уже есть"),
	ErrInvalidReceivingMode: echo.NewHTTPError(http.StatusUnprocessableEntity, "Неизвестный режим приёмки"),
	ErrInvalidEmail:         echo.NewHTTPError(http.StatusUnprocessableEntity, "Проверьте адрес электронной почты"),

	ErrImportTooLarge: echo.NewHTTPError(http.StatusUnprocessableEntity, "Файл больше 5 МБ или 5000 строк — разбейте его на части"),
	ErrImportEmpty:    echo.NewHTTPError(http.StatusUnprocessableEntity, "В файле нет строк с данными"),
	ErrImportFormat:   echo.NewHTTPError(http.StatusUnprocessableEntity, "Нужен файл XLSX или CSV по шаблону"),

	ErrInvalidMinQty: echo.NewHTTPError(http.StatusUnprocessableEntity, "Минимальный остаток — от 1 до 9999"),
	ErrSameLocation:  echo.NewHTTPError(http.StatusUnprocessableEntity, "Выберите другую точку: копировать матрицу в саму себя незачем"),
}

var attributeReasons = map[error]string{
	ErrUnknownAttribute:  "её нет у этой категории",
	ErrArchivedAttribute: "она в архиве, новое значение не принимается",
	ErrWrongValueType:    "значение не того типа",
	ErrUnknownOption:     "такого значения нет в списке",
	ErrArchivedOption:    "значение в архиве",
}

func problem(err error) error {
	if ii, ok := errors.AsType[importIssue](err); ok {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, string(ii))
	}
	if be, ok := errors.AsType[*BelowMinPriceError](err); ok {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "Цена ниже минимальной розничной (МРЦ): не меньше "+rubles(be.Min))
	}
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

// userMessage — текст ошибки для пользователя, если ошибка из числа ожидаемых (неверные данные).
// Импорт кладёт такие ошибки в отчёт по строкам, а неожиданные обрывают загрузку.
func userMessage(err error) (string, bool) {
	he, ok := errors.AsType[*echo.HTTPError](problem(err))
	if !ok {
		return "", false
	}
	return fmt.Sprint(he.Message), true
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
	items, err := list(rows, err, func(k store.ListProductKindsRow) apispec.ProductKind {
		return apispec.ProductKind{Code: k.Code, Name: k.Name, EgaisAccounting: apispec.EgaisAccounting(k.EgaisAccounting), MinPriceGroup: k.MinPriceGroup}
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
