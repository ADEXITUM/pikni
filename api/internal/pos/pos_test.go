package pos

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/crpt"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/locations"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/stock"
)

const (
	storeFSRAR = "030000100011"
	// Марки старого образца с алкокодом 0177207000002463655; отличаются последними знаками.
	mark1 = "20N00001CGUMZYCB99J1NKN31105001000056NQQMS5VP4HTF5SB46ZSQQJD8BNJP891"
	mark2 = "20N00001CGUMZYCB99J1NKN31105001000056NQQMS5VP4HTF5SB46ZSQQJD8BNJP892"
)

// Среда, 14:00 в Благовещенске: продажа алкоголя в Амурской области разрешена с 11 до 21.
var daytime = time.Date(2026, 10, 21, 14, 0, 0, 0, time.FixedZone("+09", 9*3600))

type fakeUTM struct {
	mu      sync.Mutex
	cheques []egais.Cheque
	down    bool
	reject  string
}

func (f *fakeUTM) Send(context.Context, string, string, []byte) (string, error) {
	return uuid.NewString(), nil
}
func (f *fakeUTM) Inbox(context.Context, string) ([]egais.Inbound, error) { return nil, nil }
func (f *fakeUTM) Ack(context.Context, string, string) error              { return nil }

func (f *fakeUTM) Cheque(_ context.Context, _ string, c egais.Cheque) (egais.ChequeReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return egais.ChequeReply{}, egais.ErrUnavailable
	}
	if f.reject != "" {
		return egais.ChequeReply{Result: "rejected", Reason: f.reject}, nil
	}
	f.cheques = append(f.cheques, c)
	return egais.ChequeReply{Result: "accepted", QRURL: "https://check.egais.ru/" + c.ReceiptID}, nil
}

type fakeCRPT struct{}

func (fakeCRPT) Check(_ context.Context, _ string, codes []string) ([]crpt.Verdict, error) {
	out := make([]crpt.Verdict, len(codes))
	for i, c := range codes {
		out[i] = crpt.Verdict{Code: c, Allowed: true}
	}
	return out, nil
}
func (fakeCRPT) Sell(context.Context, string, []string) error   { return nil }
func (fakeCRPT) Return(context.Context, string, []string) error { return nil }

type fixture struct {
	db      *dbtest.DB
	svc     *Service
	docs    *documents.Service
	stock   *stock.Service
	utm     *fakeUTM
	store   uuid.UUID
	floor   uuid.UUID
	key     string
	cashier uuid.UUID

	vodka, beer, snack, cigarettes uuid.UUID
	admin                          context.Context
}

func ean(body string) string {
	sum := 0
	for i := len(body) - 1; i >= 0; i-- {
		d := int(body[i] - '0')
		if (len(body)-1-i)%2 == 0 {
			d *= 3
		}
		sum += d
	}
	return fmt.Sprintf("%s%d", body, (10-sum%10)%10)
}

func ptr[T any](v T) *T { return &v }

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.New(t)
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	cat := catalog.NewService(db.App)
	mk := marks.NewService(db.App)
	f := &fixture{db: db, utm: &fakeUTM{}, docs: documents.NewService(db.App, cat, mk)}
	f.stock = stock.NewService(db.App, mk)
	f.svc = NewService(db.App, cat, mk, f.docs, locations.NewService(db.App), f.utm, fakeCRPT{})
	f.svc.now = func() time.Time { return daytime }

	must(db.App.QueryRow(ctx, `insert into locations (code, name, kind, city, timezone, subject_code, fsrar_id)
		values ('МГ-01', 'Магазин', 'store', 'Благовещенск', 'Asia/Yakutsk', '28', $1) returning id`, storeFSRAR).Scan(&f.store))
	must(db.App.QueryRow(ctx, `select id from zones where location_id = $1 and kind = 'floor'`, f.store).Scan(&f.floor))
	var backroom uuid.UUID
	must(db.App.QueryRow(ctx, `select id from zones where location_id = $1 and kind = 'backroom'`, f.store).Scan(&backroom))

	admin := f.session(t, "admin")
	f.admin = admin
	keeper := f.session(t, "storekeeper", f.store)
	manager := f.session(t, "manager")

	producer, err := cat.SaveProducer(admin, nil, apispec.ProducerFields{Name: "ООО «Тайга»", CountryCode: "643"}, true)
	must(err)
	category := func(name string) uuid.UUID {
		var id uuid.UUID
		must(db.App.QueryRow(ctx, `select id from categories where name = $1`, name).Scan(&id))
		return id
	}
	add := func(fields apispec.ProductFields, spec catalog.VariantSpec, code string, purchase, retail int64) uuid.UUID {
		t.Helper()
		fields.ProducerID, fields.CountryCode = producer.ID, "643"
		p, err := cat.CreateProduct(admin, fields)
		must(err)
		v, err := cat.CreateVariant(admin, p.ID, spec)
		must(err)
		_, err = cat.AddBarcode(admin, v.ID, &code)
		must(err)
		if purchase > 0 {
			_, err = cat.SetPrice(admin, v.ID, apispec.PriceKindPurchase, purchase, nil, nil)
			must(err)
		}
		_, err = cat.SetPrice(admin, v.ID, apispec.PriceKindRetail, retail, nil, nil)
		must(err)
		return v.ID
	}
	f.vodka = add(apispec.ProductFields{Name: "Тайга", CategoryID: category("Водка"), KindCode: ptr("200"), Abv: ptr(40.0)},
		catalog.VariantSpec{VolumeMl: ptr(int32(500)), Alcocode: ptr("0177207000002463655")}, ean("460000000001"), 35000, 59900)
	f.beer = add(apispec.ProductFields{Name: "Амурское", CategoryID: category("Пиво и сидр"), KindCode: ptr("500"), Abv: ptr(4.5)},
		catalog.VariantSpec{VolumeMl: ptr(int32(500)), Alcocode: ptr("0310000000000000132")}, ean("460000000002"), 6000, 11900)
	f.snack = add(apispec.ProductFields{Name: "Сухарики", CategoryID: category("Снеки")},
		catalog.VariantSpec{NetWeightG: ptr(int32(70))}, ean("460000000003"), 3000, 7900)
	f.cigarettes = add(apispec.ProductFields{Name: "Северный ветер", CategoryID: category("Сигареты"), CrptMarked: true},
		catalog.VariantSpec{GTIN: ptr("04601234567893")}, ean("460000000004"), 15000, 25000)
	_, err = cat.CreateMinPriceRule(admin, catalog.MinPriceRuleSpec{
		GroupCode: "vodka", ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), AbvFrom: 0, Kopecks: 34900, VolumeMl: 500,
		Source: "приказ Минфина",
	})
	must(err)

	// Остатки: две бутылки водки по маркам и пиво — на складе магазина; снеков нет.
	doc, err := f.docs.CreateOpening(keeper, backroom, "")
	must(err)
	for _, raw := range []string{ean("460000000001"), mark1, mark2} {
		_, err := f.docs.AddScan(keeper, doc, documents.NewScan{ID: uuid.New(), Raw: raw, Source: "manual", ScannedAt: time.Now(), VariantID: &f.vodka})
		must(err)
	}
	must(f.docs.SetLine(keeper, doc, f.beer, 10))
	d, err := f.docs.Get(manager, doc)
	must(err)
	must(f.docs.Post(manager, doc, d.Doc.Version))

	_, f.key, err = f.svc.RegisterDevice(admin, f.store, "Касса 1")
	must(err)
	f.cashier = f.user(t, "kassir", "cashier", f.store)
	must(f.svc.SetPIN(admin, f.cashier, "1234"))
	return f
}

func (f *fixture) session(t *testing.T, role string, locs ...uuid.UUID) context.Context {
	t.Helper()
	id := f.user(t, role+"-"+uuid.NewString()[:8], role, locs...)
	return auth.WithSession(t.Context(), auth.Session{UserID: id, Role: role, LocationIDs: locs})
}

func (f *fixture) user(t *testing.T, login, role string, locs ...uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := f.db.App.QueryRow(t.Context(), `insert into users (login, full_name, role, password_hash)
		values ($1, 'Сотрудник', $2, 'x') returning id`, login, role).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range locs {
		if _, err := f.db.App.Exec(t.Context(), `insert into user_locations (user_id, location_id) values ($1, $2)`, id, l); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// till входит кассиром и открывает смену.
func (f *fixture) till(t *testing.T) Context {
	t.Helper()
	ctx := t.Context()
	d, err := f.svc.Device(ctx, "Device "+f.key)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := f.svc.Login(ctx, d, "kassir", "1234")
	if err != nil {
		t.Fatal(err)
	}
	pc, err := f.svc.Authenticate(ctx, d, sess.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.OpenShift(ctx, pc); err != nil {
		t.Fatal(err)
	}
	return pc
}

func (f *fixture) onHand(t *testing.T, variant uuid.UUID) (onHand, deficit int32) {
	t.Helper()
	err := f.db.App.QueryRow(t.Context(), `select coalesce(sum(on_hand), 0)::integer, coalesce(sum(deficit), 0)::integer
		from stock_balances b join zones z on z.id = b.zone_id where b.variant_id = $1 and z.location_id = $2`,
		variant, f.store).Scan(&onHand, &deficit)
	if err != nil {
		t.Fatal(err)
	}
	return onHand, deficit
}

func (f *fixture) markStatus(t *testing.T, code string) string {
	t.Helper()
	var s string
	if err := f.db.App.QueryRow(t.Context(), `select status from excise_marks where code = $1`, code).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *fixture) integrity(t *testing.T) {
	t.Helper()
	problems, err := f.stock.Integrity(t.Context())
	if err != nil || len(problems) > 0 {
		t.Fatalf("integrity: %+v, %v", problems, err)
	}
}

func refusalCode(err error) string {
	if re, ok := errors.AsType[*RefusalError](err); ok {
		return re.Code
	}
	return ""
}

func vodkaLine(f *fixture, mark string) NewLine {
	return NewLine{VariantID: f.vodka, Qty: 1, Price: 59900, Mark: mark}
}

func TestLogin(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	d, err := f.svc.Device(ctx, "Device "+f.key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Device(ctx, "Device "+f.key+"x"); !errors.Is(err, ErrDeviceKey) {
		t.Fatalf("forged key: %v", err)
	}

	f.user(t, "keeper", "storekeeper", f.store)
	f.svc.now = time.Now
	if err := f.svc.SetPIN(f.session(t, "admin"), f.cashier, "12a4"); !errors.Is(err, ErrInvalidPIN) {
		t.Fatalf("letters in PIN: %v", err)
	}
	for range 5 {
		if _, err := f.svc.Login(ctx, d, "kassir", "0000"); !errors.Is(err, ErrBadPIN) {
			t.Fatalf("wrong PIN: %v", err)
		}
	}
	if _, err := f.svc.Login(ctx, d, "kassir", "1234"); !errors.Is(err, ErrLocked) {
		t.Fatalf("after five failures the cashier is locked, got %v", err)
	}

	other := f.user(t, "kassir2", "cashier")
	if err := f.svc.SetPIN(f.session(t, "admin"), other, "4321"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Login(ctx, d, "kassir2", "4321"); !errors.Is(err, ErrNotCashier) {
		t.Fatalf("cashier of another store: %v", err)
	}

	devices, err := f.svc.Devices(f.admin)
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices: %v %v", devices, err)
	}
	if err := f.svc.RevokeDevice(f.session(t, "admin"), devices[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Device(ctx, "Device "+f.key); !errors.Is(err, ErrDeviceKey) {
		t.Fatalf("revoked till still works: %v", err)
	}
}

func TestSaleAndShift(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	pc := f.till(t)

	c, err := f.svc.Check(ctx, pc, ean("460000000001"))
	if err != nil || c.Item == nil || len(c.Refusals) != 1 || c.Refusals[0].Code != "mark.required" {
		t.Fatalf("vodka by EAN needs a mark: %+v %v", c, err)
	}
	c, err = f.svc.Check(ctx, pc, mark1)
	if err != nil || c.Item == nil || c.Item.ID != f.vodka || len(c.Refusals) != 0 || c.Mark != mark1 {
		t.Fatalf("vodka by mark: %+v %v", c, err)
	}
	if c.Item.MinPrice == nil || *c.Item.MinPrice != 34900 {
		t.Fatalf("min price: %v", c.Item.MinPrice)
	}

	sale := NewReceipt{ID: uuid.New(), CreatedAt: daytime, Lines: []NewLine{vodkaLine(f, mark1), {VariantID: f.beer, Qty: 3, Price: 11900}}}
	if _, err := f.svc.CreateReceipt(ctx, pc, sale); refusalCode(err) != "sale.age" {
		t.Fatalf("alcohol without age check: %v", err)
	}
	cheap := sale
	cheap.ID, cheap.AgeConfirmed, cheap.Lines = uuid.New(), true, []NewLine{{VariantID: f.vodka, Qty: 1, Price: 30000, Mark: mark1}}
	if _, err := f.svc.CreateReceipt(ctx, pc, cheap); refusalCode(err) != "price.below_min" {
		t.Fatalf("price below min: %v", err)
	}

	sale.AgeConfirmed = true
	if _, err := f.svc.CreateReceipt(ctx, pc, sale); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateReceipt(ctx, pc, sale); err != nil {
		t.Fatalf("resend: %v", err)
	}
	if len(f.utm.cheques) != 1 || f.utm.cheques[0].Marks[0] != mark1 {
		t.Fatalf("one cheque with the mark goes to ЕГАИС: %+v", f.utm.cheques)
	}
	if s := f.markStatus(t, mark1); s != marks.Sold {
		t.Fatalf("mark status %s", s)
	}
	if on, _ := f.onHand(t, f.vodka); on != 1 {
		t.Fatalf("vodka left %d", on)
	}

	again := NewReceipt{ID: uuid.New(), AgeConfirmed: true, CreatedAt: daytime, Lines: []NewLine{vodkaLine(f, mark1)}}
	if _, err := f.svc.CreateReceipt(ctx, pc, again); refusalCode(err) != "mark.sold" {
		t.Fatalf("second sale of the same bottle: %v", err)
	}

	// Снеков нет на остатке: продажа не блокируется, недостача уходит в дефицит до ревизии.
	snack := NewReceipt{ID: uuid.New(), Card: true, CreatedAt: daytime, Lines: []NewLine{{VariantID: f.snack, Qty: 2, Price: 7900}}}
	if _, err := f.svc.CreateReceipt(ctx, pc, snack); err != nil {
		t.Fatal(err)
	}
	if on, deficit := f.onHand(t, f.snack); on != 0 || deficit != 2 {
		t.Fatalf("snack on hand %d, deficit %d", on, deficit)
	}
	f.integrity(t)

	found, err := f.svc.FindReceipts(ctx, pc, nil, ptr(mark1))
	if err != nil || len(found) != 1 || found[0].ID != sale.ID || found[0].EgaisQr == nil {
		t.Fatalf("find by mark: %+v %v", found, err)
	}

	sh, err := f.svc.CloseShift(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	if sh.Totals.Receipts != 2 || sh.Totals.CashKopecks != 59900+3*11900 || sh.Totals.CardKopecks != 2*7900 || !sh.EgaisAct {
		t.Fatalf("shift report: %+v", sh)
	}
	var docType string
	if err := f.db.App.QueryRow(ctx, `select doc_type from egais_messages order by created_at desc limit 1`).Scan(&docType); err != nil {
		t.Fatal(err)
	}
	if docType != egais.ActWriteOffShop {
		t.Fatalf("beer write-off act: %s", docType)
	}
	if cur, err := f.svc.CurrentShift(ctx, pc); err != nil || cur != nil {
		t.Fatalf("shift is closed: %+v %v", cur, err)
	}
}

func TestReturn(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	pc := f.till(t)
	sale := NewReceipt{ID: uuid.New(), AgeConfirmed: true, CreatedAt: daytime, Lines: []NewLine{vodkaLine(f, mark1)}}
	if _, err := f.svc.CreateReceipt(ctx, pc, sale); err != nil {
		t.Fatal(err)
	}
	ret := NewReceipt{ID: uuid.New(), Refund: true, ReturnOf: &sale.ID, CreatedAt: daytime, Lines: []NewLine{vodkaLine(f, mark1)}}
	if _, err := f.svc.CreateReceipt(ctx, pc, ret); err != nil {
		t.Fatal(err)
	}
	if s := f.markStatus(t, mark1); s != marks.InStock {
		t.Fatalf("returned mark status %s", s)
	}
	if on, _ := f.onHand(t, f.vodka); on != 2 {
		t.Fatalf("vodka after return %d", on)
	}
	if last := f.utm.cheques[len(f.utm.cheques)-1]; last.Kind != "return" {
		t.Fatalf("return cheque: %+v", last)
	}
	ret.ID = uuid.New()
	if _, err := f.svc.CreateReceipt(ctx, pc, ret); refusalCode(err) != "return.qty" {
		t.Fatalf("second return: %v", err)
	}
	f.integrity(t)
}

func TestSaleRefusals(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	pc := f.till(t)

	f.utm.down = true
	r := NewReceipt{ID: uuid.New(), AgeConfirmed: true, CreatedAt: daytime, Lines: []NewLine{vodkaLine(f, mark2)}}
	if _, err := f.svc.CreateReceipt(ctx, pc, r); refusalCode(err) != "utm.unavailable" {
		t.Fatalf("UTM down: %v", err)
	}
	if s := f.markStatus(t, mark2); s != marks.InStock {
		t.Fatalf("refused sale changed the mark: %s", s)
	}
	f.utm.down = false

	f.utm.reject = "mark.already_sold"
	if _, err := f.svc.CreateReceipt(ctx, pc, r); refusalCode(err) != "mark.sold" {
		t.Fatalf("ЕГАИС rejection: %v", err)
	}
	f.utm.reject = ""

	offline := r
	offline.Offline = true
	if _, err := f.svc.CreateReceipt(ctx, pc, offline); refusalCode(err) != "offline.marked" {
		t.Fatalf("marked goods offline: %v", err)
	}

	f.svc.now = func() time.Time { return daytime.Add(8 * time.Hour) }
	if _, err := f.svc.CreateReceipt(ctx, pc, r); refusalCode(err) != "sale.alcohol_time" {
		t.Fatalf("22:00 in Amur region: %v", err)
	}
	snack := NewReceipt{ID: uuid.New(), CreatedAt: daytime, Lines: []NewLine{{VariantID: f.snack, Qty: 1, Price: 7900}}}
	if _, err := f.svc.CreateReceipt(ctx, pc, snack); err != nil {
		t.Fatalf("snacks are sold any time: %v", err)
	}
	var n int
	if err := f.db.App.QueryRow(ctx, `select count(*) from pos_receipts`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("receipts %d %v", n, err)
	}
	f.integrity(t)
}

// pack — код сигаретной пачки с МРЦ в копейках, как его печатает имитатор.
func pack(serial string, mrp int64) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!\"%&'*+-./_,:;=<>?"
	b := []byte("AAAA")
	for i := 3; i >= 0; i-- {
		b[i] = alphabet[mrp%80]
		mrp /= 80
	}
	return "04601234567893" + serial + string(b) + "tail"
}

func TestTobaccoPrice(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	pc := f.till(t)

	// В каталоге 250 ₽, на пачке МРЦ 219 ₽: пачка продаётся по МРЦ.
	c, err := f.svc.Check(ctx, pc, pack("5AAAAA1", 21900))
	if err != nil || c.Item == nil || c.Item.ID != f.cigarettes || len(c.Refusals) != 0 {
		t.Fatalf("pack check: %+v %v", c, err)
	}
	if c.Price != 21900 || c.MaxPrice == nil || *c.MaxPrice != 21900 || c.Item.MinPrice == nil || *c.Item.MinPrice != 15300 {
		t.Fatalf("price %d, max %v, min %v", c.Price, c.MaxPrice, c.Item.MinPrice)
	}
	line := func(code string, price int64) NewReceipt {
		return NewReceipt{ID: uuid.New(), AgeConfirmed: true, CreatedAt: daytime,
			Lines: []NewLine{{VariantID: f.cigarettes, Qty: 1, Price: price, Crpt: code}}}
	}
	if _, err := f.svc.CreateReceipt(ctx, pc, line(pack("5AAAAA2", 21900), 25000)); refusalCode(err) != "price.above_max" {
		t.Fatalf("above pack MRP: %v", err)
	}
	if _, err := f.svc.CreateReceipt(ctx, pc, line(pack("5AAAAA3", 21900), 14000)); refusalCode(err) != "price.below_min" {
		t.Fatalf("below ЕМЦ: %v", err)
	}
	r := line(pack("5AAAAA4", 21900), 21900)
	if _, err := f.svc.CreateReceipt(ctx, pc, r); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := f.db.App.QueryRow(ctx, "select crpt_code from pos_receipt_lines where receipt_id = $1", r.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "04601234567893"+"5AAAAA4" {
		t.Fatalf("stored code %q: the unit, not the scanned pack code", stored)
	}
}

func TestDevicePermissions(t *testing.T) {
	f := newFixture(t)
	other := uuid.New()
	if _, err := f.db.App.Exec(t.Context(), `insert into locations (id, code, name, kind, city, timezone, subject_code)
		values ($1, 'МГ-02', 'Магазин', 'store', 'Хабаровск', 'Asia/Vladivostok', '27')`, other); err != nil {
		t.Fatal(err)
	}
	boss := f.session(t, "store_manager", f.store)
	if _, _, err := f.svc.RegisterDevice(boss, f.store, "Касса 2"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.RegisterDevice(boss, other, "Касса 1"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("till in another store: %v", err)
	}
	if _, _, err := f.svc.RegisterDevice(f.admin, other, "Касса 1"); err != nil {
		t.Fatal(err)
	}
	own, err := f.svc.Devices(boss)
	if err != nil || len(own) != 2 {
		t.Fatalf("store manager sees own tills only: %d %v", len(own), err)
	}
	all, _ := f.svc.Devices(f.admin)
	if len(all) != 3 {
		t.Fatalf("admin sees all tills: %d", len(all))
	}
	var foreign uuid.UUID
	for _, d := range all {
		if d.LocationID == other {
			foreign = d.ID
		}
	}
	if err := f.svc.RevokeDevice(boss, foreign); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("revoke a till of another store: %v", err)
	}
	if _, err := f.svc.Devices(f.session(t, "cashier", f.store)); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("cashier lists tills: %v", err)
	}
}
