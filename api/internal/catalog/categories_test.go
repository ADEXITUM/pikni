package catalog_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func categoriesByName(t *testing.T, svc *catalog.Service) map[string]store.Category {
	t.Helper()
	tree, err := svc.Categories(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]store.Category{}
	for _, c := range tree.Categories {
		byName[c.Name] = c
	}
	return byName
}

func TestSeededCategoryTree(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := catalog.NewService(db.App)

	tree, err := svc.Categories(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byName := categoriesByName(t, svc)
	roots := 0
	for _, c := range tree.Categories {
		if c.ParentID == nil {
			roots++
		}
	}
	whisky, strong := byName["Виски"], byName["Крепкий алкоголь"]
	// Признак алкоголя наследуется от категории верхнего уровня; у табака и снеков его нет.
	if roots != 6 || whisky.Depth != 2 || whisky.ParentID == nil || *whisky.ParentID != strong.ID ||
		!whisky.Alcohol || byName["Снеки"].Alcohol || byName["Сигареты"].Alcohol {
		t.Errorf("roots = %d, whisky = %+v", roots, whisky)
	}

	codes := map[string][]string{}
	for _, a := range tree.Attributes {
		codes[a.CategoryID.String()] = append(codes[a.CategoryID.String()], a.Code)
		if a.Type == "enum" && !strings.Contains(string(a.Options), `"archived": false`) {
			t.Errorf("%s options = %s", a.Code, a.Options)
		}
	}
	if got := strings.Join(codes[whisky.ID.String()], ","); got != "aging,whisky_type" {
		t.Errorf("whisky attributes = %s", got)
	}
	if got := strings.Join(codes[byName["Вино"].ID.String()], ","); got != "color,sugar,grape,vintage" {
		t.Errorf("wine attributes = %s", got)
	}
}

func TestCategoryRules(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := catalog.NewService(db.App)
	ctx := t.Context()
	vodka := categoriesByName(t, svc)["Водка"]

	premium, err := svc.CreateCategory(ctx, apispec.NewCategory{ParentID: &vodka.ID, Name: " Премиальная ", Sort: 10})
	if err != nil {
		t.Fatal(err)
	}
	if premium.Name != "Премиальная" || premium.Depth != 3 || !premium.IsActive {
		t.Errorf("premium = %+v", premium)
	}

	invalid := []struct {
		name string
		f    apispec.NewCategory
		want error
	}{
		{"четвёртый уровень", apispec.NewCategory{ParentID: &premium.ID, Name: "Ещё ниже"}, catalog.ErrCategoryTooDeep},
		{"то же имя у родителя", apispec.NewCategory{ParentID: &vodka.ID, Name: "Премиальная"}, catalog.ErrDuplicate},
		{"то же имя в корне", apispec.NewCategory{Name: "Вино"}, catalog.ErrDuplicate},
		{"нет родителя", apispec.NewCategory{ParentID: ptr(uuid.New()), Name: "Сироты"}, catalog.ErrUnknownReference},
		{"без названия", apispec.NewCategory{Name: " "}, catalog.ErrEmptyName},
	}
	for _, tt := range invalid {
		if _, err := svc.CreateCategory(ctx, tt.f); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}

	archive := func(c store.Category, active bool) error {
		_, err := svc.UpdateCategory(ctx, c.ID, apispec.CategoryUpdate{Name: c.Name, Sort: c.Sort, IsActive: active})
		return err
	}
	if err := archive(vodka, false); !errors.Is(err, catalog.ErrActiveSubcategories) {
		t.Errorf("archive vodka with active subcategory: error = %v", err)
	}
	if err := archive(premium, false); err != nil {
		t.Fatal(err)
	}
	if err := archive(vodka, false); err != nil {
		t.Fatal(err)
	}
	if err := archive(premium, true); !errors.Is(err, catalog.ErrInactiveParent) {
		t.Errorf("restore under archived parent: error = %v", err)
	}
	if _, err := svc.CreateCategory(ctx, apispec.NewCategory{ParentID: &vodka.ID, Name: "Новая"}); !errors.Is(err, catalog.ErrInactiveParent) {
		t.Errorf("create under archived parent: error = %v", err)
	}

	if _, err := db.App.Exec(ctx, `insert into categories (parent_id, name) values ($1, 'В обход сервиса')`, premium.ID); err == nil {
		t.Error("database accepted a fourth level")
	}
}

func TestAttributeRules(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := catalog.NewService(db.App)
	ctx := t.Context()
	byName := categoriesByName(t, svc)
	vodka := byName["Водка"]

	newAttr := func(code string, typ apispec.AttributeType, opts ...string) apispec.NewCategoryAttribute {
		return apispec.NewCategoryAttribute{Code: code, Name: "Характеристика", Type: typ, Options: &opts}
	}
	invalid := []struct {
		name     string
		category store.Category
		f        apispec.NewCategoryAttribute
		want     error
	}{
		{"код есть у предка", byName["Тихое"], newAttr("color", "text"), catalog.ErrCodeTaken},
		{"код есть у потомка", byName["Крепкий алкоголь"], newAttr("aging", "number"), catalog.ErrCodeTaken},
		{"кириллица в коде", vodka, newAttr("крепость", "number"), catalog.ErrInvalidCode},
		{"неизвестный тип", vodka, newAttr("strength", "float"), catalog.ErrInvalidAttributeType},
		{"список без значений", vodka, newAttr("region", "enum"), catalog.ErrNoActiveOptions},
		{"повтор без учёта регистра", vodka, newAttr("region", "enum", "Сибирь", "сибирь "), catalog.ErrInvalidOptions},
	}
	for _, tt := range invalid {
		if _, err := svc.CreateAttribute(ctx, tt.category.ID, tt.f); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}
	if _, err := svc.CreateAttribute(ctx, uuid.New(), newAttr("region", "text")); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("unknown category: error = %v", err)
	}

	note, err := svc.CreateAttribute(ctx, vodka.ID, apispec.NewCategoryAttribute{Code: "note", Name: "Заметка", Type: "text", Unit: ptr("лет")})
	if err != nil {
		t.Fatal(err)
	}
	if note.Unit != nil || string(note.Options) != "[]" {
		t.Errorf("text attribute keeps unit %v and options %s", note.Unit, note.Options)
	}

	region, err := svc.CreateAttribute(ctx, vodka.ID, newAttr("region", "enum", "Сибирь", "Алтай"))
	if err != nil {
		t.Fatal(err)
	}
	update := func(opts ...apispec.AttributeOption) error {
		_, err := svc.UpdateAttribute(ctx, region.ID, apispec.CategoryAttributeUpdate{Name: "Регион", Options: opts, IsActive: true})
		return err
	}
	opt := func(v string, archived bool) apispec.AttributeOption {
		return apispec.AttributeOption{Value: v, Archived: archived}
	}
	if err := update(opt("Сибирь", false)); !errors.Is(err, catalog.ErrOptionRemoved) {
		t.Errorf("remove option: error = %v", err)
	}
	if err := update(opt("Сибирь", true), opt("Алтай", true)); !errors.Is(err, catalog.ErrNoActiveOptions) {
		t.Errorf("archive every option: error = %v", err)
	}
	if err := update(opt("Сибирь", false), opt("Алтай", true), opt("Урал", false)); err != nil {
		t.Fatal(err)
	}

	var changes string
	err = db.App.QueryRow(ctx, `select data->'changes' from audit_log where action = 'ref.updated' and entity_id = $1`, region.ID.String()).Scan(&changes)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"archivedOptions": [[], ["Алтай"]]`, `"Урал"`, `"name": ["Характеристика", "Регион"]`} {
		if !strings.Contains(changes, want) {
			t.Errorf("changes = %s, want %s", changes, want)
		}
	}
}
