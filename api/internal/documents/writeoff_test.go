package documents_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/documents"
)

func (f *fixture) reason(t *testing.T, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.db.App.QueryRow(t.Context(), `select id from writeoff_reasons where name = $1`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestWriteoffNeedsPhotoAndSecondKey(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	f.opening(t, []string{"PKN0000000000001", "PKN0000000000002"}, 5)

	doc, err := f.svc.CreateWriteoff(f.keeper, &f.backroom, f.reason(t, "Бой"), "Уронили при выкладке", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res := f.scan(t, f.keeper, doc, "PKN0000000000001", nil); res.Scan.Status != documents.ScanAccepted {
		t.Fatalf("mark: %+v", res.Scan)
	}
	if err := f.svc.SetLine(f.keeper, doc, f.beer.variant, 2); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Post(f.keeper, doc, f.version(t, doc)); !errors.Is(err, documents.ErrPhotoRequired) {
		t.Fatalf("breakage without a photo: %v", err)
	}
	if _, err := f.svc.AddPhoto(f.keeper, doc, "image/jpeg", []byte{0xff, 0xd8, 0xff, 0xd9}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Post(f.keeper, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.svc.Get(f.keeper, doc); d.Doc.Status != documents.StatusPendingApproval {
		t.Fatalf("submitted: %s", d.Doc.Status)
	}
	if n := f.onHand(t, f.beer.variant, f.backroom); n != 5 {
		t.Fatalf("stock moves only after approval, beer %d", n)
	}
	// Утверждает другой сотрудник с правом утверждения; кладовщику права нет.
	if err := f.svc.Decide(f.keeper, doc, f.version(t, doc), true, ""); err == nil {
		t.Fatal("storekeeper approved")
	}
	if err := f.svc.Decide(f.manager, doc, f.version(t, doc), true, "Фото есть"); err != nil {
		t.Fatal(err)
	}
	d, _ := f.svc.Get(f.manager, doc)
	if d.Doc.Status != documents.StatusAwaitingEgais || d.Approval == nil || d.Approval.Decision != "approved" {
		t.Fatalf("approved: %s %+v", d.Doc.Status, d.Approval)
	}
	if n := f.onHand(t, f.beer.variant, f.backroom); n != 3 {
		t.Fatalf("beer after write-off %d", n)
	}
	if m, _ := f.marks.Find(ctx, "PKN0000000000001"); m.Status != "written_off" {
		t.Fatalf("mark: %s", m.Status)
	}
	var docType string
	if err := f.db.App.QueryRow(ctx, `select doc_type from egais_messages where document_id = $1`, doc).Scan(&docType); err != nil || docType != "ActWriteOff" {
		t.Fatalf("egais act: %s %v", docType, err)
	}
	f.integrity(t)
}

func TestWriteoffReturnedAndSameApprover(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.opening(t, nil, 5)
	doc, err := f.svc.CreateWriteoff(f.manager, &f.backroom, f.reason(t, "Истёк срок годности"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetLine(f.manager, doc, f.beer.variant, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Post(f.manager, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Decide(f.manager, doc, f.version(t, doc), true, ""); !errors.Is(err, documents.ErrSameApprover) {
		t.Fatalf("author approves own write-off: %v", err)
	}
	other := f.session(t, "manager")
	if err := f.svc.Decide(other, doc, f.version(t, doc), false, "Укажите партию"); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.svc.Get(f.manager, doc); d.Doc.Status != documents.StatusDraft || d.Approval.Comment != "Укажите партию" {
		t.Fatalf("returned: %s %+v", d.Doc.Status, d.Approval)
	}
}

func TestStornoOfFloorTransfer(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.opening(t, []string{"PKN0000000000001"}, 0)
	doc, err := f.svc.CreateFloorTransfer(f.keeper, f.store, apispec.FloorDirectionToFloor, "")
	if err != nil {
		t.Fatal(err)
	}
	f.scan(t, f.keeper, doc, "PKN0000000000001", nil)
	if err := f.svc.Post(f.keeper, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}
	storno, err := f.svc.CreateStorno(f.keeper, doc, "Ошиблись зоной")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateStorno(f.keeper, doc, ""); !errors.Is(err, documents.ErrStornoForbidden) {
		t.Fatalf("second storno: %v", err)
	}
	if err := f.svc.Post(f.keeper, storno, f.version(t, storno)); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Decide(f.manager, storno, f.version(t, storno), true, ""); err != nil {
		t.Fatal(err)
	}
	if n := f.onHand(t, f.vodka.variant, f.backroom); n != 1 {
		t.Fatalf("vodka back in the backroom: %d", n)
	}
	if m, _ := f.marks.Find(t.Context(), "PKN0000000000001"); m.ZoneID == nil || *m.ZoneID != f.backroom {
		t.Fatalf("mark back: %+v", m)
	}
	if d, _ := f.svc.Get(f.keeper, doc); d.Doc.Status != documents.StatusReversed {
		t.Fatalf("original: %s", d.Doc.Status)
	}
	f.integrity(t)
}
