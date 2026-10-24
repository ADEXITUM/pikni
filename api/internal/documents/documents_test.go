package documents_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/marks"
)

func TestOpening(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.keeper

	doc, err := f.svc.CreateOpening(ctx, f.backroom, "первая ревизия")
	if err != nil {
		t.Fatal(err)
	}

	// Штрихкод бутылки с марками только выбирает товар: количество считают марки.
	if res := f.scan(t, ctx, doc, f.vodka.ean, nil); res.Scan.Status != documents.ScanSelected || res.Line != nil {
		t.Fatalf("vodka barcode: %+v", res)
	}
	// В марке нового образца нет алкокода: без выбранного товара её не к чему отнести.
	if res := f.scan(t, ctx, doc, "PKN0000000000001", nil); res.Scan.Status != documents.ScanRejected {
		t.Errorf("new mark without variant: %+v", res.Scan)
	}
	f.scan(t, ctx, doc, "PKN0000000000001", &f.vodka.variant)
	// Марка старого образца сама называет товар по алкокоду.
	if res := f.scan(t, ctx, doc, oldMark, nil); res.Scan.Status != documents.ScanAccepted || res.Line.Qty != 2 {
		t.Fatalf("old mark: %+v", res)
	}
	if res := f.scan(t, ctx, doc, "PKN0000000000001", &f.vodka.variant); res.Scan.Status != documents.ScanDuplicate {
		t.Errorf("same mark twice: %+v", res.Scan)
	}
	if res := f.scan(t, ctx, doc, "не код", nil); res.Scan.Status != documents.ScanUnknown {
		t.Errorf("garbage: %+v", res.Scan)
	}

	for range 3 {
		f.scan(t, ctx, doc, f.beer.ean, nil)
	}
	last := f.scan(t, ctx, doc, f.beer.ean, nil)
	undone, err := f.svc.UndoScan(ctx, doc, last.Scan.ID)
	if err != nil || undone.Line.Qty != 3 || undone.Scan.UndoneAt == nil {
		t.Fatalf("undo: %+v, %v", undone, err)
	}
	if again, err := f.svc.UndoScan(ctx, doc, last.Scan.ID); err != nil || again.Line.Qty != 3 {
		t.Errorf("repeated undo must not change the line: %+v, %v", again, err)
	}
	if err := f.svc.SetLine(ctx, doc, f.vodka.variant, 5); !errors.Is(err, documents.ErrMarkedQty) {
		t.Errorf("manual quantity of marked goods: %v", err)
	}
	if err := f.svc.SetLine(ctx, doc, f.snack.variant, 4); err != nil {
		t.Fatal(err)
	}

	// Кладовщик считает, проводит руководитель или бухгалтер.
	if err := f.svc.Post(ctx, doc, f.version(t, doc)); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("storekeeper posts: %v", err)
	}
	var missing *documents.MissingCostError
	if err := f.svc.Post(f.accountant, doc, f.version(t, doc)); !errors.As(err, &missing) || len(missing.Articles) != 1 {
		t.Fatalf("snack without purchase price: %v", err)
	}
	if err := f.svc.SetLine(ctx, doc, f.snack.variant, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Post(f.accountant, doc, f.version(t, doc)-1); !errors.Is(err, documents.ErrConflict) {
		t.Errorf("stale version: %v", err)
	}
	if err := f.svc.Post(f.accountant, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}

	d, err := f.svc.Get(f.manager, doc)
	if err != nil || d.Doc.Status != documents.StatusPosted || d.CanEdit || len(d.Lines) != 2 {
		t.Fatalf("posted document: %+v, %v", d.Doc, err)
	}
	for _, l := range d.Lines {
		// Ввод оценивается справочной закупочной ценой: 350 ₽ и 60 ₽.
		want := map[uuid.UUID]int64{f.vodka.variant: 3500000, f.beer.variant: 600000}[l.VariantID]
		if !l.Costed || l.UnitCostE4 != want {
			t.Errorf("line %s cost = %d, want %d", l.Article, l.UnitCostE4, want)
		}
	}
	if got := f.onHand(t, f.vodka.variant, f.backroom); got != 2 {
		t.Errorf("vodka on hand = %d", got)
	}
	if got := f.onHand(t, f.beer.variant, f.backroom); got != 3 {
		t.Errorf("beer on hand = %d", got)
	}
	m, events, err := f.marks.History(t.Context(), "PKN0000000000001")
	if err != nil || m.Status != marks.InStock || len(events) != 1 {
		t.Errorf("mark history: %+v %+v %v", m, events, err)
	}
	if _, err := f.svc.AddScan(ctx, doc, documents.NewScan{ID: uuid.New(), Raw: f.beer.ean, Source: "manual"}); !errors.Is(err, documents.ErrNotDraft) {
		t.Errorf("scan into posted document: %v", err)
	}
	if _, err := f.svc.CreateOpening(ctx, f.backroom, ""); !errors.Is(err, documents.ErrOpeningExists) {
		t.Errorf("second opening of the zone: %v", err)
	}
	f.integrity(t)

	// Марку, которая уже учтена, второй раз ввести нельзя — даже в другой зоне.
	floorDoc, err := f.svc.CreateOpening(ctx, f.floor, "")
	if err != nil {
		t.Fatal(err)
	}
	if res := f.scan(t, ctx, floorDoc, oldMark, nil); res.Scan.Status != documents.ScanRejected {
		t.Errorf("registered mark in another opening: %+v", res.Scan)
	}
}

func TestFloorTransfer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.keeper
	f.opening(t, []string{"PKN0000000000001", "PKN0000000000002"}, 5)

	if _, err := f.svc.CreateFloorTransfer(f.session(t, "storekeeper"), f.store, apispec.FloorDirectionToFloor, ""); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("storekeeper of another store: %v", err)
	}
	doc, err := f.svc.CreateFloorTransfer(ctx, f.store, apispec.FloorDirectionToFloor, "")
	if err != nil {
		t.Fatal(err)
	}
	if res := f.scan(t, ctx, doc, f.vodka.ean, nil); res.Scan.Status != documents.ScanRejected {
		t.Errorf("barcode of marked goods: %+v", res.Scan)
	}
	if res := f.scan(t, ctx, doc, "PKN0000000000009", nil); res.Scan.Status != documents.ScanForeign {
		t.Errorf("unknown mark: %+v", res.Scan)
	}
	if res := f.scan(t, ctx, doc, "PKN0000000000001", nil); res.Scan.Status != documents.ScanAccepted || res.Line.Qty != 1 {
		t.Fatalf("mark: %+v", res)
	}
	f.scan(t, ctx, doc, f.beer.ean, nil)
	f.scan(t, ctx, doc, f.beer.ean, nil)
	if err := f.svc.Post(ctx, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}

	// Пиво учитывается в ЕГАИС по регистрам склада и зала: передача ждёт ответа ЕГАИС.
	d, _ := f.svc.Get(ctx, doc)
	if d.Doc.Status != documents.StatusAwaitingEgais {
		t.Errorf("status = %s", d.Doc.Status)
	}
	var docType string
	var qty int
	if err := f.db.App.QueryRow(t.Context(), `select doc_type, (payload -> 'items' -> 0 ->> 'qty')::int
		from egais_messages where document_id = $1 and status = 'queued'`, doc).Scan(&docType, &qty); err != nil || docType != "TransferToShop" || qty != 2 {
		t.Errorf("egais message: %s %d %v", docType, qty, err)
	}
	for _, c := range []struct {
		variant, zone uuid.UUID
		want          int32
	}{
		{f.vodka.variant, f.backroom, 1}, {f.vodka.variant, f.floor, 1},
		{f.beer.variant, f.backroom, 3}, {f.beer.variant, f.floor, 2},
	} {
		if got := f.onHand(t, c.variant, c.zone); got != c.want {
			t.Errorf("on hand %s/%s = %d, want %d", c.variant, c.zone, got, c.want)
		}
	}
	m, events, _ := f.marks.History(t.Context(), "PKN0000000000001")
	if m.ZoneKind == nil || *m.ZoneKind != "floor" || len(events) != 2 {
		t.Errorf("mark after transfer: %+v, %d events", m, len(events))
	}
	f.integrity(t)

	// Больше, чем есть на складе, передать нельзя.
	over, _ := f.svc.CreateFloorTransfer(ctx, f.store, apispec.FloorDirectionToFloor, "")
	if err := f.svc.SetLine(ctx, over, f.beer.variant, 10); err != nil {
		t.Fatal(err)
	}
	var short *documents.ShortageError
	if err := f.svc.Post(ctx, over, f.version(t, over)); !errors.As(err, &short) || short.Available != 3 || short.Need != 10 {
		t.Errorf("shortage: %v", err)
	}
	f.integrity(t)
}

// Две передачи одной марки и последних бутылок пива проводятся одновременно: проходит одна,
// остатки не уходят в минус, регистр сходится с остатками (сценарий П-15 для учёта).
func TestConcurrentPosting(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.opening(t, []string{"PKN0000000000001"}, 2)

	docs := make([]uuid.UUID, 2)
	for i := range docs {
		var err error
		if docs[i], err = f.svc.CreateFloorTransfer(f.keeper, f.store, apispec.FloorDirectionToFloor, ""); err != nil {
			t.Fatal(err)
		}
		f.scan(t, f.keeper, docs[i], "PKN0000000000001", nil)
		if err := f.svc.SetLine(f.keeper, docs[i], f.beer.variant, 2); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, len(docs))
	for i, doc := range docs {
		wg.Go(func() { errs[i] = f.svc.Post(f.keeper, doc, 1) })
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
			continue
		}
		var short *documents.ShortageError
		var misplaced *marks.MisplacedError
		if !errors.As(err, &short) && !errors.As(err, &misplaced) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("posted %d documents, want 1: %v", ok, errs)
	}
	if got := f.onHand(t, f.beer.variant, f.floor); got != 2 {
		t.Errorf("beer on floor = %d", got)
	}
	f.integrity(t)
}

func TestDocumentVisibility(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.opening(t, nil, 1)
	transfer, err := f.svc.CreateFloorTransfer(f.keeper, f.store, apispec.FloorDirectionToFloor, "")
	if err != nil {
		t.Fatal(err)
	}
	other := f.newStore(t, "МГ-02")
	stranger := f.session(t, "storekeeper", other)
	cashier := f.session(t, "cashier", f.store)

	for _, c := range []struct {
		name          string
		ctx           context.Context
		opening, move int
	}{
		{"manager", f.manager, 1, 1},
		{"storekeeper of the store", f.keeper, 1, 1},
		{"storekeeper of another store", stranger, 0, 0},
		// Кассир видит передачи в зал своей точки, но не ввод остатков.
		{"cashier", cashier, 0, 1},
		{"accountant", f.accountant, 1, 1},
	} {
		rows, err := f.svc.List(c.ctx, documents.ListFilter{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		n := map[string]int{}
		for _, r := range rows {
			n[r.Kind]++
		}
		if n[documents.Opening] != c.opening || n[documents.FloorTransfer] != c.move {
			t.Errorf("%s sees %v", c.name, n)
		}
	}
	if _, err := f.svc.Get(stranger, transfer); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("stranger opens the document: %v", err)
	}
}
