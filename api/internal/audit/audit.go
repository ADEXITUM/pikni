// Package audit — журнал действий: запись событий в транзакции изменения и чтение с фильтрами.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit/store"
)

const (
	EntitySession = "session"
	EntityUser    = "user"
)

// DB — пул или транзакция. Запись передают транзакцию самого изменения,
// чтобы изменение и запись о нём сохранились вместе.
type DB = store.DBTX

type Event struct {
	Action     apispec.AuditAction
	EntityType string
	EntityID   string
	Data       map[string]any
}

// Actor — кто выполняет действие. Без входа (неудачный вход, консоль сервера) UserID пустой.
type Actor struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	IP        string
}

type actorKey struct{}

func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

func Record(ctx context.Context, db DB, e Event) error {
	a, _ := ctx.Value(actorKey{}).(Actor)

	data := []byte("{}")
	if e.Data != nil {
		var err error
		if data, err = json.Marshal(e.Data); err != nil {
			return fmt.Errorf("encode audit data: %w", err)
		}
	}

	err := store.New(db).InsertEntry(ctx, store.InsertEntryParams{
		UserID:     nonNil(a.UserID),
		SessionID:  nonNil(a.SessionID),
		IP:         a.IP,
		Action:     string(e.Action),
		EntityType: nonEmpty(e.EntityType),
		EntityID:   nonEmpty(e.EntityID),
		Data:       data,
	})
	if err != nil {
		return fmt.Errorf("record %s: %w", e.Action, err)
	}
	return nil
}

func nonNil(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
