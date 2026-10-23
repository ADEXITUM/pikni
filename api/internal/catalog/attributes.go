package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

var (
	ErrUnknownAttribute  = errors.New("attribute is not in the category schema")
	ErrArchivedAttribute = errors.New("attribute is archived")
	ErrWrongValueType    = errors.New("attribute value has a wrong type")
	ErrUnknownOption     = errors.New("value is not in the attribute options")
	ErrArchivedOption    = errors.New("attribute option is archived")
)

// AttributeError — ошибка в значении одной характеристики; Attribute — её название или код.
type AttributeError struct {
	Attribute string
	Err       error
}

func (e *AttributeError) Error() string { return fmt.Sprintf("attribute %s: %v", e.Attribute, e.Err) }
func (e *AttributeError) Unwrap() error { return e.Err }

// checkAttributes сверяет характеристики позиции со схемой категории. Пустое значение значит
// «не указано» и не сохраняется. Значение, которое у позиции уже было, принимается, даже если
// характеристику или значение списка с тех пор отправили в архив.
func checkAttributes(schema []store.CategoryAttribute, before, next map[string]any) ([]byte, error) {
	byCode := make(map[string]store.CategoryAttribute, len(schema))
	for _, a := range schema {
		byCode[a.Code] = a
	}
	clean := map[string]any{}
	for code, v := range next {
		if s, ok := v.(string); ok {
			v = strings.TrimSpace(s)
		}
		if v == nil || v == "" {
			continue
		}
		a, ok := byCode[code]
		if !ok {
			return nil, &AttributeError{Attribute: code, Err: ErrUnknownAttribute}
		}
		if err := checkValue(a, v, before[code]); err != nil {
			return nil, &AttributeError{Attribute: a.Name, Err: err}
		}
		clean[code] = v
	}
	return json.Marshal(clean)
}

func checkValue(a store.CategoryAttribute, v, old any) error {
	var typeOK bool
	switch a.Type {
	case string(apispec.AttributeTypeNumber):
		_, typeOK = v.(float64)
	case string(apispec.AttributeTypeBool):
		_, typeOK = v.(bool)
	case string(apispec.AttributeTypeText), string(apispec.AttributeTypeEnum):
		_, typeOK = v.(string)
	}
	if !typeOK {
		return ErrWrongValueType
	}
	// Тип проверен, поэтому v — число, строка или bool и сравнение не паникует.
	kept := old == v
	if !a.IsActive && !kept {
		return ErrArchivedAttribute
	}
	if a.Type != string(apispec.AttributeTypeEnum) {
		return nil
	}
	for _, o := range options(a.Options) {
		if o.Value == v {
			if o.Archived && !kept {
				return ErrArchivedOption
			}
			return nil
		}
	}
	return ErrUnknownOption
}

// describeAttributes записывает характеристики для журнала одной строкой: «Сырьё: пшеница; …».
func describeAttributes(raw []byte, names map[string]string) string {
	var attrs map[string]any
	_ = json.Unmarshal(raw, &attrs)
	parts := make([]string, 0, len(attrs))
	for code, v := range attrs {
		label := names[code]
		if label == "" {
			label = code
		}
		value := fmt.Sprint(v)
		if b, ok := v.(bool); ok {
			value = map[bool]string{true: "да", false: "нет"}[b]
		}
		parts = append(parts, label+": "+value)
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}
