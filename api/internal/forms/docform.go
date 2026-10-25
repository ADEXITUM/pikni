package forms

import (
	"bytes"
	"fmt"
	"time"
)

// DocForm — печатная форма документа по унифицированной форме (ОТ-5): реквизиты организации,
// номер и дата, стороны, таблица, итоги и места для подписей. ТОРГ-2, ТОРГ-13 и ТОРГ-16 утверждены
// постановлением Госкомстата России от 25.12.1998 № 132; с 2013 года формы рекомендованные.
type DocForm struct {
	Code, Title            string
	Organization, Location string
	Number                 string
	Date                   time.Time
	Parties                [][2]string
	Columns                []TableColumn
	Widths                 []float64
	Rows                   [][]string
	Totals                 []string
	Note                   string
	Signatures             []string
}

func DocFormPDF(f DocForm) ([]byte, error) {
	p := newPDF(true)
	p.SetFont("dejavu", "", 7.5)
	p.CellFormat(0, 4, "Унифицированная форма № "+f.Code+" · утверждена постановлением Госкомстата России от 25.12.1998 № 132",
		"", 1, "R", false, 0, "")
	p.SetFont("dejavu", "", 10)
	p.CellFormat(0, 6, f.Organization, "B", 1, "L", false, 0, "")
	p.SetFont("dejavu", "", 7)
	p.CellFormat(0, 4, "организация", "", 1, "C", false, 0, "")
	p.SetFont("dejavu", "", 10)
	p.CellFormat(0, 6, f.Location, "B", 1, "L", false, 0, "")
	p.SetFont("dejavu", "", 7)
	p.CellFormat(0, 4, "структурное подразделение", "", 1, "C", false, 0, "")
	p.Ln(2)
	p.SetFont("dejavu", "B", 12)
	p.CellFormat(0, 7, f.Title, "", 1, "C", false, 0, "")
	p.SetFont("dejavu", "", 10)
	p.CellFormat(0, 6, fmt.Sprintf("№ %s от %s", f.Number, f.Date.Format("02.01.2006")), "", 1, "C", false, 0, "")
	p.Ln(2)
	p.SetFont("dejavu", "", 9)
	for _, pt := range f.Parties {
		p.CellFormat(45, 5.5, pt[0], "", 0, "L", false, 0, "")
		p.CellFormat(0, 5.5, pt[1], "B", 1, "L", false, 0, "")
	}
	p.Ln(3)
	widths := f.Widths
	if widths == nil {
		widths = columnWidths(p, f.Columns, f.Rows, 273)
	}
	p.SetFont("dejavu", "B", 7.5)
	p.SetFillColor(235, 237, 245)
	for i, c := range f.Columns {
		p.CellFormat(widths[i], 6, fitText(p, c.Title, widths[i]), "1", 0, "C", true, 0, "")
	}
	p.Ln(-1)
	p.SetFont("dejavu", "", 7.5)
	for _, r := range f.Rows {
		if p.GetY() > 185 {
			p.AddPage()
		}
		for i, c := range f.Columns {
			align := "L"
			if c.Numeric {
				align = "R"
			}
			p.CellFormat(widths[i], 5, fitText(p, r[i], widths[i]), "1", 0, align, false, 0, "")
		}
		p.Ln(-1)
	}
	if f.Totals != nil {
		p.SetFont("dejavu", "B", 7.5)
		for i, c := range f.Columns {
			align := "L"
			if c.Numeric {
				align = "R"
			}
			p.CellFormat(widths[i], 5, f.Totals[i], "1", 0, align, false, 0, "")
		}
		p.Ln(-1)
	}
	if f.Note != "" {
		p.Ln(2)
		p.SetFont("dejavu", "", 9)
		p.MultiCell(0, 5, f.Note, "", "L", false)
	}
	p.Ln(6)
	p.SetFont("dejavu", "", 9)
	for _, role := range f.Signatures {
		p.CellFormat(70, 7, role, "", 0, "L", false, 0, "")
		p.CellFormat(45, 7, "", "B", 0, "L", false, 0, "")
		p.CellFormat(5, 7, "", "", 0, "L", false, 0, "")
		p.CellFormat(60, 7, "", "B", 1, "L", false, 0, "")
	}
	p.SetFont("dejavu", "", 7)
	p.CellFormat(70, 4, "", "", 0, "L", false, 0, "")
	p.CellFormat(45, 4, "подпись", "", 0, "C", false, 0, "")
	p.CellFormat(5, 4, "", "", 0, "L", false, 0, "")
	p.CellFormat(60, 4, "расшифровка подписи", "", 1, "C", false, 0, "")
	var buf bytes.Buffer
	err := p.Output(&buf)
	return buf.Bytes(), err
}

// Rub — сумма в копейках прописью цифрами: «1 234,50».
func Rub(kopecks int64) string { return rub(kopecks) }
