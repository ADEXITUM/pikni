package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/documents/store"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

var (
	ErrSameLocation = errors.New("sender and receiver are the same location")
	ErrNoStorage    = errors.New("location has no storage zone")
	ErrNotRequested = errors.New("transfer is not awaiting approval")
	ErrNoFSRAR      = errors.New("location has no FSRAR ID")
)

type TransferLine struct {
	VariantID uuid.UUID
	Qty       int32
}

// CreateTransfer — заявка точки-получателя на перемещение из другой точки (СО-4, НЛ-3): что,
// сколько, откуда и почему. Отправитель её согласует или отклонит.
func (s *Service) CreateTransfer(ctx context.Context, from, to uuid.UUID, lines []TransferLine, comment string) (uuid.UUID, error) {
	if from == to {
		return uuid.Nil, ErrSameLocation
	}
	if err := auth.CheckLocation(ctx, apispec.PermissionTransferRequest, to); err != nil {
		return uuid.Nil, err
	}
	if len(lines) == 0 {
		return uuid.Nil, ErrEmpty
	}
	src, err := s.q.StorageZone(ctx, from)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoStorage
	}
	if err != nil {
		return uuid.Nil, err
	}
	dst, err := s.q.StorageZone(ctx, to)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoStorage
	}
	if err != nil {
		return uuid.Nil, err
	}
	if !src.LocationActive || !dst.LocationActive {
		return uuid.Nil, ErrInactiveLocation
	}
	var id uuid.UUID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		requested := StatusRequested
		id, err = create(ctx, tx, Transfer, dst.LocalDate, store.CreateDocumentParams{
			LocationID: from, ZoneID: src.ID, DestZoneID: &dst.ID, DestLocationID: &to,
			Comment: strings.TrimSpace(comment), Status: &requested,
		})
		if err != nil {
			return err
		}
		seen := map[uuid.UUID]bool{}
		for i, l := range lines {
			if l.Qty <= 0 || seen[l.VariantID] {
				return ErrEmpty
			}
			seen[l.VariantID] = true
			info, err := q.VariantInfo(ctx, l.VariantID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownVariant
			}
			if err != nil {
				return err
			}
			if !info.IsActive {
				return ErrInactiveVariant
			}
			qty := l.Qty
			if err := q.InsertRequestLine(ctx, store.InsertRequestLineParams{
				DocumentID: id, LineNo: int32(i + 1), VariantID: l.VariantID, QtyPlan: &qty,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// DecideTransfer — отправитель согласует заявку (начинается сборка) или отклоняет с причиной.
func (s *Service) DecideTransfer(ctx context.Context, id uuid.UUID, version int32, approve bool, reason string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := q.LockDocument(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if doc.Kind != Transfer || !canView(ctx, doc.Kind, doc.LocationID, doc.DestLocationID) {
			return ErrNotFound
		}
		if err := auth.CheckLocation(ctx, apispec.PermissionTransferEdit, doc.LocationID); err != nil {
			return err
		}
		if doc.Status != StatusRequested {
			return ErrNotRequested
		}
		if doc.Version != version {
			return ErrConflict
		}
		status := StatusDraft
		if !approve {
			status = StatusRejected
			if reason = strings.TrimSpace(reason); reason != "" {
				comment := strings.TrimSpace(doc.Comment + "\nОтклонено: " + reason)
				if err := q.SetComment(ctx, store.SetCommentParams{ID: id, Comment: comment}); err != nil {
					return err
				}
			}
		}
		if err := q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: id, Status: status}); err != nil {
			return err
		}
		action := apispec.AuditActionTransferApproved
		if !approve {
			action = apispec.AuditActionTransferRejected
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: action, EntityType: audit.EntityDocument, EntityID: id.String(),
			Data: map[string]any{"number": doc.Number, "reason": reason},
		})
	})
}

// shipTransfer проводит отгрузку: собранное списывается со склада отправителя и встаёт «в пути»
// у получателя, марки уходят в путь, в ЕГАИС — ТТН. Себестоимость — средняя отправителя.
func (s *Service) shipTransfer(ctx context.Context, tx pgx.Tx, q *store.Queries, doc store.LockDocumentRow,
	lines []store.ListLinesRow, docMarks []store.DocumentMarksRow, ev marks.Event) (map[uuid.UUID]int64, error) {
	dest, destZone := *doc.DestLocationID, *doc.DestZoneID
	src, err := q.StorageZone(ctx, doc.LocationID)
	if err != nil {
		return nil, err
	}
	dst, err := q.StorageZone(ctx, dest)
	if err != nil {
		return nil, err
	}
	if src.FsrarID == nil || dst.FsrarID == nil {
		return nil, ErrNoFSRAR
	}
	var moves []stock.Move
	for _, l := range lines {
		if l.Qty == 0 {
			continue
		}
		moves = append(moves,
			stock.Move{LineID: l.ID, VariantID: l.VariantID, LocationID: doc.LocationID, ZoneID: doc.ZoneID, Qty: -l.Qty},
			stock.Move{VariantID: l.VariantID, LocationID: dest, ZoneID: destZone, Qty: l.Qty, InTransit: true},
		)
	}
	if len(moves) == 0 {
		return nil, ErrEmpty
	}
	byVariant := map[uuid.UUID][]string{}
	var codes []string
	for _, m := range docMarks {
		byVariant[*m.VariantID] = append(byVariant[*m.VariantID], m.Code)
		codes = append(codes, m.Code)
	}
	if len(codes) > 0 {
		if err := marks.Ship(ctx, tx, ev, doc.ZoneID, dest, codes); err != nil {
			return nil, err
		}
	}
	costs, err := stock.Apply(ctx, tx, stock.Doc{ID: doc.ID, Date: doc.DocDate}, moves)
	if se, ok := errors.AsType[*stock.ShortageError](err); ok {
		return nil, s.describeShortage(ctx, q, lines, se)
	}
	if err != nil {
		return nil, err
	}
	ttn := egais.TTN{
		Number: doc.Number, Date: doc.DocDate.Format("2006-01-02"),
		Shipper: egais.Party{FSRARID: *src.FsrarID, Name: src.Name}, Consignee: *dst.FsrarID,
	}
	identities := map[uuid.UUID]int32{}
	for _, l := range lines {
		if l.Qty == 0 || l.Alcocode == nil {
			continue
		}
		ttn.Positions = append(ttn.Positions, egais.TTNPosition{
			Identity: len(ttn.Positions) + 1, Alcocode: *l.Alcocode, Name: l.Name, Qty: int(l.Qty),
			Price: float64(costs[l.ID]) / 10000, Marked: marked(l.EgaisAccounting), Marks: byVariant[l.VariantID],
		})
		identities[l.VariantID] = int32(len(ttn.Positions))
	}
	if err := s.createTransferReceipt(ctx, q, doc, dst.LocalDate, lines, costs, identities, byVariant); err != nil {
		return nil, err
	}
	// Без алкоголя ТТН в ЕГАИС не нужна: перемещение сразу «в пути», приёмка — у получателя.
	if len(ttn.Positions) > 0 {
		if err := egais.EnqueuePayload(ctx, tx, doc.LocationID, egais.WayBill, doc.ID, ttn); err != nil {
			return nil, err
		}
	}
	return costs, nil
}

// createTransferReceipt заводит у получателя поступление по отгруженному перемещению: план —
// отгруженное, ожидаемые марки — отгруженные. Принимается поштучно. Когда ТТН дойдёт из ЕГАИС,
// она привяжется к этому поступлению (у алкоголя без неё акт не отправить).
func (s *Service) createTransferReceipt(ctx context.Context, q *store.Queries, doc store.LockDocumentRow, date time.Time,
	lines []store.ListLinesRow, costs map[uuid.UUID]int64, identities map[uuid.UUID]int32, byVariant map[uuid.UUID][]string) error {
	spec := kinds[Receipt]
	n, err := q.NextDocNumber(ctx, store.NextDocNumberParams{Prefix: spec.prefix, Year: int32(date.Year())})
	if err != nil {
		return err
	}
	mode := ModePiece
	id, err := q.CreateReceipt(ctx, store.CreateReceiptParams{
		Number: fmt.Sprintf("%s-%d-%06d", spec.prefix, date.Year(), n), DocDate: date,
		LocationID: *doc.DestLocationID, ZoneID: *doc.DestZoneID, ReceivingMode: &mode,
		TtnNumber: &doc.Number, TtnDate: &doc.DocDate, Unmapped: []byte("[]"),
	})
	if err != nil {
		return err
	}
	if err := q.SetTransfer(ctx, store.SetTransferParams{ID: id, TransferID: &doc.ID}); err != nil {
		return err
	}
	var tm store.InsertTTNMarksParams
	tm.DocumentID = id
	for i, l := range lines {
		if l.Qty == 0 {
			continue
		}
		qty := l.Qty
		var identity *int32
		if v, ok := identities[l.VariantID]; ok {
			identity = &v
		}
		if err := q.InsertPlanLine(ctx, store.InsertPlanLineParams{
			DocumentID: id, LineNo: int32(i + 1), VariantID: l.VariantID, QtyPlan: &qty,
			PriceKopecks: costs[l.ID] / 100, TtnIdentity: identity,
		}); err != nil {
			return err
		}
		for _, m := range byVariant[l.VariantID] {
			tm.Codes = append(tm.Codes, m)
			tm.VariantIds = append(tm.VariantIds, l.VariantID)
			tm.PackageCodes = append(tm.PackageCodes, "")
		}
	}
	if len(tm.Codes) > 0 {
		return q.InsertTTNMarks(ctx, tm)
	}
	return nil
}

var ErrNotRejected = errors.New("transfer was not rejected by EGAIS")

// lockRejected — перемещение, ТТН которого отклонила ЕГАИС; решает точка-отправитель.
func (s *Service) lockRejected(ctx context.Context, q *store.Queries, id uuid.UUID, version int32) (store.LockDocumentRow, error) {
	doc, err := q.LockDocument(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return doc, ErrNotFound
	}
	if err != nil {
		return doc, err
	}
	if doc.Kind != Transfer || !canView(ctx, doc.Kind, doc.LocationID, doc.DestLocationID) {
		return doc, ErrNotFound
	}
	if err := auth.CheckLocation(ctx, apispec.PermissionTransferEdit, doc.LocationID); err != nil {
		return doc, err
	}
	if doc.Status != StatusEgaisRejected {
		return doc, ErrNotRejected
	}
	if doc.Version != version {
		return doc, ErrConflict
	}
	return doc, nil
}

// ResendTransfer отправляет ТТН повторно — например, после того как в имитаторе или у получателя
// исправили причину отказа. Учёт не меняется: товар и так «в пути».
func (s *Service) ResendTransfer(ctx context.Context, id uuid.UUID, version int32) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := s.lockRejected(ctx, q, id, version)
		if err != nil {
			return err
		}
		var payload []byte
		if err := tx.QueryRow(ctx, `select payload from egais_messages where document_id = $1 and direction = 'out'
			and doc_type = $2 order by created_at desc limit 1`, id, egais.WayBill).Scan(&payload); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into egais_messages (location_id, direction, doc_type, document_id, payload)
			values ($1, 'out', $2, $3, $4)`, doc.LocationID, egais.WayBill, id, payload); err != nil {
			return err
		}
		if err := q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: id, Status: StatusInTransit}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{Action: apispec.AuditActionTransferResent, EntityType: audit.EntityDocument,
			EntityID: id.String(), Data: map[string]any{"number": doc.Number}})
	})
}

// ReturnTransfer отменяет отклонённое перемещение: товар из «в пути» получателя возвращается в зону
// отправителя, марки — на его остаток, поступление у получателя отменяется. В ЕГАИС ничего не
// уходит: отклонённая ТТН баланс не меняла.
func (s *Service) ReturnTransfer(ctx context.Context, id uuid.UUID, version int32) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := s.lockRejected(ctx, q, id, version)
		if err != nil {
			return err
		}
		lines, err := q.ListLines(ctx, store.ListLinesParams{DocumentID: id})
		if err != nil {
			return err
		}
		dest, destZone := *doc.DestLocationID, *doc.DestZoneID
		var moves []stock.Move
		for _, l := range lines {
			if l.Qty == 0 {
				continue
			}
			moves = append(moves,
				stock.Move{VariantID: l.VariantID, LocationID: dest, ZoneID: destZone, Qty: -l.Qty, InTransit: true},
				stock.Move{VariantID: l.VariantID, LocationID: doc.LocationID, ZoneID: doc.ZoneID, Qty: l.Qty})
		}
		docMarks, err := q.DocumentMarks(ctx, id)
		if err != nil {
			return err
		}
		codes := make([]string, len(docMarks))
		for i, m := range docMarks {
			codes[i] = m.Code
		}
		ev := marks.Event{DocumentID: id, UserID: userID(ctx)}
		if len(codes) > 0 {
			if err := marks.Unship(ctx, tx, ev, dest, doc.LocationID, doc.ZoneID, codes); err != nil {
				return err
			}
		}
		if _, err := stock.Apply(ctx, tx, stock.Doc{ID: id, Date: doc.DocDate}, moves); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update documents set status = 'cancelled', version = version + 1
			where transfer_id = $1 and kind = 'receipt' and status = 'draft'`, id); err != nil {
			return err
		}
		if err := q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: id, Status: StatusCancelled}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{Action: apispec.AuditActionTransferReturned, EntityType: audit.EntityDocument,
			EntityID: id.String(), Data: map[string]any{"number": doc.Number}})
	})
}
