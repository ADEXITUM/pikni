package forms_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/ADEXITUM/pikni/api/internal/forms"
)

func TestForms(t *testing.T) {
	d := forms.Doc{Organization: "ООО «Чекушка»", Location: "МГ-01 · Магазин на ул. Ленина", Number: "ИН-2026-000001",
		Date: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), Started: time.Now(), Zones: "склад магазина, торговый зал"}
	lines := []forms.Line{
		{Name: "Водка «Тайга» классическая, 0,5 л", Code: "ЧК-000002", Book: 6, Fact: 5, PriceKopeck: 29000},
		{Name: "Пиво «Амурское», 0,5 л", Code: "ЧК-000010", Book: 12, Fact: 13, PriceKopeck: 6000},
		{Name: "Сухарики", Code: "ЧК-000020", Book: 3, Fact: 3, PriceKopeck: 3000},
	}
	for name, make := range map[string]func(forms.Doc, []forms.Line) ([]byte, error){"inv3": forms.Inv3PDF, "inv19": forms.Inv19PDF} {
		pdf, err := make(d, lines)
		if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) {
			t.Fatalf("%s pdf: %v", name, err)
		}
	}
	raw, err := forms.Inv19XLSX(d, lines)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := f.GetRows("ИНВ-19")
	// Шапка, заголовок таблицы, две строки с расхождениями и итог: строка без расхождения не попадает.
	if len(rows) != 9 || rows[6][1] != lines[0].Name || rows[7][6] != "1" {
		t.Fatalf("inv19 rows: %q", rows)
	}
	if _, err := forms.Inv3XLSX(d, lines); err != nil {
		t.Fatal(err)
	}
}
