// Package sim — состояние и правила имитатора: ЕГАИС с УТМ точек, ГИС МТ «Честный знак», поставщик.
// Имитатор хранит состояние, поэтому проверки работают по-настоящему: марка, принятая одной точкой,
// не появится у другой, а перевод пива сверх остатка на регистре отклоняется (docs/design/govsim.md).
package sim

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	ErrUnknownParticipant = errors.New("unknown participant")
	ErrUTMDown            = errors.New("utm is down")
	ErrNotFound           = errors.New("not found")
)

type Sim struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Sim {
	return &Sim{pool: pool}
}

// Migrate применяет схему имитатора.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer func(db *sql.DB) { _ = db.Close() }(db)
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, "migrations")
}

// Participant — учебный участник ЕГАИС и состояние его УТМ.
type Participant struct {
	FSRARID      string
	INN, KPP     string
	Name, Kind   string
	Address      string
	UTMDown      bool
	DelaySeconds int
}

func (s *Sim) Participants(ctx context.Context) ([]Participant, error) {
	rows, err := s.pool.Query(ctx, `select fsrar_id, inn, kpp, name, kind, address, utm_down, delay_seconds
		from participants order by kind, fsrar_id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Participant])
}

func (s *Sim) Participant(ctx context.Context, fsrar string) (Participant, error) {
	rows, err := s.pool.Query(ctx, `select fsrar_id, inn, kpp, name, kind, address, utm_down, delay_seconds
		from participants where fsrar_id = $1`, fsrar)
	if err != nil {
		return Participant{}, err
	}
	p, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Participant])
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrUnknownParticipant
	}
	return p, err
}

// utm проверяет, что УТМ участника существует и «включён» (сценарий ИС-5).
func (s *Sim) utm(ctx context.Context, fsrar string) (Participant, error) {
	p, err := s.Participant(ctx, fsrar)
	if err != nil {
		return p, err
	}
	if p.UTMDown {
		return p, ErrUTMDown
	}
	return p, nil
}

func (s *Sim) Status(ctx context.Context, fsrar string) (Participant, error) {
	return s.utm(ctx, fsrar)
}

// SetScenario включает и выключает сценарии участника.
func (s *Sim) SetScenario(ctx context.Context, fsrar string, utmDown bool, delay int) error {
	tag, err := s.pool.Exec(ctx, `update participants set utm_down = $2, delay_seconds = $3 where fsrar_id = $1`,
		fsrar, utmDown, max(0, min(delay, 3600)))
	if err == nil && tag.RowsAffected() == 0 {
		return ErrUnknownParticipant
	}
	return err
}

// Message — сообщение в «исходящих» УТМ участника.
type Message struct {
	ID        uuid.UUID       `json:"id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	Payload   json.RawMessage `json:"payload"`
}

// Outbox — непрочитанные сообщения участника, которые уже «дошли» с учётом задержки.
func (s *Sim) Outbox(ctx context.Context, fsrar string) ([]Message, error) {
	if _, err := s.utm(ctx, fsrar); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `select id, type, created_at, payload from outbox
		where fsrar_id = $1 and acked_at is null and visible_after <= now() order by created_at, id`, fsrar)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Message])
}

func (s *Sim) Message(ctx context.Context, fsrar string, id uuid.UUID) (Message, error) {
	if _, err := s.utm(ctx, fsrar); err != nil {
		return Message{}, err
	}
	rows, err := s.pool.Query(ctx, `select id, type, created_at, payload from outbox
		where fsrar_id = $1 and id = $2 and acked_at is null`, fsrar, id)
	if err != nil {
		return Message{}, err
	}
	m, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Message])
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// Ack удаляет сообщение из «исходящих»: участник его обработал. Повтор безопасен.
func (s *Sim) Ack(ctx context.Context, fsrar string, id uuid.UUID) error {
	if _, err := s.utm(ctx, fsrar); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `update outbox set acked_at = coalesce(acked_at, now()) where fsrar_id = $1 and id = $2`, fsrar, id)
	return err
}

func put(ctx context.Context, tx pgx.Tx, fsrar, kind string, payload any, doc *uuid.UUID) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into outbox (fsrar_id, type, payload, document_id, visible_after)
		select $1, $2, $3, $4, now() + make_interval(secs => delay_seconds) from participants where fsrar_id = $1`,
		fsrar, kind, data, doc)
	return err
}

func markEvent(ctx context.Context, tx pgx.Tx, codes []string, event, fsrar string, doc uuid.UUID) error {
	_, err := tx.Exec(ctx, `insert into mark_events (code, event, fsrar_id, document_id)
		select unnest($1::text[]), $2, $3, $4`, codes, event, fsrar, doc)
	return err
}

// Ticket — квитанция ЕГАИС по документу.
type Ticket struct {
	ReplyID string `json:"reply_id"`
	DocType string `json:"doc_type"`
	Result  string `json:"result"` // accepted или rejected
	Reason  string `json:"reason,omitempty"`
}

// rejection — отказ ЕГАИС по существу документа: он уходит квитанцией, а не ошибкой запроса.
type rejection struct{ reason string }

func (r rejection) Error() string { return r.reason }

func reject(format string, args ...any) error {
	return rejection{reason: fmt.Sprintf(format, args...)}
}

// Receive принимает документ во «входящие» ЕГАИС от УТМ участника. Ответ — идентификатор документа
// сразу; результат проверки приходит квитанцией в «исходящие» отправителя, как в настоящем обмене.
func (s *Sim) Receive(ctx context.Context, fsrar, docType string, payload json.RawMessage) (uuid.UUID, error) {
	if _, err := s.utm(ctx, fsrar); err != nil {
		return uuid.Nil, err
	}
	if !json.Valid(payload) {
		return uuid.Nil, errors.New("payload is not JSON")
	}
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `insert into documents (fsrar_from, type, payload, status)
			values ($1, $2, $3, 'received') returning id`, fsrar, docType, payload).Scan(&id); err != nil {
			return err
		}
		ticket := Ticket{ReplyID: id.String(), DocType: docType, Result: "accepted"}
		err := s.process(ctx, tx, fsrar, docType, id, payload)
		if r, ok := errors.AsType[rejection](err); ok {
			ticket.Result, ticket.Reason = "rejected", r.reason
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update documents set status = $2, reject_reason = nullif($3, '') where id = $1`,
			id, ticket.Result, ticket.Reason); err != nil {
			return err
		}
		return put(ctx, tx, fsrar, "Ticket", ticket, &id)
	})
	return id, err
}

func (s *Sim) process(ctx context.Context, tx pgx.Tx, fsrar, docType string, doc uuid.UUID, payload []byte) error {
	switch docType {
	case "WayBillAct":
		return wayBillAct(ctx, tx, fsrar, doc, payload)
	case "TransferToShop":
		return transfer(ctx, tx, fsrar, payload, 1, 2)
	case "TransferFromShop":
		return transfer(ctx, tx, fsrar, payload, 2, 1)
	case "QueryRests":
		return queryRests(ctx, tx, fsrar, doc)
	}
	return reject("Тип документа %s имитатор не поддерживает", docType)
}

// TTN — товарно-транспортная накладная в упрощённом формате имитатора.
type TTN struct {
	ID        uuid.UUID     `json:"ttn_id"`
	Number    string        `json:"number"`
	Date      string        `json:"date"`
	Shipper   Party         `json:"shipper"`
	Consignee string        `json:"consignee"`
	Positions []TTNPosition `json:"positions"`
	Packages  []TTNPackage  `json:"packages"`
}

type Party struct {
	FSRARID string `json:"fsrar_id"`
	Name    string `json:"name"`
	INN     string `json:"inn"`
	KPP     string `json:"kpp"`
}

type TTNPosition struct {
	Identity int      `json:"identity"`
	Alcocode string   `json:"alcocode"`
	Name     string   `json:"name"`
	VolumeMl *int     `json:"volume_ml,omitempty"`
	Qty      int      `json:"qty"`
	Price    float64  `json:"price"`
	Marked   bool     `json:"marked"`
	Marks    []string `json:"marks,omitempty"`
}

// TTNPackage — короб: марки или количество продукции без марок одной позиции.
type TTNPackage struct {
	Code     string   `json:"code"`
	Identity int      `json:"identity"`
	Marks    []string `json:"marks,omitempty"`
	Qty      int      `json:"qty"`
}

// WayBillAct — акт подтверждения или расхождения по ТТН: что принято по каждой позиции.
type WayBillAct struct {
	TTNID     uuid.UUID     `json:"ttn_id"`
	Positions []ActPosition `json:"positions"`
}

type ActPosition struct {
	Identity    int      `json:"identity"`
	QtyAccepted int      `json:"qty_accepted"`
	Marks       []string `json:"marks,omitempty"`
}

func wayBillAct(ctx context.Context, tx pgx.Tx, fsrar string, doc uuid.UUID, payload []byte) error {
	var act WayBillAct
	if err := json.Unmarshal(payload, &act); err != nil {
		return reject("Акт не разобран: %v", err)
	}
	var raw []byte
	var to *string
	err := tx.QueryRow(ctx, `select payload, fsrar_to from documents where id = $1 and type = 'WayBill' for update`,
		act.TTNID).Scan(&raw, &to)
	if errors.Is(err, pgx.ErrNoRows) {
		return reject("ТТН %s не найдена", act.TTNID)
	}
	if err != nil {
		return err
	}
	if to == nil || *to != fsrar {
		return reject("ТТН адресована другому получателю")
	}
	var done bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from documents where type = 'WayBillAct' and status = 'accepted'
		and payload ->> 'ttn_id' = $1)`, act.TTNID.String()).Scan(&done); err != nil {
		return err
	}
	if done {
		return reject("По ТТН уже принят акт")
	}
	var ttn TTN
	if err := json.Unmarshal(raw, &ttn); err != nil {
		return err
	}
	accepted := make(map[int]ActPosition, len(act.Positions))
	for _, p := range act.Positions {
		accepted[p.Identity] = p
	}
	for _, pos := range ttn.Positions {
		a := accepted[pos.Identity]
		if a.QtyAccepted < 0 || a.QtyAccepted > pos.Qty {
			return reject("Позиция %d: принято %d из %d", pos.Identity, a.QtyAccepted, pos.Qty)
		}
		if !pos.Marked {
			if a.QtyAccepted > 0 {
				if _, err := tx.Exec(ctx, `insert into rests (fsrar_id, alcocode, register, qty) values ($1, $2, 1, $3)
					on conflict (fsrar_id, alcocode, register) do update set qty = rests.qty + excluded.qty`,
					fsrar, pos.Alcocode, a.QtyAccepted); err != nil {
					return err
				}
			}
			// Владелец кодов «Честного знака» меняется вместе с приёмкой — упрощённо вместо УПД.
			if _, err := tx.Exec(ctx, `update crpt_codes set owner_fsrar = $1 where code in (
				select code from crpt_codes where ttn_id = $2 and alcocode = $3 order by code limit $4)`,
				fsrar, ttn.ID, pos.Alcocode, a.QtyAccepted); err != nil {
				return err
			}
			continue
		}
		inTTN := make(map[string]bool, len(pos.Marks))
		for _, m := range pos.Marks {
			inTTN[m] = true
		}
		take := make(map[string]bool, len(a.Marks))
		for _, m := range a.Marks {
			if !inTTN[m] {
				return reject("Марки %s нет в позиции %d ТТН", m, pos.Identity)
			}
			take[m] = true
		}
		if len(take) != a.QtyAccepted {
			return reject("Позиция %d: принято %d, а марок указано %d", pos.Identity, a.QtyAccepted, len(take))
		}
		var mine, back []string
		for _, m := range pos.Marks {
			if take[m] {
				mine = append(mine, m)
			} else {
				back = append(back, m)
			}
		}
		if _, err := tx.Exec(ctx, `update marks set owner_fsrar = $1, status = 'on_balance', updated_at = now()
			where code = any($2)`, fsrar, mine); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update marks set owner_fsrar = $1, status = 'on_balance', updated_at = now()
			where code = any($2)`, ttn.Shipper.FSRARID, back); err != nil {
			return err
		}
		if err := markEvent(ctx, tx, mine, "Принята по акту", fsrar, doc); err != nil {
			return err
		}
		if err := markEvent(ctx, tx, back, "Возвращена поставщику по акту расхождения", ttn.Shipper.FSRARID, doc); err != nil {
			return err
		}
	}
	return nil
}

// Item — позиция продукции без марок в документах перевода и запроса остатков.
type Item struct {
	Alcocode string `json:"alcocode"`
	Qty      int    `json:"qty"`
}

func transfer(ctx context.Context, tx pgx.Tx, fsrar string, payload []byte, from, to int) error {
	var doc struct {
		Items []Item `json:"items"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		return reject("Документ не разобран: %v", err)
	}
	for _, it := range doc.Items {
		if it.Alcocode == "" {
			return reject("Не указан алкокод")
		}
		var have int
		err := tx.QueryRow(ctx, `select qty from rests where fsrar_id = $1 and alcocode = $2 and register = $3 for update`,
			fsrar, it.Alcocode, from).Scan(&have)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if have < it.Qty {
			return reject("На регистре %d недостаточно продукции %s: есть %d, нужно %d", from, it.Alcocode, have, it.Qty)
		}
		if _, err := tx.Exec(ctx, `update rests set qty = qty - $4 where fsrar_id = $1 and alcocode = $2 and register = $3`,
			fsrar, it.Alcocode, from, it.Qty); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into rests (fsrar_id, alcocode, register, qty) values ($1, $2, $3, $4)
			on conflict (fsrar_id, alcocode, register) do update set qty = rests.qty + excluded.qty`,
			fsrar, it.Alcocode, to, it.Qty); err != nil {
			return err
		}
	}
	return nil
}

// Rest — остаток продукции без марок на регистре.
type Rest struct {
	Alcocode string `json:"alcocode"`
	Name     string `json:"name"`
	Register int    `json:"register"`
	Qty      int    `json:"qty"`
}

// BalanceMark — марка на балансе участника.
type BalanceMark struct {
	Code     string `json:"code"`
	Alcocode string `json:"alcocode"`
	Name     string `json:"name"`
}

func rests(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, fsrar string) ([]Rest, []BalanceMark, error) {
	rows, err := q.Query(ctx, `select r.alcocode, p.name, r.register::int, r.qty from rests r join products p using (alcocode)
		where r.fsrar_id = $1 and r.qty > 0 order by p.name, r.register`, fsrar)
	if err != nil {
		return nil, nil, err
	}
	rs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Rest])
	if err != nil {
		return nil, nil, err
	}
	rows, err = q.Query(ctx, `select m.code, m.alcocode, p.name from marks m join products p using (alcocode)
		where m.owner_fsrar = $1 and m.status = 'on_balance' order by p.name, m.code`, fsrar)
	if err != nil {
		return nil, nil, err
	}
	ms, err := pgx.CollectRows(rows, pgx.RowToStructByPos[BalanceMark])
	return rs, ms, err
}

func queryRests(ctx context.Context, tx pgx.Tx, fsrar string, doc uuid.UUID) error {
	rs, ms, err := rests(ctx, tx, fsrar)
	if err != nil {
		return err
	}
	return put(ctx, tx, fsrar, "ReplyRests", map[string]any{"reply_id": doc.String(), "rests": rs, "marks": ms}, &doc)
}

// Rests — остатки участника для веб-интерфейса.
func (s *Sim) Rests(ctx context.Context, fsrar string) ([]Rest, []BalanceMark, error) {
	return rests(ctx, s.pool, fsrar)
}
