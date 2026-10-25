package seed

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/exchange"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/reconcile"
	"github.com/ADEXITUM/pikni/api/internal/reservations"
)

// Env — окружение сценариев: база с демо-данными и учётными записями (seed-users) и, для сценариев
// с ЕГАИС, имитатор. Сценарий работает через те же сервисы, что и экраны, поэтому состояние —
// настоящее: его можно продолжить руками.
type Env struct {
	Pool           *pgxpool.Pool
	Govsim         string // адрес имитатора; пусто — сценарии с поставщиком недоступны
	GovsimPassword string
	Log            *slog.Logger

	docs *documents.Service
	res  *reservations.Service
	rec  *reconcile.Service
}

func NewEnv(pool *pgxpool.Pool, govsim, govsimPassword string, log *slog.Logger) *Env {
	cat := catalog.NewService(pool)
	mk := marks.NewService(pool)
	return &Env{Pool: pool, Govsim: strings.TrimRight(govsim, "/"), GovsimPassword: govsimPassword, Log: log,
		docs: documents.NewService(pool, cat, mk), res: reservations.NewService(pool, cat, mk), rec: reconcile.NewService(pool)}
}

// Scenario — состояние, которое удобно проверить или показать руками. Login — под кем войти.
type Scenario struct {
	Name, Title, Login, Hint string
	run                      func(ctx context.Context, e *Env) (string, error)
}

// Scenarios — каталог сценариев: `pikni-admin scenario <name>` или `task scenario -- <name>`.
var Scenarios = []Scenario{
	{"stocktake-draft", "Ревизия создана, подсчёт не начат", "upr", "МГ-01 → Документы → ревизия «Черновик»: «Начать подсчёт»", stocktakeDraft},
	{"stocktake-counting", "Полная ревизия идёт: склад посчитан, зал — наполовину", "upr-mg02", "МГ-02: зона «Торговый зал» открыта, продажи идут; досчитайте и закройте зону", stocktakeCounting},
	{"stocktake-reconciling", "Выборочная ревизия на сличении с расхождениями", "upr-mg03", "МГ-03: коньяк и виски — две марки не найдены, одна переместилась, одна чужая", stocktakeReconciling},
	{"stocktake-approval", "Ревизия пива ждёт утверждения директора", "director", "МГ-04: недостача пива, «Утвердить» или вернуть на пересчёт", stocktakeApproval},
	{"transfer-requested", "Заявка магазина на перемещение ждёт склад", "keeper-sk01", "СК-01 → Документы: заявка МГ-01, «Согласовать», собрать и отгрузить", transferRequested},
	{"transfer-in-transit", "Перемещение в пути, магазин принимает", "keeper", "МГ-01: поступление по перемещению из СК-01 — принять поштучно", transferInTransit},
	{"transfer-rejected", "ЕГАИС отклонила ТТН перемещения", "keeper-sk02", "СК-02 → МГ-04: «Отправить ТТН повторно» или «Вернуть товар»", transferRejected},
	{"writeoff-approval", "Бой ждёт второго ключа", "director", "МГ-04: списание двух бутылок — «Утвердить» или «Отклонить»", writeoffApproval},
	{"reservation", "Бронь покупателя отложена в зале", "kassir", "МГ-01: бронь на 2 бутылки — продать на кассе по номеру брони", reservation},
	{"ttn-incoming", "Поставщик отгрузил ТТН на склад", "keeper-sk01", "СК-01: новое поступление по ТТН — принять по коробам (нужен имитатор)", ttnIncoming},
	{"ttn-discrepancy", "ТТН с недостачей, пересортом и чужой маркой", "keeper-sk02", "СК-02: при приёмке — расхождения и акт (нужен имитатор)", ttnDiscrepancy},
	{"ttn-overdue", "ТТН не подтверждена больше суток", "keeper-sk01", "Уведомление «ТТН не подтверждена» (нужен имитатор)", ttnOverdue},
	{"price-below-mrc", "Цена водки ниже МРЦ", "upr", "Уведомление и ценник: цена ниже МРЦ, касса такую не пробьёт", priceBelowMRC},
	{"utm-down", "УТМ магазина недоступен", "director", "МГ-04: сообщение в ЕГАИС ждёт дольше 5 минут — уведомление и оповещение Grafana", utmDown},
	{"egais-mismatch", "Расхождение с остатками ЕГАИС", "upr", "МГ-01 → Сверка с ЕГАИС: пиво не сходится (нужен имитатор)", egaisMismatch},
}

// Run выполняет сценарий по имени; "all" — все по очереди.
func (e *Env) Run(ctx context.Context, name string) error {
	for _, s := range Scenarios {
		if name != "all" && s.Name != name {
			continue
		}
		e.Log.Info("scenario", "name", s.Name)
		what, err := s.run(ctx, e)
		if errors.Is(err, errNoGovsim) && name == "all" {
			e.Log.Warn("scenario skipped: no EGAIS emulator", "name", s.Name)
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		fmt.Printf("%-22s %s\n  войти: %s / %s\n  %s\n  %s\n", s.Name, s.Title, s.Login, Password, s.Hint, what)
		if name != "all" {
			return nil
		}
	}
	if name != "all" {
		return fmt.Errorf("unknown scenario %q", name)
	}
	return nil
}

var errNoGovsim = errors.New("scenario needs the EGAIS emulator: set EGAIS_URL")

// as — контекст сотрудника, как после входа: роль и точки из учётной записи.
func (e *Env) as(ctx context.Context, login string) (context.Context, error) {
	var s auth.Session
	err := e.Pool.QueryRow(ctx, `select u.id, u.login, u.full_name, u.role,
		coalesce(array_agg(ul.location_id) filter (where ul.location_id is not null), '{}')
		from users u left join user_locations ul on ul.user_id = u.id where u.login = $1 group by u.id`, login).
		Scan(&s.UserID, &s.Login, &s.FullName, &s.Role, &s.LocationIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("user %s not found: run pikni-admin seed-users", login)
	}
	s.TOTPEnabled = true
	return auth.WithSession(ctx, s), err
}

type place struct {
	id, zone, floor uuid.UUID
	fsrar           string
}

func (e *Env) place(ctx context.Context, code string) (place, error) {
	var p place
	err := e.Pool.QueryRow(ctx, `select l.id, coalesce(l.fsrar_id, ''),
		(select id from zones where location_id = l.id and kind in ('backroom', 'warehouse') order by kind limit 1),
		coalesce((select id from zones where location_id = l.id and kind = 'floor'), '00000000-0000-0000-0000-000000000000')
		from locations l where l.code = $1`, code).Scan(&p.id, &p.fsrar, &p.zone, &p.floor)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, fmt.Errorf("location %s not found: load dev.sql", code)
	}
	return p, err
}

func (e *Env) version(ctx context.Context, id uuid.UUID) (int32, error) {
	var v int32
	err := e.Pool.QueryRow(ctx, `select version from documents where id = $1`, id).Scan(&v)
	return v, err
}

func (e *Env) number(ctx context.Context, id uuid.UUID) string {
	var n string
	_ = e.Pool.QueryRow(ctx, `select number from documents where id = $1`, id).Scan(&n)
	return n
}

// zoneStock — что числится в зоне: марки по товарам и количество товаров без марок.
type zoneStock struct {
	marks    map[uuid.UUID][]string
	quantity map[uuid.UUID]int32
	order    []uuid.UUID
}

func (e *Env) zoneStock(ctx context.Context, zone uuid.UUID, categories []string) (zoneStock, error) {
	zs := zoneStock{marks: map[uuid.UUID][]string{}, quantity: map[uuid.UUID]int32{}}
	filter := `true`
	args := []any{zone}
	if len(categories) > 0 {
		filter = `c.name = any($2)`
		args = append(args, categories)
	}
	rows, err := e.Pool.Query(ctx, `select b.variant_id, b.on_hand,
			coalesce((select array_agg(m.code order by m.code) from excise_marks m where m.zone_id = b.zone_id
				and m.variant_id = b.variant_id and m.status = 'in_stock'), '{}')
		from stock_balances b join variants v on v.id = b.variant_id join products p on p.id = v.product_id
		join categories c on c.id = p.category_id
		where b.zone_id = $1 and b.on_hand > 0 and `+filter+` order by v.article`, args...)
	if err != nil {
		return zs, err
	}
	defer rows.Close()
	for rows.Next() {
		var v uuid.UUID
		var qty int32
		var codes []string
		if err := rows.Scan(&v, &qty, &codes); err != nil {
			return zs, err
		}
		zs.order = append(zs.order, v)
		if len(codes) > 0 {
			zs.marks[v] = codes
		} else {
			zs.quantity[v] = qty
		}
	}
	return zs, rows.Err()
}

func scan(raw string) documents.NewScan {
	return documents.NewScan{ID: uuid.New(), Raw: raw, Source: "manual", ScannedAt: time.Now()}
}

// count считает зону ревизии: часть share товаров, кроме skipMarks марок и недостачи short штук
// у первых товаров без марок.
func (e *Env) count(ctx context.Context, doc, zone uuid.UUID, zs zoneStock, share float64, skipMarks, short int) error {
	n := int(float64(len(zs.order)) * share)
	for _, v := range zs.order[:n] {
		for _, code := range zs.marks[v] {
			if skipMarks > 0 {
				skipMarks--
				continue
			}
			if _, err := e.docs.StocktakeScan(ctx, doc, zone, scan(code)); err != nil {
				return fmt.Errorf("scan %s: %w", code, err)
			}
		}
		if q, ok := zs.quantity[v]; ok {
			cut := min(int32(short), q)
			short -= int(cut)
			if err := e.docs.SetStocktakeCount(ctx, doc, zone, v, q-cut); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Env) startStocktake(ctx context.Context, store place, scope documents.Scope, comment string) (uuid.UUID, error) {
	id, err := e.docs.CreateStocktake(ctx, store.id, scope, comment)
	if err != nil {
		return uuid.Nil, err
	}
	v, err := e.version(ctx, id)
	if err != nil {
		return uuid.Nil, err
	}
	return id, e.docs.StartStocktake(ctx, id, v)
}

func (e *Env) categories(ctx context.Context, names ...string) ([]uuid.UUID, error) {
	rows, err := e.Pool.Query(ctx, `select id from categories where name = any($1)`, names)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

func stocktakeDraft(ctx context.Context, e *Env) (string, error) {
	ctx, err := e.as(ctx, "upr")
	if err != nil {
		return "", err
	}
	store, err := e.place(ctx, "МГ-01")
	if err != nil {
		return "", err
	}
	id, err := e.docs.CreateStocktake(ctx, store.id, documents.Scope{}, "Плановая ревизия всего магазина")
	return "ревизия " + e.number(ctx, id), err
}

func stocktakeCounting(ctx context.Context, e *Env) (string, error) {
	ctx, err := e.as(ctx, "upr-mg02")
	if err != nil {
		return "", err
	}
	store, err := e.place(ctx, "МГ-02")
	if err != nil {
		return "", err
	}
	id, err := e.startStocktake(ctx, store, documents.Scope{}, "Полная ревизия магазина у дома")
	if err != nil {
		return "", err
	}
	back, err := e.zoneStock(ctx, store.zone, nil)
	if err != nil {
		return "", err
	}
	// Склад посчитан и закрыт: трёх бутылок с марками и двух банок пива не хватает.
	if err := e.count(ctx, id, store.zone, back, 1, 3, 2); err != nil {
		return "", err
	}
	if err := e.docs.SetZone(ctx, id, store.zone, "close"); err != nil {
		return "", err
	}
	floor, err := e.zoneStock(ctx, store.floor, nil)
	if err != nil {
		return "", err
	}
	return "ревизия " + e.number(ctx, id), e.count(ctx, id, store.floor, floor, 0.5, 0, 0)
}

func stocktakeReconciling(ctx context.Context, e *Env) (string, error) {
	ctx, err := e.as(ctx, "upr-mg03")
	if err != nil {
		return "", err
	}
	store, err := e.place(ctx, "МГ-03")
	if err != nil {
		return "", err
	}
	cats := []string{"Коньяк и бренди", "Виски"}
	ids, err := e.categories(ctx, cats...)
	if err != nil {
		return "", err
	}
	id, err := e.startStocktake(ctx, store, documents.Scope{Categories: ids}, "Выборочная ревизия коньяка и виски")
	if err != nil {
		return "", err
	}
	back, err := e.zoneStock(ctx, store.zone, cats)
	if err != nil {
		return "", err
	}
	floor, err := e.zoneStock(ctx, store.floor, cats)
	if err != nil {
		return "", err
	}
	if err := e.count(ctx, id, store.zone, back, 1, 2, 0); err != nil {
		return "", err
	}
	// Бутылка из зала нашлась на складе («переместилась»), и одна марка — чужая.
	var moved string
	for _, v := range floor.order {
		if c := floor.marks[v]; len(c) > 0 {
			moved = c[len(c)-1]
			floor.marks[v] = c[:len(c)-1]
			break
		}
	}
	if moved != "" {
		if _, err := e.docs.StocktakeScan(ctx, id, store.zone, scan(moved)); err != nil {
			return "", err
		}
	}
	if _, err := e.docs.StocktakeScan(ctx, id, store.zone, scan("PKN"+strings.ToUpper(rand.Text()[:13]))); err != nil {
		return "", err
	}
	if err := e.count(ctx, id, store.floor, floor, 1, 0, 0); err != nil {
		return "", err
	}
	for _, z := range []uuid.UUID{store.zone, store.floor} {
		if err := e.docs.SetZone(ctx, id, z, "close"); err != nil {
			return "", err
		}
	}
	v, err := e.version(ctx, id)
	if err != nil {
		return "", err
	}
	return "ревизия " + e.number(ctx, id), e.docs.FinishCounting(ctx, id, v)
}

func stocktakeApproval(ctx context.Context, e *Env) (string, error) {
	ctx, err := e.as(ctx, "upr-mg04")
	if err != nil {
		return "", err
	}
	store, err := e.place(ctx, "МГ-04")
	if err != nil {
		return "", err
	}
	cats := []string{"Пиво"}
	ids, err := e.categories(ctx, cats...)
	if err != nil {
		return "", err
	}
	id, err := e.startStocktake(ctx, store, documents.Scope{Categories: ids}, "Ревизия пива после жалобы покупателя")
	if err != nil {
		return "", err
	}
	for i, z := range []uuid.UUID{store.zone, store.floor} {
		zs, err := e.zoneStock(ctx, z, cats)
		if err != nil {
			return "", err
		}
		if err := e.count(ctx, id, z, zs, 1, 0, 5*(1-i)); err != nil {
			return "", err
		}
		if err := e.docs.SetZone(ctx, id, z, "close"); err != nil {
			return "", err
		}
	}
	v, err := e.version(ctx, id)
	if err != nil {
		return "", err
	}
	if err := e.docs.FinishCounting(ctx, id, v); err != nil {
		return "", err
	}
	if v, err = e.version(ctx, id); err != nil {
		return "", err
	}
	return "ревизия " + e.number(ctx, id), e.docs.SubmitStocktake(ctx, id, v)
}

// popular — ходовые позиции, которые есть на складе точки: с марками и без.
func (e *Env) popular(ctx context.Context, zone uuid.UUID, n int) ([]documents.TransferLine, error) {
	rows, err := e.Pool.Query(ctx, `select b.variant_id, least(b.on_hand - b.reserved, 6)::int
		from stock_balances b where b.zone_id = $1 and b.on_hand - b.reserved >= 6
		order by b.on_hand desc limit $2`, zone, n)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (documents.TransferLine, error) {
		var l documents.TransferLine
		err := r.Scan(&l.VariantID, &l.Qty)
		return l, err
	})
}

func (e *Env) request(ctx context.Context, requester, fromCode, toCode string) (uuid.UUID, place, error) {
	rctx, err := e.as(ctx, requester)
	if err != nil {
		return uuid.Nil, place{}, err
	}
	from, err := e.place(ctx, fromCode)
	if err != nil {
		return uuid.Nil, from, err
	}
	to, err := e.place(ctx, toCode)
	if err != nil {
		return uuid.Nil, from, err
	}
	lines, err := e.popular(ctx, from.zone, 6)
	if err != nil {
		return uuid.Nil, from, err
	}
	id, err := e.docs.CreateTransfer(rctx, from.id, to.id, lines, "Пополнение к выходным")
	return id, from, err
}

// ship — склад согласует заявку, собирает её сканами (марки и штрихкоды) и отгружает.
func (e *Env) ship(ctx context.Context, keeper string, id uuid.UUID, from place) error {
	kctx, err := e.as(ctx, keeper)
	if err != nil {
		return err
	}
	v, err := e.version(ctx, id)
	if err != nil {
		return err
	}
	if err := e.docs.DecideTransfer(kctx, id, v, true, ""); err != nil {
		return err
	}
	rows, err := e.Pool.Query(ctx, `select l.variant_id, coalesce(l.qty_plan, l.qty)::int,
			coalesce((select array_agg(m.code order by m.code) from excise_marks m where m.zone_id = $2
				and m.variant_id = l.variant_id and m.status = 'in_stock'), '{}'),
			(select code from barcodes where variant_id = l.variant_id limit 1)
		from document_lines l where l.document_id = $1 order by l.line_no`, id, from.zone)
	if err != nil {
		return err
	}
	type pick struct {
		Variant uuid.UUID
		Qty     int
		Marks   []string
		Barcode *string
	}
	picks, err := pgx.CollectRows(rows, pgx.RowToStructByPos[pick])
	if err != nil {
		return err
	}
	for _, p := range picks {
		for i := range p.Qty {
			raw := ""
			if len(p.Marks) > 0 {
				raw = p.Marks[i]
			} else if p.Barcode != nil {
				raw = *p.Barcode
			}
			if raw == "" {
				break
			}
			if _, err := e.docs.AddScan(kctx, id, scan(raw)); err != nil {
				return fmt.Errorf("pick %s: %w", raw, err)
			}
		}
	}
	if v, err = e.version(ctx, id); err != nil {
		return err
	}
	return e.docs.Post(kctx, id, v)
}

func transferRequested(ctx context.Context, e *Env) (string, error) {
	id, _, err := e.request(ctx, "upr", "СК-01", "МГ-01")
	return "заявка " + e.number(ctx, id), err
}

func transferInTransit(ctx context.Context, e *Env) (string, error) {
	id, from, err := e.request(ctx, "upr", "СК-01", "МГ-01")
	if err != nil {
		return "", err
	}
	return "перемещение " + e.number(ctx, id), e.ship(ctx, "keeper-sk01", id, from)
}

func transferRejected(ctx context.Context, e *Env) (string, error) {
	id, from, err := e.request(ctx, "upr-mg04", "СК-02", "МГ-04")
	if err != nil {
		return "", err
	}
	if err := e.ship(ctx, "keeper-sk02", id, from); err != nil {
		return "", err
	}
	// Так обмен применяет отказ ЕГАИС: ТТН ушла, пришла квитанция «отклонено».
	err = pgx.BeginFunc(ctx, e.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update egais_messages set status = 'rejected', last_error = 'Получатель не найден в ЕГАИС'
			where document_id = $1 and doc_type = 'WayBill'`, id); err != nil {
			return err
		}
		return e.docs.ApplyTicket(ctx, tx, id, false)
	})
	return "перемещение " + e.number(ctx, id), err
}

func writeoffApproval(ctx context.Context, e *Env) (string, error) {
	// В МГ-04 идёт только ревизия пива: бутылки с марками она не блокирует.
	kctx, err := e.as(ctx, "keeper-mg04")
	if err != nil {
		return "", err
	}
	store, err := e.place(ctx, "МГ-04")
	if err != nil {
		return "", err
	}
	var reason uuid.UUID
	if err := e.Pool.QueryRow(ctx, `select id from writeoff_reasons where name = 'Бой'`).Scan(&reason); err != nil {
		return "", err
	}
	id, err := e.docs.CreateWriteoff(kctx, &store.floor, reason, "Покупатель уронил с полки", nil)
	if err != nil {
		return "", err
	}
	var codes []string
	rows, err := e.Pool.Query(ctx, `select code from excise_marks where zone_id = $1 and status = 'in_stock' order by code limit 2`, store.floor)
	if err != nil {
		return "", err
	}
	if codes, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return "", err
	}
	for _, c := range codes {
		if _, err := e.docs.AddScan(kctx, id, scan(c)); err != nil {
			return "", err
		}
	}
	// Для боя нужен снимок: в сценарии — условная фотография осколков.
	if _, err := e.docs.AddPhoto(kctx, id, "image/jpeg", photo()); err != nil {
		return "", err
	}
	v, err := e.version(ctx, id)
	if err != nil {
		return "", err
	}
	return "списание " + e.number(ctx, id), e.docs.Post(kctx, id, v)
}

func reservation(ctx context.Context, e *Env) (string, error) {
	cctx, err := e.as(ctx, "kassir")
	if err != nil {
		return "", err
	}
	store, err := e.place(ctx, "МГ-01")
	if err != nil {
		return "", err
	}
	var variant uuid.UUID
	var codes []string
	if err := e.Pool.QueryRow(ctx, `select variant_id, (array_agg(code order by code))[1:2] from excise_marks
		where zone_id = $1 and status = 'in_stock' group by variant_id having count(*) >= 4 order by count(*) desc limit 1`, store.floor).
		Scan(&variant, &codes); err != nil {
		return "", err
	}
	id, err := e.res.Create(cctx, reservations.New{LocationID: store.id, VariantID: variant, Qty: 2,
		CustomerName: "Андрей", Phone: "+79140000000", Consent: true, Comment: "Заберёт после 19:00"})
	if err != nil {
		return "", err
	}
	return "бронь отложена", e.res.Confirm(cctx, id, codes)
}

func (e *Env) govsimPost(ctx context.Context, path string, body any) error {
	if e.Govsim == "" {
		return errNoGovsim
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Govsim+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("govsim %s: %s", path, resp.Status)
	}
	return nil
}

func (e *Env) govsimForm(ctx context.Context, path string, form url.Values) error {
	if e.Govsim == "" {
		return errNoGovsim
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Govsim+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", e.GovsimPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("govsim %s: %s", path, resp.Status)
	}
	return nil
}

// exchangeOnce — один круг обмена с УТМ, как у запущенного API: отправка очереди и приём входящих.
func (e *Env) exchangeOnce(ctx context.Context) error {
	return exchange.New(e.Pool, egais.NewGovsim(e.Govsim), e.docs, e.Log).Once(ctx)
}

// shipment — поставщик отгружает на точку ходовые позиции: крепкий алкоголь и вино с марками, пиво.
func (e *Env) shipment(ctx context.Context, to string, extra map[string]any) (uuid.UUID, error) {
	p, err := e.place(ctx, to)
	if err != nil {
		return uuid.Nil, err
	}
	var supplier string
	if err := e.Pool.QueryRow(ctx, `select fsrar_id from suppliers order by inn limit 1`).Scan(&supplier); err != nil {
		return uuid.Nil, err
	}
	rows, err := e.Pool.Query(ctx, `select v.alcocode from variants v join products p on p.id = v.product_id
		join product_kinds k on k.code = p.kind_code
		where v.alcocode is not null and v.is_active and k.egais_accounting = 'marked'
		  and exists (select 1 from stock_balances b where b.variant_id = v.id and b.zone_id = $1)
		order by v.article limit 4`, p.zone)
	if err != nil {
		return uuid.Nil, err
	}
	codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return uuid.Nil, err
	}
	var positions []map[string]any
	for _, c := range codes {
		positions = append(positions, map[string]any{"alcocode": c, "qty": 6})
	}
	body := map[string]any{"supplier": supplier, "recipient": p.fsrar, "box_size": 6, "positions": positions}
	for k, v := range extra {
		body[k] = v
	}
	if extra["mixup"] != nil && len(codes) > 1 {
		body["mixup"] = map[string]any{"from": codes[0], "to": codes[1], "qty": 2}
	}
	var before time.Time
	_ = e.Pool.QueryRow(ctx, `select now()`).Scan(&before)
	if err := e.govsimPost(ctx, "/supplier/shipments", body); err != nil {
		return uuid.Nil, err
	}
	for range 20 {
		if err := e.exchangeOnce(ctx); err != nil {
			return uuid.Nil, err
		}
		var id uuid.UUID
		err := e.Pool.QueryRow(ctx, `select id from documents where kind = 'receipt' and location_id = $1 and created_at >= $2
			order by created_at desc limit 1`, p.id, before).Scan(&id)
		if err == nil {
			return id, nil
		}
		time.Sleep(time.Second)
	}
	return uuid.Nil, errors.New("TTN did not arrive from the emulator")
}

func ttnIncoming(ctx context.Context, e *Env) (string, error) {
	id, err := e.shipment(ctx, "СК-01", nil)
	return "поступление " + e.number(ctx, id), err
}

func ttnDiscrepancy(ctx context.Context, e *Env) (string, error) {
	id, err := e.shipment(ctx, "СК-02", map[string]any{"shortage": 2, "foreign_mark": true, "mixup": true})
	return "поступление " + e.number(ctx, id), err
}

func ttnOverdue(ctx context.Context, e *Env) (string, error) {
	id, err := e.shipment(ctx, "СК-01", nil)
	if err != nil {
		return "", err
	}
	_, err = e.Pool.Exec(ctx, `update documents set created_at = now() - interval '30 hours' where id = $1`, id)
	return "поступление " + e.number(ctx, id) + " пришло 30 часов назад", err
}

func priceBelowMRC(ctx context.Context, e *Env) (string, error) {
	var article string
	// Цену назначили до повышения МРЦ: теперь она ниже минимальной (приказ Минфина № 181н).
	err := e.Pool.QueryRow(ctx, `with v as (
			select v.id, v.article from variants v join products p on p.id = v.product_id
			where p.kind_code = '200' and v.volume_ml = 500 and v.is_active order by v.article limit 1)
		insert into prices (variant_id, kind, price, valid_from)
		select id, 'retail', 389, now() from v returning (select article from v)`).Scan(&article)
	return "водка " + article + " — 389 ₽ при МРЦ 409 ₽", err
}

func utmDown(ctx context.Context, e *Env) (string, error) {
	p, err := e.place(ctx, "МГ-04")
	if err != nil {
		return "", err
	}
	if e.Govsim != "" {
		if err := e.govsimForm(ctx, "/ui/participants/"+p.fsrar+"/scenario", url.Values{"utm_down": {"on"}}); err != nil {
			return "", err
		}
	}
	_, err = e.Pool.Exec(ctx, `insert into egais_messages (location_id, direction, doc_type, payload, status, attempts,
			next_attempt_at, created_at, updated_at, last_error)
		values ($1, 'out', 'QueryRests', '{}', 'queued', 3, now() + interval '1 minute', now() - interval '12 minutes', now(), 'УТМ недоступен')`, p.id)
	return "запрос остатков ждёт 12 минут; вернуть УТМ — сценарий в имитаторе (http://localhost:8090)", err
}

func egaisMismatch(ctx context.Context, e *Env) (string, error) {
	p, err := e.place(ctx, "МГ-01")
	if err != nil {
		return "", err
	}
	var alcocode, register string
	var qty int
	if err := e.Pool.QueryRow(ctx, `select v.alcocode, case when z.kind = 'floor' then '2' else '1' end, b.on_hand
		from stock_balances b join zones z on z.id = b.zone_id join variants v on v.id = b.variant_id
		join products p on p.id = v.product_id join product_kinds k on k.code = p.kind_code
		where b.location_id = $1 and k.egais_accounting = 'quantity' and b.on_hand > 4 and v.alcocode is not null
		order by b.on_hand desc limit 1`, p.id).Scan(&alcocode, &register, &qty); err != nil {
		return "", fmt.Errorf("beer on hand: %w", err)
	}
	if err := e.govsimForm(ctx, "/ui/points/"+p.fsrar+"/rests", url.Values{
		"alcocode": {alcocode}, "register": {register}, "qty": {fmt.Sprint(qty - 4)}}); err != nil {
		return "", err
	}
	uctx, err := e.as(ctx, "upr")
	if err != nil {
		return "", err
	}
	if err := e.rec.Request(uctx, p.id); err != nil {
		return "", err
	}
	for range 3 {
		if err := e.exchangeOnce(ctx); err != nil {
			return "", err
		}
		time.Sleep(time.Second)
	}
	return fmt.Sprintf("в ЕГАИС пива %s на 4 банки меньше, чем в учёте", alcocode), nil
}

// photo — снимок для документа: серый кадр с тёмным пятном, JPEG.
func photo() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := range 240 {
		for x := range 320 {
			c := uint8(170)
			if (x-160)*(x-160)+(y-120)*(y-120) < 3600 {
				c = 60
			}
			img.Set(x, y, color.RGBA{c, c, c + 10, 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	return buf.Bytes()
}
