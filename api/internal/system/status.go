// Package system — страница администратора «Состояние системы» (п. 4.1.5): версия, очередь
// фоновых задач, резервная копия, целостность остатков, связь с УТМ точек и с имитатором.
package system

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
)

type Handler struct {
	pool      *pgxpool.Pool
	version   string
	govsimURL string
}

func NewHandler(pool *pgxpool.Pool, version, govsimURL string) *Handler {
	return &Handler{pool: pool, version: version, govsimURL: govsimURL}
}

// УТМ точки считается недоступным, если сообщение ждёт отправки дольше 5 минут (п. 4.1.5).
const utmDownAfter = 5 * time.Minute

func (h *Handler) GetSystemStatus(ctx context.Context, _ apispec.GetSystemStatusRequestObject) (apispec.GetSystemStatusResponseObject, error) {
	out := apispec.GetSystemStatus200JSONResponse{Version: h.version, CheckedAt: time.Now()}
	err := h.pool.QueryRow(ctx, `select
		  count(*) filter (where state = 'available'), count(*) filter (where state = 'running'),
		  count(*) filter (where state = 'retryable'),
		  count(*) filter (where state = 'discarded' and finalized_at > now() - interval '1 day')
		from river_job`).Scan(&out.Jobs.Available, &out.Jobs.Running, &out.Jobs.Retryable, &out.Jobs.FailedDay)
	if err != nil {
		return nil, err
	}
	var b struct {
		at   time.Time
		ok   bool
		file string
		size *int64
	}
	err = h.pool.QueryRow(ctx, `select started_at, ok, file, size_bytes from backups order by id desc limit 1`).Scan(&b.at, &b.ok, &b.file, &b.size)
	switch {
	case err == nil:
		out.LastBackup = &struct {
			At        time.Time `json:"at"`
			File      string    `json:"file"`
			Ok        bool      `json:"ok"`
			SizeBytes *int64    `json:"sizeBytes,omitempty"`
		}{At: b.at, File: b.file, Ok: b.ok, SizeBytes: b.size}
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}
	// Целостность — по последней фоновой проверке: на большой сети она идёт минуты (решение 0064).
	if err := h.pool.QueryRow(ctx, `select
		  coalesce((select problems from integrity_checks order by id desc limit 1), 0),
		  (select checked_at from integrity_checks order by id desc limit 1),
		  (select count(*) from documents where kind = 'receipt' and status = 'draft' and ttn_id is not null),
		  (select count(*) from pos_shifts where status = 'open')`).
		Scan(&out.IntegrityProblems, &out.IntegrityCheckedAt, &out.UnconfirmedTtn, &out.OpenShifts); err != nil {
		return nil, err
	}
	rows, err := h.pool.Query(ctx, `select l.code, l.name, l.fsrar_id is not null,
		  count(m.id) filter (where m.status = 'queued'), min(m.created_at) filter (where m.status = 'queued'),
		  bool_or(m.status = 'queued' and m.attempts > 0),
		  max(m.updated_at) filter (where m.status in ('sent', 'accepted', 'rejected', 'processed'))
		from locations l left join egais_messages m on m.location_id = l.id and m.direction = 'out'
		where l.is_active group by l.id
		order by min(m.created_at) filter (where m.status = 'queued') nulls last, l.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code, name string
		var fsrar bool
		var queued int
		var oldest, last *time.Time
		var failing *bool
		if err := rows.Scan(&code, &name, &fsrar, &queued, &oldest, &failing, &last); err != nil {
			return nil, err
		}
		utm := "ok"
		switch {
		case !fsrar:
			utm = "none"
		case oldest != nil && time.Since(*oldest) > utmDownAfter && failing != nil && *failing:
			utm = "down"
		case oldest != nil && time.Since(*oldest) > time.Minute:
			utm = "delayed"
		}
		out.Locations = append(out.Locations, struct {
			Code           string                           `json:"code"`
			LastExchangeAt *time.Time                       `json:"lastExchangeAt,omitempty"`
			Name           string                           `json:"name"`
			OldestQueuedAt *time.Time                       `json:"oldestQueuedAt,omitempty"`
			Queued         int                              `json:"queued"`
			Utm            apispec.SystemStatusLocationsUtm `json:"utm"`
		}{Code: code, Name: name, Queued: queued, OldestQueuedAt: oldest, LastExchangeAt: last, Utm: apispec.SystemStatusLocationsUtm(utm)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	govsim := apispec.SystemStatusGovsimOff
	if h.govsimURL != "" {
		govsim = apispec.SystemStatusGovsimDown
		hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(hctx, http.MethodGet, h.govsimURL+"/healthz", nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				govsim = apispec.SystemStatusGovsimOk
			}
		}
	}
	out.Govsim = &govsim
	return out, nil
}
