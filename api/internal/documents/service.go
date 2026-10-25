// Package documents — общий жизненный цикл документов движения: нумерация, строки, сканы,
// проведение (СО-1). Остатки меняет модуль stock, марки — модуль marks, в одной транзакции.
package documents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/documents/store"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	egaisstore "github.com/ADEXITUM/pikni/api/internal/egais/store"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

const (
	Opening       = "opening"
	FloorTransfer = "floor_transfer"
	Transfer      = "transfer"

	StatusDraft         = "draft"
	StatusAwaitingEgais = "awaiting_egais"
	StatusPosted        = "posted"
	StatusEgaisRejected = "egais_rejected"
	StatusCancelled     = "cancelled"
	StatusRequested     = "requested"
	StatusRejected      = "rejected"
	StatusInTransit     = "in_transit"
)

var (
	ErrNotFound         = errors.New("document not found")
	ErrNotDraft         = errors.New("document is not a draft")
	ErrConflict         = errors.New("document was changed by another user")
	ErrEmpty            = errors.New("document has no lines")
	ErrOpeningExists    = errors.New("opening balance for the zone already exists")
	ErrNotStore         = errors.New("floor transfer is possible only in a store")
	ErrInactiveLocation = errors.New("location is archived")
	ErrMarkedQty        = errors.New("quantity of marked goods is counted by mark scans")
	ErrUnknownVariant   = errors.New("variant not found")
	ErrInactiveVariant  = errors.New("variant is archived")
	ErrForeignScan      = errors.New("scan belongs to another document")
)

// kindSpec — права на вид документа: просмотр (любое из прав), изменение черновика и проведение.
// Право проверяется в пределах точки документа, поэтому операции с документом общие для всех видов.
type kindSpec struct {
	prefix string
	view   []apispec.Permission
	edit   apispec.Permission
	post   apispec.Permission
}

var kinds = map[string]kindSpec{
	// Ввод начальных остатков — первая ревизия: считают управляющий и кладовщик, проводят
	// руководитель или бухгалтер, которые видят закупочные цены (п. 3.3.1 backend.md).
	Opening: {
		prefix: "ВО",
		view:   []apispec.Permission{apispec.PermissionStocktakeManage, apispec.PermissionOpeningPost},
		edit:   apispec.PermissionStocktakeManage,
		post:   apispec.PermissionOpeningPost,
	},
	Receipt: {
		prefix: "ПТ",
		view:   []apispec.Permission{apispec.PermissionReceiptView},
		edit:   apispec.PermissionReceiptEdit,
		post:   apispec.PermissionReceiptEdit,
	},
	// «Продажа» ведёт смена кассы: документ создаётся при открытии смены и проводится при закрытии.
	Sale: {
		prefix: "ПР",
		view:   []apispec.Permission{apispec.PermissionPosView},
		edit:   apispec.PermissionPosSell,
		post:   apispec.PermissionPosSell,
	},
	// Перемещение между точками: заявку видит и подаёт получатель, согласует, собирает и отгружает
	// отправитель. Принимает получатель — документом поступления.
	Transfer: {
		prefix: "ПМ",
		view:   []apispec.Permission{apispec.PermissionTransferView, apispec.PermissionTransferRequest},
		edit:   apispec.PermissionTransferEdit,
		post:   apispec.PermissionTransferEdit,
	},
	FloorTransfer: {
		prefix: "ПЗ",
		view:   []apispec.Permission{apispec.PermissionFloorView},
		edit:   apispec.PermissionFloorEdit,
		post:   apispec.PermissionFloorEdit,
	},
}

type Service struct {
	pool    *pgxpool.Pool
	q       *store.Queries
	catalog *catalog.Service
	marks   *marks.Service
}

func NewService(pool *pgxpool.Pool, cat *catalog.Service, mk *marks.Service) *Service {
	return &Service{pool: pool, q: store.New(pool), catalog: cat, marks: mk}
}

// canView — документ виден в своей точке; перемещение — и в точке-получателе.
func canView(ctx context.Context, kind string, location uuid.UUID, dest *uuid.UUID) bool {
	for _, p := range kinds[kind].view {
		if auth.CheckLocation(ctx, p, location) == nil || dest != nil && auth.CheckLocation(ctx, p, *dest) == nil {
			return true
		}
	}
	return false
}

func userID(ctx context.Context) uuid.UUID {
	sess, _ := auth.FromContext(ctx)
	return sess.UserID
}

func lockDraft(ctx context.Context, q *store.Queries, id uuid.UUID, perm func(kindSpec) apispec.Permission) (store.LockDocumentRow, error) {
	doc, err := q.LockDocument(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return doc, ErrNotFound
	}
	if err != nil {
		return doc, err
	}
	if !canView(ctx, doc.Kind, doc.LocationID, doc.DestLocationID) {
		return doc, ErrNotFound
	}
	if err := auth.CheckLocation(ctx, perm(kinds[doc.Kind]), doc.LocationID); err != nil {
		return doc, err
	}
	if doc.Status != StatusDraft {
		return doc, ErrNotDraft
	}
	return doc, nil
}

func editPerm(k kindSpec) apispec.Permission { return k.edit }
func postPerm(k kindSpec) apispec.Permission { return k.post }

func create(ctx context.Context, tx pgx.Tx, kind string, date time.Time, p store.CreateDocumentParams) (uuid.UUID, error) {
	q := store.New(tx)
	spec := kinds[kind]
	n, err := q.NextDocNumber(ctx, store.NextDocNumberParams{Prefix: spec.prefix, Year: int32(date.Year())})
	if err != nil {
		return uuid.Nil, err
	}
	if p.AuthorID == nil {
		author := userID(ctx)
		p.AuthorID = &author
	}
	p.Kind, p.DocDate = kind, date
	p.Number = fmt.Sprintf("%s-%d-%06d", spec.prefix, date.Year(), n)
	id, err := q.CreateDocument(ctx, p)
	if db.IsUniqueViolationOn(err, "documents_opening_zone") {
		return uuid.Nil, ErrOpeningExists
	}
	if err != nil {
		return uuid.Nil, err
	}
	return id, audit.Record(ctx, tx, audit.Event{
		Action: apispec.AuditActionDocumentCreated, EntityType: audit.EntityDocument, EntityID: id.String(),
		Data: map[string]any{"number": p.Number, "kind": kind},
	})
}

// CreateOpening создаёт ввод начальных остатков по зоне. Документ на зону один: повторный ввод
// исказил бы учёт, остатки потом исправляет только ревизия.
func (s *Service) CreateOpening(ctx context.Context, zoneID uuid.UUID, comment string) (uuid.UUID, error) {
	zone, err := s.q.GetZone(ctx, zoneID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if err := auth.CheckLocation(ctx, apispec.PermissionStocktakeManage, zone.LocationID); err != nil {
		return uuid.Nil, err
	}
	if !zone.LocationActive {
		return uuid.Nil, ErrInactiveLocation
	}
	var id uuid.UUID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		id, err = create(ctx, tx, Opening, zone.LocalDate, store.CreateDocumentParams{
			LocationID: zone.LocationID, ZoneID: zone.ID, Comment: comment,
		})
		return err
	})
	return id, err
}

// CreateFloorTransfer создаёт передачу со склада магазина в торговый зал или обратно (СО-3).
func (s *Service) CreateFloorTransfer(ctx context.Context, locationID uuid.UUID, direction apispec.FloorDirection, comment string) (uuid.UUID, error) {
	zones, err := s.q.LocationZones(ctx, locationID)
	if err != nil {
		return uuid.Nil, err
	}
	if len(zones) == 0 {
		return uuid.Nil, ErrNotFound
	}
	if err := auth.CheckLocation(ctx, apispec.PermissionFloorEdit, locationID); err != nil {
		return uuid.Nil, err
	}
	var backroom, floor *store.LocationZonesRow
	for i, z := range zones {
		switch z.Kind {
		case "backroom":
			backroom = &zones[i]
		case "floor":
			floor = &zones[i]
		}
	}
	if backroom == nil || floor == nil {
		return uuid.Nil, ErrNotStore
	}
	if !floor.LocationActive {
		return uuid.Nil, ErrInactiveLocation
	}
	from, to := backroom.ID, floor.ID
	if direction == apispec.FloorDirectionFromFloor {
		from, to = to, from
	}
	var id uuid.UUID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		id, err = create(ctx, tx, FloorTransfer, floor.LocalDate, store.CreateDocumentParams{
			LocationID: locationID, ZoneID: from, DestZoneID: &to, Comment: comment,
		})
		return err
	})
	return id, err
}

// Detail — документ для экрана: шапка, строки, последние сканы и что можно делать текущему пользователю.
type Detail struct {
	Doc      store.GetDocumentRow
	Receipt  *ReceiptState
	Exchange []egaisstore.DocumentExchangeRow
	Lines    []store.ListLinesRow
	Scans    []store.ListScansRow
	CanEdit  bool
	CanPost  bool
	// CanApprove — заявку на перемещение можно согласовать или отклонить.
	CanApprove bool
	ShowCost   bool
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Detail, error) {
	var d Detail
	doc, err := s.q.GetDocument(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	if !canView(ctx, doc.Kind, doc.LocationID, doc.DestLocationID) {
		return d, ErrNotFound
	}
	d.Doc = doc
	if d.Lines, err = s.q.ListLines(ctx, store.ListLinesParams{DocumentID: id}); err != nil {
		return d, err
	}
	if d.Scans, err = s.q.ListScans(ctx, id); err != nil {
		return d, err
	}
	spec := kinds[doc.Kind]
	draft := doc.Status == StatusDraft
	d.CanEdit = draft && auth.CheckLocation(ctx, spec.edit, doc.LocationID) == nil
	d.CanPost = draft && auth.CheckLocation(ctx, spec.post, doc.LocationID) == nil
	d.CanApprove = doc.Kind == Transfer && doc.Status == StatusRequested &&
		auth.CheckLocation(ctx, apispec.PermissionTransferEdit, doc.LocationID) == nil
	d.ShowCost = auth.Has(ctx, apispec.PermissionCostView)
	// Факт приёмки считается по сканам, пока документ черновик; у проведённого он уже в строках.
	if doc.Kind == Receipt && doc.ReceivingMode != nil && doc.Status == StatusDraft {
		st, err := receiptState(ctx, s.q, id, *doc.ReceivingMode, doc.Unmapped)
		if err != nil {
			return d, err
		}
		d.Receipt = &st
	}
	if d.Exchange, err = egais.Exchange(ctx, s.pool, id); err != nil {
		return d, err
	}
	return d, nil
}

// ListFilter — отбор документов; видны только виды и точки, на которые у пользователя есть право.
type ListFilter struct {
	Kind, Status *string
	LocationID   *uuid.UUID
	After        *uuid.UUID
	Limit        int32
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]store.ListDocumentsRow, error) {
	params := store.ListDocumentsParams{
		Kind: f.Kind, Status: f.Status, LocationID: f.LocationID, After: f.After, MaxRows: f.Limit,
		KindsAll: []string{}, KindsOwn: []string{}, OwnLocations: []uuid.UUID{},
	}
	for kind, spec := range kinds {
		own := false
		for _, p := range spec.view {
			allowed, err := auth.AllowedLocations(ctx, p)
			if err != nil {
				continue
			}
			if allowed == nil {
				params.KindsAll = append(params.KindsAll, kind)
				own = false
				break
			}
			own = true
			params.OwnLocations = allowed
		}
		if own {
			params.KindsOwn = append(params.KindsOwn, kind)
		}
	}
	return s.q.ListDocuments(ctx, params)
}

// SetLine задаёт количество строки вручную; 0 убирает строку. Товар с марками считается только
// по сканам марок: иначе количество разошлось бы со списком марок.
func (s *Service) SetLine(ctx context.Context, id, variantID uuid.UUID, qty int32) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := lockDraft(ctx, q, id, editPerm); err != nil {
			return err
		}
		info, err := q.VariantInfo(ctx, variantID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownVariant
		}
		if err != nil {
			return err
		}
		if marked(info.EgaisAccounting) {
			return ErrMarkedQty
		}
		n, err := q.SetLineQty(ctx, store.SetLineQtyParams{DocumentID: id, VariantID: variantID, Qty: qty})
		if err != nil || n > 0 || qty == 0 {
			return err
		}
		if !info.IsActive {
			return ErrInactiveVariant
		}
		_, err = q.AddToLine(ctx, store.AddToLineParams{DocumentID: id, VariantID: variantID, Qty: qty})
		return err
	})
}

// Cancel отменяет черновик. Проведённый документ отменяется только сторнированием (итерация 8).
func (s *Service) Cancel(ctx context.Context, id uuid.UUID, version int32) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := lockDraft(ctx, q, id, editPerm)
		if err != nil {
			return err
		}
		if doc.Version != version {
			return ErrConflict
		}
		if err := q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: id, Status: StatusCancelled}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionDocumentCancelled, EntityType: audit.EntityDocument, EntityID: id.String(),
			Data: map[string]any{"number": doc.Number, "kind": doc.Kind},
		})
	})
}

func marked(accounting *string) bool {
	return accounting != nil && *accounting == "marked"
}

func variantIDs(lines []store.ListLinesRow) []uuid.UUID {
	ids := make([]uuid.UUID, len(lines))
	for i, l := range lines {
		ids[i] = l.VariantID
	}
	return slices.Compact(ids)
}
