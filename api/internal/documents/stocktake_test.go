package documents_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

func (f *fixture) countScan(t *testing.T, doc, zone uuid.UUID, raw string, hint *uuid.UUID) documents.ScanResult {
	t.Helper()
	res, err := f.svc.StocktakeScan(f.keeper, doc, zone, documents.NewScan{ID: uuid.New(), Raw: raw, Source: "manual", ScannedAt: time.Now(), VariantID: hint})
	if err != nil {
		t.Fatalf("scan %s: %v", raw, err)
	}
	return res
}

func TestStocktake(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	const m1, m2, m3, alien = "PKN0000000000001", "PKN0000000000002", "PKN0000000000003", "PKN0000000000009"
	f.opening(t, []string{m1, m2, m3}, 5)

	doc, err := f.svc.CreateStocktake(f.keeper, f.store, documents.Scope{}, "Плановая")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.StartStocktake(f.keeper, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}
	// Пока идёт подсчёт, товар ревизии не перемещается.
	tr, _ := f.svc.CreateFloorTransfer(f.keeper, f.store, apispec.FloorDirectionToFloor, "")
	if err := f.svc.SetLine(f.keeper, tr, f.beer.variant, 1); err != nil {
		t.Fatal(err)
	}
	var be *stock.BlockedError
	if err := f.svc.Post(f.keeper, tr, f.version(t, tr)); !errors.As(err, &be) {
		t.Fatalf("move during stocktake: %v", err)
	}

	f.countScan(t, doc, f.backroom, m1, nil)
	if res := f.countScan(t, doc, f.backroom, m1, nil); res.Scan.Status != documents.ScanDuplicate {
		t.Fatalf("second scan of a mark: %+v", res.Scan)
	}
	f.countScan(t, doc, f.floor, m2, nil) // бутылку переставили в зал
	if res := f.countScan(t, doc, f.backroom, alien, &f.vodka.variant); res.Scan.Status != documents.ScanAccepted {
		t.Fatalf("foreign mark: %+v", res.Scan)
	}
	for range 6 {
		f.countScan(t, doc, f.backroom, f.beer.ean, nil)
	}
	sh, _, err := f.svc.Sheet(f.keeper, doc)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, m := range sh.Found {
		kinds[m.MarkCode] = m.Kind
	}
	if kinds[m1] != "found" || kinds[m2] != "moved" || kinds[alien] != "foreign" || len(sh.NotFound) != 1 || sh.NotFound[0].MarkCode != m3 {
		t.Fatalf("mark matching: %v, not found %+v", kinds, sh.NotFound)
	}

	if err := f.svc.FinishCounting(f.keeper, doc, f.version(t, doc)); !errors.Is(err, documents.ErrZonesOpen) {
		t.Fatalf("finish with open zones: %v", err)
	}
	for _, z := range []uuid.UUID{f.backroom, f.floor} {
		if err := f.svc.SetZone(f.keeper, doc, z, "close"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.svc.FinishCounting(f.keeper, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Post(f.keeper, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}
	// Возвращённую ведомость досчитывают: зону открывают заново, учёт и блокировка остаются.
	if err := f.svc.Decide(f.manager, doc, f.version(t, doc), false, "Пересчитайте склад"); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.svc.Get(f.keeper, doc); d.Doc.Status != documents.StatusCounting || !d.CanEdit {
		t.Fatalf("returned stocktake: status %s, can edit %v", d.Doc.Status, d.CanEdit)
	}
	if err := f.svc.Post(f.keeper, tr, f.version(t, tr)); !errors.As(err, &be) {
		t.Fatalf("move after return: %v", err)
	}
	for _, action := range []string{"reopen", "close"} {
		if err := f.svc.SetZone(f.keeper, doc, f.backroom, action); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.svc.FinishCounting(f.keeper, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Post(f.keeper, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Decide(f.manager, doc, f.version(t, doc), true, ""); err != nil {
		t.Fatal(err)
	}
	if n := f.onHand(t, f.beer.variant, f.backroom); n != 6 {
		t.Fatalf("beer surplus charged on: %d", n)
	}
	if n := f.onHand(t, f.vodka.variant, f.backroom); n != 2 {
		t.Fatalf("vodka in the backroom: %d (m1 and the foreign bottle)", n)
	}
	if n := f.onHand(t, f.vodka.variant, f.floor); n != 1 {
		t.Fatalf("vodka moved to the floor: %d", n)
	}
	for code, want := range map[string]string{m3: "written_off", alien: "in_stock"} {
		if m, _ := f.marks.Find(ctx, code); m == nil || m.Status != want {
			t.Fatalf("mark %s: %+v", code, m)
		}
	}
	var acts []string
	rows, _ := f.db.App.Query(ctx, `select doc_type from egais_messages where document_id = $1 order by doc_type`, doc)
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		acts = append(acts, s)
	}
	if len(acts) != 2 || acts[0] != "ActChargeOn" || acts[1] != "ActWriteOff" {
		t.Fatalf("egais acts: %v", acts)
	}
	// После утверждения блокировка снята.
	if err := f.svc.Post(f.keeper, tr, f.version(t, tr)); err != nil {
		t.Fatalf("move after stocktake: %v", err)
	}
	f.integrity(t)
}

func TestStocktakeCancelUnblocks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.opening(t, nil, 5)
	doc, err := f.svc.CreateStocktake(f.keeper, f.store, documents.Scope{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.StartStocktake(f.keeper, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}
	tr, _ := f.svc.CreateFloorTransfer(f.keeper, f.store, apispec.FloorDirectionToFloor, "")
	if err := f.svc.SetLine(f.keeper, tr, f.beer.variant, 1); err != nil {
		t.Fatal(err)
	}
	var be *stock.BlockedError
	if err := f.svc.Post(f.keeper, tr, f.version(t, tr)); !errors.As(err, &be) {
		t.Fatalf("move during stocktake: %v", err)
	}
	if err := f.svc.Cancel(f.keeper, doc, f.version(t, doc)); err != nil {
		t.Fatalf("cancel a started stocktake: %v", err)
	}
	if err := f.svc.Post(f.keeper, tr, f.version(t, tr)); err != nil {
		t.Fatalf("move after cancel: %v", err)
	}
}

func TestTorgForms(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	opening := f.opening(t, nil, 5)
	tr, err := f.svc.CreateFloorTransfer(f.keeper, f.store, apispec.FloorDirectionToFloor, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetLine(f.keeper, tr, f.beer.variant, 2); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Post(f.keeper, tr, f.version(t, tr)); err != nil {
		t.Fatal(err)
	}
	pdf, err := f.svc.TorgForm(f.keeper, tr, "torg13")
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("torg13: %v", err)
	}
	if _, err := f.svc.TorgForm(f.keeper, tr, "torg16"); !errors.Is(err, documents.ErrFormNotApplicable) {
		t.Fatalf("torg16 for a transfer: %v", err)
	}
	if _, err := f.svc.TorgForm(f.keeper, opening, "torg2"); !errors.Is(err, documents.ErrFormNotApplicable) {
		t.Fatalf("torg2 for an opening: %v", err)
	}
}
