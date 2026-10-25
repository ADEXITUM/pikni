package realtime_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/platform/realtime"
)

func TestEventsReachSubscribers(t *testing.T) {
	db := dbtest.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	hub := realtime.NewHub(db.App, slog.Default())
	go hub.Run(ctx)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = hub.Serve(w, r, "u1")
	}))
	defer srv.Close()

	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = conn.CloseNow() }()

	doc, other := uuid.New(), uuid.New()
	sub, _ := json.Marshal(map[string][]string{"subscribe": {"doc:" + doc.String(), "bogus", "doc:x"}})
	if err := conn.Write(ctx, websocket.MessageText, sub); err != nil {
		t.Fatal(err)
	}
	// Пока подписка не дошла до сервера, событие может пройти мимо — объявляем, пока не придёт.
	got := make(chan realtime.Event, 1)
	go func() {
		_, data, err := conn.Read(ctx)
		if err == nil {
			var ev realtime.Event
			_ = json.Unmarshal(data, &ev)
			got <- ev
		}
	}()
	for {
		if _, err := db.App.Exec(ctx, "select pikni_event('document', $1::uuid, array['doc:' || $2::text])", other, other.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := db.App.Exec(ctx, "select pikni_event('document', $1::uuid, array['doc:' || $2::text])", doc, doc.String()); err != nil {
			t.Fatal(err)
		}
		select {
		case ev := <-got:
			if ev.Type != "document" || ev.ID != doc.String() {
				t.Fatalf("event %+v: only the subscribed document arrives", ev)
			}
			return
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("no event within 20 s")
		}
	}
}
