package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/documents/store"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

const (
	Receipt = "receipt"

	ModeDocument = "document"
	ModeBoxes    = "boxes"
	ModeSample   = "sample"
	ModePiece    = "piece"

	// При выборочной приёмке вскрывается каждый пятый короб (СО-2).
	sampleEvery = 5
)

var (
	ErrModeTooWeak  = errors.New("marked goods need sample or piece receiving")
	ErrUnknownMode  = errors.New("unknown receiving mode")
	ErrUnknownPoint = errors.New("no location with this FSRAR ID")
)

// UnmappedError — в ТТН есть продукция, алкокода которой нет в каталоге.
type UnmappedError struct{ Names []string }

func (e *UnmappedError) Error() string { return fmt.Sprintf("unmapped TTN positions: %v", e.Names) }

// Unmapped — позиция ТТН, которую не удалось сопоставить с вариантом каталога.
type Unmapped struct {
	Identity int    `json:"identity"`
	Alcocode string `json:"alcocode"`
	Name     string `json:"name"`
	Qty      int    `json:"qty"`
}

var ssccPattern = regexp.MustCompile(`^(00)?([0-9]{18})$`)

// packageCode — номер короба из скана: SSCC с идентификатором применения (00) или без него.
func packageCode(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if len(s) > 3 && s[0] == ']' {
		s = s[3:]
	}
	m := ssccPattern.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	return m[2], true
}

// CreateReceipt заводит черновик поступления по ТТН, пришедшей из ЕГАИС. Режим приёмки берётся
// у поставщика, для продукции с марками — не ниже «выборочно». Повторная ТТН ничего не создаёт.
func (s *Service) CreateReceipt(ctx context.Context, tx pgx.Tx, fsrar string, ttn egais.TTN) (uuid.UUID, error) {
	q := s.q.WithTx(tx)
	loc, err := q.LocationByFSRAR(ctx, &fsrar)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrUnknownPoint
	}
	if err != nil {
		return uuid.Nil, err
	}
	mode := ModePiece
	var supplierID *uuid.UUID
	if sp, err := q.SupplierByFSRAR(ctx, &ttn.Shipper.FSRARID); err == nil {
		supplierID, mode = &sp.ID, sp.DefaultReceivingMode
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}

	variants := map[int]uuid.UUID{}
	var unmapped []Unmapped
	marked := false
	for _, p := range ttn.Positions {
		v, err := s.catalog.FindVariant(ctx, catalog.Scan{Kind: apispec.ScanKindAlcocode, Code: p.Alcocode})
		if err != nil {
			return uuid.Nil, err
		}
		if v == nil {
			unmapped = append(unmapped, Unmapped{Identity: p.Identity, Alcocode: p.Alcocode, Name: p.Name, Qty: p.Qty})
			continue
		}
		variants[p.Identity] = *v
		marked = marked || p.Marked
	}
	if marked && (mode == ModeDocument || mode == ModeBoxes) {
		mode = ModeSample
	}
	if unmapped == nil {
		unmapped = []Unmapped{}
	}
	unmappedJSON, _ := json.Marshal(unmapped)

	spec := kinds[Receipt]
	n, err := q.NextDocNumber(ctx, store.NextDocNumberParams{Prefix: spec.prefix, Year: int32(loc.LocalDate.Year())})
	if err != nil {
		return uuid.Nil, err
	}
	var ttnDate *time.Time
	if d, err := time.Parse("2006-01-02", ttn.Date); err == nil {
		ttnDate = &d
	}
	id, err := q.CreateReceipt(ctx, store.CreateReceiptParams{
		Number: fmt.Sprintf("%s-%d-%06d", spec.prefix, loc.LocalDate.Year(), n), DocDate: loc.LocalDate,
		LocationID: loc.ID, ZoneID: loc.ZoneID, SupplierID: supplierID, ReceivingMode: &mode,
		TtnID: &ttn.ID, TtnNumber: &ttn.Number, TtnDate: ttnDate, Unmapped: unmappedJSON,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}

	packageOf := map[string]string{}
	for _, pkg := range ttn.Packages {
		for _, m := range pkg.Marks {
			packageOf[m] = pkg.Code
		}
	}
	var tm store.InsertTTNMarksParams
	tm.DocumentID = id
	for _, p := range ttn.Positions {
		v, ok := variants[p.Identity]
		if !ok {
			continue
		}
		identity, qty := int32(p.Identity), int32(p.Qty)
		if err := q.InsertPlanLine(ctx, store.InsertPlanLineParams{
			DocumentID: id, LineNo: identity, VariantID: v, QtyPlan: &qty,
			PriceKopecks: int64(p.Price*100 + 0.5), TtnIdentity: &identity,
		}); err != nil {
			return uuid.Nil, err
		}
		for _, m := range p.Marks {
			tm.Codes = append(tm.Codes, m)
			tm.VariantIds = append(tm.VariantIds, v)
			tm.PackageCodes = append(tm.PackageCodes, packageOf[m])
		}
	}
	if len(tm.Codes) > 0 {
		if err := q.InsertTTNMarks(ctx, tm); err != nil {
			return uuid.Nil, err
		}
	}
	for _, pkg := range ttn.Packages {
		var v *uuid.UUID
		if id, ok := variants[pkg.Identity]; ok {
			v = &id
		}
		if err := q.InsertPackage(ctx, store.InsertPackageParams{DocumentID: id, Code: pkg.Code, VariantID: v, Qty: int32(pkg.Qty)}); err != nil {
			return uuid.Nil, err
		}
	}
	if mode == ModeSample {
		if err := chooseToOpen(ctx, q, id); err != nil {
			return uuid.Nil, err
		}
	}
	return id, audit.Record(ctx, tx, audit.Event{
		Action: apispec.AuditActionDocumentCreated, EntityType: audit.EntityDocument, EntityID: id.String(),
		Data: map[string]any{"number": ttn.Number, "kind": Receipt},
	})
}

// chooseToOpen отмечает короба для вскрытия: первый и далее каждый пятый.
func chooseToOpen(ctx context.Context, q *store.Queries, doc uuid.UUID) error {
	pkgs, err := q.ReceiptPackages(ctx, doc)
	if err != nil {
		return err
	}
	if err := q.ResetPackagesToOpen(ctx, doc); err != nil {
		return err
	}
	var codes []string
	for i, p := range pkgs {
		if i%sampleEvery == 0 {
			codes = append(codes, p.Code)
		}
	}
	return q.MarkPackagesToOpen(ctx, store.MarkPackagesToOpenParams{DocumentID: doc, Codes: codes})
}

// SetMode меняет режим приёмки черновика. Продукцию с марками нельзя принять только по документу
// или по коробам: марки проверяются хотя бы выборочно.
func (s *Service) SetMode(ctx context.Context, id uuid.UUID, mode string) error {
	if !slices.Contains([]string{ModeDocument, ModeBoxes, ModeSample, ModePiece}, mode) {
		return ErrUnknownMode
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		doc, err := lockDraft(ctx, q, id, editPerm)
		if err != nil {
			return err
		}
		if doc.Kind != Receipt {
			return ErrUnknownMode
		}
		ttnMarks, err := q.ReceiptMarks(ctx, id)
		if err != nil {
			return err
		}
		if len(ttnMarks) > 0 && (mode == ModeDocument || mode == ModeBoxes) {
			return ErrModeTooWeak
		}
		if err := q.SetReceivingMode(ctx, store.SetReceivingModeParams{ID: id, ReceivingMode: &mode}); err != nil {
			return err
		}
		if mode == ModeSample {
			return chooseToOpen(ctx, q, id)
		}
		return nil
	})
}

// PackageState — короб ТТН на экране приёмки.
type PackageState struct {
	Code    string
	Qty     int32
	ToOpen  bool
	Scanned bool
	// Counted — сколько марок вскрытого короба отсканировано поштучно.
	Counted int32
}

// LineState — строка поступления: план из ТТН, факт по сканам и сколько будет принято в ЕГАИС.
type LineState struct {
	store.ListLinesRow
	Fact, Accepted int32
	Marks          []string
}

// ReceiptState — приёмка целиком: строки, короба, расхождения.
type ReceiptState struct {
	Mode     string
	Lines    []LineState
	Packages []PackageState
	Foreign  int32
	Shortage int32
	Surplus  int32
	Unmapped []Unmapped
}

// Differences — по итогам уйдёт акт расхождения, а не подтверждения.
func (r ReceiptState) Differences() bool {
	return r.Shortage > 0 || r.Surplus > 0 || r.Foreign > 0
}

// receiptState считает факт приёмки по режиму. В режиме «по документу» факт равен ТТН; «по коробам»
// засчитываются марки и количество отсканированных коробов; «выборочно» — так же, кроме коробов,
// которые надо вскрыть: их марки считаются поштучно; «поштучно» — только сканы единиц.
func receiptState(ctx context.Context, q *store.Queries, id uuid.UUID, mode string, unmappedJSON []byte) (ReceiptState, error) {
	st := ReceiptState{Mode: mode}
	_ = json.Unmarshal(unmappedJSON, &st.Unmapped)
	lines, err := q.ListLines(ctx, store.ListLinesParams{DocumentID: id})
	if err != nil {
		return st, err
	}
	slices.SortStableFunc(lines, func(a, b store.ListLinesRow) int {
		return int(lineNo(a) - lineNo(b))
	})

	ttnMarks, err := q.ReceiptMarks(ctx, id)
	if err != nil {
		return st, err
	}
	pkgs, err := q.ReceiptPackages(ctx, id)
	if err != nil {
		return st, err
	}
	scans, err := q.AcceptedScans(ctx, id)
	if err != nil {
		return st, err
	}
	if st.Foreign, err = q.CountForeign(ctx, id); err != nil {
		return st, err
	}
	markScanned, pkgScanned := map[string]bool{}, map[string]bool{}
	for _, sc := range scans {
		if sc.MarkCode != nil {
			markScanned[*sc.MarkCode] = true
		}
		if sc.PackageCode != nil {
			pkgScanned[*sc.PackageCode] = true
		}
	}
	byBox := mode == ModeBoxes || mode == ModeSample
	toOpen := map[string]bool{}
	boxHasMarks := map[string]int32{}
	for _, m := range ttnMarks {
		if m.PackageCode != nil {
			boxHasMarks[*m.PackageCode]++
		}
	}
	boxQty := map[uuid.UUID]int32{}
	for _, p := range pkgs {
		toOpen[p.Code] = p.ToOpen && mode == ModeSample
		ps := PackageState{Code: p.Code, Qty: p.Qty, ToOpen: toOpen[p.Code], Scanned: pkgScanned[p.Code]}
		if byBox && ps.Scanned && !ps.ToOpen && boxHasMarks[p.Code] == 0 && p.VariantID != nil {
			boxQty[*p.VariantID] += p.Qty
		}
		st.Packages = append(st.Packages, ps)
	}
	counted := map[uuid.UUID][]string{}
	openedCount := map[string]int32{}
	for _, m := range ttnMarks {
		box := ""
		if m.PackageCode != nil {
			box = *m.PackageCode
		}
		ok := mode == ModeDocument || markScanned[m.Code] || byBox && box != "" && pkgScanned[box] && !toOpen[box]
		if ok {
			counted[m.VariantID] = append(counted[m.VariantID], m.Code)
		}
		if markScanned[m.Code] && toOpen[box] {
			openedCount[box]++
		}
	}
	for i := range st.Packages {
		st.Packages[i].Counted = openedCount[st.Packages[i].Code]
	}

	for _, l := range lines {
		ls := LineState{ListLinesRow: l}
		switch {
		case marked(l.EgaisAccounting):
			ls.Marks = counted[l.VariantID]
			ls.Fact = int32(len(ls.Marks))
		case mode == ModeDocument && l.QtyPlan != nil:
			ls.Fact = *l.QtyPlan
		default:
			ls.Fact = l.Qty + boxQty[l.VariantID]
		}
		switch {
		case l.QtyPlan != nil:
			ls.Accepted = min(ls.Fact, *l.QtyPlan)
			st.Shortage += *l.QtyPlan - ls.Accepted
			st.Surplus += max(ls.Fact-*l.QtyPlan, 0)
		case l.EgaisAccounting != nil:
			// Алкоголя нет в ТТН — принять его в ЕГАИС нельзя: это излишек или пересорт.
			st.Surplus += ls.Fact
		default:
			ls.Accepted = ls.Fact
		}
		st.Lines = append(st.Lines, ls)
	}
	return st, nil
}

func lineNo(l store.ListLinesRow) int32 {
	if l.TtnIdentity != nil {
		return *l.TtnIdentity
	}
	return 1 << 20
}

// classifyReceipt разбирает скан в поступлении: короб, марку ТТН, чужую марку или штрихкод товара.
func (s *Service) classifyReceipt(ctx context.Context, q *store.Queries, doc store.LockDocumentRow, in NewScan) (verdict, error) {
	mode := ModePiece
	if doc.ReceivingMode != nil {
		mode = *doc.ReceivingMode
	}
	if mode == ModeDocument {
		return verdict{status: ScanRejected, reason: "В режиме «по документу» сканировать не нужно: факт равен ТТН"}, nil
	}
	if code, ok := packageCode(in.Raw); ok {
		kind := "package"
		v := verdict{codeKind: &kind, pkg: &code}
		pkgs, err := q.ReceiptPackages(ctx, doc.ID)
		if err != nil {
			return v, err
		}
		i := slices.IndexFunc(pkgs, func(p store.ReceiptPackagesRow) bool { return p.Code == code })
		if i < 0 {
			v.pkg = nil
			return reject(v, ScanForeign, "Короба нет в ТТН"), nil
		}
		if mode == ModePiece {
			v.pkg = nil
			return reject(v, ScanRejected, "Поштучная приёмка: сканируйте каждую бутылку, а не короб"), nil
		}
		v.variant = pkgs[i].VariantID
		scans, err := q.AcceptedScans(ctx, doc.ID)
		if err != nil {
			return v, err
		}
		for _, sc := range scans {
			if sc.PackageCode != nil && *sc.PackageCode == code {
				v.pkg = nil
				return reject(v, ScanDuplicate, "Этот короб уже отсканирован"), nil
			}
		}
		v.status = ScanAccepted
		if mode == ModeSample && pkgs[i].ToOpen {
			v.reason = "Вскройте короб и отсканируйте каждую бутылку"
		}
		return v, nil
	}

	scan, ok := catalog.ParseScan(in.Raw)
	if !ok {
		return verdict{status: ScanUnknown, reason: "Код не распознан"}, nil
	}
	if scan.Mark == "" {
		return s.classify(ctx, q, doc, in)
	}
	kind := string(scan.Kind)
	v := verdict{codeKind: &kind, mark: &scan.Mark}
	ttnMarks, err := q.ReceiptMarks(ctx, doc.ID)
	if err != nil {
		return v, err
	}
	i := slices.IndexFunc(ttnMarks, func(m store.ReceiptMarksRow) bool { return m.Code == scan.Mark })
	if i < 0 {
		// Чужая марка тоже записывается: она войдёт в акт расхождения как излишек.
		if existing, err := s.marks.Find(ctx, scan.Mark); err == nil && existing != nil {
			v.variant = &existing.VariantID
		}
		return reject(v, ScanForeign, "Марки нет в ТТН — бутылку не принимать"), nil
	}
	m := ttnMarks[i]
	v.variant = &m.VariantID
	dup, err := q.CodeScanned(ctx, store.CodeScannedParams{DocumentID: doc.ID, MarkCode: v.mark})
	if err != nil {
		return v, err
	}
	if dup {
		v.mark = nil
		return reject(v, ScanDuplicate, "Эта марка уже отсканирована"), nil
	}
	if m.PackageCode != nil && (mode == ModeBoxes || mode == ModeSample) {
		pkgs, err := q.ReceiptPackages(ctx, doc.ID)
		if err != nil {
			return v, err
		}
		scans, err := q.AcceptedScans(ctx, doc.ID)
		if err != nil {
			return v, err
		}
		for _, p := range pkgs {
			if p.Code != *m.PackageCode || (mode == ModeSample && p.ToOpen) {
				continue
			}
			for _, sc := range scans {
				if sc.PackageCode != nil && *sc.PackageCode == p.Code {
					v.mark = nil
					return reject(v, ScanDuplicate, "Марка уже засчитана вместе с коробом"), nil
				}
			}
		}
	}
	v.status = ScanAccepted
	return v, nil
}

// postReceipt проводит поступление: принятое встаёт на остаток по цене ТТН, принятые марки — на учёт,
// в ЕГАИС уходит акт подтверждения или расхождения. До квитанции документ «ожидает ЕГАИС».
func (s *Service) postReceipt(ctx context.Context, tx pgx.Tx, q *store.Queries, doc store.LockDocumentRow, ev marks.Event) (map[uuid.UUID]int64, string, error) {
	mode := ModePiece
	if doc.ReceivingMode != nil {
		mode = *doc.ReceivingMode
	}
	st, err := receiptState(ctx, q, doc.ID, mode, doc.Unmapped)
	if err != nil {
		return nil, "", err
	}
	if len(st.Unmapped) > 0 {
		names := make([]string, len(st.Unmapped))
		for i, u := range st.Unmapped {
			names[i] = fmt.Sprintf("%s (алкокод %s)", u.Name, u.Alcocode)
		}
		return nil, "", &UnmappedError{Names: names}
	}

	var extra []uuid.UUID
	for _, l := range st.Lines {
		if l.QtyPlan == nil && l.Accepted > 0 {
			extra = append(extra, l.VariantID)
		}
	}
	prices := map[uuid.UUID]int64{}
	if len(extra) > 0 {
		if prices, err = s.catalog.PurchasePrices(ctx, extra, time.Now()); err != nil {
			return nil, "", err
		}
	}
	var moves []stock.Move
	var missing, codes []string
	var markVariants []uuid.UUID
	act := egais.Act{TTNID: *doc.TtnID}
	for _, l := range st.Lines {
		if l.TtnIdentity != nil {
			act.Positions = append(act.Positions, egais.ActPosition{
				Identity: int(*l.TtnIdentity), QtyAccepted: int(l.Accepted), Marks: l.Marks,
			})
		}
		if l.Accepted == 0 {
			continue
		}
		price := l.PriceKopecks
		if l.QtyPlan == nil {
			p, ok := prices[l.VariantID]
			if !ok {
				missing = append(missing, l.Article)
				continue
			}
			price = p
		}
		moves = append(moves, stock.Move{
			LineID: l.ID, VariantID: l.VariantID, LocationID: doc.LocationID, ZoneID: doc.ZoneID,
			Qty: l.Accepted, External: true, UnitCostE4: price * 100,
		})
		for _, m := range l.Marks {
			codes = append(codes, m)
			markVariants = append(markVariants, l.VariantID)
		}
	}
	if len(missing) > 0 {
		return nil, "", &MissingCostError{Articles: missing}
	}
	if len(moves) == 0 && !st.Differences() && len(act.Positions) == 0 {
		return nil, "", ErrEmpty
	}
	if len(codes) > 0 {
		if err := marks.Register(ctx, tx, ev, doc.LocationID, doc.ZoneID, codes, markVariants); err != nil {
			return nil, "", err
		}
	}
	if err := egais.EnqueuePayload(ctx, tx, doc.LocationID, egais.WayBillAct, doc.ID, act); err != nil {
		return nil, "", err
	}
	costs, err := stock.Apply(ctx, tx, stock.Doc{ID: doc.ID, Date: doc.DocDate}, moves)
	if err != nil {
		return nil, "", err
	}
	// Количество строки — принятое: по нему строится карточка движения и акт.
	for _, l := range st.Lines {
		if _, err := q.SetLineQty(ctx, store.SetLineQtyParams{DocumentID: doc.ID, VariantID: l.VariantID, Qty: l.Accepted}); err != nil {
			return nil, "", err
		}
	}
	return costs, StatusAwaitingEgais, nil
}

// ApplyTicket применяет квитанцию ЕГАИС к документу: все сообщения приняты — документ проведён,
// отказ — «отклонён ЕГАИС», пользователь увидит причину в журнале обмена.
func (s *Service) ApplyTicket(ctx context.Context, tx pgx.Tx, docID uuid.UUID, accepted bool) error {
	q := s.q.WithTx(tx)
	doc, err := q.LockDocument(ctx, docID)
	if err != nil {
		return err
	}
	if doc.Status != StatusAwaitingEgais {
		return nil
	}
	status := StatusEgaisRejected
	if accepted {
		pending, rejected, err := egais.Pending(ctx, tx, docID)
		if err != nil {
			return err
		}
		if pending > 0 {
			return nil
		}
		status = StatusPosted
		if rejected > 0 {
			status = StatusEgaisRejected
		}
	}
	return q.SetDocumentStatus(ctx, store.SetDocumentStatusParams{ID: docID, Status: status})
}
