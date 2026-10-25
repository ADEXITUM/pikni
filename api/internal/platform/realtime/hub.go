// Package realtime доставляет события об изменениях в открытые экраны (УВ-1). Источник событий —
// канал PostgreSQL pikni_events: триггеры объявляют в него изменения, а каждый экземпляр сервера
// слушает канал и рассылает событие тем, кто подписан на его тему по WebSocket.
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const channel = "pikni_events"

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

// Run слушает канал до отмены контекста. Потерянное соединение восстанавливается; события,
// объявленные, пока его не было, теряются — клиенты после переподключения перезапрашивают данные.
func (h *Hub) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := h.listen(ctx); err != nil && ctx.Err() == nil {
			h.log.Warn("realtime listen", "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (h *Hub) listen(ctx context.Context) error {
	pooled, err := h.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// Соединение после LISTEN в пул не возвращается: в нём осталась бы подписка.
	conn := pooled.Hijack()
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	if _, err := conn.Exec(ctx, "listen "+channel); err != nil {
		return err
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var ev Event
		if err := json.Unmarshal([]byte(n.Payload), &ev); err != nil {
			h.log.Warn("realtime payload", "error", err)
			continue
		}
		h.dispatch(ev)
	}
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
