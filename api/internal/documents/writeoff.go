package documents

import (
	"context"
	"errors"
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
	marksstore "github.com/ADEXITUM/pikni/api/internal/marks/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

const (
	Writeoff = "writeoff"
	Storno   = "storno"
)

var (
	ErrUnknownReason   = errors.New("unknown or archived write-off reason")
	ErrPhotoRequired   = errors.New("photo is required for this reason")
	ErrNotPending      = errors.New("document is not awaiting approval")
	ErrSameApprover    = errors.New("approver must be another user")
	ErrNoTransitLeft   = errors.New("nothing left in transit for the transfer")
	ErrStornoForbidden = errors.New("document cannot be reversed")
	ErrBadPhoto        = errors.New("photo must be JPEG, PNG or WebP up to 3 MB")
)

// CreateWriteoff создаёт списание из зоны (СО-5) или, если указано перемещение, — недостачи,
// которая осталась «в пути» после приёмки: строки и марки подставляются сами.
func (s *Service) CreateWriteoff(ctx context.Context, zoneID *uuid.UUID, reasonID uuid.UUID, comment string, transferID *uuid.UUID) (uuid.UUID, error) {
	reason, err := s.q.GetWriteoffReason(ctx, reasonID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !reason.IsActive {
		return uuid.Nil, ErrUnknownReason
	}
	if err != nil {
		return uuid.Nil, err
	}
	var zone uuid.UUID
	var date time.Time
	if transferID != nil {
		tr, err := s.q.GetDocument(ctx, *transferID)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && tr.Kind != Transfer {
			return uuid.Nil, ErrNotFound
		}
		if err != nil {
			return uuid.Nil, err
		}
		zone = *tr.DestZoneID
	} else {
		if zoneID == nil {
			return uuid.Nil, ErrNotFound
		}
		zone = *zoneID
	}
	z, err := s.q.GetZone(ctx, zone)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	location, date := z.LocationID, z.LocalDate
	if err := auth.CheckLocation(ctx, apispec.PermissionWriteoffCreate, location); err != nil {
		return uuid.Nil, err
	}
	if !z.LocationActive {
		return uuid.Nil, ErrInactiveLocation
	}
	var id uuid.UUID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		id, err = create(ctx, tx, Writeoff, date, store.CreateDocumentParams{
			LocationID: location, ZoneID: zone, Comment: strings.TrimSpace(comment), ReasonID: &reasonID, TransferID: transferID,
		})
		if err != nil || transferID == nil {
			return err
		}
		left, err := q.TransitLeft(ctx, transferID)
		if err != nil {
			return err
		}
		if len(left) == 0 {
			return ErrNoTransitLeft
		}
		transit, err := q.TransitMarks(ctx, transferID)
		if err != nil {
			return err
		}
		markOf := map[uuid.UUID][]string{}
		for _, m := range transit {
			markOf[m.VariantID] = append(markOf[m.VariantID], m.Code)
		}
		for _, l := range left {
			codes := markOf[l.VariantID]
			if len(codes) > 0 {
				// Марки записываются сканами документа — так же, как если бы их отсканировали.
				for _, c := range codes {
					code := c
					variant := l.VariantID
					if err := q.InsertScan(ctx, store.InsertScanParams{
						ID: uuid.New(), DocumentID: id, UserID: userID(ctx), Source: "manual", Raw: code,
						VariantID: &variant, MarkCode: &code, Qty: 1, Status: ScanAccepted, ScannedAt: time.Now(),
					}); err != nil {
						return err
					}
				}
			}
			if _, err := q.AddToLine(ctx, store.AddToLineParams{DocumentID: id, VariantID: l.VariantID, Qty: l.Qty}); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// CreateStorno создаёт сторно проведённого документа (ПБ-8). Сторнируются документы без следа
// в ЕГАИС: ввод остатков и передача в зал без пива. Остальное исправляется ответным документом —
// возвратом, перемещением, списанием.
func (s *Service) CreateStorno(ctx context.Context, original uuid.UUID, comment string) (uuid.UUID, error) {
	doc, err := s.q.GetDocument(ctx, original)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if !canView(ctx, doc.Kind, doc.LocationID, doc.DestLocationID) {
		return uuid.Nil, ErrNotFound
	}
	if err := auth.CheckLocation(ctx, kinds[doc.Kind].post, doc.LocationID); err != nil {
		return uuid.Nil, err
	}
	if err := s.stornoAllowed(ctx, s.q, doc.Kind, doc.Status, original); err != nil {
		return uuid.Nil, err
	}
	lines, err := s.q.ListLines(ctx, store.ListLinesParams{DocumentID: original})
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		id, err = create(ctx, tx, Storno, doc.DocDate, store.CreateDocumentParams{
			LocationID: doc.LocationID, ZoneID: doc.ZoneID, Comment: strings.TrimSpace(comment), ReversesID: &original,
		})
		if db.IsUniqueViolationOn(err, "documents_reverses_key") {
			return ErrStornoForbidden
		}
		if err != nil {
			return err
		}
		for _, l := range lines {
			if _, err := q.AddToLine(ctx, store.AddToLineParams{DocumentID: id, VariantID: l.VariantID, Qty: l.Qty}); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

func (s *Service) stornoAllowed(ctx context.Context, q *store.Queries, kind, status string, id uuid.UUID) error {
	if (kind != Opening && kind != FloorTransfer) || status != StatusPosted {
		return ErrStornoForbidden
	}
	exchanged, err := q.DocumentHasExchange(ctx, &id)
	if err != nil {
		return err
	}
	if exchanged {
		return ErrStornoForbidden
	}
	return nil
}

// submit — списание и сторно не проводятся сразу, а уходят на утверждение (ПБ-8).
func (s *Service) submit(ctx context.Context, q *store.Queries, doc store.LockDocumentRow) error {
	if doc.Kind == Writeoff {
		reason, err := q.GetWriteoffReason(ctx, *doc.ReasonID)
		if err != nil {
			return err
		}
		if reason.PhotoRequired {
			photos, err := q.ListPhotos(ctx, doc.ID)
			if err != nil {
				return err
			}
			if len(photos) == 0 {
				return ErrPhotoRequired
			}
		}
	}
	return nil
}

// Decide — утверждение или возврат документа другим пользователем с правом утверждения (ПБ-8).
// Утверждённое списание проводится: остаток уменьшается, марки списываются, в ЕГАИС уходит акт.
// Утверждённое сторно отменяет движения исходного документа. Возвращённый документ снова черновик.
func (s *Service) Decide(ctx context.Context, id uuid.UUID, version int32, approve bool, comment string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := q.LockDocument(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !canView(ctx, doc.Kind, doc.LocationID, doc.DestLocationID) && auth.CheckLocation(ctx, apispec.PermissionDocsApprove, doc.LocationID) != nil {
			return ErrNotFound
		}
		if err := auth.CheckLocation(ctx, apispec.PermissionDocsApprove, doc.LocationID); err != nil {
			return err
		}
		if doc.Status != StatusPendingApproval {
			return ErrNotPending
		}
		if doc.Version != version {
			return ErrConflict
		}
		me := userID(ctx)
		submitter, err := q.Submitter(ctx, id)
		if err != nil {
			return err
		}
		if doc.AuthorID != nil && *doc.AuthorID == me || submitter != nil && *submitter == me {
			return ErrSameApprover
		}
		decision := "approved"
		if !approve {
			decision = "rejected"
		}
		if err := q.InsertApproval(ctx, store.InsertApprovalParams{
			DocumentID: id, UserID: me, Decision: decision, Comment: strings.TrimSpace(comment),
		}); err != nil {
			return err
		}
		action := apispec.AuditActionDocumentApproved
		status := StatusDraft
		if approve {
			ev := marks.Event{DocumentID: id, UserID: me}
			if status, err = s.apply(ctx, tx, q, doc, ev); err != nil {
				return err
			}
		} else {
			action = apispec.AuditActionDocumentReturned
		}
		if err := q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: id, Status: status}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: action, EntityType: audit.EntityDocument, EntityID: id.String(),
			Data: map[string]any{"number": doc.Number, "kind": doc.Kind, "comment": strings.TrimSpace(comment)},
		})
	})
}

func (s *Service) apply(ctx context.Context, tx pgx.Tx, q *store.Queries, doc store.LockDocumentRow, ev marks.Event) (string, error) {
	lines, err := q.ListLines(ctx, store.ListLinesParams{DocumentID: doc.ID})
	if err != nil {
		return "", err
	}
	if doc.Kind == Storno {
		return s.applyStorno(ctx, tx, q, doc, ev)
	}
	if doc.Kind == Stocktake {
		return s.applyStocktake(ctx, tx, q, doc, ev)
	}
	docMarks, err := q.DocumentMarks(ctx, doc.ID)
	if err != nil {
		return "", err
	}
	transit := doc.TransferID != nil
	var moves []stock.Move
	for _, l := range lines {
		if l.Qty > 0 {
			moves = append(moves, stock.Move{
				LineID: l.ID, VariantID: l.VariantID, LocationID: doc.LocationID, ZoneID: doc.ZoneID,
				Qty: -l.Qty, External: true, InTransit: transit,
			})
		}
	}
	if len(moves) == 0 {
		return "", ErrEmpty
	}
	byVariant := map[uuid.UUID][]string{}
	var codes []string
	for _, m := range docMarks {
		byVariant[*m.VariantID] = append(byVariant[*m.VariantID], m.Code)
		codes = append(codes, m.Code)
	}
	if len(codes) > 0 {
		var zone *uuid.UUID
		if !transit {
			zone = &doc.ZoneID
		}
		if err := marks.WriteOff(ctx, tx, ev, doc.LocationID, zone, codes); err != nil {
			return "", err
		}
	}
	costs, err := stock.Apply(ctx, tx, stock.Doc{ID: doc.ID, Date: doc.DocDate}, moves)
	if se, ok := errors.AsType[*stock.ShortageError](err); ok {
		return "", s.describeShortage(ctx, q, lines, se)
	}
	if err != nil {
		return "", err
	}
	set := store.SetLineCostsParams{}
	for _, l := range lines {
		set.Ids = append(set.Ids, l.ID)
		set.CostsE4 = append(set.CostsE4, costs[l.ID])
	}
	if err := q.SetLineCosts(ctx, set); err != nil {
		return "", err
	}

	var items []egais.Item
	for _, l := range lines {
		if l.Qty > 0 && l.Alcocode != nil {
			items = append(items, egais.Item{VariantID: l.VariantID, Alcocode: l.Alcocode, Qty: l.Qty, Marks: byVariant[l.VariantID]})
		}
	}
	if len(items) == 0 {
		return StatusPosted, nil
	}
	reason, err := q.GetWriteoffReason(ctx, *doc.ReasonID)
	if err != nil {
		return "", err
	}
	// Акт уходит от того, на чьём балансе продукция в ЕГАИС: недостачу перемещения ЕГАИС вернула
	// отправителю. Из торгового зала — актом списания из зала (регистр 2).
	docType, from := egais.ActWriteOff, doc.LocationID
	if transit {
		tr, err := q.GetDocument(ctx, *doc.TransferID)
		if err != nil {
			return "", err
		}
		from = tr.LocationID
	} else if zone, err := q.GetZone(ctx, doc.ZoneID); err != nil {
		return "", err
	} else if zone.Kind == "floor" {
		docType = egais.ActWriteOffShop
	}
	if err := egais.EnqueuePayload(ctx, tx, from, docType, doc.ID, map[string]any{"reason": reason.EgaisType, "items": items}); err != nil {
		return "", err
	}
	return StatusAwaitingEgais, nil
}

// applyStorno отменяет движения исходного документа обратными и возвращает марки на прежнее место.
func (s *Service) applyStorno(ctx context.Context, tx pgx.Tx, q *store.Queries, doc store.LockDocumentRow, ev marks.Event) (string, error) {
	orig, err := q.LockDocument(ctx, *doc.ReversesID)
	if err != nil {
		return "", err
	}
	if err := s.stornoAllowed(ctx, q, orig.Kind, orig.Status, orig.ID); err != nil {
		return "", err
	}
	moved, err := q.DocumentMoves(ctx, orig.ID)
	if err != nil {
		return "", err
	}
	var moves []stock.Move
	for _, m := range moved {
		if m.Bucket == "deficit" {
			continue
		}
		moves = append(moves, stock.Move{
			VariantID: m.VariantID, LocationID: m.LocationID, ZoneID: m.ZoneID, Qty: -m.Qty,
			External: orig.Kind == Opening, InTransit: m.Bucket == "in_transit",
		})
	}
	origMarks, err := q.DocumentMarks(ctx, orig.ID)
	if err != nil {
		return "", err
	}
	codes := make([]string, len(origMarks))
	for i, m := range origMarks {
		codes[i] = m.Code
	}
	if len(codes) > 0 {
		switch orig.Kind {
		case Opening:
			err = marks.Unregister(ctx, tx, ev, orig.ZoneID, codes)
		case FloorTransfer:
			err = marks.MoveWithin(ctx, tx, ev, *orig.DestZoneID, orig.LocationID, orig.ZoneID, codes)
		}
		if err != nil {
			return "", err
		}
	}
	if _, err := stock.Apply(ctx, tx, stock.Doc{ID: doc.ID, Date: doc.DocDate}, moves); err != nil {
		if se, ok := errors.AsType[*stock.ShortageError](err); ok {
			lines, _ := q.ListLines(ctx, store.ListLinesParams{DocumentID: orig.ID})
			return "", s.describeShortage(ctx, q, lines, se)
		}
		return "", err
	}
	if err := q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: orig.ID, Status: StatusReversed}); err != nil {
		return "", err
	}
	return StatusPosted, nil
}

// AddPhoto прикрепляет фотографию к черновику списания (бой).
func (s *Service) AddPhoto(ctx context.Context, id uuid.UUID, contentType string, data []byte) (uuid.UUID, error) {
	if len(data) == 0 || len(data) > 3<<20 || (contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp") {
		return uuid.Nil, ErrBadPhoto
	}
	var photo uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := lockDraft(ctx, q, id, editPerm)
		if err != nil {
			return err
		}
		if doc.Kind != Writeoff {
			return ErrBadPhoto
		}
		row, err := q.InsertPhoto(ctx, store.InsertPhotoParams{DocumentID: id, ContentType: contentType, Data: data, CreatedBy: userID(ctx)})
		photo = row.ID
		return err
	})
	return photo, err
}

func (s *Service) DeletePhoto(ctx context.Context, id, photo uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := lockDraft(ctx, q, id, editPerm); err != nil {
			return err
		}
		n, err := q.DeletePhoto(ctx, store.DeletePhotoParams{ID: photo, DocumentID: id})
		if err == nil && n == 0 {
			return ErrNotFound
		}
		return err
	})
}

func (s *Service) Photos(ctx context.Context, id uuid.UUID) ([]store.ListPhotosRow, error) {
	return s.q.ListPhotos(ctx, id)
}

// Photo отдаёт фотографию тому, кто видит документ.
func (s *Service) Photo(ctx context.Context, photo uuid.UUID) (string, []byte, error) {
	p, err := s.q.GetPhoto(ctx, photo)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	if !canView(ctx, p.Kind, p.LocationID, p.DestLocationID) && auth.CheckLocation(ctx, apispec.PermissionDocsApprove, p.LocationID) != nil {
		return "", nil, ErrNotFound
	}
	return p.ContentType, p.Data, nil
}

// DecideAny — решение по документу: заявку на перемещение согласует отправитель, списание и сторно
// утверждает руководитель.
func (s *Service) DecideAny(ctx context.Context, id uuid.UUID, version int32, approve bool, comment string) error {
	doc, err := s.q.GetDocument(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if doc.Kind == Transfer && doc.Status == StatusRequested {
		return s.DecideTransfer(ctx, id, version, approve, comment)
	}
	return s.Decide(ctx, id, version, approve, comment)
}

// ZoneMarks — марки зоны для выбора повреждённой при списании.
func (s *Service) ZoneMarks(ctx context.Context, zone uuid.UUID, variant *uuid.UUID) ([]marksstore.ZoneMarksRow, error) {
	z, err := s.q.GetZone(ctx, zone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := auth.CheckLocation(ctx, apispec.PermissionWriteoffCreate, z.LocationID); err != nil {
		return nil, err
	}
	return s.marks.ZoneMarks(ctx, zone, variant)
}
