package documents

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/forms"
)

var ErrFormNotApplicable = errors.New("form does not fit the document kind")

// TorgForm — унифицированные формы по документам (ОТ-2): ТОРГ-2 — акт о расхождении при приёмке,
// ТОРГ-13 — накладная на внутреннее перемещение (передача в зал и между точками), ТОРГ-16 — акт о
// списании. Суммы — в учётных ценах, для приёмки — в ценах поставщика из ТТН.
func (s *Service) TorgForm(ctx context.Context, id uuid.UUID, form string) ([]byte, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	doc := d.Doc
	fits := map[string]bool{
		"torg2":  doc.Kind == Receipt,
		"torg13": doc.Kind == FloorTransfer || doc.Kind == Transfer,
		"torg16": doc.Kind == Writeoff,
	}
	if !fits[form] {
		return nil, ErrFormNotApplicable
	}
	org, err := s.q.OrganizationName(ctx)
	if err != nil {
		return nil, err
	}
	f := forms.DocForm{Organization: org, Location: doc.LocationName, Number: doc.Number, Date: doc.DocDate,
		Signatures: []string{"Сдал", "Принял"}}
	if doc.PostedAt != nil {
		f.Date = *doc.PostedAt
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	name := func(l int) string {
		ln := d.Lines[l]
		return ln.Name + ", " + catalog.PackLabel(ln.VolumeMl, ln.NetWeightG, ln.Container, ln.GiftPack)
	}
	var total int64
	switch form {
	case "torg2":
		f.Code, f.Title = "ТОРГ-2", "Акт об установленном расхождении по количеству и качеству при приёмке товаров"
		f.Parties = [][2]string{{"Поставщик", str(doc.SupplierName)}, {"Товарно-транспортная накладная", str(doc.TtnNumber)}}
		f.Columns = []forms.TableColumn{{Title: "№"}, {Title: "Наименование"}, {Title: "Код"}, {Title: "По документу, шт", Numeric: true},
			{Title: "Фактически, шт", Numeric: true}, {Title: "Недостача", Numeric: true}, {Title: "Излишек", Numeric: true},
			{Title: "Цена, руб.", Numeric: true}, {Title: "Сумма расхождения, руб.", Numeric: true}}
		f.Widths = []float64{10, 95, 25, 22, 22, 20, 20, 25, 34}
		for i, l := range d.Lines {
			plan := int32(0)
			if l.QtyPlan != nil {
				plan = *l.QtyPlan
			}
			diff := l.Qty - plan
			if diff == 0 {
				continue
			}
			short, extra := "", ""
			if diff < 0 {
				short = fmt.Sprint(-diff)
			} else {
				extra = fmt.Sprint(diff)
			}
			sum := l.PriceKopecks * int64(diff)
			total += sum
			f.Rows = append(f.Rows, []string{fmt.Sprint(len(f.Rows) + 1), name(i), l.Article, fmt.Sprint(plan), fmt.Sprint(l.Qty),
				short, extra, forms.Rub(l.PriceKopecks), forms.Rub(sum)})
		}
		f.Totals = []string{"", "Итого", "", "", "", "", "", "", forms.Rub(total)}
		if len(f.Rows) == 0 {
			f.Note = "Расхождений не установлено."
		}
		f.Signatures = []string{"Материально ответственное лицо", "Член комиссии", "Член комиссии"}
	case "torg13":
		f.Code, f.Title = "ТОРГ-13", "Накладная на внутреннее перемещение, передачу товаров, тары"
		from, to := zoneNames[doc.ZoneKind], ""
		if doc.DestZoneKind != nil {
			to = zoneNames[*doc.DestZoneKind]
		}
		if doc.Kind == Transfer {
			from, to = doc.LocationName, str(doc.DestLocationName)
		}
		f.Parties = [][2]string{{"Отправитель", from}, {"Получатель", to}}
		f = lineTable(f, d, name, &total)
	case "torg16":
		f.Code, f.Title = "ТОРГ-16", "Акт о списании товаров"
		f.Parties = [][2]string{{"Место хранения", zoneNames[doc.ZoneKind]}, {"Причина списания", str(doc.ReasonName)}}
		f = lineTable(f, d, name, &total)
		f.Signatures = []string{"Председатель комиссии", "Член комиссии", "Материально ответственное лицо"}
		if d.Approval != nil && d.Approval.Decision == "approved" {
			f.Note = fmt.Sprintf("Списание утвердил %s %s.", d.Approval.FullName, d.Approval.CreatedAt.Format("02.01.2006 15:04"))
		}
	}
	return forms.DocFormPDF(f)
}

func lineTable(f forms.DocForm, d Detail, name func(int) string, total *int64) forms.DocForm {
	f.Columns = []forms.TableColumn{{Title: "№"}, {Title: "Наименование"}, {Title: "Код"}, {Title: "Ед."},
		{Title: "Количество", Numeric: true}, {Title: "Цена, руб.", Numeric: true}, {Title: "Сумма, руб.", Numeric: true}}
	f.Widths = []float64{10, 125, 30, 15, 28, 30, 35}
	var qty int32
	for i, l := range d.Lines {
		price := (l.UnitCostE4 + 50) / 100
		sum := price * int64(l.Qty)
		*total += sum
		qty += l.Qty
		f.Rows = append(f.Rows, []string{fmt.Sprint(i + 1), name(i), l.Article, "шт", fmt.Sprint(l.Qty), forms.Rub(price), forms.Rub(sum)})
	}
	f.Totals = []string{"", "Итого", "", "", fmt.Sprint(qty), "", forms.Rub(*total)}
	return f
}
