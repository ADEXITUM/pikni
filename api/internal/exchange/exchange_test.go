package exchange_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/exchange"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

const (
	storeFSRAR    = "030000100011"
	supplierFSRAR = "030000200001"
	vodkaCode     = "0310000000000000102"
	beerCode      = "0310000000000000132"
)

// fakeUTM — УТМ в памяти: «исходящие» по точкам, принятые документы и выключатель.
type fakeUTM struct {
	mu   sync.Mutex
	out  map[string][]egais.Inbound
	sent []sent
	down bool
}

type sent struct {
	fsrar, docType, reply string
	payload               []byte
}

func (f *fakeUTM) Send(_ context.Context, fsrar, docType string, payload []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return "", egais.ErrUnavailable
	}
	reply := uuid.NewString()
	f.sent = append(f.sent, sent{fsrar: fsrar, docType: docType, reply: reply, payload: payload})
	return reply, nil
}

func (f *fakeUTM) Inbox(_ context.Context, fsrar string) ([]egais.Inbound, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return nil, egais.ErrUnavailable
	}
	return append([]egais.Inbound(nil), f.out[fsrar]...), nil
}

func (f *fakeUTM) Ack(_ context.Context, fsrar, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, m := range f.out[fsrar] {
		if m.ID == id {
			f.out[fsrar] = append(f.out[fsrar][:i], f.out[fsrar][i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeUTM) Cheque(context.Context, string, egais.Cheque) (egais.ChequeReply, error) {
	return egais.ChequeReply{Result: "accepted"}, nil
}

func (f *fakeUTM) put(fsrar, kind string, v any) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, _ := json.Marshal(v)
	id := uuid.NewString()
	f.out[fsrar] = append(f.out[fsrar], egais.Inbound{ID: id, Type: kind, Payload: data})
	return id
}

type env struct {
	db     *dbtest.DB
	utm    *fakeUTM
	ex     *exchange.Exchanger
	docs   *documents.Service
	keeper context.Context
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var store, user uuid.UUID
	must(db.App.QueryRow(ctx, `insert into locations (code, name, kind, city, timezone, subject_code, fsrar_id)
		values ('МГ-01', 'Магазин', 'store', 'Благовещенск', 'Asia/Yakutsk', '28', $1) returning id`, storeFSRAR).Scan(&store))
	must(db.App.QueryRow(ctx, `insert into users (login, full_name, role, password_hash)
		values ('keeper', 'Кладовщик', 'storekeeper', 'x') returning id`).Scan(&user))
	_, err := db.App.Exec(ctx, `insert into suppliers (name, inn, kpp, fsrar_id, default_receiving_mode)
		values ('ООО «Амур-Дистрибуция»', '2801001056', '280101004', $1, 'boxes')`, supplierFSRAR)
	must(err)

	cat := catalog.NewService(db.App)
	producer, err := cat.SaveProducer(ctx, nil, apispec.ProducerFields{Name: "ООО «Тайга»", CountryCode: "643"}, true)
	must(err)
	category := func(name string) uuid.UUID {
		var id uuid.UUID
		must(db.App.QueryRow(ctx, `select id from categories where name = $1`, name).Scan(&id))
		return id
	}
	add := func(name, cat_, kind string, abv float64, alcocode, barcode string) {
		p, err := cat.CreateProduct(ctx, apispec.ProductFields{
			Name: name, CategoryID: category(cat_), KindCode: &kind, Abv: &abv, ProducerID: producer.ID, CountryCode: "643",
		})
		must(err)
		vol := int32(500)
		v, err := cat.CreateVariant(ctx, p.ID, catalog.VariantSpec{VolumeMl: &vol, Alcocode: &alcocode})
		must(err)
		_, err = cat.AddBarcode(ctx, v.ID, &barcode)
		must(err)
	}
	add("Тайга", "Водка", "200", 40, vodkaCode, "4600000000015")
	add("Амурское", "Пиво и сидр", "500", 4.5, beerCode, "4600000000022")

	utm := &fakeUTM{out: map[string][]egais.Inbound{}}
	docs := documents.NewService(db.App, cat, marks.NewService(db.App))
	return &env{
		db: db, utm: utm, docs: docs,
		ex:     exchange.New(db.App, utm, docs, slog.New(slog.DiscardHandler)),
		keeper: auth.WithSession(ctx, auth.Session{UserID: user, Role: "storekeeper", LocationIDs: []uuid.UUID{store}}),
	}
}

func (e *env) once(t *testing.T) {
	t.Helper()
	if err := e.ex.Once(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func (e *env) scan(t *testing.T, doc uuid.UUID, raw string) documents.ScanResult {
	t.Helper()
	res, err := e.docs.AddScan(e.keeper, doc, documents.NewScan{ID: uuid.New(), Raw: raw, Source: "manual", ScannedAt: time.Now()})
	if err != nil {
		t.Fatalf("scan %s: %v", raw, err)
	}
	return res
}

// ttn — поставка: 12 бутылок водки в двух коробах по 6 и короб пива на 6 банок.
func ttn(id string) egais.TTN {
	t := egais.TTN{
		ID: id, Number: "ТТН-000001", Date: "2026-10-24", Consignee: storeFSRAR,
		Shipper: egais.Party{FSRARID: supplierFSRAR, Name: "ООО «Амур-Дистрибуция»"},
	}
	vodka := egais.TTNPosition{Identity: 1, Alcocode: vodkaCode, Name: "Водка «Тайга»", Qty: 12, Price: 290, Marked: true}
	for i := range 12 {
		vodka.Marks = append(vodka.Marks, fmt.Sprintf("PKN%013d", i+1))
	}
	t.Positions = []egais.TTNPosition{vodka, {Identity: 2, Alcocode: beerCode, Name: "Пиво «Амурское»", Qty: 6, Price: 52}}
	t.Packages = []egais.TTNPackage{
		{Code: "046000000000000017", Identity: 1, Marks: vodka.Marks[:6], Qty: 6},
		{Code: "046000000000000024", Identity: 1, Marks: vodka.Marks[6:], Qty: 6},
		{Code: "046000000000000031", Identity: 2, Qty: 6},
	}
	return t
}

func TestReceiptThroughEgais(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()

	e.utm.put(storeFSRAR, egais.WayBill, ttn("ttn-1"))
	first := e.utm.out[storeFSRAR][0]
	e.once(t)
	// Повторно полученная ТТН (например, после сбоя подтверждения) не создаёт второй документ.
	e.utm.out[storeFSRAR] = append(e.utm.out[storeFSRAR], first)
	e.once(t)

	var doc uuid.UUID
	var count int
	if err := e.db.App.QueryRow(ctx, `select max(id::text)::uuid, count(*) from documents where kind = 'receipt'`).Scan(&doc, &count); err != nil || count != 1 {
		t.Fatalf("receipts: %d, %v", count, err)
	}
	d, err := e.docs.Get(e.keeper, doc)
	if err != nil {
		t.Fatal(err)
	}
	// У поставщика режим «по коробам», но для водки с марками — не ниже «выборочно».
	if d.Receipt == nil || d.Receipt.Mode != documents.ModeSample || !d.Receipt.Packages[0].ToOpen || d.Receipt.Packages[1].ToOpen {
		t.Fatalf("receipt state: %+v", d.Receipt)
	}

	// Вскрываемый короб: скан короба не засчитывает содержимое, марки сканируются поштучно —
	// одной не хватает. Второй короб и пиво засчитываются по коробам; одна марка чужая.
	if res := e.scan(t, doc, "00046000000000000017"); res.Scan.Status != documents.ScanAccepted || res.Scan.Reason == nil {
		t.Fatalf("box to open: %+v", res.Scan)
	}
	for i := range 5 {
		e.scan(t, doc, fmt.Sprintf("PKN%013d", i+1))
	}
	e.scan(t, doc, "046000000000000024")
	if res := e.scan(t, doc, fmt.Sprintf("PKN%013d", 8)); res.Scan.Status != documents.ScanDuplicate {
		t.Errorf("mark of a counted box: %+v", res.Scan)
	}
	if res := e.scan(t, doc, "PKN9999999999999"); res.Scan.Status != documents.ScanForeign {
		t.Errorf("foreign mark: %+v", res.Scan)
	}
	e.scan(t, doc, "046000000000000031")

	d, _ = e.docs.Get(e.keeper, doc)
	st := d.Receipt
	if st.Lines[0].Fact != 11 || st.Lines[1].Fact != 6 || st.Shortage != 1 || st.Foreign != 1 || !st.Differences() {
		t.Fatalf("plan/fact: %+v", st)
	}

	// Акт уходит при проведении; пока УТМ выключен, сообщение ждёт в очереди.
	if err := e.docs.Post(e.keeper, doc, d.Doc.Version); err != nil {
		t.Fatal(err)
	}
	e.utm.down = true
	e.once(t)
	var attempts int
	var lastError *string
	if err := e.db.App.QueryRow(ctx, `select attempts, last_error from egais_messages where document_id = $1 and direction = 'out'`, doc).
		Scan(&attempts, &lastError); err != nil || attempts != 1 || lastError == nil {
		t.Fatalf("queued while UTM is down: %d %v %v", attempts, lastError, err)
	}
	e.utm.down = false
	if _, err := e.db.App.Exec(ctx, `update egais_messages set next_attempt_at = now()`); err != nil {
		t.Fatal(err)
	}
	e.once(t)
	if len(e.utm.sent) != 1 || e.utm.sent[0].docType != egais.WayBillAct {
		t.Fatalf("sent: %+v", e.utm.sent)
	}
	var act egais.Act
	_ = json.Unmarshal(e.utm.sent[0].payload, &act)
	if act.TTNID != "ttn-1" || act.Positions[0].QtyAccepted != 11 || len(act.Positions[0].Marks) != 11 || act.Positions[1].QtyAccepted != 6 {
		t.Fatalf("act: %+v", act)
	}
	if d, _ := e.docs.Get(e.keeper, doc); d.Doc.Status != documents.StatusAwaitingEgais {
		t.Errorf("status before ticket: %s", d.Doc.Status)
	}

	e.utm.put(storeFSRAR, egais.Ticket, egais.TicketReply{ReplyID: e.utm.sent[0].reply, DocType: egais.WayBillAct, Result: "accepted"})
	e.once(t)
	d, _ = e.docs.Get(e.keeper, doc)
	// В журнале обмена: пришедшая ТТН, отправленный акт и квитанция.
	if d.Doc.Status != documents.StatusPosted || len(d.Exchange) != 3 {
		t.Fatalf("after ticket: %s, exchange %+v", d.Doc.Status, d.Exchange)
	}
	var vodka, beer, inStock int
	_ = e.db.App.QueryRow(ctx, `select coalesce(sum(on_hand) filter (where v.alcocode = $1), 0), coalesce(sum(on_hand) filter (where v.alcocode = $2), 0)
		from stock_balances b join variants v on v.id = b.variant_id`, vodkaCode, beerCode).Scan(&vodka, &beer)
	_ = e.db.App.QueryRow(ctx, `select count(*) from excise_marks where status = 'in_stock'`).Scan(&inStock)
	if vodka != 11 || beer != 6 || inStock != 11 {
		t.Errorf("stock: vodka %d, beer %d, marks %d", vodka, beer, inStock)
	}
}

func TestRejectedTicket(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.utm.put(storeFSRAR, egais.WayBill, ttn("ttn-2"))
	e.once(t)
	var doc uuid.UUID
	if err := e.db.App.QueryRow(t.Context(), `select id from documents where kind = 'receipt'`).Scan(&doc); err != nil {
		t.Fatal(err)
	}
	if err := e.docs.SetMode(e.keeper, doc, documents.ModeBoxes); err == nil {
		t.Error("boxes mode for marked goods must be refused")
	}
	if err := e.docs.SetMode(e.keeper, doc, documents.ModePiece); err != nil {
		t.Fatal(err)
	}
	if res := e.scan(t, doc, "046000000000000017"); res.Scan.Status != documents.ScanRejected {
		t.Errorf("box in piece mode: %+v", res.Scan)
	}
	e.scan(t, doc, "PKN0000000000001")
	d, _ := e.docs.Get(e.keeper, doc)
	if err := e.docs.Post(e.keeper, doc, d.Doc.Version); err != nil {
		t.Fatal(err)
	}
	e.once(t)
	e.utm.put(storeFSRAR, egais.Ticket, egais.TicketReply{ReplyID: e.utm.sent[0].reply, Result: "rejected", Reason: "ТТН уже подтверждена"})
	e.once(t)
	d, _ = e.docs.Get(e.keeper, doc)
	if d.Doc.Status != documents.StatusEgaisRejected || d.Exchange[1].Reason != "ТТН уже подтверждена" {
		t.Errorf("rejected: %s %+v", d.Doc.Status, d.Exchange)
	}
}
