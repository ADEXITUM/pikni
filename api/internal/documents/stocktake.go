package documents

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/documents/store"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

const (
	Stocktake         = "stocktake"
	StatusCounting    = "counting"
	StatusReconciling = "reconciling"
)

var (
	ErrStocktakeRunning = errors.New("another stocktake is running at the location")
	ErrZoneClosed       = errors.New("zone is closed or not in the stocktake")
	ErrZonesOpen        = errors.New("not all zones are closed")
	ErrNotCounting      = errors.New("stocktake is not counting")
	ErrNotReconciling   = errors.New("stocktake is not reconciling")
	ErrBadScope         = errors.New("scope zone belongs to another location")
)

// Scope — состав выборочной ревизии; пустой — вся точка (СО-6).
type Scope struct {
	Zones      []uuid.UUID `json:"zones,omitempty"`
	Categories []uuid.UUID `json:"categories,omitempty"`
	Brands     []uuid.UUID `json:"brands,omitempty"`
	// Strict — на время подсчёта остановить и продажи, а не только приход и перемещения.
	Strict bool `json:"strict,omitempty"`
}

func (s Scope) empty() bool { return len(s.Zones)+len(s.Categories)+len(s.Brands) == 0 && !s.Strict }

// CreateStocktake заводит ревизию точки: состав задаётся сразу, подсчёт начинается кнопкой «Старт».
func (s *Service) CreateStocktake(ctx context.Context, location uuid.UUID, scope Scope, comment string) (uuid.UUID, error) {
	if err := auth.CheckLocation(ctx, apispec.PermissionStocktakeManage, location); err != nil {
		return uuid.Nil, err
	}
	zones, err := s.q.LocationZones(ctx, location)
	if err != nil {
		return uuid.Nil, err
	}
	if len(zones) == 0 {
		return uuid.Nil, ErrNotFound
	}
	for _, z := range scope.Zones {
		if !slices.ContainsFunc(zones, func(r store.LocationZonesRow) bool { return r.ID == z }) {
			return uuid.Nil, ErrBadScope
		}
	}
	if !zones[0].LocationActive {
		return uuid.Nil, ErrInactiveLocation
	}
	storage, err := s.q.StorageZone(ctx, location)
	if err != nil {
		return uuid.Nil, err
	}
	var raw []byte
	if !scope.empty() {
		raw, _ = json.Marshal(scope)
	}
	var id uuid.UUID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		id, err = create(ctx, tx, Stocktake, zones[0].LocalDate, store.CreateDocumentParams{
			LocationID: location, ZoneID: storage.ID, Comment: strings.TrimSpace(comment), Scope: raw,
		})
		return err
	})
	return id, err
}

func (s *Service) lockStocktake(ctx context.Context, q *store.Queries, id uuid.UUID, perm apispec.Permission) (store.LockDocumentRow, error) {
	doc, err := q.LockDocument(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return doc, ErrNotFound
	}
	if err != nil {
		return doc, err
	}
	if doc.Kind != Stocktake || !canView(ctx, doc.Kind, doc.LocationID, nil) {
		return doc, ErrNotFound
	}
	return doc, auth.CheckLocation(ctx, perm, doc.LocationID)
}

// StartStocktake начинает подсчёт: учёт зон и марок фиксируется; приход, перемещения и списания
// товаров состава в точке блокируются до конца ревизии, продажи — только у строгой ревизии.
func (s *Service) StartStocktake(ctx context.Context, id uuid.UUID, version int32) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := s.lockStocktake(ctx, q, id, apispec.PermissionStocktakeManage)
		if err != nil {
			return err
		}
		if doc.Status != StatusDraft {
			return ErrNotDraft
		}
		if doc.Version != version {
			return ErrConflict
		}
		if _, err := q.ActiveStocktake(ctx, store.ActiveStocktakeParams{LocationID: doc.LocationID, ID: id}); err == nil {
			return ErrStocktakeRunning
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var scope Scope
		_ = json.Unmarshal(doc.Scope, &scope)
		zones := scope.Zones
		if len(zones) == 0 {
			all, err := q.LocationZones(ctx, doc.LocationID)
			if err != nil {
				return err
			}
			for _, z := range all {
				zones = append(zones, z.ID)
			}
		}
		if err := q.InsertStocktakeZones(ctx, store.InsertStocktakeZonesParams{DocumentID: id, ZoneIds: zones}); err != nil {
			return err
		}
		// Сначала статус: снимок учёта отбирает товары по блокировке этой ревизии.
		if err := q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: id, Status: StatusCounting}); err != nil {
			return err
		}
		// С этого момента продажи по товарам ревизии уменьшают её учёт.
		if _, err := tx.Exec(ctx, `update documents set counting_at = now() where id = $1`, id); err != nil {
			return err
		}
		if err := q.SnapshotBook(ctx, id); err != nil {
			return err
		}
		if err := q.SnapshotMarks(ctx, id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionStocktakeStarted, EntityType: audit.EntityDocument, EntityID: id.String(),
			Data: map[string]any{"number": doc.Number},
		})
	})
}

// StocktakeScan засчитывает скан в зону ревизии: марка — найдена в своей зоне, найдена в другой
// зоне точки или чужая; товар без марок — плюс одна единица.
func (s *Service) StocktakeScan(ctx context.Context, id, zone uuid.UUID, in NewScan) (ScanResult, error) {
	var res ScanResult
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := s.lockStocktake(ctx, q, id, apispec.PermissionStocktakeCount)
		if err != nil {
			return err
		}
		if doc.Status != StatusCounting {
			return ErrNotCounting
		}
		if prev, err := q.GetScan(ctx, in.ID); err == nil {
			if prev.DocumentID != id {
				return ErrForeignScan
			}
			res.Scan = prev
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if st, err := q.LockStocktakeZone(ctx, store.LockStocktakeZoneParams{DocumentID: id, ZoneID: zone}); errors.Is(err, pgx.ErrNoRows) || err == nil && st != "counting" {
			return ErrZoneClosed
		} else if err != nil {
			return err
		}
		v, kind, err := s.withTx(tx).classifyStocktake(ctx, q, doc, zone, in)
		if err != nil {
			return err
		}
		if v.status == ScanAccepted {
			if v.mark != nil {
				n, err := q.AddFoundMark(ctx, store.AddFoundMarkParams{DocumentID: id, ZoneID: zone, MarkCode: *v.mark, VariantID: v.variant, Kind: kind})
				if err != nil {
					return err
				}
				if n == 0 {
					v = reject(v, ScanDuplicate, "Эта марка уже отсканирована в ревизии")
					v.mark = nil
				}
			} else if err := q.AddCount(ctx, store.AddCountParams{DocumentID: id, ZoneID: zone, VariantID: *v.variant, Delta: 1}); err != nil {
				return err
			}
		}
		var reason *string
		if v.reason != "" {
			reason = &v.reason
		}
		if err := q.InsertScan(ctx, store.InsertScanParams{
			ID: in.ID, DocumentID: id, UserID: userID(ctx), Source: in.Source, Raw: in.Raw, CodeKind: v.codeKind,
			VariantID: v.variant, MarkCode: v.mark, Qty: 1, Status: v.status, Reason: reason, ScannedAt: in.ScannedAt, ZoneID: &zone,
		}); err != nil {
			return err
		}
		res.Scan, err = q.GetScan(ctx, in.ID)
		return err
	})
	return res, err
}

func (s *Service) classifyStocktake(ctx context.Context, q *store.Queries, doc store.LockDocumentRow, zone uuid.UUID, in NewScan) (verdict, string, error) {
	scan, ok := catalog.ParseScan(in.Raw)
	if !ok {
		return verdict{status: ScanUnknown, reason: "Код не распознан"}, "", nil
	}
	code := string(scan.Kind)
	v := verdict{codeKind: &code}
	inScope := func(variant uuid.UUID) (bool, error) {
		return q.InScope(ctx, store.InScopeParams{ID: doc.ID, ZoneID: zone, VariantID: variant})
	}
	if scan.Mark != "" {
		v.mark = &scan.Mark
		kind := "found"
		if bookZone, err := q.BookMarkZone(ctx, store.BookMarkZoneParams{DocumentID: doc.ID, MarkCode: scan.Mark}); err == nil {
			if bookZone != zone {
				kind = "moved"
			}
			m, err := s.marks.Find(ctx, scan.Mark)
			if err != nil {
				return v, "", err
			}
			v.variant = &m.VariantID
			v.status = ScanAccepted
			return v, kind, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return v, "", err
		}
		// Марки нет в учёте зон ревизии: чужая. Если её знает система (другая точка, продана),
		// она только попадёт в ведомость; незнакомая встанет на баланс после утверждения.
		existing, err := s.marks.Find(ctx, scan.Mark)
		if err != nil {
			return v, "", err
		}
		switch {
		case existing != nil:
			v.variant = &existing.VariantID
		case scan.Kind == apispec.ScanKindEgaisMark:
			if v.variant, err = s.catalog.FindVariant(ctx, scan); err != nil {
				return v, "", err
			}
		default:
			v.variant = in.VariantID
		}
		if v.variant == nil {
			v.mark = nil
			return reject(v, ScanUnknown, "Товар марки не определён: сначала отсканируйте штрихкод бутылки"), "", nil
		}
		if ok, err := inScope(*v.variant); err != nil || !ok {
			v.mark = nil
			return reject(v, ScanRejected, "Товар не входит в состав ревизии"), "", err
		}
		v.status = ScanAccepted
		return v, "foreign", nil
	}

	id, err := s.catalog.FindVariant(ctx, scan)
	if err != nil {
		return v, "", err
	}
	if id == nil {
		return reject(v, ScanUnknown, "Товара с таким кодом нет в каталоге"), "", nil
	}
	v.variant = id
	info, err := q.VariantInfo(ctx, *id)
	if err != nil {
		return v, "", err
	}
	if marked(info.EgaisAccounting) {
		return reject(v, ScanSelected, "Отсканируйте марку каждой бутылки"), "", nil
	}
	if ok, err := inScope(*id); err != nil || !ok {
		return reject(v, ScanRejected, "Товар не входит в состав ревизии"), "", err
	}
	v.status = ScanAccepted
	return v, "", nil
}

// UndoStocktakeScan отменяет скан ревизии: марка перестаёт числиться найденной, количество — минус один.
func (s *Service) UndoStocktakeScan(ctx context.Context, id, scanID uuid.UUID) (ScanResult, error) {
	var res ScanResult
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := s.lockStocktake(ctx, q, id, apispec.PermissionStocktakeCount)
		if err != nil {
			return err
		}
		if doc.Status != StatusCounting {
			return ErrNotCounting
		}
		undone, err := q.UndoScan(ctx, store.UndoScanParams{ID: scanID, DocumentID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			res.Scan, err = q.GetScan(ctx, scanID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if err != nil {
			return err
		}
		if undone.Status == ScanAccepted {
			switch {
			case undone.MarkCode != nil:
				err = q.RemoveFoundMark(ctx, store.RemoveFoundMarkParams{DocumentID: id, MarkCode: *undone.MarkCode})
			case undone.VariantID != nil && undone.ZoneID != nil:
				err = q.AddCount(ctx, store.AddCountParams{DocumentID: id, ZoneID: *undone.ZoneID, VariantID: *undone.VariantID, Delta: -undone.Qty})
			}
			if err != nil {
				return err
			}
		}
		res.Scan, err = q.GetScan(ctx, scanID)
		return err
	})
	return res, err
}

// SetStocktakeCount задаёт посчитанное количество товара без марок — например, целый короб.
func (s *Service) SetStocktakeCount(ctx context.Context, id, zone, variant uuid.UUID, qty int32) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := s.lockStocktake(ctx, q, id, apispec.PermissionStocktakeCount)
		if err != nil {
			return err
		}
		if doc.Status != StatusCounting {
			return ErrNotCounting
		}
		if st, err := q.LockStocktakeZone(ctx, store.LockStocktakeZoneParams{DocumentID: id, ZoneID: zone}); err != nil || st != "counting" {
			return ErrZoneClosed
		}
		info, err := q.VariantInfo(ctx, variant)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownVariant
		}
		if err != nil {
			return err
		}
		if marked(info.EgaisAccounting) {
			return ErrMarkedQty
		}
		return q.SetCount(ctx, store.SetCountParams{DocumentID: id, ZoneID: zone, VariantID: variant, Qty: max(qty, 0)})
	})
}

// SetZone закрывает зону, открывает её снова или начинает пересчёт заново (сканы зоны отменяются).
func (s *Service) SetZone(ctx context.Context, id, zone uuid.UUID, action string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		perm := apispec.PermissionStocktakeCount
		if action == "recount" {
			perm = apispec.PermissionStocktakeManage
		}
		doc, err := s.lockStocktake(ctx, q, id, perm)
		if err != nil {
			return err
		}
		if doc.Status != StatusCounting {
			return ErrNotCounting
		}
		if _, err := q.LockStocktakeZone(ctx, store.LockStocktakeZoneParams{DocumentID: id, ZoneID: zone}); errors.Is(err, pgx.ErrNoRows) {
			return ErrZoneClosed
		} else if err != nil {
			return err
		}
		me := userID(ctx)
		switch action {
		case "close":
			return q.SetStocktakeZone(ctx, store.SetStocktakeZoneParams{DocumentID: id, ZoneID: zone, Status: "closed", ClosedBy: &me})
		case "reopen":
			return q.SetStocktakeZone(ctx, store.SetStocktakeZoneParams{DocumentID: id, ZoneID: zone, Status: "counting"})
		case "recount":
			for _, f := range []func(context.Context, store.ClearZoneCountsParams) error{q.ClearZoneCounts} {
				if err := f(ctx, store.ClearZoneCountsParams{DocumentID: id, ZoneID: zone}); err != nil {
					return err
				}
			}
			if err := q.ClearZoneMarks(ctx, store.ClearZoneMarksParams{DocumentID: id, ZoneID: zone}); err != nil {
				return err
			}
			if err := q.UndoZoneScans(ctx, store.UndoZoneScansParams{DocumentID: id, ZoneID: &zone}); err != nil {
				return err
			}
			return q.SetStocktakeZone(ctx, store.SetStocktakeZoneParams{DocumentID: id, ZoneID: zone, Status: "counting"})
		}
		return ErrZoneClosed
	})
}

// FinishCounting переводит ревизию к сличению, когда все зоны закрыты.
func (s *Service) FinishCounting(ctx context.Context, id uuid.UUID, version int32) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := s.lockStocktake(ctx, q, id, apispec.PermissionStocktakeManage)
		if err != nil {
			return err
		}
		if doc.Status != StatusCounting {
			return ErrNotCounting
		}
		if doc.Version != version {
			return ErrConflict
		}
		zones, err := q.StocktakeZones(ctx, id)
		if err != nil {
			return err
		}
		for _, z := range zones {
			if z.Status != "closed" {
				return ErrZonesOpen
			}
		}
		return q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: id, Status: StatusReconciling})
	})
}

// Sheet — сличительная ведомость (ИНВ-19): по зонам и вариантам учёт и факт, марки не найденные
// и чужие.
type Sheet struct {
	Lines    []store.StocktakeSheetRow
	NotFound []store.NotFoundMarksRow
	Found    []store.FoundMarksRow
	Zones    []store.StocktakeZonesRow
}

func (s *Service) Sheet(ctx context.Context, id uuid.UUID) (Sheet, store.GetDocumentRow, error) {
	var sh Sheet
	doc, err := s.q.GetDocument(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (doc.Kind != Stocktake || !canView(ctx, doc.Kind, doc.LocationID, nil) &&
		auth.CheckLocation(ctx, apispec.PermissionDocsApprove, doc.LocationID) != nil) {
		return sh, doc, ErrNotFound
	}
	if err != nil {
		return sh, doc, err
	}
	if sh.Zones, err = s.q.StocktakeZones(ctx, id); err != nil {
		return sh, doc, err
	}
	if sh.Lines, err = s.q.StocktakeSheet(ctx, id); err != nil {
		return sh, doc, err
	}
	if sh.NotFound, err = s.q.NotFoundMarks(ctx, id); err != nil {
		return sh, doc, err
	}
	sh.Found, err = s.q.FoundMarks(ctx, id)
	return sh, doc, err
}

// SubmitStocktake отправляет сличительную ведомость на утверждение (ПБ-8). Строки документа —
// итог по вариантам: учёт (план) и факт.
func (s *Service) SubmitStocktake(ctx context.Context, id uuid.UUID, version int32) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := s.lockStocktake(ctx, q, id, apispec.PermissionStocktakeManage)
		if err != nil {
			return err
		}
		if doc.Status != StatusReconciling {
			return ErrNotReconciling
		}
		if doc.Version != version {
			return ErrConflict
		}
		rows, err := q.StocktakeSheet(ctx, id)
		if err != nil {
			return err
		}
		type total struct{ book, fact int32 }
		totals := map[uuid.UUID]*total{}
		var order []uuid.UUID
		for _, r := range rows {
			t, ok := totals[r.VariantID]
			if !ok {
				t = &total{}
				totals[r.VariantID] = t
				order = append(order, r.VariantID)
			}
			t.book += r.BookQty
			t.fact += r.FactQty
		}
		if err := q.ReplaceLines(ctx, id); err != nil {
			return err
		}
		for i, v := range order {
			book := totals[v].book
			if err := q.InsertStocktakeLine(ctx, store.InsertStocktakeLineParams{
				DocumentID: id, LineNo: int32(i + 1), VariantID: v, Qty: totals[v].fact, QtyPlan: &book,
			}); err != nil {
				return err
			}
		}
		me := userID(ctx)
		if err := q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: id, Status: StatusPendingApproval, PostedBy: &me}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionDocumentSubmitted, EntityType: audit.EntityDocument, EntityID: id.String(),
			Data: map[string]any{"number": doc.Number, "kind": doc.Kind},
		})
	})
}

type actItems struct{ writeOff, chargeOn []egais.Item }

// applyStocktake корректирует остатки по утверждённой ведомости: недостача списывается, излишек
// приходуется по средней, марки из другой зоны переносятся, незнакомые чужие — встают на учёт.
// В ЕГАИС по каждому регистру — акт списания «Недостача» и акт постановки на баланс.
func (s *Service) applyStocktake(ctx context.Context, tx pgx.Tx, q *store.Queries, doc store.LockDocumentRow, ev marks.Event) (string, error) {
	rows, err := q.StocktakeSheet(ctx, doc.ID)
	if err != nil {
		return "", err
	}
	notFound, err := q.NotFoundMarks(ctx, doc.ID)
	if err != nil {
		return "", err
	}
	found, err := q.FoundMarks(ctx, doc.ID)
	if err != nil {
		return "", err
	}
	type key struct{ zone, variant uuid.UUID }
	info := map[uuid.UUID]store.StocktakeSheetRow{}
	for _, r := range rows {
		info[r.VariantID] = r
	}
	zoneKind := map[uuid.UUID]string{}
	var moves []stock.Move
	acts := map[bool]*actItems{false: {}, true: {}} // ключ — торговый зал (регистр 2)
	addAct := func(zone uuid.UUID, variant uuid.UUID, qty int32, code string, charge bool) {
		r := info[variant]
		if r.Alcocode == nil {
			return
		}
		a := acts[zoneKind[zone] == "floor"]
		list := &a.writeOff
		if charge {
			list = &a.chargeOn
		}
		for i := range *list {
			if (*list)[i].VariantID == variant {
				(*list)[i].Qty += qty
				if code != "" {
					(*list)[i].Marks = append((*list)[i].Marks, code)
				}
				return
			}
		}
		it := egais.Item{VariantID: variant, Alcocode: r.Alcocode, Qty: qty}
		if code != "" {
			it.Marks = []string{code}
		}
		*list = append(*list, it)
	}
	for _, r := range rows {
		zoneKind[r.ZoneID] = r.ZoneKind
	}
	// Товар без марок — по количеству в каждой зоне.
	for _, r := range rows {
		if marked(r.EgaisAccounting) {
			continue
		}
		switch diff := r.FactQty - r.BookQty; {
		case diff < 0:
			moves = append(moves, stock.Move{VariantID: r.VariantID, LocationID: doc.LocationID, ZoneID: r.ZoneID, Qty: diff, External: true})
			addAct(r.ZoneID, r.VariantID, -diff, "", false)
		case diff > 0:
			moves = append(moves, stock.Move{VariantID: r.VariantID, LocationID: doc.LocationID, ZoneID: r.ZoneID, Qty: diff, External: true, UnitCostE4: r.CostE4})
			addAct(r.ZoneID, r.VariantID, diff, "", true)
		}
	}
	// Марки: не найденные — списание, из другой зоны — перенос, незнакомые чужие — постановка на учёт.
	byZone := map[uuid.UUID][]string{}
	for _, m := range notFound {
		zoneKind[m.ZoneID] = m.ZoneKind
		byZone[m.ZoneID] = append(byZone[m.ZoneID], m.MarkCode)
		moves = append(moves, stock.Move{VariantID: m.VariantID, LocationID: doc.LocationID, ZoneID: m.ZoneID, Qty: -1, External: true})
		if _, ok := info[m.VariantID]; ok {
			addAct(m.ZoneID, m.VariantID, 1, m.MarkCode, false)
		}
	}
	for zone, codes := range byZone {
		z := zone
		if err := marks.WriteOff(ctx, tx, ev, doc.LocationID, &z, codes); err != nil {
			return "", err
		}
	}
	movedPairs := map[key][]string{}
	var newCodes []string
	var newVariants []uuid.UUID
	newZone := map[string]uuid.UUID{}
	for _, m := range found {
		zoneKind[m.ZoneID] = m.ZoneKind
		switch {
		// Найденную, но проданную во время подсчёта марку переносить уже некуда.
		case m.Kind == "moved" && m.BookZoneID != nil && m.VariantID != nil && m.OnHand:
			movedPairs[key{*m.BookZoneID, m.ZoneID}] = append(movedPairs[key{*m.BookZoneID, m.ZoneID}], m.MarkCode)
			moves = append(moves,
				stock.Move{VariantID: *m.VariantID, LocationID: doc.LocationID, ZoneID: *m.BookZoneID, Qty: -1},
				stock.Move{VariantID: *m.VariantID, LocationID: doc.LocationID, ZoneID: m.ZoneID, Qty: 1})
		case m.Kind == "foreign" && !m.Known && m.VariantID != nil:
			newCodes = append(newCodes, m.MarkCode)
			newVariants = append(newVariants, *m.VariantID)
			newZone[m.MarkCode] = m.ZoneID
			cost := info[*m.VariantID].CostE4
			moves = append(moves, stock.Move{VariantID: *m.VariantID, LocationID: doc.LocationID, ZoneID: m.ZoneID, Qty: 1, External: true, UnitCostE4: cost})
			addAct(m.ZoneID, *m.VariantID, 1, m.MarkCode, true)
		}
	}
	for p, codes := range movedPairs {
		if err := marks.MoveWithin(ctx, tx, ev, p.zone, doc.LocationID, p.variant, codes); err != nil {
			return "", err
		}
	}
	for zone := range zoneKind {
		var codes []string
		var variants []uuid.UUID
		for i, c := range newCodes {
			if newZone[c] == zone {
				codes, variants = append(codes, c), append(variants, newVariants[i])
			}
		}
		if len(codes) > 0 {
			if err := marks.Register(ctx, tx, ev, doc.LocationID, zone, codes, variants); err != nil {
				return "", err
			}
		}
	}
	if _, err := stock.Apply(ctx, tx, stock.Doc{ID: doc.ID, Date: doc.DocDate}, moves); err != nil {
		if se, ok := errors.AsType[*stock.ShortageError](err); ok {
			lines, _ := q.ListLines(ctx, store.ListLinesParams{DocumentID: doc.ID})
			return "", s.describeShortage(ctx, q, lines, se)
		}
		return "", err
	}
	status := StatusPosted
	for floor, a := range acts {
		writeOff, chargeOn := egais.ActWriteOff, egais.ActChargeOn
		if floor {
			writeOff, chargeOn = egais.ActWriteOffShop, egais.ActChargeOnShop
		}
		if len(a.writeOff) > 0 {
			if err := egais.EnqueuePayload(ctx, tx, doc.LocationID, writeOff, doc.ID, map[string]any{"reason": "Недостача", "items": a.writeOff}); err != nil {
				return "", err
			}
			status = StatusAwaitingEgais
		}
		if len(a.chargeOn) > 0 {
			if err := egais.EnqueuePayload(ctx, tx, doc.LocationID, chargeOn, doc.ID, map[string]any{"reason": "Пересортица или излишек по ревизии", "items": a.chargeOn}); err != nil {
				return "", err
			}
			status = StatusAwaitingEgais
		}
	}
	return status, nil
}
