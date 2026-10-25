// Package notify — центр уведомлений (УВ-2). Фоновая задача раз в минуту ищет события по
// состоянию данных и раздаёт уведомления тем, у кого есть право в точке события. Ключ события
// не даёт уведомить человека дважды; новое уведомление сразу приходит в открытое приложение.
package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/reconcile"
)

var ErrNotFound = errors.New("notification not found")

type Service struct {
	pool      *pgxpool.Pool
	reconcile *reconcile.Service
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, reconcile: reconcile.NewService(pool)}
}

// Event — событие для уведомления: кому (право в точке), о чём и куда перейти.
type Event struct {
	Key        string
	Kind       string
	Location   uuid.UUID
	Permission apispec.Permission
	Title      string
	Body       string
	Link       string
	// Exclude — кому не уведомлять: например, автору документа о его же заявке.
	Exclude *uuid.UUID
}

type recipient struct {
	id        uuid.UUID
	role      string
	locations []uuid.UUID
}

// rule — запрос, который возвращает события одного вида.
type rule struct {
	kind       string
	permission apispec.Permission
	sql        string
	render     func(row []any) (key, title, body, link string)
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// Правила УВ-2. Окно «за последние двое суток» не даёт завалить уведомлениями о старом при
// первом запуске.
var rules = []rule{
	{
		kind: "ttn_received", permission: apispec.PermissionReceiptEdit,
		sql: `select d.id, d.location_id, null::uuid, coalesce(d.ttn_number, d.number), coalesce(sp.name, '')
			from documents d left join suppliers sp on sp.id = d.supplier_id where d.kind = 'receipt' and d.status = 'draft' and d.ttn_id is not null and d.transfer_id is null
			  and d.created_at > now() - interval '2 days'`,
		render: func(r []any) (string, string, string, string) {
			return "ttn:" + str(r[0]), "Поступила ТТН " + str(r[3]), str(r[4]), "/documents/" + str(r[0])
		},
	},
	{
		kind: "ttn_overdue", permission: apispec.PermissionReceiptEdit,
		sql: `select d.id, d.location_id, null::uuid, coalesce(d.ttn_number, d.number), ''
			from documents d where d.kind = 'receipt' and d.status = 'draft' and d.ttn_id is not null
			  and d.created_at < now() - make_interval(hours => coalesce((select value::int from app_settings where key = 'ttn_reminder_hours'), 24))
			  and d.created_at > now() - interval '14 days'`,
		render: func(r []any) (string, string, string, string) {
			return "ttn-overdue:" + str(r[0]), "ТТН " + str(r[3]) + " не подтверждена",
				"Подтвердите или отклоните ТТН в ЕГАИС: срок ответа ограничен.", "/documents/" + str(r[0])
		},
	},
	{
		kind: "transfer_requested", permission: apispec.PermissionTransferEdit,
		sql: `select d.id, d.location_id, d.author_id, d.number, dl.name
			from documents d join locations dl on dl.id = d.dest_location_id
			where d.kind = 'transfer' and d.status = 'requested' and d.created_at > now() - interval '2 days'`,
		render: func(r []any) (string, string, string, string) {
			return "transfer-req:" + str(r[0]), "Заявка на перемещение " + str(r[3]), "Просит " + str(r[4]), "/documents/" + str(r[0])
		},
	},
	{
		kind: "transfer_shipped", permission: apispec.PermissionReceiptEdit,
		sql: `select d.id, d.dest_location_id, null::uuid, d.number, l.name
			from documents d join locations l on l.id = d.location_id
			where d.kind = 'transfer' and d.status = 'in_transit' and d.posted_at > now() - interval '2 days'`,
		render: func(r []any) (string, string, string, string) {
			return "transfer-ship:" + str(r[0]), "Товар отгружен: " + str(r[3]), "Из " + str(r[4]) + " — примите поступлением",
				"/documents/" + str(r[0])
		},
	},
	{
		kind: "transfer_arrived", permission: apispec.PermissionTransferView,
		sql: `select t.id, t.location_id, null::uuid, t.number, l.name
			from documents r join documents t on t.id = r.transfer_id join locations l on l.id = r.location_id
			where r.kind = 'receipt' and r.status in ('posted', 'awaiting_egais') and r.posted_at > now() - interval '2 days'`,
		render: func(r []any) (string, string, string, string) {
			return "transfer-arrived:" + str(r[0]), "Товар прибыл: " + str(r[3]), "Принят в " + str(r[4]), "/documents/" + str(r[0])
		},
	},
	{
		kind: "reservation_created", permission: apispec.PermissionReservationsManage,
		sql: `select r.id, r.location_id, r.created_by, r.number, r.customer_name
			from reservations r where r.status = 'new' and r.created_at > now() - interval '1 day'`,
		render: func(r []any) (string, string, string, string) {
			return "res:" + str(r[0]), "Новая бронь " + str(r[3]), "Покупатель: " + str(r[4]) + ". Подтвердите сканом бутылки.", "/reservations"
		},
	},
	{
		kind: "reservation_expiring", permission: apispec.PermissionReservationsManage,
		sql: `select r.id, r.location_id, null::uuid, r.number, to_char(r.expires_at at time zone l.timezone, 'HH24:MI')
			from reservations r join locations l on l.id = r.location_id
			where r.status in ('new', 'confirmed') and r.expires_at between now() and now() + interval '2 hours'`,
		render: func(r []any) (string, string, string, string) {
			return "res-exp:" + str(r[0]), "Бронь " + str(r[3]) + " скоро истекает", "Срок — до " + str(r[4]), "/reservations"
		},
	},
	{
		kind: "approval", permission: apispec.PermissionDocsApprove,
		sql: `select d.id, d.location_id, d.author_id, d.number, d.kind || ':' || d.version
			from documents d where d.status = 'pending_approval'`,
		render: func(r []any) (string, string, string, string) {
			return "approve:" + str(r[0]) + ":" + str(r[4]), "Ждёт утверждения: " + str(r[3]), "", "/documents/" + str(r[0])
		},
	},
	{
		kind: "approval", permission: apispec.PermissionDocsApprove,
		sql: `select w.id, a.location_id, w.requested_by, a.number, a.name
			from asset_writeoffs w join assets a on a.id = w.asset_id where w.status = 'pending'`,
		render: func(r []any) (string, string, string, string) {
			return "asset-wo:" + str(r[0]), "Списание имущества " + str(r[3]) + " ждёт утверждения", str(r[4]), ""
		},
	},
	{
		kind: "utm_down", permission: apispec.PermissionReceiptEdit,
		sql: `select l.id, l.id, null::uuid, l.code, to_char(now(), 'YYYYMMDDHH24')
			from locations l
			where exists (select 1 from egais_messages m where m.location_id = l.id and m.direction = 'out'
			              and m.status = 'queued' and m.attempts > 0 and m.created_at < now() - interval '10 minutes')`,
		render: func(r []any) (string, string, string, string) {
			return "utm:" + str(r[0]) + ":" + str(r[4]), "УТМ точки " + str(r[3]) + " недоступен",
				"Сообщения в ЕГАИС ждут отправки больше 10 минут. Проверьте компьютер с УТМ и ключ.", ""
		},
	},
	{
		kind: "shift_open", permission: apispec.PermissionPosDevices,
		sql: `select s.id, d.location_id, null::uuid, d.name, s.number::text
			from pos_shifts s join pos_devices d on d.id = s.device_id
			where s.status = 'open' and s.opened_at < now() - interval '13 hours'`,
		render: func(r []any) (string, string, string, string) {
			return "shift:" + str(r[0]), "Смена не закрыта: " + str(r[3]), "Смена № " + str(r[4]) + " открыта больше 13 часов", ""
		},
	},
	{
		kind: "backup_missing", permission: apispec.PermissionUsersManage,
		sql: `select null::uuid, null::uuid, null::uuid, to_char(now(), 'YYYYMMDD'), ''
			where not exists (select 1 from backups where ok and started_at > now() - interval '26 hours')`,
		render: func(r []any) (string, string, string, string) {
			return "backup:" + str(r[3]), "Нет резервной копии за последние сутки",
				"Проверьте задание резервного копирования на сервере", "/system"
		},
	},
	{
		kind: "below_min", permission: apispec.PermissionReceiptEdit,
		sql: `select m.location_id, m.location_id, null::uuid, count(*)::text, to_char(now(), 'YYYYMMDD')
			from assortment_matrix m
			join locations l on l.id = m.location_id and l.is_active
			where coalesce((select sum(b.on_hand - b.reserved) from stock_balances b
			                where b.location_id = m.location_id and b.variant_id = m.variant_id), 0) < m.min_qty
			group by m.location_id`,
		render: func(r []any) (string, string, string, string) {
			return "min:" + str(r[0]) + ":" + str(r[4]), "Товаров ниже минимума: " + str(r[3]),
				"Остаток меньше минимального по ассортиментной матрице", "/reports?report=replenishment"
		},
	},
}

// Scan проходит все правила и раздаёт новые уведомления. Возвращает, сколько создано.
func (s *Service) Scan(ctx context.Context) (int, error) {
	people, err := s.recipients(ctx)
	if err != nil {
		return 0, err
	}
	var events []Event
	for _, r := range rules {
		rows, err := s.pool.Query(ctx, r.sql)
		if err != nil {
			return 0, fmt.Errorf("rule %s: %w", r.kind, err)
		}
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				rows.Close()
				return 0, err
			}
			var loc uuid.UUID
			switch v := vals[1].(type) {
			case [16]byte:
				loc = v
			}
			var exclude *uuid.UUID
			if v, ok := vals[2].([16]byte); ok {
				u := uuid.UUID(v)
				exclude = &u
			}
			for i, v := range vals {
				if b, ok := v.([16]byte); ok {
					vals[i] = uuid.UUID(b)
				}
			}
			key, title, body, link := r.render(vals)
			events = append(events, Event{Key: key, Kind: r.kind, Location: loc, Permission: r.permission,
				Title: title, Body: body, Link: link, Exclude: exclude})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, err
		}
	}
	diff, err := s.reconciliationEvents(ctx)
	if err != nil {
		return 0, err
	}
	events = append(events, diff...)
	return s.deliver(ctx, people, events)
}

// reconciliationEvents — расхождение с ЕГАИС по свежим снимкам остатков.
func (s *Service) reconciliationEvents(ctx context.Context) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `select distinct on (r.location_id) r.id, r.location_id, l.code
		from egais_rests_snapshots r join locations l on l.id = r.location_id
		where r.taken_at > now() - interval '1 day' order by r.location_id, r.taken_at desc`)
	if err != nil {
		return nil, err
	}
	type snap struct {
		id, location uuid.UUID
		code         string
	}
	snaps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (snap, error) {
		var s snap
		return s, r.Scan(&s.id, &s.location, &s.code)
	})
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, sn := range snaps {
		rep, err := s.reconcile.Differences(ctx, sn.location)
		if err != nil {
			return nil, err
		}
		if n := len(rep.Products) + len(rep.Marks); n > 0 {
			out = append(out, Event{Key: "recon:" + sn.id.String(), Kind: "egais_mismatch", Location: sn.location,
				Permission: apispec.PermissionEgaisReconcile, Title: fmt.Sprintf("Расхождения с ЕГАИС в %s: %d", sn.code, n),
				Body: "Остатки ЕГАИС не совпадают с учётом", Link: "/egais/reconciliation"})
		}
	}
	return out, nil
}

func (s *Service) recipients(ctx context.Context) ([]recipient, error) {
	rows, err := s.pool.Query(ctx, `select u.id, u.role, coalesce(array_agg(ul.location_id) filter (where ul.location_id is not null), '{}')
		from users u left join user_locations ul on ul.user_id = u.id
		where not u.is_blocked group by u.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (recipient, error) {
		var p recipient
		return p, r.Scan(&p.id, &p.role, &p.locations)
	})
}

// deliver раздаёт события тем, у кого есть право в точке события. Уже выданные пропускаются.
func (s *Service) deliver(ctx context.Context, people []recipient, events []Event) (int, error) {
	var users []uuid.UUID
	var kinds, titles, bodies, links, keys []string
	for _, e := range events {
		for _, p := range people {
			if e.Exclude != nil && *e.Exclude == p.id {
				continue
			}
			if !auth.RoleCan(p.role, e.Permission, e.Location, p.locations) {
				continue
			}
			users = append(users, p.id)
			kinds, titles, bodies, links, keys = append(kinds, e.Kind), append(titles, e.Title), append(bodies, e.Body),
				append(links, e.Link), append(keys, e.Key)
		}
	}
	if len(users) == 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `insert into notifications (user_id, kind, title, body, link, dedupe_key)
		select u, k, t, b, nullif(l, ''), d from unnest($1::uuid[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[])
		as x(u, k, t, b, l, d)
		on conflict (user_id, dedupe_key) do nothing`, users, kinds, titles, bodies, links, keys)
	return int(tag.RowsAffected()), err
}

// Send — уведомление одному пользователю сразу, например «отчёт готов».
func Send(ctx context.Context, db interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, user uuid.UUID, kind, key, title, body, link string) error {
	_, err := db.Exec(ctx, `insert into notifications (user_id, kind, title, body, link, dedupe_key)
		values ($1, $2, $3, $4, nullif($5, ''), $6) on conflict (user_id, dedupe_key) do nothing`, user, kind, title, body, link, key)
	return err
}

// Notification — запись центра уведомлений.
type Notification struct {
	ID                uuid.UUID
	Kind, Title, Body string
	Link              *string
	CreatedAt         time.Time
	ReadAt            *time.Time
}

func me(ctx context.Context) uuid.UUID {
	s, _ := auth.FromContext(ctx)
	return s.UserID
}

// List — последние уведомления пользователя и число непрочитанных.
func (s *Service) List(ctx context.Context, unread bool) ([]Notification, int, error) {
	rows, err := s.pool.Query(ctx, `select id, kind, title, body, link, created_at, read_at from notifications
		where user_id = $1 and (not $2 or read_at is null) order by created_at desc limit 50`, me(ctx), unread)
	if err != nil {
		return nil, 0, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notification, error) {
		var n Notification
		return n, r.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.CreatedAt, &n.ReadAt)
	})
	if err != nil {
		return nil, 0, err
	}
	var count int
	err = s.pool.QueryRow(ctx, `select count(*) from notifications where user_id = $1 and read_at is null`, me(ctx)).Scan(&count)
	return items, count, err
}

// Read отмечает прочитанным одно уведомление или, если id нет, все.
func (s *Service) Read(ctx context.Context, id *uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `update notifications set read_at = now()
		where user_id = $1 and read_at is null and ($2::uuid is null or id = $2)`, me(ctx), id)
	if err == nil && id != nil && tag.RowsAffected() == 0 {
		var exists bool
		_ = s.pool.QueryRow(ctx, `select exists (select 1 from notifications where id = $1 and user_id = $2)`, *id, me(ctx)).Scan(&exists)
		if !exists {
			return ErrNotFound
		}
	}
	return err
}
