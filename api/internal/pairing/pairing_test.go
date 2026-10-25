package pairing_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/pairing"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func TestPairing(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	session := func(login string) context.Context {
		var id uuid.UUID
		must(db.App.QueryRow(ctx, `insert into users (login, full_name, role, password_hash) values ($1, $1, 'storekeeper', 'x') returning id`,
			login).Scan(&id))
		return auth.WithSession(ctx, auth.Session{UserID: id, Role: "storekeeper"})
	}
	pc, other := session("keeper"), session("other")
	phone := pc
	svc := pairing.NewService(db.App)

	conn, err := db.App.Acquire(ctx)
	must(err)
	defer conn.Release()
	_, err = conn.Exec(ctx, "listen pikni_events")
	must(err)
	next := func() map[string]string {
		t.Helper()
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		n, err := conn.Conn().WaitForNotification(wctx)
		must(err)
		var ev map[string]any
		must(json.Unmarshal([]byte(n.Payload), &ev))
		out := map[string]string{}
		for k, v := range ev {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
		return out
	}

	id, code, _, err := svc.Open(pc)
	must(err)
	if _, _, err := svc.Join(other, code); !errors.Is(err, pairing.ErrNotFound) {
		t.Fatalf("join by another user = %v", err)
	}
	if _, _, err := svc.Join(phone, code+"x"); !errors.Is(err, pairing.ErrNotFound) {
		t.Fatalf("join with a wrong secret = %v", err)
	}
	if _, _, err := svc.Join(phone, "4600000000016"); !errors.Is(err, pairing.ErrBadCode) {
		t.Fatalf("join with a barcode = %v", err)
	}
	if err := svc.Scan(phone, id, uuid.New(), "4600000000016"); !errors.Is(err, pairing.ErrExpired) {
		t.Fatalf("scan before join = %v", err)
	}
	joined, _, err := svc.Join(phone, code)
	must(err)
	if joined != id {
		t.Fatalf("joined %s, opened %s", joined, id)
	}
	if ev := next(); ev["type"] != "paired" || ev["id"] != id.String() {
		t.Fatalf("event = %v", ev)
	}

	scan := uuid.New()
	must(svc.Scan(phone, id, scan, "4600000000016"))
	if ev := next(); ev["type"] != "paired_scan" || ev["raw"] != "4600000000016" || ev["scan"] != scan.String() {
		t.Fatalf("event = %v", ev)
	}
	if err := svc.Scan(other, id, uuid.New(), "x"); !errors.Is(err, pairing.ErrNotFound) {
		t.Fatalf("scan by another user = %v", err)
	}

	must(svc.Close(pc, id))
	if ev := next(); ev["type"] != "unpaired" {
		t.Fatalf("event = %v", ev)
	}
	status, _, err := svc.Status(phone, id)
	must(err)
	if status != "closed" {
		t.Fatalf("status = %s", status)
	}
	if err := svc.Scan(phone, id, uuid.New(), "4600000000016"); !errors.Is(err, pairing.ErrExpired) {
		t.Fatalf("scan after close = %v", err)
	}
}
