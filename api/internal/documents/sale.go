package documents

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/documents/store"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

const Sale = "sale"

// OpenSale заводит документ «Продажа» смены кассы: чеки смены списывают по нему остаток зала.
func (s *Service) OpenSale(ctx context.Context, tx pgx.Tx, location, floor, cashier uuid.UUID, date time.Time) (uuid.UUID, error) {
	return create(ctx, tx, Sale, date, store.CreateDocumentParams{LocationID: location, ZoneID: floor, AuthorID: &cashier})
}

// SaleLine — строка чека: вариант, количество и марка бутылки, если товар учитывается помарочно.
type SaleLine struct {
	VariantID uuid.UUID
	Qty       int32
	Mark      string
}

// RecordSale проводит чек по документу смены сразу, а не при закрытии смены: остатки в течение дня
// верные. Продажа списывает остаток — товар без марок и сверх учёта, в недостачу к разбору
// (решение 0013); марка выбывает. Возврат ставит товар в зал по текущей средней, марку — на остаток.
func (s *Service) RecordSale(ctx context.Context, tx pgx.Tx, doc, location, floor, cashier uuid.UUID, refund bool, lines []SaleLine) error {
	q := s.q.WithTx(tx)
	d, err := q.LockDocument(ctx, doc)
	if err != nil {
		return err
	}
	if d.Status != StatusDraft {
		return ErrNotDraft
	}
	ev := marks.Event{DocumentID: doc, UserID: cashier}
	var codes []string
	for _, l := range lines {
		if l.Mark != "" {
			codes = append(codes, l.Mark)
		}
	}
	zones := map[string]uuid.UUID{}
	if len(codes) > 0 {
		if refund {
			err = marks.Unsell(ctx, tx, ev, location, floor, codes)
		} else {
			zones, err = marks.Sell(ctx, tx, ev, location, codes)
		}
		if err != nil {
			return err
		}
	}
	moves := make([]stock.Move, 0, len(lines))
	for _, l := range lines {
		m := stock.Move{VariantID: l.VariantID, LocationID: location, ZoneID: floor, External: true}
		if refund {
			m.Qty, m.AtAverage = l.Qty, true
		} else {
			line, err := q.AddToLine(ctx, store.AddToLineParams{DocumentID: doc, VariantID: l.VariantID, Qty: l.Qty})
			if err != nil {
				return err
			}
			m.LineID, m.Qty, m.AllowDeficit = line, -l.Qty, l.Mark == ""
			if z, ok := zones[l.Mark]; ok {
				m.ZoneID = z
			}
		}
		moves = append(moves, m)
	}
	_, err = stock.Apply(ctx, tx, stock.Doc{ID: doc, Date: d.DocDate}, moves)
	return err
}

// CloseSale проводит документ смены. Проданное пиво списывается в ЕГАИС из зала актом
// «Реализация»; до квитанции документ «ожидает ЕГАИС».
func (s *Service) CloseSale(ctx context.Context, tx pgx.Tx, doc, location, cashier uuid.UUID, items []egais.Item) (bool, error) {
	q := s.q.WithTx(tx)
	status := StatusPosted
	if len(items) > 0 {
		if err := egais.EnqueuePayload(ctx, tx, location, egais.ActWriteOffShop, doc,
			map[string]any{"reason": "Реализация", "items": items}); err != nil {
			return false, err
		}
		status = StatusAwaitingEgais
	}
	return len(items) > 0, q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: doc, Status: status, PostedBy: &cashier})
}
