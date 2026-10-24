package catalog

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog/store"
)

var (
	ErrImportTooLarge = errors.New("import file is larger than 5 MB or 5000 rows")
	ErrImportEmpty    = errors.New("import file has no data rows")
	ErrImportFormat   = errors.New("import file is neither XLSX nor CSV")
)

const (
	MaxImportBytes = 5 << 20
	maxImportRows  = 5000
)

// importIssue — ошибка в данных файла с текстом для пользователя.
type importIssue string

func (i importIssue) Error() string { return string(i) }

func issuef(format string, args ...any) importIssue { return importIssue(fmt.Sprintf(format, args...)) }

// importColumn — колонка шаблона. Заголовки ищутся без учёта регистра и порядка. product —
// поле позиции: у строк одной позиции (одинаковые наименование и производитель) оно совпадает.
type importColumn struct {
	key, title, hint  string
	required, product bool
}

var importColumns = []importColumn{
	{"name", "Наименование", "", true, true},
	{"category", "Категория", "Путь через «/», например: Вино / Тихое", true, true},
	{"kind", "Вид продукции", "Код ЕГАИС, например 200; пусто — товар без ЕГАИС", false, true},
	{"abv", "Крепость, %", "Обязательна для алкоголя", false, true},
	{"crpt", "Честный знак", "да или нет", false, true},
	{"producer", "Производитель", "Нового производителя система создаст", true, true},
	{"brand", "Бренд", "Новый бренд система создаст", false, true},
	{"line", "Линейка", "Новую линейку бренда система создаст", false, true},
	{"variety", "Разновидность", "", false, true},
	{"country", "Страна", "Название или код ОКСМ, например 643", true, true},
	{"region", "Регион происхождения", "Из справочника регионов", false, true},
	{"attributes", "Характеристики", "Сырьё: пшеница; Фильтрация: угольная", false, true},
	{"volume", "Объём, л", "Из справочника объёмов, например 0,5", false, false},
	{"weight", "Масса нетто, г", "Для сопутствующих товаров вместо объёма, например 70", false, false},
	{"container", "Тара", "стекло, ПЭТ или банка", false, false},
	{"gift", "Подарочная упаковка", "да или нет", false, false},
	{"alcocode", "Алкокод", "19 цифр", false, false},
	{"gtin", "GTIN", "", false, false},
	{"barcode", "Штрихкод", "", false, false},
	{"price", "Цена, ₽", "Не ниже МРЦ", false, false},
	{"purchase", "Закупочная цена, ₽", "", false, false},
}

// variantColumns — если в строке заполнена хоть одна из них, строка добавляет вариант.
var variantColumns = []string{"volume", "weight", "container", "gift", "alcocode", "gtin", "barcode", "price", "purchase"}

type ImportError struct {
	Row     int
	Message string
}

// ImportReport — итог импорта. Products — позиций создано, Existing — найдено в каталоге.
type ImportReport struct {
	Committed                          bool
	Rows, Products, Existing, Variants int
	NewProducers, NewBrands, NewLines  []string
	Changes                            []ImportChange
	Errors                             []ImportError
}

type importRow struct {
	num int
	v   map[string]string
}

func readImportFile(name string, data []byte) ([][]string, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xlsx":
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			return nil, ErrImportFormat
		}
		defer func() { _ = f.Close() }()
		// Сырые значения: иначе длинный штрихкод в числовой ячейке придёт как 4,00638E+12.
		rows, err := f.GetRows(f.GetSheetList()[0], excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, ErrImportFormat
		}
		return rows, nil
	case ".csv":
		text := strings.TrimPrefix(string(data), string(rune(0xFEFF)))
		r := csv.NewReader(strings.NewReader(text))
		if first, _, _ := strings.Cut(text, "\n"); strings.Count(first, ";") > strings.Count(first, ",") {
			r.Comma = ';'
		}
		r.FieldsPerRecord = -1
		rows, err := r.ReadAll()
		if err != nil {
			return nil, ErrImportFormat
		}
		return rows, nil
	}
	return nil, ErrImportFormat
}

// parseImportRows сопоставляет колонки по заголовкам и пропускает пустые строки.
func parseImportRows(rows [][]string) ([]importRow, error) {
	if len(rows) < 2 {
		return nil, ErrImportEmpty
	}
	index := map[string]int{}
	for i, h := range rows[0] {
		for _, c := range importColumns {
			if strings.EqualFold(strings.TrimSpace(h), c.title) {
				index[c.key] = i
			}
		}
	}
	var missing []string
	for _, c := range importColumns {
		if _, ok := index[c.key]; c.required && !ok {
			missing = append(missing, "«"+c.title+"»")
		}
	}
	if len(missing) > 0 {
		return nil, issuef("В файле нет обязательных колонок: %s. Возьмите шаблон", strings.Join(missing, ", "))
	}

	var out []importRow
	for n, cells := range rows[1:] {
		r := importRow{num: n + 2, v: map[string]string{}}
		empty := true
		for key, i := range index {
			if i < len(cells) {
				r.v[key] = strings.TrimSpace(cells[i])
				empty = empty && r.v[key] == ""
			}
		}
		if !empty {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, ErrImportEmpty
	}
	if len(out) > maxImportRows {
		return nil, ErrImportTooLarge
	}
	return out, nil
}

// importLookups — справочники для разбора строк: по названию или коду находится запись.
type importLookups struct {
	categories map[string]uuid.UUID
	schemas    map[uuid.UUID][]store.CategoryAttribute
	countries  map[string]string
	kinds      map[string]string
	regions    map[string]uuid.UUID
	producers  map[string]store.Producer
	brands     map[string]store.Brand
	lines      map[string]store.ProductLine
}

func key(parts ...string) string {
	for i, p := range parts {
		parts[i] = strings.ToLower(strings.TrimSpace(p))
	}
	return strings.Join(parts, "|")
}

// categoryKey приводит путь «Вино › Тихое», «вино/тихое» и «Вино / Тихое» к одному виду.
func categoryKey(path string) string {
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '›' || r == '>' })
	return key(parts...)
}

func loadImportLookups(ctx context.Context, q *store.Queries) (*importLookups, error) {
	lk := &importLookups{
		categories: map[string]uuid.UUID{}, schemas: map[uuid.UUID][]store.CategoryAttribute{},
		countries: map[string]string{}, kinds: map[string]string{}, regions: map[string]uuid.UUID{},
		producers: map[string]store.Producer{}, brands: map[string]store.Brand{}, lines: map[string]store.ProductLine{},
	}
	categories, err := q.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]store.Category{}
	names := map[string][]uuid.UUID{}
	for _, c := range categories {
		byID[c.ID] = c
		names[key(c.Name)] = append(names[key(c.Name)], c.ID)
	}
	for _, c := range categories {
		path := []string{c.Name}
		for p := c.ParentID; p != nil; p = byID[*p].ParentID {
			path = append([]string{byID[*p].Name}, path...)
		}
		lk.categories[key(path...)] = c.ID
	}
	// Короткое название подходит, только если оно в дереве одно: «Пиво» есть и в корне, и внутри.
	for name, ids := range names {
		if _, taken := lk.categories[name]; !taken && len(ids) == 1 {
			lk.categories[name] = ids[0]
		}
	}

	countries, err := q.ListCountries(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range countries {
		lk.countries[key(c.Name)], lk.countries[c.Code], lk.countries[key(c.Alpha2)] = c.Code, c.Code, c.Code
	}
	kinds, err := q.ListProductKinds(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range kinds {
		lk.kinds[k.Code], lk.kinds[key(k.Name)] = k.Code, k.Code
	}
	regions, err := q.ListOriginRegions(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range regions {
		lk.regions[key(r.CountryCode, r.Name)] = r.ID
	}
	producers, err := q.ListProducers(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range producers {
		lk.producers[key(p.Name)] = p
	}
	brands, err := q.ListBrands(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range brands {
		lk.brands[key(b.ProducerID.String(), b.Name)] = b
	}
	lines, err := q.ListProductLines(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range lines {
		lk.lines[key(l.BrandID.String(), l.Name)] = l
	}
	return lk, nil
}

func yesNo(s, column string) (bool, error) {
	switch strings.ToLower(s) {
	case "", "нет", "0", "false", "-":
		return false, nil
	case "да", "1", "true", "+":
		return true, nil
	}
	return false, issuef("«%s»: ожидается «да» или «нет», а не «%s»", column, s)
}

func decimal(s, column string) (*float64, error) {
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), ",", "."), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, issuef("«%s»: ожидается число, а не «%s»", column, s)
	}
	return &v, nil
}

func textOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

var containerCodes = map[string]string{"стекло": "glass", "пэт": "pet", "банка": "can", "glass": "glass", "pet": "pet", "can": "can"}

// rowError — ошибка с номером строки файла.
type rowError struct {
	row int
	err error
}

func (e *rowError) Error() string { return fmt.Sprintf("row %d: %v", e.row, e.err) }
func (e *rowError) Unwrap() error { return e.err }

// groupImport — импорт одной позиции. Новые производители, бренды и линейки копятся отдельно
// и попадают в общий справочник, только если позиция сохранилась целиком.
type groupImport struct {
	svc      *Service
	lk       *importLookups
	setBy    *uuid.UUID
	variants int
	existing bool

	// accept — какие расхождения взять из файла; changes — все найденные расхождения.
	accept  map[string]bool
	changes []ImportChange
	// current — варианты позиции, которая уже была в каталоге.
	current []Variant

	producers map[string]store.Producer
	brands    map[string]store.Brand
	lines     map[string]store.ProductLine
}

func (g *groupImport) producer(ctx context.Context, name, country string) (store.Producer, error) {
	k := key(name)
	if p, ok := g.lk.producers[k]; ok {
		return p, nil
	}
	if p, ok := g.producers[k]; ok {
		return p, nil
	}
	p, err := g.svc.SaveProducer(ctx, nil, apispec.ProducerFields{Name: name, CountryCode: country}, true)
	g.producers[k] = p
	return p, err
}

func (g *groupImport) brand(ctx context.Context, producer store.Producer, name string) (store.Brand, error) {
	k := key(producer.ID.String(), name)
	if b, ok := g.lk.brands[k]; ok {
		return b, nil
	}
	if b, ok := g.brands[k]; ok {
		return b, nil
	}
	b, err := g.svc.SaveBrand(ctx, nil, apispec.BrandFields{ProducerID: producer.ID, Name: name}, true)
	g.brands[k] = b
	return b, err
}

func (g *groupImport) line(ctx context.Context, brand store.Brand, name string) (store.ProductLine, error) {
	k := key(brand.ID.String(), name)
	if l, ok := g.lk.lines[k]; ok {
		return l, nil
	}
	if l, ok := g.lines[k]; ok {
		return l, nil
	}
	l, err := g.svc.SaveProductLine(ctx, nil, brand.ID, name, true)
	g.lines[k] = l
	return l, err
}

func (g *groupImport) schema(ctx context.Context, category uuid.UUID) ([]store.CategoryAttribute, error) {
	if s, ok := g.lk.schemas[category]; ok {
		return s, nil
	}
	s, err := g.svc.q.CategorySchema(ctx, category)
	g.lk.schemas[category] = s
	return s, err
}

// attributes разбирает «Сырьё: пшеница; Выдержка: 5» по схеме категории. Значения списка
// проверит сервис позиций — так же, как при ручном вводе.
func attributes(text string, schema []store.CategoryAttribute) (map[string]any, error) {
	out := map[string]any{}
	for part := range strings.SplitSeq(text, ";") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		name, value, ok := strings.Cut(part, ":")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || name == "" {
			return nil, issuef("характеристику «%s» запишите как «Название: значение»", strings.TrimSpace(part))
		}
		i := slices.IndexFunc(schema, func(a store.CategoryAttribute) bool {
			return strings.EqualFold(a.Name, name) || a.Code == name
		})
		if i < 0 {
			return nil, issuef("у категории нет характеристики «%s»", name)
		}
		a := schema[i]
		switch a.Type {
		case string(apispec.AttributeTypeNumber):
			n, err := decimal(value, a.Name)
			if err != nil {
				return nil, err
			}
			if n != nil {
				out[a.Code] = *n
			}
		case string(apispec.AttributeTypeBool):
			b, err := yesNo(value, a.Name)
			if err != nil {
				return nil, err
			}
			out[a.Code] = b
		default:
			out[a.Code] = value
		}
	}
	return out, nil
}

func (g *groupImport) productFields(ctx context.Context, r importRow) (apispec.ProductFields, error) {
	v := r.v
	var f apispec.ProductFields
	category, ok := g.lk.categories[categoryKey(v["category"])]
	if !ok {
		return f, issuef("нет категории «%s»; укажите путь через «/», например «Вино / Тихое»", v["category"])
	}
	country, ok := g.lk.countries[key(v["country"])]
	if !ok {
		return f, issuef("нет страны «%s»", v["country"])
	}
	var kind *string
	if v["kind"] != "" {
		code, ok := g.lk.kinds[key(v["kind"])]
		if !ok {
			return f, issuef("нет вида продукции «%s»", v["kind"])
		}
		kind = &code
	}
	abv, err := decimal(v["abv"], "Крепость")
	if err != nil {
		return f, err
	}
	crpt, err := yesNo(v["crpt"], "Честный знак")
	if err != nil {
		return f, err
	}
	var region *uuid.UUID
	if v["region"] != "" {
		id, ok := g.lk.regions[key(country, v["region"])]
		if !ok {
			return f, issuef("в справочнике нет региона «%s» для этой страны", v["region"])
		}
		region = &id
	}
	schema, err := g.schema(ctx, category)
	if err != nil {
		return f, err
	}
	attrs, err := attributes(v["attributes"], schema)
	if err != nil {
		return f, err
	}

	producer, err := g.producer(ctx, v["producer"], country)
	if err != nil {
		return f, err
	}
	f = apispec.ProductFields{
		Name: v["name"], CategoryID: category, KindCode: kind, Abv: abv, CrptMarked: crpt, ProducerID: producer.ID,
		Variety: textOrNil(v["variety"]), CountryCode: country, OriginRegionID: region, Attributes: attrs,
	}
	if v["brand"] != "" {
		b, err := g.brand(ctx, producer, v["brand"])
		if err != nil {
			return f, err
		}
		f.BrandID = &b.ID
		if v["line"] != "" {
			l, err := g.line(ctx, b, v["line"])
			if err != nil {
				return f, err
			}
			f.ProductLineID = &l.ID
		}
	} else if v["line"] != "" {
		return f, ErrLineNeedsBrand
	}
	return f, nil
}

func (g *groupImport) variant(ctx context.Context, product store.ProductCard, r importRow) error {
	v := r.v
	if !slices.ContainsFunc(variantColumns, func(c string) bool { return v[c] != "" }) {
		return nil
	}
	var spec VariantSpec
	litres, err := decimal(v["volume"], "Объём")
	if err != nil {
		return err
	}
	if litres != nil {
		ml := int32(math.Round(*litres * 1000))
		spec.VolumeMl = &ml
	}
	grams, err := decimal(v["weight"], "Масса нетто")
	if err != nil {
		return err
	}
	if grams != nil {
		g := int32(math.Round(*grams))
		spec.NetWeightG = &g
	}
	if v["container"] != "" {
		c, ok := containerCodes[strings.ToLower(v["container"])]
		if !ok {
			return issuef("«Тара»: стекло, ПЭТ или банка, а не «%s»", v["container"])
		}
		spec.Container = &c
	}
	if spec.GiftPack, err = yesNo(v["gift"], "Подарочная упаковка"); err != nil {
		return err
	}
	spec.Alcocode, spec.GTIN = textOrNil(v["alcocode"]), textOrNil(v["gtin"])
	var prices [2]*int64
	for i, column := range []string{"price", "purchase"} {
		price, err := decimal(v[column], "Цена")
		if err != nil {
			return err
		}
		if price != nil {
			k := toKopecks(*price)
			prices[i] = &k
		}
	}

	// Вариант с той же фасовкой уже есть — сверяем его, а не создаём второй.
	for _, existing := range g.current {
		if hasVariant([]store.Variant{existing.Variant}, spec) {
			if code := v["barcode"]; code != "" && !hasBarcode(existing, code) {
				if _, err := g.svc.AddBarcode(ctx, existing.ID, &code); err != nil {
					return err
				}
			}
			return g.mergeVariant(ctx, product, existing, spec, r, prices)
		}
	}

	created, err := g.svc.CreateVariant(ctx, product.ID, spec)
	if err != nil {
		return err
	}
	g.variants++
	if code := v["barcode"]; code != "" {
		if _, err := g.svc.AddBarcode(ctx, created.ID, &code); err != nil {
			return err
		}
	}
	for i, kind := range []apispec.PriceKind{apispec.PriceKindRetail, apispec.PriceKindPurchase} {
		if prices[i] != nil {
			if _, err := g.svc.SetPrice(ctx, created.ID, kind, *prices[i], nil, g.setBy); err != nil {
				return err
			}
		}
	}
	return nil
}

func hasBarcode(v Variant, code string) bool {
	return slices.ContainsFunc(v.Barcodes, func(b store.Barcode) bool { return b.Code == code })
}

// run создаёт позицию по первой строке группы или находит её в каталоге (производитель и
// наименование) и сверяет; затем добавляет или сверяет варианты по всем строкам.
func (g *groupImport) run(ctx context.Context, rows []importRow) error {
	first := rows[0]
	for _, r := range rows {
		for _, c := range importColumns {
			if c.product && !strings.EqualFold(r.v[c.key], first.v[c.key]) {
				return &rowError{r.num, issuef("«%s» отличается от строки %d, хотя позиция та же", c.title, first.num)}
			}
		}
	}
	f, err := g.productFields(ctx, first)
	if err != nil {
		return &rowError{first.num, err}
	}
	var p store.ProductCard
	id, err := g.svc.q.FindProductByProducerName(ctx, store.FindProductByProducerNameParams{
		ProducerID: f.ProducerID, Name: strings.TrimSpace(f.Name),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		p, err = g.svc.CreateProduct(ctx, f)
	case err == nil:
		g.existing = true
		if p, err = g.mergeProduct(ctx, id, f, first); err == nil {
			g.current, err = variantsOf(ctx, g.svc.q, id)
		}
	}
	if err != nil {
		return &rowError{first.num, err}
	}
	for _, r := range rows {
		if err := g.variant(ctx, p, r); err != nil {
			return &rowError{r.num, err}
		}
	}
	return nil
}

// Import загружает номенклатуру по принципу «всё или ничего» (НСИ-7). Каждая позиция
// сохраняется в своей точке сохранения: ошибка откатывает только её, и проверка идёт дальше,
// чтобы показать ошибки всех строк сразу. В конце транзакция фиксируется, только если ошибок
// нет и это не предпросмотр. Номера артикулов из последовательности при откате не возвращаются.
//
// Позиции, которые уже есть в каталоге, не дублируются: расхождения с файлом попадают в
// Changes, а применяются только те, ключи которых переданы в accept.
func (s *Service) Import(ctx context.Context, fileName string, data []byte, commit bool, setBy *uuid.UUID, accept map[string]bool) (ImportReport, error) {
	report := ImportReport{
		NewProducers: []string{}, NewBrands: []string{}, NewLines: []string{}, Changes: []ImportChange{}, Errors: []ImportError{},
	}
	rows, err := readImportFile(fileName, data)
	if err != nil {
		return report, err
	}
	records, err := parseImportRows(rows)
	if err != nil {
		return report, err
	}
	report.Rows = len(records)

	// Строки с одинаковыми наименованием и производителем — одна позиция с несколькими вариантами.
	var order []string
	groups := map[string][]importRow{}
	for _, r := range records {
		k := key(r.v["name"], r.v["producer"])
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return report, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	lk, err := loadImportLookups(ctx, store.New(tx))
	if err != nil {
		return report, err
	}

	for _, k := range order {
		g := &groupImport{
			lk: lk, setBy: setBy, accept: accept,
			producers: map[string]store.Producer{}, brands: map[string]store.Brand{}, lines: map[string]store.ProductLine{},
		}
		err := pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
			g.svc = s.withTx(sp)
			return g.run(ctx, groups[k])
		})
		if re, ok := errors.AsType[*rowError](err); ok {
			msg, known := userMessage(re.err)
			if !known {
				return report, re.err
			}
			report.Errors = append(report.Errors, ImportError{Row: re.row, Message: msg})
			continue
		}
		if err != nil {
			return report, err
		}
		if g.existing {
			report.Existing++
		} else {
			report.Products++
		}
		report.Variants += g.variants
		report.Changes = append(report.Changes, g.changes...)
		for k, p := range g.producers {
			lk.producers[k] = p
			report.NewProducers = append(report.NewProducers, p.Name)
		}
		for k, b := range g.brands {
			lk.brands[k] = b
			report.NewBrands = append(report.NewBrands, b.Name)
		}
		for k, l := range g.lines {
			lk.lines[k] = l
			report.NewLines = append(report.NewLines, l.Name)
		}
	}

	if commit && len(report.Errors) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return report, err
		}
		report.Committed = true
	}
	return report, nil
}

// ImportTemplate — файл-шаблон: колонки с примером и лист со справкой по заполнению.
func ImportTemplate() (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	const sheet, help = "Позиции", "Справка"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}
	// Текстовый формат ячеек: иначе Excel превратит штрихкод в 4,00638E+12, а алкокод обрежет.
	text, err := f.NewStyle(&excelize.Style{NumFmt: 49})
	if err != nil {
		return nil, err
	}
	example := [][]string{
		{"Водка «Тайга» классическая", "Крепкий алкоголь / Водка", "200", "40", "нет", "ООО «Тайга»", "Тайга", "Тайга", "классическая", "643", "",
			"Сырьё: смесь зерновых; Фильтрация: угольная", "0,5", "", "стекло", "нет", "", "", "", "449,90", "310"},
		{"Водка «Тайга» классическая", "Крепкий алкоголь / Водка", "200", "40", "нет", "ООО «Тайга»", "Тайга", "Тайга", "классическая", "643", "",
			"Сырьё: смесь зерновых; Фильтрация: угольная", "0,7", "", "стекло", "нет", "", "", "", "629", "430"},
	}
	last, _ := excelize.ColumnNumberToName(len(importColumns))
	if err := f.SetColStyle(sheet, "A:"+last, text); err != nil {
		return nil, err
	}
	for i, c := range importColumns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellStr(sheet, cell, c.title); err != nil {
			return nil, err
		}
		col, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(sheet, col, col, 18); err != nil {
			return nil, err
		}
		for r, row := range example {
			cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
			if err := f.SetCellStr(sheet, cell, row[i]); err != nil {
				return nil, err
			}
		}
	}
	if err := f.SetCellStyle(sheet, "A1", last+"1", bold); err != nil {
		return nil, err
	}

	if _, err := f.NewSheet(help); err != nil {
		return nil, err
	}
	lines := [][]string{
		{"Колонка", "Обязательная", "Как заполнять"},
		{"", "", "Одна строка — позиция с одним вариантом. Строки с одинаковыми наименованием и производителем — одна позиция с несколькими вариантами."},
		{"", "", "Если хоть в одной строке ошибка, ничего не загрузится: сначала посмотрите предпросмотр."},
	}
	for _, c := range importColumns {
		req := ""
		if c.required {
			req = "да"
		}
		lines = append(lines, []string{c.title, req, c.hint})
	}
	for r, row := range lines {
		for i, v := range row {
			cell, _ := excelize.CoordinatesToCellName(i+1, r+1)
			if err := f.SetCellStr(help, cell, v); err != nil {
				return nil, err
			}
		}
	}
	if err := f.SetColWidth(help, "A", "A", 24); err != nil {
		return nil, err
	}
	if err := f.SetColWidth(help, "C", "C", 90); err != nil {
		return nil, err
	}
	if err := f.SetCellStyle(help, "A1", "C1", bold); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return &buf, nil
}
