package catalog_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func count(t *testing.T, db *dbtest.DB, table string) int {
	t.Helper()
	var n int
	if err := db.App.QueryRow(t.Context(), "select count(*) from "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestImportTemplate(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := catalog.NewService(db.App)
	ctx := t.Context()

	file, err := catalog.ImportTemplate()
	if err != nil {
		t.Fatal(err)
	}
	data := file.Bytes()

	// Пример из шаблона загружается как есть: две строки — одна позиция с двумя вариантами.
	preview, err := svc.Import(ctx, "шаблон.xlsx", data, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Committed || preview.Rows != 2 || preview.Products != 1 || preview.Variants != 2 || len(preview.Errors) != 0 ||
		strings.Join(preview.NewProducers, ",") != "ООО «Тайга»" || strings.Join(preview.NewLines, ",") != "Тайга" {
		t.Fatalf("preview = %+v", preview)
	}
	if n := count(t, db, "products"); n != 0 {
		t.Fatalf("preview saved %d products", n)
	}

	report, err := svc.Import(ctx, "шаблон.xlsx", data, true, nil)
	if err != nil || !report.Committed {
		t.Fatalf("commit = %+v, %v", report, err)
	}
	if count(t, db, "products") != 1 || count(t, db, "variants") != 2 || count(t, db, "prices") != 4 {
		t.Errorf("after import: %d products, %d variants, %d prices",
			count(t, db, "products"), count(t, db, "variants"), count(t, db, "prices"))
	}
}

const header = "Наименование;Категория;Вид продукции;Крепость, %;Производитель;Бренд;Страна;Характеристики;Объём, л;Тара;Штрихкод;Цена, ₽\n"

func TestImportErrors(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := catalog.NewService(db.App)
	ctx := t.Context()

	csv := header +
		"Водка «Север»;Крепкий алкоголь / Водка;200;40;ООО «Север»;Север;643;Сырьё: рожь;0,5;стекло;4006381333931;449\n" +
		"Водка «Север»;Крепкий алкоголь / Водка;200;40;ООО «Север»;Север;643;Сырьё: рожь;0,7;стекло;;500\n" +
		"Ром «Остров»;Крепкий алкоголь / Ром;236;40;ООО «Остров»;;Нарния;;0,7;стекло;;900\n" +
		"Водка «Кукуруза»;Водка;200;40;ООО «Север»;;643;Сырьё: кукуруза;0,5;стекло;;450\n" +
		"Вино «Тихое»;Вино / Тихое;401;12;ООО «Лоза»;;643;;0,75;банка;;300\n" +
		"Вино «Тихое»;Вино / Тихое;401;13;ООО «Лоза»;;643;;0,375;стекло;;200\n" +
		"Пиво;Пивные напитки;500;4,5;ООО «Пиво»;;643;;0,5;банка;;80\n"

	report, err := svc.Import(ctx, "товары.csv", []byte(csv), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Committed {
		t.Fatal("import with errors was committed")
	}
	want := map[int]string{
		3: "ниже минимальной розничной",           // 0,7 л водки дешевле МРЦ 572,60 ₽
		4: "нет страны «Нарния»",                  // такой страны нет в ОКСМ
		5: "Характеристика «Сырьё»",               // значения нет в списке
		7: "«Крепость, %» отличается от строки 6", // одна позиция, разная крепость
		8: "нет категории «Пивные напитки»",
	}
	got := map[int]string{}
	for _, e := range report.Errors {
		got[e.Row] = e.Message
	}
	for row, part := range want {
		if !strings.Contains(got[row], part) {
			t.Errorf("row %d: %q, want %q", row, got[row], part)
		}
	}
	if len(report.Errors) != len(want) {
		t.Errorf("errors = %+v", report.Errors)
	}
	// Всё или ничего: ни позиций, ни новых производителей.
	if count(t, db, "products") != 0 || count(t, db, "producers") != 0 {
		t.Errorf("failed import left %d products and %d producers", count(t, db, "products"), count(t, db, "producers"))
	}

	if _, err := svc.Import(ctx, "товары.csv", []byte("Наименование;Цена\nВодка;100\n"), false, nil); err == nil ||
		!strings.Contains(err.Error(), "«Категория»") {
		t.Errorf("missing columns: error = %v", err)
	}
	if _, err := svc.Import(ctx, "товары.pdf", []byte("x"), false, nil); !errors.Is(err, catalog.ErrImportFormat) {
		t.Errorf("pdf: error = %v", err)
	}
	if _, err := svc.Import(ctx, "товары.csv", []byte(header), false, nil); !errors.Is(err, catalog.ErrImportEmpty) {
		t.Errorf("header only: error = %v", err)
	}
}
