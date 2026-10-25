package documents

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/documents/store"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

const (
	ScanAccepted  = "accepted"
	ScanSelected  = "selected"
	ScanUnknown   = "unknown"
	ScanForeign   = "foreign"
	ScanDuplicate = "duplicate"
	ScanRejected  = "rejected"
)

// NewScan — скан с устройства. ID присваивает устройство: повтор запроса после обрыва связи
// возвращает уже записанный результат.
type NewScan struct {
	ID        uuid.UUID
	Raw       string
	Source    string
	ScannedAt time.Time
	// VariantID — товар, к которому отнести марку нового образца при вводе остатков.
	VariantID *uuid.UUID
}

// ScanResult — записанный скан и строка документа после него.
type ScanResult struct {
	Scan store.GetScanRow
	Line *store.ListLinesRow
}

// verdict — что система решила по скану, до записи.
type verdict struct {
	status   string
	reason   string
	codeKind *string
	variant  *uuid.UUID
	mark     *string
	crpt     *string
	pkg      *string
}

var zoneNames = map[string]string{"warehouse": "склад", "backroom": "склад магазина", "floor": "торговый зал"}

func reject(v verdict, status, reason string) verdict {
	v.status, v.reason = status, reason
	return v
}

// AddScan разбирает скан и засчитывает его в документ. Нераспознанный или чужой код тоже
// записывается — со статусом и причиной: лента сканов на экране показывает и ошибки.
func (s *Service) AddScan(ctx context.Context, docID uuid.UUID, in NewScan) (ScanResult, error) {
	var res ScanResult
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := lockDraft(ctx, q, docID, editPerm)
		if err != nil {
			return err
		}
		if prev, err := q.GetScan(ctx, in.ID); err == nil {
			if prev.DocumentID != docID {
				return ErrForeignScan
			}
			res.Scan = prev
			res.Line, err = lineOf(ctx, q, docID, prev.VariantID)
			return err
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		classify := s.classify
		if doc.Kind == Receipt {
			classify = s.classifyReceipt
		}
		v, err := classify(ctx, q, doc, in)
		if err != nil {
			return err
		}
		var reason *string
		if v.reason != "" {
			reason = &v.reason
		}
		if err := q.InsertScan(ctx, store.InsertScanParams{
			ID: in.ID, DocumentID: docID, UserID: userID(ctx), Source: in.Source, Raw: in.Raw,
			CodeKind: v.codeKind, VariantID: v.variant, MarkCode: v.mark, CrptCode: v.crpt, PackageCode: v.pkg, Qty: 1,
			Status: v.status, Reason: reason, ScannedAt: in.ScannedAt,
		}); err != nil {
			return err
		}
		// В поступлении марки и короба считаются по ТТН, строку наращивают только сканы товара без марок.
		counts := doc.Kind != Receipt || v.mark == nil && v.pkg == nil
		if v.status == ScanAccepted && counts && v.variant != nil {
			if _, err := q.AddToLine(ctx, store.AddToLineParams{DocumentID: docID, VariantID: *v.variant, Qty: 1}); err != nil {
				return err
			}
		}
		if res.Scan, err = q.GetScan(ctx, in.ID); err != nil {
			return err
		}
		res.Line, err = lineOf(ctx, q, docID, v.variant)
		return err
	})
	return res, err
}

// UndoScan отменяет скан: засчитанный уменьшает строку, марка перестаёт числиться в документе.
func (s *Service) UndoScan(ctx context.Context, docID, scanID uuid.UUID) (ScanResult, error) {
	if d, err := s.q.GetDocument(ctx, docID); err == nil && d.Kind == Stocktake {
		return s.UndoStocktakeScan(ctx, docID, scanID)
	}
	var res ScanResult
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := lockDraft(ctx, q, docID, editPerm)
		if err != nil {
			return err
		}
		undone, err := q.UndoScan(ctx, store.UndoScanParams{ID: scanID, DocumentID: docID})
		if errors.Is(err, pgx.ErrNoRows) {
			// Скан уже отменён или его нет: повтор запроса не должен уменьшать строку ещё раз.
			prev, err := q.GetScan(ctx, scanID)
			if errors.Is(err, pgx.ErrNoRows) || err == nil && prev.DocumentID != docID {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			res.Scan = prev
			res.Line, err = lineOf(ctx, q, docID, prev.VariantID)
			return err
		}
		if err != nil {
			return err
		}
		if undone.Status == ScanAccepted && undone.VariantID != nil && (doc.Kind != Receipt || undone.MarkCode == nil && undone.PackageCode == nil) {
			if err := q.ChangeLineQty(ctx, store.ChangeLineQtyParams{DocumentID: docID, VariantID: *undone.VariantID, Delta: -undone.Qty}); err != nil {
				return err
			}
		}
		if res.Scan, err = q.GetScan(ctx, scanID); err != nil {
			return err
		}
		res.Line, err = lineOf(ctx, q, docID, undone.VariantID)
		return err
	})
	return res, err
}

func lineOf(ctx context.Context, q *store.Queries, docID uuid.UUID, variant *uuid.UUID) (*store.ListLinesRow, error) {
	if variant == nil {
		return nil, nil
	}
	rows, err := q.ListLines(ctx, store.ListLinesParams{DocumentID: docID, VariantID: variant})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (s *Service) classify(ctx context.Context, q *store.Queries, doc store.LockDocumentRow, in NewScan) (verdict, error) {
	scan, ok := catalog.ParseScan(in.Raw)
	if !ok {
		return verdict{status: ScanUnknown, reason: "Код не распознан"}, nil
	}
	kind := string(scan.Kind)
	v := verdict{codeKind: &kind}
	if scan.Mark != "" {
		v.mark = &scan.Mark
		return s.classifyMark(ctx, q, doc, scan, in.VariantID, v)
	}

	id, err := s.catalog.FindVariant(ctx, scan)
	if err != nil {
		return v, err
	}
	if id == nil {
		return reject(v, ScanUnknown, "Товара с таким кодом нет в каталоге"), nil
	}
	v.variant = id
	info, err := q.VariantInfo(ctx, *id)
	if err != nil {
		return v, err
	}
	if !info.IsActive {
		return reject(v, ScanRejected, "Вариант в архиве"), nil
	}
	if marked(info.EgaisAccounting) {
		if doc.Kind == Opening {
			return reject(v, ScanSelected, "Теперь отсканируйте марки этих бутылок"), nil
		}
		return reject(v, ScanRejected, "Товар учитывается помарочно — отсканируйте акцизную марку"), nil
	}
	if scan.Unit != "" {
		// Код «Честного знака» — одна единица товара: второй скан той же пачки не засчитывается.
		v.crpt = &scan.Unit
		dup, err := q.CodeScanned(ctx, store.CodeScannedParams{DocumentID: doc.ID, CrptCode: v.crpt})
		if err != nil {
			return v, err
		}
		if dup {
			v.crpt = nil
			return reject(v, ScanDuplicate, "Этот код уже отсканирован"), nil
		}
	}
	v.status = ScanAccepted
	return v, nil
}

func (s *Service) classifyMark(ctx context.Context, q *store.Queries, doc store.LockDocumentRow, scan catalog.Scan, hint *uuid.UUID, v verdict) (verdict, error) {
	dup, err := q.CodeScanned(ctx, store.CodeScannedParams{DocumentID: doc.ID, MarkCode: v.mark})
	if err != nil {
		return v, err
	}
	if dup {
		v.mark = nil
		return reject(v, ScanDuplicate, "Эта марка уже отсканирована"), nil
	}
	existing, err := s.marks.Find(ctx, scan.Mark)
	if err != nil {
		return v, err
	}

	switch doc.Kind {
	case Opening:
		if existing != nil {
			v.variant = &existing.VariantID
			v.mark = nil
			return reject(v, ScanRejected, "Марка уже учтена: "+markPlace(existing)), nil
		}
		variant := hint
		if scan.Kind == apispec.ScanKindEgaisMark {
			if variant, err = s.catalog.FindVariant(ctx, scan); err != nil {
				return v, err
			}
			if variant == nil {
				v.mark = nil
				return reject(v, ScanUnknown, "Алкокода из марки нет в каталоге: "+scan.Code), nil
			}
		} else if variant == nil {
			v.mark = nil
			return reject(v, ScanRejected, "В марке нового образца нет кода продукции — сначала отсканируйте штрихкод бутылки"), nil
		}
		info, err := q.VariantInfo(ctx, *variant)
		if errors.Is(err, pgx.ErrNoRows) {
			return reject(v, ScanRejected, "Выбранного товара нет в каталоге"), nil
		}
		if err != nil {
			return v, err
		}
		v.variant = variant
		if !marked(info.EgaisAccounting) {
			v.mark = nil
			return reject(v, ScanRejected, "«"+info.Name+"» учитывается без марок"), nil
		}
		if !info.IsActive {
			v.mark = nil
			return reject(v, ScanRejected, "Вариант в архиве"), nil
		}

	case Writeoff:
		if existing == nil {
			v.mark = nil
			return reject(v, ScanForeign, "Марки нет в учёте точки"), nil
		}
		v.variant = &existing.VariantID
		// Недостача перемещения списывается из «в пути», остальное — из зоны документа.
		if doc.TransferID != nil {
			if existing.Status != marks.InTransit || existing.LocationID != doc.LocationID {
				v.mark = nil
				return reject(v, ScanForeign, "Марка не числится «в пути» в точке: "+markPlace(existing)), nil
			}
			break
		}
		if existing.Status != marks.InStock || existing.ZoneID == nil || *existing.ZoneID != doc.ZoneID {
			zone, err := q.GetZone(ctx, doc.ZoneID)
			if err != nil {
				return v, err
			}
			v.mark = nil
			return reject(v, ScanForeign, "Марка не числится в зоне «"+zoneNames[zone.Kind]+"»: "+markPlace(existing)), nil
		}

	case FloorTransfer, Transfer:
		if existing == nil {
			v.mark = nil
			return reject(v, ScanForeign, "Марки нет в учёте точки"), nil
		}
		v.variant = &existing.VariantID
		if existing.Status != marks.InStock || existing.ZoneID == nil || *existing.ZoneID != doc.ZoneID {
			zone, err := q.GetZone(ctx, doc.ZoneID)
			if err != nil {
				return v, err
			}
			v.mark = nil
			return reject(v, ScanForeign, "Марка не числится в зоне «"+zoneNames[zone.Kind]+"»: "+markPlace(existing)), nil
		}
	}
	v.status = ScanAccepted
	return v, nil
}

var markStatuses = map[string]string{
	marks.InStock: "на остатке", marks.InTransit: "в пути", marks.Reserved: "отложена для покупателя",
	marks.Sold: "продана", marks.WrittenOff: "списана", marks.Returned: "возвращена поставщику",
}

func markPlace(m *marks.Mark) string {
	return markStatuses[m.Status]
}
