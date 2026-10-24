package sim_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ADEXITUM/pikni/govsim/internal/sim"
	. "github.com/ADEXITUM/pikni/govsim/internal/simtest"
)

func waybill(t *testing.T, s *sim.Sim, fsrar string) (sim.Message, sim.TTN) {
	t.Helper()
	msgs, err := s.Outbox(t.Context(), fsrar)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Type == "WayBill" {
			var ttn sim.TTN
			if err := json.Unmarshal(m.Payload, &ttn); err != nil {
				t.Fatal(err)
			}
			return m, ttn
		}
	}
	t.Fatalf("no WayBill in outbox of %s: %+v", fsrar, msgs)
	return sim.Message{}, sim.TTN{}
}

func ticket(t *testing.T, s *sim.Sim, fsrar, reply string) sim.Ticket {
	t.Helper()
	msgs, err := s.Outbox(t.Context(), fsrar)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		var tk sim.Ticket
		if m.Type == "Ticket" && json.Unmarshal(m.Payload, &tk) == nil && tk.ReplyID == reply {
			return tk
		}
	}
	t.Fatalf("no ticket for %s", reply)
	return sim.Ticket{}
}

func send(t *testing.T, s *sim.Sim, fsrar, kind string, v any) string {
	t.Helper()
	data, _ := json.Marshal(v)
	id, err := s.Receive(t.Context(), fsrar, kind, data)
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func TestShipmentAndAct(t *testing.T) {
	t.Parallel()
	s := New(t)
	ctx := t.Context()

	sh, err := s.CreateShipment(ctx, sim.ShipmentRequest{
		Supplier: Supplier, Recipient: Store, BoxSize: 6, Shortage: 1, ForeignMark: true,
		Positions: []sim.Item{{Alcocode: Vodka, Qty: 12}, {Alcocode: Beer, Qty: 6}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, ttn := waybill(t, s, Store)
	if ttn.ID != sh.TTNID || len(ttn.Positions) != 2 || len(ttn.Positions[0].Marks) != 12 || len(ttn.Packages) != 3 {
		t.Fatalf("ttn = %+v", ttn)
	}
	// В ТТН всё «правильно», а в коробах недостача и чужая марка.
	info, err := s.Shipment(ctx, sh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(info.Print[1].Units); n != 5 {
		t.Errorf("second vodka box holds %d units, want 5", n)
	}
	if info.Print[0].Units[0].Mark == ttn.Positions[0].Marks[0] {
		t.Error("foreign mark was not placed")
	}
	if info.Print[2].Units[0].Crpt == "" {
		t.Error("beer has no Chestny Znak code")
	}
	if m, _ := s.Mark(ctx, ttn.Positions[0].Marks[0]); m.Status != "in_transit" || m.Owner != Supplier {
		t.Errorf("mark in transit = %+v", m)
	}

	// Акт расхождения: принято 10 марок водки и 5 банок пива.
	accepted := ttn.Positions[0].Marks[1:11]
	act := sim.WayBillAct{TTNID: ttn.ID, Positions: []sim.ActPosition{
		{Identity: 1, QtyAccepted: 10, Marks: accepted},
		{Identity: 2, QtyAccepted: 5},
	}}
	reply := send(t, s, Store, "WayBillAct", act)
	if tk := ticket(t, s, Store, reply); tk.Result != "accepted" {
		t.Fatalf("act ticket = %+v", tk)
	}
	if m, _ := s.Mark(ctx, accepted[0]); m.Status != "on_balance" || m.Owner != Store {
		t.Errorf("accepted mark = %+v", m)
	}
	if m, _ := s.Mark(ctx, ttn.Positions[0].Marks[0]); m.Owner != Supplier || m.Status != "on_balance" {
		t.Errorf("not accepted mark goes back to the supplier: %+v", m)
	}
	rests, marks, _ := s.Rests(ctx, Store)
	if len(marks) != 10 || len(rests) != 1 || rests[0].Qty != 5 || rests[0].Register != 1 {
		t.Errorf("rests = %+v, marks %d", rests, len(marks))
	}
	// Второй акт по той же ТТН отклоняется, как и марка не из ТТН.
	if tk := ticket(t, s, Store, send(t, s, Store, "WayBillAct", act)); tk.Result != "rejected" {
		t.Errorf("second act = %+v", tk)
	}
	if tk := ticket(t, s, Store2, send(t, s, Store2, "WayBillAct", act)); tk.Result != "rejected" {
		t.Errorf("act from another point = %+v", tk)
	}

	// Коды «Честного знака»: пять банок теперь у магазина, шестая — у поставщика.
	codes := make([]string, 0, 6)
	for _, u := range info.Print[2].Units {
		codes = append(codes, u.Crpt[2:16]+u.Crpt[18:25])
	}
	verdicts, err := s.CheckCodes(ctx, Store, codes)
	if err != nil {
		t.Fatal(err)
	}
	allowed := 0
	for _, v := range verdicts {
		if v.Allowed {
			allowed++
		} else if v.Reason != "code.not_owner" {
			t.Errorf("verdict %+v", v)
		}
	}
	if allowed != 5 {
		t.Errorf("allowed %d codes, want 5", allowed)
	}
}

func TestTransferAndRests(t *testing.T) {
	t.Parallel()
	s := New(t)
	ctx := t.Context()
	if err := s.SetRest(ctx, Store, Beer, 1, 10); err != nil {
		t.Fatal(err)
	}
	type items struct {
		Items []sim.Item `json:"items"`
	}
	if tk := ticket(t, s, Store, send(t, s, Store, "TransferToShop", items{[]sim.Item{{Alcocode: Beer, Qty: 11}}})); tk.Result != "rejected" {
		t.Errorf("transfer over the register: %+v", tk)
	}
	if tk := ticket(t, s, Store, send(t, s, Store, "TransferToShop", items{[]sim.Item{{Alcocode: Beer, Qty: 4}}})); tk.Result != "accepted" {
		t.Fatalf("transfer: %+v", tk)
	}
	reply := send(t, s, Store, "QueryRests", struct{}{})
	msgs, _ := s.Outbox(ctx, Store)
	found := false
	for _, m := range msgs {
		if m.Type != "ReplyRests" {
			continue
		}
		var r struct {
			ReplyID string     `json:"reply_id"`
			Rests   []sim.Rest `json:"rests"`
		}
		_ = json.Unmarshal(m.Payload, &r)
		if r.ReplyID == reply && len(r.Rests) == 2 && r.Rests[0].Qty == 6 && r.Rests[1].Qty == 4 {
			found = true
		}
	}
	if !found {
		t.Errorf("no correct ReplyRests in %+v", msgs)
	}
}

func TestUTMDownAndDelay(t *testing.T) {
	t.Parallel()
	s := New(t)
	ctx := t.Context()
	if err := s.SetScenario(ctx, Store, true, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Receive(ctx, Store, "QueryRests", []byte(`{}`)); !errors.Is(err, sim.ErrUTMDown) {
		t.Errorf("receive with UTM down: %v", err)
	}
	if _, err := s.Outbox(ctx, Store); !errors.Is(err, sim.ErrUTMDown) {
		t.Errorf("outbox with UTM down: %v", err)
	}
	// С задержкой квитанция появляется не сразу.
	if err := s.SetScenario(ctx, Store, false, 600); err != nil {
		t.Fatal(err)
	}
	send(t, s, Store, "QueryRests", struct{}{})
	if msgs, _ := s.Outbox(ctx, Store); len(msgs) != 0 {
		t.Errorf("delayed messages are visible: %+v", msgs)
	}
}

func TestCodesLifecycle(t *testing.T) {
	t.Parallel()
	s := New(t)
	ctx := t.Context()
	sh, err := s.CreateShipment(ctx, sim.ShipmentRequest{Supplier: Supplier, Recipient: Store, Positions: []sim.Item{{Alcocode: Beer, Qty: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	_, ttn := waybill(t, s, Store)
	send(t, s, Store, "WayBillAct", sim.WayBillAct{TTNID: ttn.ID, Positions: []sim.ActPosition{{Identity: 1, QtyAccepted: 2}}})
	info, _ := s.Shipment(ctx, sh.ID)
	code := func(i int) string { c := info.Print[0].Units[i].Crpt; return c[2:16] + c[18:25] }

	v, _ := s.CheckCodes(ctx, Store, []string{code(0), "04600000000000XXXXXXX"})
	if !v[0].Allowed || v[1].Reason != "code.unknown" {
		t.Fatalf("verdicts = %+v", v)
	}
	if err := s.SellCodes(ctx, Store, []string{code(0)}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.CheckCodes(ctx, Store, []string{code(0)}); v[0].Reason != "code.sold" {
		t.Errorf("sold code: %+v", v)
	}
	batches, _ := s.Batches(ctx)
	if err := s.ExpireBatch(ctx, batches[0].Batch); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.CheckCodes(ctx, Store, []string{code(1)}); v[0].Reason != "code.expired" {
		t.Errorf("expired code: %+v", v)
	}
	if err := s.BlockBatch(ctx, batches[0].Batch, "отзыв"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.CheckCodes(ctx, Store, []string{code(1)}); v[0].Reason != "code.blocked" {
		t.Errorf("blocked code: %+v", v)
	}
}

func TestChequeAndShopWriteOff(t *testing.T) {
	t.Parallel()
	s := New(t)
	ctx := t.Context()
	if _, err := s.CreateShipment(ctx, sim.ShipmentRequest{Supplier: Supplier, Recipient: Store,
		Positions: []sim.Item{{Alcocode: Vodka, Qty: 2}, {Alcocode: Beer, Qty: 4}}}); err != nil {
		t.Fatal(err)
	}
	_, ttn := waybill(t, s, Store)
	marks := ttn.Positions[0].Marks
	send(t, s, Store, "WayBillAct", sim.WayBillAct{TTNID: ttn.ID, Positions: []sim.ActPosition{
		{Identity: 1, QtyAccepted: 2, Marks: marks}, {Identity: 2, QtyAccepted: 4},
	}})

	sale := sim.ChequeRequest{Kind: "sale", ReceiptID: "r1", Marks: marks[:1]}
	if r, err := s.Cheque(ctx, Store, "http://sim", sale); err != nil || r.Result != "accepted" || r.QRURL != "http://sim/check/r1" {
		t.Fatalf("cheque: %+v %v", r, err)
	}
	// Повтор того же чека — прежний ответ; другой чек с той же маркой — отказ.
	if r, _ := s.Cheque(ctx, Store, "http://sim", sale); r.Result != "accepted" {
		t.Errorf("repeated cheque: %+v", r)
	}
	if r, _ := s.Cheque(ctx, Store, "http://sim", sim.ChequeRequest{Kind: "sale", ReceiptID: "r2", Marks: marks[:1]}); r.Reason != "mark.already_sold" {
		t.Errorf("second sale of the mark: %+v", r)
	}
	if r, _ := s.Cheque(ctx, Store2, "http://sim", sim.ChequeRequest{Kind: "sale", ReceiptID: "r3", Marks: marks[1:]}); r.Reason != "mark.not_on_balance" {
		t.Errorf("mark of another point: %+v", r)
	}
	if r, _ := s.Cheque(ctx, Store, "http://sim", sim.ChequeRequest{Kind: "return", ReceiptID: "r4", Marks: marks[:1]}); r.Result != "accepted" {
		t.Errorf("return: %+v", r)
	}
	if m, _ := s.Mark(ctx, marks[0]); m.Status != "on_balance" {
		t.Errorf("returned mark: %+v", m)
	}

	type items struct {
		Items []sim.Item `json:"items"`
	}
	send(t, s, Store, "TransferToShop", items{[]sim.Item{{Alcocode: Beer, Qty: 3}}})
	if tk := ticket(t, s, Store, send(t, s, Store, "ActWriteOffShop", items{[]sim.Item{{Alcocode: Beer, Qty: 4}}})); tk.Result != "rejected" {
		t.Errorf("write-off over register 2: %+v", tk)
	}
	if tk := ticket(t, s, Store, send(t, s, Store, "ActWriteOffShop", items{[]sim.Item{{Alcocode: Beer, Qty: 2}}})); tk.Result != "accepted" {
		t.Errorf("write-off: %+v", tk)
	}
}

func TestCigarettePacks(t *testing.T) {
	t.Parallel()
	s := New(t)
	ctx := t.Context()
	if _, err := s.CreatePacks(ctx, Store, "4601234567893", 21900, 1); !errors.Is(err, sim.ErrBadPacks) {
		t.Fatalf("13-digit GTIN: %v", err)
	}
	codes, err := s.CreatePacks(ctx, Store, "04601234567893", 21900, 3)
	if err != nil {
		t.Fatal(err)
	}
	// 219 ₽ = 21900 коп. = 3·80² + 33·80 + 60 → «D», «h», «8» после ведущего «A».
	for _, c := range codes {
		if len(c) != 29 || c[21:25] != "ADh8" {
			t.Fatalf("pack code %q", c)
		}
	}
	v, err := s.CheckCodes(ctx, Store, []string{codes[0][:21]})
	if err != nil || !v[0].Allowed {
		t.Fatalf("pack is introduced at the store: %+v %v", v, err)
	}
}
