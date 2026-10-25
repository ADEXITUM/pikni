package documents_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/egais"
)

func TestTransferBetweenStores(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	f.opening(t, []string{"PKN0000000000001", "PKN0000000000002"}, 5)

	other := f.newStore(t, "МГ-02")
	for loc, fsrar := range map[uuid.UUID]string{f.store: "030000100011", other: "030000100012"} {
		if _, err := f.db.App.Exec(ctx, `update locations set fsrar_id = $2 where id = $1`, loc, fsrar); err != nil {
			t.Fatal(err)
		}
	}
	var otherBackroom uuid.UUID
	if err := f.db.App.QueryRow(ctx, `select id from zones where location_id = $1 and kind = 'backroom'`, other).Scan(&otherBackroom); err != nil {
		t.Fatal(err)
	}
	cashier := f.session(t, "cashier", other)
	receiver := f.session(t, "storekeeper", other)

	// Заявку подаёт получатель; за чужую точку заявку не подать.
	if _, err := f.svc.CreateTransfer(cashier, other, f.store, []documents.TransferLine{{VariantID: f.vodka.variant, Qty: 1}}, ""); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("request into someone else's store: %v", err)
	}
	doc, err := f.svc.CreateTransfer(cashier, f.store, other, []documents.TransferLine{
		{VariantID: f.vodka.variant, Qty: 2}, {VariantID: f.beer.variant, Qty: 3},
	}, "Покупатель ждёт")
	if err != nil {
		t.Fatal(err)
	}
	if d, err := f.svc.Get(cashier, doc); err != nil || d.Doc.Status != documents.StatusRequested || d.CanApprove {
		t.Fatalf("receiver sees the request but cannot approve it: %+v %v", d.Doc.Status, err)
	}
	if err := f.svc.DecideTransfer(receiver, doc, f.version(t, doc), true, ""); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("receiver approves: %v", err)
	}
	if err := f.svc.DecideTransfer(f.keeper, doc, f.version(t, doc), true, ""); err != nil {
		t.Fatal(err)
	}

	// Сборка: марки — сканом, пиво — количеством.
	for _, m := range []string{"PKN0000000000001", "PKN0000000000002"} {
		if res := f.scan(t, f.keeper, doc, m, nil); res.Scan.Status != documents.ScanAccepted {
			t.Fatalf("pick %s: %+v", m, res.Scan)
		}
	}
	if err := f.svc.SetLine(f.keeper, doc, f.beer.variant, 3); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Post(f.keeper, doc, f.version(t, doc)); err != nil {
		t.Fatal(err)
	}
	d, _ := f.svc.Get(f.keeper, doc)
	if d.Doc.Status != documents.StatusInTransit || d.Doc.ReceiptID == nil {
		t.Fatalf("shipped: %s, receipt %v", d.Doc.Status, d.Doc.ReceiptID)
	}
	if n := f.onHand(t, f.beer.variant, f.backroom); n != 2 {
		t.Fatalf("sender beer %d", n)
	}
	var inTransit int32
	if err := f.db.App.QueryRow(ctx, `select in_transit from stock_balances where variant_id = $1 and zone_id = $2`,
		f.beer.variant, otherBackroom).Scan(&inTransit); err != nil || inTransit != 3 {
		t.Fatalf("receiver beer in transit %d %v", inTransit, err)
	}
	f.integrity(t)

	// ТТН ушла в ЕГАИС; УТМ ответил её идентификатором, и она пришла получателю.
	ttnID := "ttn-" + uuid.NewString()
	var payload []byte
	if err := f.db.App.QueryRow(ctx, `update egais_messages set status = 'sent', external_id = $2
		where document_id = $1 and doc_type = 'WayBill' returning payload`, doc, ttnID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	receipt := *d.Doc.ReceiptID
	if err := pgx.BeginFunc(ctx, f.db.App, func(tx pgx.Tx) error {
		got, err := f.svc.CreateReceipt(ctx, tx, "030000100012", egais.TTN{ID: ttnID, Number: d.Doc.Number})
		if err == nil && got != receipt {
			t.Errorf("TTN attaches to the receipt made at shipment: %s vs %s", got, receipt)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Получатель нашёл одну бутылку из двух и всё пиво.
	if res := f.scan(t, receiver, receipt, "PKN0000000000001", nil); res.Scan.Status != documents.ScanAccepted {
		t.Fatalf("receive mark: %+v", res.Scan)
	}
	for range 3 {
		f.scan(t, receiver, receipt, f.beer.ean, nil)
	}
	if err := f.svc.Post(receiver, receipt, f.version(t, receipt)); err != nil {
		t.Fatal(err)
	}
	if n := f.onHand(t, f.beer.variant, otherBackroom); n != 3 {
		t.Fatalf("receiver beer %d", n)
	}
	if n := f.onHand(t, f.vodka.variant, otherBackroom); n != 1 {
		t.Fatalf("receiver vodka %d", n)
	}
	if m, _ := f.marks.Find(ctx, "PKN0000000000002"); m == nil || m.Status != "in_transit" {
		t.Fatalf("the missing bottle stays in transit: %+v", m)
	}
	f.integrity(t)

	if _, err := f.db.App.Exec(ctx, `update egais_messages set status = 'accepted' where document_id = $1`, receipt); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, f.db.App, func(tx pgx.Tx) error {
		return f.svc.ApplyTicket(ctx, tx, receipt, true)
	}); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.svc.Get(f.keeper, doc); d.Doc.Status != documents.StatusPosted {
		t.Fatalf("transfer after the act: %s", d.Doc.Status)
	}
}
