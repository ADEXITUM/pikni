package forms

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/ean"
	"github.com/boombuler/barcode/qr"
	"github.com/go-pdf/fpdf"
	"github.com/xuri/excelize/v2"
)

// Format — носитель печати: лист A4 с сеткой или термоэтикетка 58 × 40 мм (ЭТ-2, ИМ-2).
type Format string

const (
	A4      Format = "a4"
	Thermal Format = "thermal"
)

// PriceTag — ценник: по правилам продажи товаров (постановление № 2463) — наименование, цена за
// единицу и за литр; для алкоголя — объём и крепость. Штрихкод нужен кассе и для сверки ценника.
type PriceTag struct {
	Name, Pack, Abv  string
	PriceKopeck      int64
	PerLiterKopeck   int64
	Barcode, Article string
	Date             time.Time
}

// AssetLabel — инвентарная наклейка: номер штрихкодом Code 128 и QR-кодом.
type AssetLabel struct {
	Number, Name, Location string
}

// grid — сетка этикеток: на A4 — 3 × 7 по 66 × 40 мм, на термопринтере — одна на страницу.
type grid struct {
	p          *fpdf.Fpdf
	format     Format
	w, h       float64
	cols, rows int
	n          int
}

func newGrid(format Format) *grid {
	g := &grid{format: format, w: 58, h: 40, cols: 1, rows: 1}
	if format == A4 {
		g.w, g.h, g.cols, g.rows = 66, 40, 3, 7
		g.p = fpdf.New("P", "mm", "A4", "")
	} else {
		g.p = fpdf.NewCustom(&fpdf.InitType{UnitStr: "mm", Size: fpdf.SizeType{Wd: 58, Ht: 40}})
	}
	g.p.AddUTF8FontFromBytes("dejavu", "", fontRegular)
	g.p.AddUTF8FontFromBytes("dejavu", "B", fontBold)
	g.p.SetMargins(0, 0, 0)
	g.p.SetAutoPageBreak(false, 0)
	return g
}

// next — левый верхний угол следующей этикетки.
func (g *grid) next() (float64, float64) {
	per := g.cols * g.rows
	if g.n%per == 0 {
		g.p.AddPage()
	}
	i := g.n % per
	g.n++
	if g.format != A4 {
		return 0, 0
	}
	x, y := 6+float64(i%g.cols)*g.w, 8.5+float64(i/g.cols)*g.h
	// Линии реза — тонкие, чтобы ценники было удобно резать с листа.
	g.p.SetDrawColor(200, 200, 200)
	g.p.SetLineWidth(0.1)
	g.p.Rect(x, y, g.w, g.h, "D")
	return x, y
}

func (g *grid) bytes() ([]byte, error) {
	var buf bytes.Buffer
	err := g.p.Output(&buf)
	return buf.Bytes(), err
}

// bars рисует одномерный штрихкод векторными прямоугольниками — сканер читает их с любой печати.
func bars(p *fpdf.Fpdf, code barcode.Barcode, x, y, w, h float64) {
	n := code.Bounds().Dx()
	module := w / float64(n)
	p.SetFillColor(0, 0, 0)
	for i := 0; i < n; {
		if r, _, _, _ := code.At(i, 0).RGBA(); r != 0 {
			i++
			continue
		}
		j := i
		for j < n {
			if r, _, _, _ := code.At(j, 0).RGBA(); r != 0 {
				break
			}
			j++
		}
		p.Rect(x+float64(i)*module, y, float64(j-i)*module, h, "F")
		i = j
	}
}

func square(p *fpdf.Fpdf, code barcode.Barcode, x, y, size float64) {
	n := code.Bounds().Dx()
	module := size / float64(n)
	p.SetFillColor(0, 0, 0)
	for row := range n {
		for col := range n {
			if r, _, _, _ := code.At(col, row).RGBA(); r == 0 {
				p.Rect(x+float64(col)*module, y+float64(row)*module, module, module, "F")
			}
		}
	}
}

// fit уменьшает шрифт, пока текст не поместится в заданное число строк.
func fit(p *fpdf.Fpdf, text string, w float64, lines int, size float64) float64 {
	for ; size > 5; size -= 0.5 {
		p.SetFont("dejavu", "B", size)
		if len(p.SplitText(text, w)) <= lines {
			break
		}
	}
	return size
}

// PriceTagsPDF — ценники на A4 или термоэтикетках.
func PriceTagsPDF(format Format, tags []PriceTag) ([]byte, error) {
	g := newGrid(format)
	for _, t := range tags {
		x, y := g.next()
		p := g.p
		pad := 2.5
		w := g.w - 2*pad
		size := fit(p, t.Name, w, 2, 9)
		p.SetXY(x+pad, y+pad)
		p.SetFont("dejavu", "B", size)
		p.MultiCell(w, size*0.42, t.Name, "", "L", false)
		p.SetXY(x+pad, y+11.5)
		p.SetFont("dejavu", "", 7.5)
		p.CellFormat(w, 3.5, strings.Join(nonEmpty(t.Pack, t.Abv), " · "), "", 0, "L", false, 0, "")

		p.SetXY(x+pad, y+15.5)
		p.SetFont("dejavu", "B", 18)
		p.CellFormat(w, 8, rub(t.PriceKopeck)+" руб.", "", 0, "L", false, 0, "")
		if t.PerLiterKopeck > 0 {
			p.SetXY(x+pad, y+23.5)
			p.SetFont("dejavu", "", 6.5)
			p.CellFormat(w, 3, "за 1 л: "+rub(t.PerLiterKopeck)+" руб.", "", 0, "L", false, 0, "")
		}

		if code, err := ean.Encode(t.Barcode); err == nil && t.Barcode != "" {
			bars(p, code, x+pad, y+27.5, 30, 8)
			p.SetXY(x+pad, y+35.6)
			p.SetFont("dejavu", "", 6)
			p.CellFormat(30, 2.5, t.Barcode, "", 0, "C", false, 0, "")
		}
		p.SetXY(x+g.w-pad-22, y+30)
		p.SetFont("dejavu", "", 6)
		p.CellFormat(22, 2.5, t.Article, "", 2, "R", false, 0, "")
		p.SetX(x + g.w - pad - 22)
		p.CellFormat(22, 2.5, t.Date.Format("02.01.2006"), "", 0, "R", false, 0, "")
	}
	if len(tags) == 0 {
		g.p.AddPage()
	}
	return g.bytes()
}

// AssetLabelsPDF — инвентарные наклейки: номер крупно, Code 128 и QR-код с тем же номером.
func AssetLabelsPDF(format Format, labels []AssetLabel) ([]byte, error) {
	g := newGrid(format)
	for _, l := range labels {
		x, y := g.next()
		p := g.p
		pad := 2.5
		p.SetXY(x+pad, y+pad)
		p.SetFont("dejavu", "", 6.5)
		p.CellFormat(g.w-2*pad, 3, "Инвентарный номер", "", 2, "L", false, 0, "")
		p.SetX(x + pad)
		p.SetFont("dejavu", "B", 12)
		p.CellFormat(g.w-2*pad, 5.5, l.Number, "", 2, "L", false, 0, "")
		p.SetX(x + pad)
		size := fit(p, l.Name, g.w-2*pad-17, 2, 7.5)
		p.SetFont("dejavu", "", size)
		p.MultiCell(g.w-2*pad-17, size*0.42, l.Name, "", "L", false)
		p.SetXY(x+pad, y+21)
		p.SetFont("dejavu", "", 6)
		p.CellFormat(g.w-2*pad-17, 3, l.Location, "", 0, "L", false, 0, "")
		if code, err := code128.Encode(l.Number); err == nil {
			bars(p, code, x+pad, y+26, g.w-2*pad-17, 10)
		}
		if code, err := qr.Encode(l.Number, qr.M, qr.Auto); err == nil {
			square(p, code, x+g.w-pad-15, y+g.h-pad-15, 15)
		}
	}
	if len(labels) == 0 {
		g.p.AddPage()
	}
	return g.bytes()
}

// AssetWriteoff — акт о списании объекта имущества (ИМ-5).
type AssetWriteoff struct {
	Organization, Location  string
	Number, Name, Category  string
	Serial, Responsible     string
	AcceptedAt              time.Time
	CostKopeck              int64
	Reason                  string
	RequestedBy, ApprovedBy string
	RequestedAt, ApprovedAt time.Time
}

func AssetWriteoffPDF(a AssetWriteoff) ([]byte, error) {
	p := newPDF(false)
	p.SetFont("dejavu", "", 10)
	p.CellFormat(0, 6, a.Organization, "B", 1, "L", false, 0, "")
	p.SetFont("dejavu", "", 7)
	p.CellFormat(0, 4, "организация", "", 1, "C", false, 0, "")
	p.Ln(4)
	p.SetFont("dejavu", "B", 13)
	p.CellFormat(0, 7, "Акт о списании объекта имущества", "", 1, "C", false, 0, "")
	p.SetFont("dejavu", "", 10)
	p.CellFormat(0, 6, "от "+a.ApprovedAt.Format("02.01.2006"), "", 1, "C", false, 0, "")
	p.Ln(4)
	rows := [][2]string{
		{"Инвентарный номер", a.Number},
		{"Наименование", a.Name},
		{"Категория", a.Category},
		{"Серийный номер", a.Serial},
		{"Местонахождение", a.Location},
		{"Материально ответственное лицо", a.Responsible},
		{"Дата принятия к учёту", a.AcceptedAt.Format("02.01.2006")},
		{"Первоначальная стоимость, руб.", rub(a.CostKopeck)},
	}
	for _, r := range rows {
		p.SetFont("dejavu", "", 9)
		p.CellFormat(70, 7, r[0], "1", 0, "L", false, 0, "")
		p.CellFormat(0, 7, r[1], "1", 1, "L", false, 0, "")
	}
	p.Ln(4)
	p.SetFont("dejavu", "B", 10)
	p.CellFormat(0, 6, "Причина списания", "", 1, "L", false, 0, "")
	p.SetFont("dejavu", "", 10)
	p.MultiCell(0, 5.5, a.Reason, "", "L", false)
	p.Ln(4)
	p.SetFont("dejavu", "", 9)
	p.MultiCell(0, 5, fmt.Sprintf("Списание оформил %s %s, утвердил %s %s.",
		a.RequestedBy, a.RequestedAt.Format("02.01.2006 15:04"), a.ApprovedBy, a.ApprovedAt.Format("02.01.2006 15:04")), "", "L", false)
	signatures(p)
	var buf bytes.Buffer
	err := p.Output(&buf)
	return buf.Bytes(), err
}

func nonEmpty(items ...string) []string {
	out := items[:0]
	for _, s := range items {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// AssetRow — строка ведомости имущества.
type AssetRow struct {
	Number, Name, Category, Serial string
	AcceptedAt                     time.Time
	CostKopeck                     int64
	Location, Responsible, Status  string
}

// AssetRegisterXLSX — ведомость имущества по точкам и МОЛ (ИМ-6) с итогом стоимости.
func AssetRegisterXLSX(rows []AssetRow) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "Имущество"
	_ = f.SetSheetName("Sheet1", sheet)
	_ = f.SetCellValue(sheet, "A1", "Ведомость имущества")
	_ = f.SetCellValue(sheet, "A2", "на "+time.Now().Format("02.01.2006"))
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 12}})
	_ = f.SetCellStyle(sheet, "A1", "A1", bold)
	_ = f.SetSheetRow(sheet, "A4", &[]any{"Инв. номер", "Наименование", "Категория", "Серийный номер",
		"Принят к учёту", "Стоимость, руб.", "Точка", "МОЛ", "Состояние"})
	for i, r := range rows {
		_ = f.SetSheetRow(sheet, fmt.Sprintf("A%d", 5+i), &[]any{r.Number, r.Name, r.Category, r.Serial,
			r.AcceptedAt.Format("02.01.2006"), float64(r.CostKopeck) / 100, r.Location, r.Responsible, r.Status})
	}
	last := 4 + len(rows)
	_ = f.SetCellValue(sheet, fmt.Sprintf("B%d", last+1), "Итого")
	_ = f.SetCellFormula(sheet, fmt.Sprintf("F%d", last+1), fmt.Sprintf("SUM(F5:F%d)", max(last, 5)))
	_ = f.SetColWidth(sheet, "B", "B", 40)
	_ = f.SetColWidth(sheet, "G", "H", 28)
	return xlsx(f)
}
