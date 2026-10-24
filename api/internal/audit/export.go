package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/xuri/excelize/v2"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit/store"
)

const exportLimit = 50_000

// Названия событий для выгрузки. На экране клиент берёт их из своего словаря.
var actionTitles = map[apispec.AuditAction]string{
	apispec.AuditActionAuthLogin:           "Вход",
	apispec.AuditActionAuthLoginFailed:     "Неудачный вход",
	apispec.AuditActionAuthLogout:          "Выход",
	apispec.AuditActionAuthPasswordChanged: "Смена пароля",
	apispec.AuditActionAuthSessionEnded:    "Завершение сеанса",
	apispec.AuditActionUserCreated:         "Создание сотрудника",
	apispec.AuditActionUserUpdated:         "Изменение сотрудника",
	apispec.AuditActionUserBlocked:         "Блокировка сотрудника",
	apispec.AuditActionUserUnblocked:       "Разблокировка сотрудника",
	apispec.AuditActionUserPasswordReset:   "Сброс пароля",
	apispec.AuditActionUserPinSet:          "PIN для кассы",
	apispec.AuditActionPosDeviceCreated:    "Регистрация кассы",
	apispec.AuditActionPosDeviceRevoked:    "Отключение кассы",
	apispec.AuditActionLocationCreated:     "Создание точки",
	apispec.AuditActionLocationUpdated:     "Изменение точки",
	apispec.AuditActionSaleRuleCreated:     "Новая редакция правила продажи",
	apispec.AuditActionSaleRuleDeleted:     "Удаление редакции правила продажи",
	apispec.AuditActionSaleBanCreated:      "Новый день запрета продажи",
	apispec.AuditActionSaleBanDeleted:      "Удаление дня запрета продажи",
	apispec.AuditActionRefCreated:          "Новая запись справочника",
	apispec.AuditActionRefUpdated:          "Изменение записи справочника",
	apispec.AuditActionProductCreated:      "Создание позиции",
	apispec.AuditActionProductUpdated:      "Изменение позиции",
	apispec.AuditActionVariantCreated:      "Новый вариант позиции",
	apispec.AuditActionVariantUpdated:      "Изменение варианта",
	apispec.AuditActionBarcodeCreated:      "Привязка штрихкода",
	apispec.AuditActionBarcodeDeleted:      "Отвязка штрихкода",
	apispec.AuditActionPriceCreated:        "Назначение цены",
	apispec.AuditActionPriceDeleted:        "Отмена назначенной цены",
	apispec.AuditActionMinPriceCreated:     "Новая редакция МРЦ",
	apispec.AuditActionMinPriceDeleted:     "Удаление редакции МРЦ",
	apispec.AuditActionAssortmentSet:       "Изменение ассортиментной матрицы",
	apispec.AuditActionAssortmentRemoved:   "Удаление из ассортиментной матрицы",
	apispec.AuditActionAssortmentCopied:    "Копирование ассортиментной матрицы",
	apispec.AuditActionDocumentCreated:     "Создание документа",
	apispec.AuditActionDocumentPosted:      "Проведение документа",
	apispec.AuditActionDocumentCancelled:   "Отмена черновика документа",
}

var errTooManyRows = echo.NewHTTPError(http.StatusUnprocessableEntity,
	"В выгрузку попадает больше 50 000 записей. Сузьте период или добавьте фильтр")

func (h *Handler) ExportAuditLog(ctx context.Context, req apispec.ExportAuditLogRequestObject) (apispec.ExportAuditLogResponseObject, error) {
	p := req.Params
	loc := time.UTC
	if p.Tz != nil {
		if l, err := time.LoadLocation(*p.Tz); err == nil {
			loc = l
		}
	}

	f := filters{From: p.From, To: p.To, UserID: p.UserID, Action: p.Action, EntityType: p.EntityType, EntityID: p.EntityID}
	rows, err := h.q.ListEntries(ctx, f.params(nil, exportLimit+1))
	if err != nil {
		return nil, fmt.Errorf("list audit log: %w", err)
	}
	if len(rows) > exportLimit {
		return nil, errTooManyRows
	}

	file, err := writeXLSX(rows, loc)
	if err != nil {
		return nil, fmt.Errorf("write audit xlsx: %w", err)
	}
	disposition := fmt.Sprintf(`attachment; filename="pikni-audit-%s.xlsx"`, time.Now().In(loc).Format("2006-01-02"))
	return apispec.ExportAuditLog200ApplicationVndOpenxmlformatsOfficedocumentSpreadsheetmlSheetResponse{
		Body:          file,
		ContentLength: int64(file.Len()),
		Headers:       apispec.ExportAuditLog200ResponseHeaders{ContentDisposition: &disposition},
	}, nil
}

func writeXLSX(rows []store.ListEntriesRow, loc *time.Location) (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	const sheet = "Журнал"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	timeFormat := "dd.mm.yyyy hh:mm:ss"
	timeStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &timeFormat})
	if err != nil {
		return nil, err
	}
	headStyle, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}

	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return nil, err
	}
	header := []string{"№", "Время", "Событие", "Сотрудник", "Логин", "IP", "Объект", "Подробности"}
	widths := []float64{8, 20, 26, 30, 16, 16, 24, 60}
	head := make([]any, len(header))
	for i, title := range header {
		if err := sw.SetColWidth(i+1, i+1, widths[i]); err != nil {
			return nil, err
		}
		head[i] = excelize.Cell{StyleID: headStyle, Value: title}
	}
	if err := sw.SetRow("A1", head); err != nil {
		return nil, err
	}

	for i, r := range rows {
		cell, err := excelize.CoordinatesToCellName(1, i+2)
		if err != nil {
			return nil, err
		}
		// В Excel у даты нет часового пояса: записываем время «как на часах» в поясе loc.
		at := r.At.In(loc)
		wall := time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), at.Minute(), at.Second(), 0, time.UTC)
		err = sw.SetRow(cell, []any{
			r.ID,
			excelize.Cell{StyleID: timeStyle, Value: wall},
			actionTitle(r.Action),
			deref(r.UserFullName),
			deref(r.UserLogin),
			ipString(r),
			object(r),
			details(r.Data),
		})
		if err != nil {
			return nil, err
		}
	}
	if err := sw.Flush(); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return &buf, nil
}

func HasTitle(action apispec.AuditAction) bool {
	_, ok := actionTitles[action]
	return ok
}

func actionTitle(action string) string {
	if t, ok := actionTitles[apispec.AuditAction(action)]; ok {
		return t
	}
	return action
}

func object(r store.ListEntriesRow) string {
	if r.EntityType == nil {
		return ""
	}
	var d struct {
		Login    string `json:"login"`
		Article  string `json:"article"`
		Name     string `json:"name"`
		Location string `json:"location"`
	}
	_ = json.Unmarshal(r.Data, &d)

	switch *r.EntityType {
	case EntityUser:
		if d.Login != "" {
			return "сотрудник " + d.Login
		}
		return "сотрудник " + deref(r.EntityID)
	case EntitySession:
		return "сеанс"
	case EntityPosDevice:
		return "касса «" + d.Name + "»"
	case EntityProduct:
		return "позиция «" + d.Name + "»"
	case EntityVariant:
		return "вариант " + d.Article + " «" + d.Name + "»"
	case EntityAssortment:
		return "матрица точки " + d.Location
	case EntityLocation:
		var l struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(r.Data, &l)
		return "точка " + l.Code
	}
	return *r.EntityType + " " + deref(r.EntityID)
}

func details(data []byte) string {
	if string(data) == "{}" {
		return ""
	}
	return string(data)
}

func ipString(r store.ListEntriesRow) string {
	if r.IP == nil {
		return ""
	}
	return r.IP.String()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
