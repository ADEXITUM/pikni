package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/coder/websocket"
)

// Тема — вид и UUID; лишнего клиент подписать не может.
var topicPattern = regexp.MustCompile(`^(doc|location|product|shift):[0-9a-f-]{36}$`)

// maxTopics — экран подписывается на несколько тем; сотня — с большим запасом.
const maxTopics = 100

type command struct {
	Subscribe   []string `json:"subscribe"`
	Unsubscribe []string `json:"unsubscribe"`
}

// Serve обслуживает WebSocket. Вход проверяет вызывающий; библиотека сверяет Origin с адресом
// сервера, поэтому чужой сайт не откроет соединение с cookie пользователя.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request) error {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	s := h.add()
	defer h.remove(s)

	go func() {
		defer cancel()
		for {
			var cmd command
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if json.Unmarshal(data, &cmd) != nil {
				continue
			}
			h.subscribe(s, valid(cmd.Subscribe), true)
			h.subscribe(s, valid(cmd.Unsubscribe), false)
		}
	}()

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-s.out:
			data, _ := json.Marshal(ev)
			wctx, done := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Write(wctx, websocket.MessageText, data)
			done()
			if err != nil {
				return nil
			}
		case <-ping.C:
			if err := conn.Ping(ctx); err != nil {
				return nil
			}
		}
	}
}

func valid(topics []string) []string {
	out := make([]string, 0, len(topics))
	for _, t := range topics {
		if topicPattern.MatchString(t) {
			out = append(out, t)
		}
	}
	return out
}
