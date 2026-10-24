package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

var (
	ErrCategoryTooDeep      = errors.New("categories are limited to three levels")
	ErrInactiveParent       = errors.New("parent category is archived")
	ErrActiveSubcategories  = errors.New("category has active subcategories")
	ErrInvalidCode          = errors.New("invalid attribute code")
	ErrInvalidAttributeType = errors.New("unknown attribute type")
	ErrCodeTaken            = errors.New("attribute code is already used in this branch")
	ErrInvalidOptions       = errors.New("options must be non-empty and unique")
	ErrOptionRemoved        = errors.New("options can be archived but not removed")
	ErrNoActiveOptions      = errors.New("enum attribute needs an active option")
	ErrAlcoholInherited     = errors.New("alcohol flag is set on the top-level category")
	ErrAlcoholProducts      = errors.New("category tree has alcohol products")
)

const maxCategoryDepth = 3

var attributeCode = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

// CategoryTree — категории и их собственные характеристики.
type CategoryTree struct {
	Categories []store.Category
	Attributes []store.CategoryAttribute
}

func (s *Service) Categories(ctx context.Context) (CategoryTree, error) {
	categories, err := s.q.ListCategories(ctx)
	if err != nil {
		return CategoryTree{}, err
	}
	attributes, err := s.q.ListCategoryAttributes(ctx)
	return CategoryTree{Categories: categories, Attributes: attributes}, err
}

func categoryFields(c store.Category) map[string]any {
	return map[string]any{"name": c.Name, "parentId": c.ParentID, "sort": c.Sort, "isActive": c.IsActive, "alcohol": c.Alcohol}
}

// CreateCategory блокирует родителя (GetCategory читает for update), чтобы его не отправили
// в архив, пока создаётся подкатегория.
func (s *Service) CreateCategory(ctx context.Context, f apispec.NewCategory) (store.Category, error) {
	n, err := name(f.Name)
	if err != nil {
		return store.Category{}, err
	}
	var saved store.Category
	err = s.write(ctx, "category", func(q *store.Queries) (change, error) {
		if f.ParentID != nil {
			parent, err := q.GetCategory(ctx, *f.ParentID)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return change{}, ErrUnknownReference
			case err != nil:
				return change{}, err
			case parent.Depth >= maxCategoryDepth:
				return change{}, ErrCategoryTooDeep
			case !parent.IsActive:
				return change{}, ErrInactiveParent
			}
		}
		alcohol := f.Alcohol != nil && *f.Alcohol
		c, err := q.CreateCategory(ctx, store.CreateCategoryParams{ParentID: f.ParentID, Name: n, Sort: f.Sort, Alcohol: alcohol})
		saved = c
		return change{id: c.ID.String(), after: categoryFields(c)}, err
	})
	return saved, err
}

// UpdateCategory меняет название, порядок и архив. Архив идёт сверху вниз: категорию нельзя
// убрать в архив, пока в ней есть действующие подкатегории, и нельзя вернуть, пока в архиве родитель.
func (s *Service) UpdateCategory(ctx context.Context, id uuid.UUID, f apispec.CategoryUpdate) (store.Category, error) {
	n, err := name(f.Name)
	if err != nil {
		return store.Category{}, err
	}
	var saved store.Category
	err = s.write(ctx, "category", func(q *store.Queries) (change, error) {
		old, err := q.GetCategory(ctx, id)
		if err != nil {
			return change{}, err
		}
		switch {
		case old.IsActive && !f.IsActive:
			busy, err := q.HasActiveSubcategories(ctx, id)
			if err != nil {
				return change{}, err
			}
			if busy {
				return change{}, ErrActiveSubcategories
			}
		case !old.IsActive && f.IsActive && old.ParentID != nil:
			parent, err := q.GetCategory(ctx, *old.ParentID)
			if err != nil {
				return change{}, err
			}
			if !parent.IsActive {
				return change{}, ErrInactiveParent
			}
		}
		// Признак алкоголя — у категории верхнего уровня; снять его нельзя, пока в ветке есть
		// позиции с видом продукции или крепостью.
		if f.Alcohol != nil && *f.Alcohol != old.Alcohol {
			if old.ParentID != nil {
				return change{}, ErrAlcoholInherited
			}
			if !*f.Alcohol {
				busy, err := q.CategoryTreeHasAlcoholProducts(ctx, id)
				if err != nil {
					return change{}, err
				}
				if busy {
					return change{}, ErrAlcoholProducts
				}
			}
			if err := q.SetCategoryTreeAlcohol(ctx, store.SetCategoryTreeAlcoholParams{ID: id, Alcohol: *f.Alcohol}); err != nil {
				return change{}, err
			}
		}
		saved, err = q.UpdateCategory(ctx, store.UpdateCategoryParams{ID: id, Name: n, Sort: f.Sort, IsActive: f.IsActive})
		return change{id: id.String(), before: categoryFields(old), after: categoryFields(saved)}, err
	})
	return saved, err
}

func options(raw []byte) []apispec.AttributeOption {
	opts := []apispec.AttributeOption{}
	_ = json.Unmarshal(raw, &opts)
	return opts
}

func attributeFields(a store.CategoryAttribute) map[string]any {
	fields := map[string]any{
		"name": a.Name, "code": a.Code, "type": a.Type, "unit": a.Unit, "filterable": a.Filterable,
		"usedForAnalogs": a.UsedForAnalogs, "sort": a.Sort, "isActive": a.IsActive,
	}
	if a.Type == string(apispec.AttributeTypeEnum) {
		active, archived := []string{}, []string{}
		for _, o := range options(a.Options) {
			if o.Archived {
				archived = append(archived, o.Value)
			} else {
				active = append(active, o.Value)
			}
		}
		fields["options"], fields["archivedOptions"] = active, archived
	}
	return fields
}

// cleanOptions проверяет значения списка: не пустые и не повторяются без учёта регистра,
// прежние на месте (их можно только архивировать), хотя бы одно действует.
func cleanOptions(old, next []apispec.AttributeOption) ([]byte, error) {
	clean := make([]apispec.AttributeOption, len(next))
	seen, kept := map[string]bool{}, map[string]bool{}
	active := false
	for i, o := range next {
		v := strings.TrimSpace(o.Value)
		if v == "" || seen[strings.ToLower(v)] {
			return nil, ErrInvalidOptions
		}
		seen[strings.ToLower(v)], kept[v] = true, true
		active = active || !o.Archived
		clean[i] = apispec.AttributeOption{Value: v, Archived: o.Archived}
	}
	for _, o := range old {
		if !kept[o.Value] {
			return nil, ErrOptionRemoved
		}
	}
	if !active {
		return nil, ErrNoActiveOptions
	}
	return json.Marshal(clean)
}

// Единица нужна только числу, у остальных типов не сохраняется.
func unit(t apispec.AttributeType, u *string) *string {
	if t != apispec.AttributeTypeNumber || u == nil || strings.TrimSpace(*u) == "" {
		return nil
	}
	v := strings.TrimSpace(*u)
	return &v
}

func (s *Service) CreateAttribute(ctx context.Context, categoryID uuid.UUID, f apispec.NewCategoryAttribute) (store.CategoryAttribute, error) {
	n, err := name(f.Name)
	if err != nil {
		return store.CategoryAttribute{}, err
	}
	code := strings.TrimSpace(f.Code)
	if !attributeCode.MatchString(code) {
		return store.CategoryAttribute{}, ErrInvalidCode
	}
	if !f.Type.Valid() {
		return store.CategoryAttribute{}, ErrInvalidAttributeType
	}
	opts := []byte("[]")
	if f.Type == apispec.AttributeTypeEnum {
		var next []apispec.AttributeOption
		if f.Options != nil {
			for _, v := range *f.Options {
				next = append(next, apispec.AttributeOption{Value: v})
			}
		}
		if opts, err = cleanOptions(nil, next); err != nil {
			return store.CategoryAttribute{}, err
		}
	}

	var saved store.CategoryAttribute
	err = s.write(ctx, "category_attribute", func(q *store.Queries) (change, error) {
		if _, err := q.GetCategory(ctx, categoryID); err != nil {
			return change{}, err
		}
		taken, err := q.AttributeCodeTaken(ctx, store.AttributeCodeTakenParams{Code: code, CategoryID: categoryID})
		if err != nil {
			return change{}, err
		}
		if taken {
			return change{}, ErrCodeTaken
		}
		a, err := q.CreateCategoryAttribute(ctx, store.CreateCategoryAttributeParams{
			CategoryID: categoryID, Code: code, Name: n, Type: string(f.Type), Unit: unit(f.Type, f.Unit),
			Options: opts, Filterable: f.Filterable, UsedForAnalogs: f.UsedForAnalogs, Sort: f.Sort,
		})
		saved = a
		return change{id: a.ID.String(), after: attributeFields(a)}, err
	})
	return saved, err
}

// UpdateAttribute не трогает код и тип: от них зависят значения, уже заведённые у позиций.
func (s *Service) UpdateAttribute(ctx context.Context, id uuid.UUID, f apispec.CategoryAttributeUpdate) (store.CategoryAttribute, error) {
	n, err := name(f.Name)
	if err != nil {
		return store.CategoryAttribute{}, err
	}
	var saved store.CategoryAttribute
	err = s.write(ctx, "category_attribute", func(q *store.Queries) (change, error) {
		old, err := q.GetCategoryAttribute(ctx, id)
		if err != nil {
			return change{}, err
		}
		t := apispec.AttributeType(old.Type)
		opts := old.Options
		if t == apispec.AttributeTypeEnum {
			if opts, err = cleanOptions(options(old.Options), f.Options); err != nil {
				return change{}, err
			}
		}
		saved, err = q.UpdateCategoryAttribute(ctx, store.UpdateCategoryAttributeParams{
			ID: id, Name: n, Unit: unit(t, f.Unit), Options: opts, Filterable: f.Filterable,
			UsedForAnalogs: f.UsedForAnalogs, Sort: f.Sort, IsActive: f.IsActive,
		})
		return change{id: id.String(), before: attributeFields(old), after: attributeFields(saved)}, err
	})
	return saved, err
}
