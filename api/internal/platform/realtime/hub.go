// Package realtime доставляет события об изменениях в открытые экраны (УВ-1). Источник событий —
// таблица realtime_events: триггеры записывают в неё изменения в той же транзакции, а каждый
// экземпляр сервера раз в 200 мс забирает новые события и рассылает их подписчикам по WebSocket
// (решение 0064: NOTIFY выстраивал коммиты в очередь на весь сервер).
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	pollInterval = 200 * time.Millisecond
	// lookback — сколько последних номеров перечитывать: транзакция, начатая раньше, могла
	// зафиксироваться позже соседней и получить меньший номер, чем уже прочитанный.
	lookback = 1000
	keep     = time.Minute
)

// Event — что изменилось: тип (document, scan, stock, exchange, receipt), идентификатор и темы
// вида doc:{id}, location:{id}, product:{id}, shift:{id}.
type Event struct {
	Type   string   `json:"type"`
	ID     string   `json:"id"`
	Topics []string `json:"topics"`
	// Raw и Scan — у кода со смартфона (СК-6): его нечего перезапрашивать, он приходит сам.
	Raw  string `json:"raw,omitempty"`
	Scan string `json:"scan,omitempty"`
}

type subscriber struct {
	topics map[string]bool
	out    chan Event
}

type Hub struct {
	pool *pgxpool.Pool
	log  *slog.Logger

	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

func NewHub(pool *pgxpool.Pool, log *slog.Logger) *Hub {
	return &Hub{pool: pool, log: log, subs: map[*subscriber]struct{}{}}
}

// Run забирает события до отмены контекста. Событие, записанное, пока сервер не работал, не
// доставляется: клиенты после переподключения перезапрашивают данные.
func (h *Hub) Run(ctx context.Context) {
	var last int64
	_ = h.pool.QueryRow(ctx, `select coalesce(max(id), 0) from realtime_events`).Scan(&last)
	seen := map[int64]time.Time{}
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	cleaned := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		var err error
		if last, err = h.poll(ctx, last, seen); err != nil && ctx.Err() == nil {
			h.log.Warn("realtime poll", "error", err)
		}
		for id, at := range seen {
			if time.Since(at) > 10*time.Second {
				delete(seen, id)
			}
		}
		if time.Since(cleaned) > keep {
			cleaned = time.Now()
			if _, err := h.pool.Exec(ctx, `delete from realtime_events where created_at < now() - $1::interval`, keep.String()); err != nil && ctx.Err() == nil {
				h.log.Warn("realtime cleanup", "error", err)
			}
		}
	}
}

func (h *Hub) poll(ctx context.Context, last int64, seen map[int64]time.Time) (int64, error) {
	rows, err := h.pool.Query(ctx, `select id, payload from realtime_events where id > $1 order by id limit 20000`, last-lookback)
	if err != nil {
		return last, err
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var id int64
		var payload []byte
		if err := rows.Scan(&id, &payload); err != nil {
			return last, err
		}
		last = max(last, id)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = now
		var ev Event
		if err := json.Unmarshal(payload, &ev); err != nil {
			h.log.Warn("realtime payload", "error", err)
			continue
		}
		h.dispatch(ev)
	}
	return last, rows.Err()
}

func (h *Hub) dispatch(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		for _, t := range ev.Topics {
			if s.topics[t] {
				// Медленный клиент не задерживает остальных: лишнее событие отбрасывается,
				// клиент всё равно перезапросит данные по следующему.
				select {
				case s.out <- ev:
				default:
				}
				break
			}
		}
	}
}

func (h *Hub) add() *subscriber {
	s := &subscriber{topics: map[string]bool{}, out: make(chan Event, 64)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (h *Hub) remove(s *subscriber) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

func (h *Hub) subscribe(s *subscriber, topics []string, on bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range topics {
		if on && len(s.topics) < maxTopics {
			s.topics[t] = true
		} else if !on {
			delete(s.topics, t)
		}
	}
}
