// Package forms — печатные формы ревизии: инвентаризационная опись ИНВ-3 и сличительная ведомость
// ИНВ-19 (постановление Госкомстата России № 88; с 2013 г. формы рекомендованные, организация
// утверждает их сама). Каждая — в PDF для подписи комиссией и в Excel для бухгалтерии.
package forms

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/xuri/excelize/v2"
)

//go:embed fonts/DejaVuSansCondensed.ttf
var fontRegular []byte

//go:embed fonts/DejaVuSansCondensed-Bold.ttf
var fontBold []byte

// Doc — шапка формы.
type Doc struct {
	Organization string
	Location     string
	Number       string
	Date         time.Time
	Started      time.Time
	Zones        string
}

// Line — позиция: учёт на начало подсчёта, факт и учётная цена единицы, копейки.
type Line struct {
	Name, Code  string
	Book, Fact  int32
	PriceKopeck int64
}

func rub(kopecks int64) string {
	sign := ""
	if kopecks < 0 {
		sign, kopecks = "-", -kopecks
	}
	whole := fmt.Sprint(kopecks / 100)
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(c)
	}
	return fmt.Sprintf("%s%s,%02d", sign, b.String(), kopecks%100)
}

func newPDF(landscape bool) *fpdf.Fpdf {
	orientation := "P"
	if landscape {
		orientation = "L"
	}
	p := fpdf.New(orientation, "mm", "A4", "")
	p.AddUTF8FontFromBytes("dejavu", "", fontRegular)
	p.AddUTF8FontFromBytes("dejavu", "B", fontBold)
	p.SetMargins(12, 12, 12)
	p.SetAutoPageBreak(true, 15)
	p.AddPage()
	return p
}

func header(p *fpdf.Fpdf, title, form string, d Doc) {
	p.SetFont("dejavu", "", 8)
	p.CellFormat(0, 4, "Унифицированная форма № "+form+" · утверждена постановлением Госкомстата России от 18.08.1998 № 88", "", 1, "R", false, 0, "")
	p.SetFont("dejavu", "", 10)
	p.CellFormat(0, 6, d.Organization, "B", 1, "L", false, 0, "")
	p.SetFont("dejavu", "", 7)
	p.CellFormat(0, 4, "организация", "", 1, "C", false, 0, "")
	p.SetFont("dejavu", "", 10)
	p.CellFormat(0, 6, d.Location, "B", 1, "L", false, 0, "")
	p.SetFont("dejavu", "", 7)
	p.CellFormat(0, 4, "структурное подразделение", "", 1, "C", false, 0, "")
	p.Ln(3)
	p.SetFont("dejavu", "B", 13)
	p.CellFormat(0, 7, title, "", 1, "C", false, 0, "")
	p.SetFont("dejavu", "", 10)
	p.CellFormat(0, 6, fmt.Sprintf("№ %s от %s", d.Number, d.Date.Format("02.01.2006")), "", 1, "C", false, 0, "")
	p.SetFont("dejavu", "", 9)
	p.MultiCell(0, 5, fmt.Sprintf("Основание: приказ о проведении инвентаризации. Подсчёт начат %s. Зоны: %s.",
		d.Started.Format("02.01.2006 15:04"), d.Zones), "", "L", false)
	p.Ln(2)
}

func signatures(p *fpdf.Fpdf) {
	p.Ln(6)
	p.SetFont("dejavu", "", 9)
	for _, role := range []string{"Председатель комиссии", "Член комиссии", "Член комиссии", "Материально ответственное лицо"} {
		p.CellFormat(70, 7, role, "", 0, "L", false, 0, "")
		p.CellFormat(45, 7, "", "B", 0, "L", false, 0, "")
		p.CellFormat(5, 7, "", "", 0, "L", false, 0, "")
		p.CellFormat(55, 7, "", "B", 1, "L", false, 0, "")
	}
	p.SetFont("dejavu", "", 7)
	p.CellFormat(70, 4, "", "", 0, "L", false, 0, "")
	p.CellFormat(45, 4, "подпись", "", 0, "C", false, 0, "")
	p.CellFormat(5, 4, "", "", 0, "L", false, 0, "")
	p.CellFormat(55, 4, "расшифровка подписи", "", 1, "C", false, 0, "")
}

func row(p *fpdf.Fpdf, widths []float64, cells []string, aligns string, bold bool) {
	style := ""
	if bold {
		style = "B"
	}
	p.SetFont("dejavu", style, 8)
	for i, c := range cells {
		p.CellFormat(widths[i], 6, c, "1", 0, string(aligns[i]), false, 0, "")
	}
	p.Ln(-1)
}

// Inv3PDF — инвентаризационная опись: фактическое наличие по позициям.
func Inv3PDF(d Doc, lines []Line) ([]byte, error) {
	p := newPDF(false)
	header(p, "ИНВЕНТАРИЗАЦИОННАЯ ОПИСЬ ТОВАРНО-МАТЕРИАЛЬНЫХ ЦЕННОСТЕЙ", "ИНВ-3", d)
	w := []float64{10, 82, 22, 12, 18, 20, 22}
	row(p, w, []string{"№", "Наименование, характеристика", "Код", "Ед.", "Факт, кол.", "Цена, руб.", "Сумма, руб."}, "CCCCCCC", true)
	var qty int32
	var sum int64
	for i, l := range lines {
		total := l.PriceKopeck * int64(l.Fact)
		qty += l.Fact
		sum += total
		row(p, w, []string{fmt.Sprint(i + 1), l.Name, l.Code, "шт", fmt.Sprint(l.Fact), rub(l.PriceKopeck), rub(total)}, "RLLCRRR", false)
	}
	row(p, w, []string{"", "Итого", "", "", fmt.Sprint(qty), "", rub(sum)}, "RLLCRRR", true)
	p.Ln(3)
	p.SetFont("dejavu", "", 9)
	p.MultiCell(0, 5, fmt.Sprintf("Порядковых номеров: %d; общее количество единиц фактически: %d; на сумму %s руб.", len(lines), qty, rub(sum)), "", "L", false)
	signatures(p)
	var buf bytes.Buffer
	err := p.Output(&buf)
	return buf.Bytes(), err
}

// Inv19PDF — сличительная ведомость: только расхождения — излишки и недостачи в количестве и сумме.
func Inv19PDF(d Doc, lines []Line) ([]byte, error) {
	p := newPDF(true)
	header(p, "СЛИЧИТЕЛЬНАЯ ВЕДОМОСТЬ РЕЗУЛЬТАТОВ ИНВЕНТАРИЗАЦИИ ТОВАРНО-МАТЕРИАЛЬНЫХ ЦЕННОСТЕЙ", "ИНВ-19", d)
	w := []float64{10, 92, 24, 12, 20, 20, 22, 26, 22, 26}
	row(p, w, []string{"№", "Наименование, характеристика", "Код", "Ед.", "Учёт", "Факт", "Излишек", "Сумма, руб.", "Недостача", "Сумма, руб."}, "CCCCCCCCCC", true)
	var surplus, shortage int32
	var surplusSum, shortageSum int64
	n := 0
	for _, l := range lines {
		diff := l.Fact - l.Book
		if diff == 0 {
			continue
		}
		n++
		cells := []string{fmt.Sprint(n), l.Name, l.Code, "шт", fmt.Sprint(l.Book), fmt.Sprint(l.Fact), "", "", "", ""}
		if diff > 0 {
			surplus += diff
			surplusSum += l.PriceKopeck * int64(diff)
			cells[6], cells[7] = fmt.Sprint(diff), rub(l.PriceKopeck*int64(diff))
		} else {
			shortage -= diff
			shortageSum -= l.PriceKopeck * int64(diff)
			cells[8], cells[9] = fmt.Sprint(-diff), rub(-l.PriceKopeck*int64(diff))
		}
		row(p, w, cells, "RLLCRRRRRR", false)
	}
	if n == 0 {
		p.SetFont("dejavu", "", 9)
		p.CellFormat(0, 8, "Расхождений нет: фактическое наличие совпало с учётом.", "1", 1, "C", false, 0, "")
	}
	row(p, w, []string{"", "Итого", "", "", "", "", fmt.Sprint(surplus), rub(surplusSum), fmt.Sprint(shortage), rub(shortageSum)}, "RLLCRRRRRR", true)
	signatures(p)
	var buf bytes.Buffer
	err := p.Output(&buf)
	return buf.Bytes(), err
}

func sheetHeader(f *excelize.File, sheet, title string, d Doc) {
	_ = f.SetCellValue(sheet, "A1", d.Organization)
	_ = f.SetCellValue(sheet, "A2", d.Location)
	_ = f.SetCellValue(sheet, "A3", title)
	_ = f.SetCellValue(sheet, "A4", fmt.Sprintf("№ %s от %s; подсчёт начат %s; зоны: %s", d.Number, d.Date.Format("02.01.2006"),
		d.Started.Format("02.01.2006 15:04"), d.Zones))
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 12}})
	_ = f.SetCellStyle(sheet, "A3", "A3", bold)
}

func xlsx(f *excelize.File) ([]byte, error) {
	var buf bytes.Buffer
	err := f.Write(&buf)
	return buf.Bytes(), err
}

// Inv3XLSX — опись в Excel: те же графы.
func Inv3XLSX(d Doc, lines []Line) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "ИНВ-3"
	_ = f.SetSheetName("Sheet1", sheet)
	sheetHeader(f, sheet, "Инвентаризационная опись товарно-материальных ценностей (ИНВ-3)", d)
	_ = f.SetSheetRow(sheet, "A6", &[]any{"№", "Наименование", "Код", "Ед.", "Факт, кол.", "Цена, руб.", "Сумма, руб."})
	for i, l := range lines {
		_ = f.SetSheetRow(sheet, fmt.Sprintf("A%d", 7+i), &[]any{i + 1, l.Name, l.Code, "шт", l.Fact,
			float64(l.PriceKopeck) / 100, float64(l.PriceKopeck*int64(l.Fact)) / 100})
	}
	last := 6 + len(lines)
	_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", last+1), "Итого")
	_ = f.SetCellFormula(sheet, fmt.Sprintf("E%d", last+1), fmt.Sprintf("SUM(E7:E%d)", last))
	_ = f.SetCellFormula(sheet, fmt.Sprintf("G%d", last+1), fmt.Sprintf("SUM(G7:G%d)", last))
	_ = f.SetColWidth(sheet, "B", "B", 50)
	return xlsx(f)
}

// Inv19XLSX — сличительная ведомость в Excel: расхождения в количестве и сумме.
func Inv19XLSX(d Doc, lines []Line) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "ИНВ-19"
	_ = f.SetSheetName("Sheet1", sheet)
	sheetHeader(f, sheet, "Сличительная ведомость результатов инвентаризации (ИНВ-19)", d)
	_ = f.SetSheetRow(sheet, "A6", &[]any{"№", "Наименование", "Код", "Ед.", "Учёт", "Факт", "Излишек", "Сумма, руб.", "Недостача", "Сумма, руб."})
	r := 7
	for _, l := range lines {
		diff := l.Fact - l.Book
		if diff == 0 {
			continue
		}
		vals := []any{r - 6, l.Name, l.Code, "шт", l.Book, l.Fact, nil, nil, nil, nil}
		if diff > 0 {
			vals[6], vals[7] = diff, float64(l.PriceKopeck*int64(diff))/100
		} else {
			vals[8], vals[9] = -diff, float64(-l.PriceKopeck*int64(diff))/100
		}
		_ = f.SetSheetRow(sheet, fmt.Sprintf("A%d", r), &vals)
		r++
	}
	_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", r), "Итого")
	for _, col := range []string{"G", "H", "I", "J"} {
		_ = f.SetCellFormula(sheet, fmt.Sprintf("%s%d", col, r), fmt.Sprintf("SUM(%s7:%s%d)", col, col, max(r-1, 7)))
	}
	_ = f.SetColWidth(sheet, "B", "B", 50)
	return xlsx(f)
}
