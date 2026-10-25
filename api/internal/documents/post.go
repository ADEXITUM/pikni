package documents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/documents/store"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

// MissingCostError — у вариантов нет справочной закупочной цены: начальный остаток нечем оценить.
type MissingCostError struct{ Articles []string }

func (e *MissingCostError) Error() string {
	return fmt.Sprintf("no purchase price for %v", e.Articles)
}

// ShortageError — расход больше свободного остатка; описан для пользователя.
type ShortageError struct {
	Article, Name, Zone string
	Available, Need     int32
}

func (e *ShortageError) Error() string {
	return fmt.Sprintf("not enough %s in %s: %d available, %d needed", e.Article, e.Zone, e.Available, e.Need)
}

// Post проводит документ: движения в регистр, остатки, средняя себестоимость и марки меняются
// в одной транзакции (п. 3.2 backend.md). Документ, который надо отразить в ЕГАИС, получает
// статус «ожидает ЕГАИС»: учёт уже изменён, сообщение ждёт отправки в очереди.
func (s *Service) Post(ctx context.Context, id uuid.UUID, version int32) error {
	// Ревизия не «проводится», а отправляет сличительную ведомость на утверждение.
	if d, err := s.q.GetDocument(ctx, id); err == nil && d.Kind == Stocktake {
		return s.SubmitStocktake(ctx, id, version)
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := lockDraft(ctx, q, id, postPerm)
		if err != nil {
			return err
		}
		if doc.Version != version {
			return ErrConflict
		}
		lines, err := q.ListLines(ctx, store.ListLinesParams{DocumentID: id})
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return ErrEmpty
		}
		docMarks, err := q.DocumentMarks(ctx, id)
		if err != nil {
			return err
		}
		// У сторно марки — исходного документа, у поступления они считаются по ТТН.
		if doc.Kind != Receipt && doc.Kind != Storno {
			if err := checkMarkCounts(lines, docMarks); err != nil {
				return err
			}
		}
		ev := marks.Event{DocumentID: id, UserID: userID(ctx)}
		codes := make([]string, len(docMarks))
		markVariants := make([]uuid.UUID, len(docMarks))
		for i, m := range docMarks {
			codes[i], markVariants[i] = m.Code, *m.VariantID
		}

		var moves []stock.Move
		var unitCosts map[uuid.UUID]int64
		status := StatusPosted
		switch doc.Kind {
		case Receipt:
			if unitCosts, status, err = s.postReceipt(ctx, tx, q, doc, ev); err != nil {
				return err
			}
		case Opening:
			prices, err := s.catalog.PurchasePrices(ctx, variantIDs(lines), time.Now())
			if err != nil {
				return err
			}
			var missing []string
			for _, l := range lines {
				price, ok := prices[l.VariantID]
				if !ok {
					missing = append(missing, l.Article)
					continue
				}
				moves = append(moves, stock.Move{
					LineID: l.ID, VariantID: l.VariantID, LocationID: doc.LocationID, ZoneID: doc.ZoneID,
					Qty: l.Qty, External: true, UnitCostE4: price * 100,
				})
			}
			if len(missing) > 0 {
				return &MissingCostError{Articles: missing}
			}
			if len(codes) > 0 {
				if err := marks.Register(ctx, tx, ev, doc.LocationID, doc.ZoneID, codes, markVariants); err != nil {
					return err
				}
			}

		case Transfer:
			if unitCosts, err = s.shipTransfer(ctx, tx, q, doc, lines, docMarks, ev); err != nil {
				return err
			}
			status = StatusInTransit

		case Writeoff, Storno:
			if err := s.submit(ctx, q, doc); err != nil {
				return err
			}
			status = StatusPendingApproval

		case FloorTransfer:
			dest := *doc.DestZoneID
			var items []egais.Item
			for _, l := range lines {
				moves = append(moves,
					stock.Move{LineID: l.ID, VariantID: l.VariantID, LocationID: doc.LocationID, ZoneID: doc.ZoneID, Qty: -l.Qty},
					stock.Move{LineID: l.ID, VariantID: l.VariantID, LocationID: doc.LocationID, ZoneID: dest, Qty: l.Qty},
				)
				// Пиво без марок учитывается в ЕГАИС по регистрам склада и зала: перевод нужно отразить.
				if l.EgaisAccounting != nil && *l.EgaisAccounting == "quantity" {
					items = append(items, egais.Item{VariantID: l.VariantID, Alcocode: l.Alcocode, Qty: l.Qty})
				}
			}
			if len(codes) > 0 {
				if err := marks.MoveWithin(ctx, tx, ev, doc.ZoneID, doc.LocationID, dest, codes); err != nil {
					return err
				}
			}
			if len(items) > 0 {
				zone, err := q.GetZone(ctx, dest)
				if err != nil {
					return err
				}
				docType := egais.TransferToShop
				if zone.Kind != "floor" {
					docType = egais.TransferFromShop
				}
				if err := egais.Enqueue(ctx, tx, doc.LocationID, docType, id, items); err != nil {
					return err
				}
				status = StatusAwaitingEgais
			}
		}

		if doc.Kind != Receipt && doc.Kind != Transfer && status != StatusPendingApproval {
			unitCosts, err = stock.Apply(ctx, tx, stock.Doc{ID: id, Date: doc.DocDate}, moves)
			if se, ok := errors.AsType[*stock.ShortageError](err); ok {
				return s.describeShortage(ctx, q, lines, se)
			}
			if err != nil {
				return err
			}
		}
		costs := store.SetLineCostsParams{}
		for _, l := range lines {
			costs.Ids = append(costs.Ids, l.ID)
			costs.CostsE4 = append(costs.CostsE4, unitCosts[l.ID])
		}
		if err := q.SetLineCosts(ctx, costs); err != nil {
			return err
		}
		poster := userID(ctx)
		if err := q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: id, Status: status, PostedBy: &poster}); err != nil {
			return err
		}
		var total int32
		for _, l := range lines {
			total += l.Qty
		}
		action := apispec.AuditActionDocumentPosted
		if status == StatusPendingApproval {
			action = apispec.AuditActionDocumentSubmitted
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: action, EntityType: audit.EntityDocument, EntityID: id.String(),
			Data: map[string]any{"number": doc.Number, "kind": doc.Kind, "lines": len(lines), "qty": total, "status": status},
		})
	})
}

// checkMarkCounts — у товара с марками количество строки равно числу его марок в документе.
// Так устроен ввод (строку меняют только сканы марок), проверка страхует от ошибки в коде.
func checkMarkCounts(lines []store.ListLinesRow, docMarks []store.DocumentMarksRow) error {
	count := map[uuid.UUID]int32{}
	for _, m := range docMarks {
		count[*m.VariantID]++
	}
	for _, l := range lines {
		if marked(l.EgaisAccounting) && count[l.VariantID] != l.Qty {
			return fmt.Errorf("line %s: %d marks for quantity %d", l.Article, count[l.VariantID], l.Qty)
		}
	}
	return nil
}

func (s *Service) describeShortage(ctx context.Context, q *store.Queries, lines []store.ListLinesRow, se *stock.ShortageError) error {
	e := &ShortageError{Available: se.Available, Need: se.Need}
	for _, l := range lines {
		if l.VariantID == se.VariantID {
			e.Article, e.Name = l.Article, l.Name
		}
	}
	zone, err := q.GetZone(ctx, se.ZoneID)
	if err != nil {
		return err
	}
	e.Zone = zoneNames[zone.Kind]
	return e
}
