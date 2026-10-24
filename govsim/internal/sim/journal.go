package sim

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// JournalEntry — документ во «входящих» ЕГАИС или созданный генератором, с итогом проверки.
type JournalEntry struct {
	ID           uuid.UUID
	CreatedAt    time.Time
	From         string
	To           *string
	Type         string
	Status       string
	RejectReason *string
	Payload      json.RawMessage
}

func (s *Sim) Journal(ctx context.Context, fsrar string) ([]JournalEntry, error) {
	rows, err := s.pool.Query(ctx, `select id, created_at, fsrar_from, fsrar_to, type, status, reject_reason, payload
		from documents where $1 = '' or fsrar_from = $1 or fsrar_to = $1 order by created_at desc limit 200`, fsrar)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[JournalEntry])
}

// MarkEvent — событие в истории марки.
type MarkEvent struct {
	At         time.Time
	Event      string
	FSRARID    *string
	DocumentID *uuid.UUID
}

// MarkInfo — марка «глазами ЕГАИС»: владелец, статус и история.
type MarkInfo struct {
	Code, Alcocode, Name string
	Owner, Status        string
	Blocked              bool
	Events               []MarkEvent
}

func (s *Sim) Mark(ctx context.Context, code string) (MarkInfo, error) {
	var m MarkInfo
	err := s.pool.QueryRow(ctx, `select m.code, m.alcocode, p.name, m.owner_fsrar, m.status, m.blocked
		from marks m join products p using (alcocode) where m.code = $1`, code).
		Scan(&m.Code, &m.Alcocode, &m.Name, &m.Owner, &m.Status, &m.Blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	rows, err := s.pool.Query(ctx, `select at, event, fsrar_id, document_id from mark_events where code = $1 order by id`, code)
	if err != nil {
		return m, err
	}
	m.Events, err = pgx.CollectRows(rows, pgx.RowToStructByPos[MarkEvent])
	return m, err
}

// SetMarkBlocked — сценарий «марка заблокирована».
func (s *Sim) SetMarkBlocked(ctx context.Context, code string, blocked bool) error {
	tag, err := s.pool.Exec(ctx, `update marks set blocked = $2 where code = $1`, code, blocked)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SetRest — сценарий «расхождение для сверки»: остаток на регистре меняется в обход документов.
func (s *Sim) SetRest(ctx context.Context, fsrar, alcocode string, register, qty int) error {
	_, err := s.pool.Exec(ctx, `insert into rests (fsrar_id, alcocode, register, qty) values ($1, $2, $3, $4)
		on conflict (fsrar_id, alcocode, register) do update set qty = excluded.qty`, fsrar, alcocode, register, max(qty, 0))
	return err
}

// Products — справочник продукции для формы поставки.
type Product struct {
	Alcocode, Name, Accounting string
}

func (s *Sim) Products(ctx context.Context) ([]Product, error) {
	rows, err := s.pool.Query(ctx, `select alcocode, name || coalesce(', ' || (volume_ml::numeric / 1000)::text || ' л', ''),
		egais_accounting from products order by name, volume_ml`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Product])
}
