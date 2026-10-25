package forms

import (
	"bytes"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/go-pdf/fpdf"
	"github.com/xuri/excelize/v2"
)

// TableHeader — реквизиты отчёта (ОТ-5): организация, название, параметры, кто и когда сформировал.
type TableHeader struct {
	Organization, Title, Subtitle, Author string
	Created                               time.Time
}

type TableColumn struct {
	Title   string
	Numeric bool
	Sum     bool
}

// TablePDF — отчёт таблицей на альбомном листе A4. Ширина колонок — по самому длинному значению.
func TablePDF(h TableHeader, cols []TableColumn, rows [][]string, totals []string) ([]byte, error) {
	p := newPDF(true)
	p.SetFont("dejavu", "", 9)
	p.CellFormat(0, 5, h.Organization, "", 1, "L", false, 0, "")
	p.SetFont("dejavu", "B", 13)
	p.CellFormat(0, 7, h.Title, "", 1, "L", false, 0, "")
	p.SetFont("dejavu", "", 9)
	if h.Subtitle != "" {
		p.MultiCell(0, 5, h.Subtitle, "", "L", false)
	}
	p.Ln(2)

	widths := columnWidths(p, cols, rows, 273)
	header := func() {
		p.SetFont("dejavu", "B", 7.5)
		p.SetFillColor(235, 237, 245)
		for i, c := range cols {
			p.CellFormat(widths[i], 6, fitText(p, c.Title, widths[i]), "1", 0, "C", true, 0, "")
		}
		p.Ln(-1)
	}
	header()
	p.SetFont("dejavu", "", 7.5)
	for _, r := range rows {
		if p.GetY() > 190 {
			p.AddPage()
			header()
			p.SetFont("dejavu", "", 7.5)
		}
		for i, c := range cols {
			align := "L"
			if c.Numeric {
				align = "R"
			}
			p.CellFormat(widths[i], 5, fitText(p, r[i], widths[i]), "1", 0, align, false, 0, "")
		}
		p.Ln(-1)
	}
	if totals != nil {
		p.SetFont("dejavu", "B", 7.5)
		for i, c := range cols {
			align := "L"
			if c.Numeric {
				align = "R"
			}
			p.CellFormat(widths[i], 5, fitText(p, totals[i], widths[i]), "1", 0, align, false, 0, "")
		}
		p.Ln(-1)
	}
	p.Ln(4)
	p.SetFont("dejavu", "", 8)
	p.CellFormat(0, 5, fmt.Sprintf("Сформировал: %s, %s. Строк: %d.", h.Author, h.Created.Format("02.01.2006 15:04"), len(rows)), "", 1, "L", false, 0, "")
	var buf bytes.Buffer
	err := p.Output(&buf)
	return buf.Bytes(), err
}

func columnWidths(p *fpdf.Fpdf, cols []TableColumn, rows [][]string, total float64) []float64 {
	p.SetFont("dejavu", "", 7.5)
	want := make([]float64, len(cols))
	for i, c := range cols {
		want[i] = min(p.GetStringWidth(c.Title)/1.6+4, 40)
	}
	for _, r := range rows[:min(len(rows), 300)] {
		for i := range cols {
			want[i] = max(want[i], min(p.GetStringWidth(r[i])+3, 90))
		}
	}
	sum := 0.0
	for _, w := range want {
		sum += w
	}
	for i := range want {
		want[i] *= total / sum
	}
	return want
}

// fitText обрезает значение, которое не помещается в ячейку, многоточием.
func fitText(p *fpdf.Fpdf, s string, w float64) string {
	if p.GetStringWidth(s) <= w-1.5 {
		return s
	}
	for utf8.RuneCountInString(s) > 1 {
		r := []rune(s)
		s = string(r[:len(r)-1])
		if p.GetStringWidth(s+"…") <= w-1.5 {
			return s + "…"
		}
	}
	return s
}

// TableXLSX — тот же отчёт в Excel: числа остаются числами, итоги — формулами.
func TableXLSX(h TableHeader, cols []TableColumn, rows [][]any) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "Отчёт"
	_ = f.SetSheetName("Sheet1", sheet)
	_ = f.SetCellValue(sheet, "A1", h.Organization)
	_ = f.SetCellValue(sheet, "A2", h.Title)
	_ = f.SetCellValue(sheet, "A3", h.Subtitle)
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 12}})
	head, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"EBEDF5"}, Pattern: 1}})
	_ = f.SetCellStyle(sheet, "A2", "A2", bold)
	titles := make([]any, len(cols))
	for i, c := range cols {
		titles[i] = c.Title
	}
	_ = f.SetSheetRow(sheet, "A5", &titles)
	last, _ := excelize.CoordinatesToCellName(len(cols), 5)
	_ = f.SetCellStyle(sheet, "A5", last, head)
	for i, r := range rows {
		row := r
		_ = f.SetSheetRow(sheet, fmt.Sprintf("A%d", 6+i), &row)
	}
	end := 5 + len(rows)
	for i, c := range cols {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, col, col, 14)
		if c.Sum && len(rows) > 0 {
			_ = f.SetCellFormula(sheet, fmt.Sprintf("%s%d", col, end+1), fmt.Sprintf("SUM(%s6:%s%d)", col, col, end))
		}
	}
	_ = f.SetCellValue(sheet, fmt.Sprintf("A%d", end+3), fmt.Sprintf("Сформировал: %s, %s", h.Author, h.Created.Format("02.01.2006 15:04")))
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 5, TopLeftCell: "A6", ActivePane: "bottomLeft"})
	return xlsx(f)
}
