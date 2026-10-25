// Package marks — помарочный учёт ЕГАИС: где находится каждая марка, в каком она статусе
// и как туда попала (СО-1, СО-8). Марки меняются только при проведении документов.
package marks

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/marks/store"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

const (
	InStock    = "in_stock"
	InTransit  = "in_transit"
	Reserved   = "reserved"
	Sold       = "sold"
	WrittenOff = "written_off"
	Returned   = "returned"
)

var ErrNotFound = errors.New("mark not found")

// TakenError — марка уже есть в учёте: повторно её ввести нельзя.
type TakenError struct{ Code string }

func (e *TakenError) Error() string { return fmt.Sprintf("mark %s is already registered", e.Code) }

// MisplacedError — марка не там, откуда её перемещают, или уже выбыла.
type MisplacedError struct {
	Code   string
	Status string
}

func (e *MisplacedError) Error() string {
	return fmt.Sprintf("mark %s is not in the source zone (status %s)", e.Code, e.Status)
}

type Mark = store.FindMarkRow

type Service struct {
	q *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{q: store.New(pool)}
}

// Find возвращает марку или nil, если её нет в учёте.
func (s *Service) Find(ctx context.Context, code string) (*Mark, error) {
	m, err := s.q.FindMark(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MarkVariant — вариант бутылки с этой маркой; nil, если марки нет в учёте.
func (s *Service) MarkVariant(ctx context.Context, code string) (*uuid.UUID, error) {
	m, err := s.Find(ctx, code)
	if m == nil || err != nil {
		return nil, err
	}
	return &m.VariantID, nil
}

// History — марка и её события по порядку.
func (s *Service) History(ctx context.Context, code string) (store.GetMarkRow, []store.ListMarkEventsRow, error) {
	m, err := s.q.GetMark(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, nil, ErrNotFound
	}
	if err != nil {
		return m, nil, err
	}
	events, err := s.q.ListMarkEvents(ctx, code)
	return m, events, err
}

// Event — кто и каким документом меняет марки.
type Event struct {
	DocumentID uuid.UUID
	UserID     uuid.UUID
}

// Register ставит новые марки на учёт в зоне (ввод остатков). Марка, которая уже есть в учёте, —
// TakenError: одна бутылка не может быть учтена дважды.
func Register(ctx context.Context, tx pgx.Tx, ev Event, location, zone uuid.UUID, codes []string, variants []uuid.UUID) error {
	q := store.New(tx)
	existing, err := q.LockMarks(ctx, codes)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return &TakenError{Code: existing[0].Code}
	}
	err = q.CreateMarks(ctx, store.CreateMarksParams{
		Codes: codes, VariantIds: variants, LocationID: location, ZoneID: &zone, DocumentID: &ev.DocumentID,
	})
	if db.IsUniqueViolation(err) {
		return &TakenError{Code: codes[0]}
	}
	if err != nil {
		return err
	}
	from := make([]string, len(codes))
	fromZones := make([]uuid.UUID, len(codes))
	return q.InsertMarkEvents(ctx, store.InsertMarkEventsParams{
		Codes: codes, FromStatuses: from, FromZoneIds: fromZones,
		DocumentID: &ev.DocumentID, UserID: &ev.UserID, ToStatus: InStock, ToZoneID: &zone,
	})
}

// MoveWithin переносит марки, стоящие в зоне from, в зону to той же или другой точки.
// Марка не на месте или уже выбывшая — MisplacedError.
func MoveWithin(ctx context.Context, tx pgx.Tx, ev Event, from, toLocation, to uuid.UUID, codes []string) error {
	q := store.New(tx)
	rows, err := q.LockMarks(ctx, codes)
	if err != nil {
		return err
	}
	found := make(map[string]store.LockMarksRow, len(rows))
	for _, r := range rows {
		found[r.Code] = r
	}
	fromStatuses := make([]string, len(codes))
	fromZones := make([]uuid.UUID, len(codes))
	for i, c := range codes {
		r, ok := found[c]
		if !ok {
			return &MisplacedError{Code: c}
		}
		if r.Status != InStock || r.ZoneID == nil || *r.ZoneID != from {
			return &MisplacedError{Code: c, Status: r.Status}
		}
		fromStatuses[i], fromZones[i] = r.Status, from
	}
	if err := q.MoveMarks(ctx, store.MoveMarksParams{
		Codes: codes, LocationID: toLocation, ZoneID: &to, Status: InStock, DocumentID: &ev.DocumentID,
	}); err != nil {
		return err
	}
	return q.InsertMarkEvents(ctx, store.InsertMarkEventsParams{
		Codes: codes, FromStatuses: fromStatuses, FromZoneIds: fromZones,
		DocumentID: &ev.DocumentID, UserID: &ev.UserID, ToStatus: InStock, ToZoneID: &to,
	})
}

// Sell отмечает марки проданными: каждая должна числиться на остатке в точке location.
// Возвращает зону, где стояла каждая марка, — оттуда списывается остаток.
func Sell(ctx context.Context, tx pgx.Tx, ev Event, location uuid.UUID, codes []string) (map[string]uuid.UUID, error) {
	q := store.New(tx)
	rows, err := q.LockMarks(ctx, codes)
	if err != nil {
		return nil, err
	}
	found := make(map[string]store.LockMarksRow, len(rows))
	for _, r := range rows {
		found[r.Code] = r
	}
	zones := make(map[string]uuid.UUID, len(codes))
	statuses := make([]string, len(codes))
	fromZones := make([]uuid.UUID, len(codes))
	for i, c := range codes {
		r, ok := found[c]
		if !ok || r.Status != InStock || r.LocationID != location || r.ZoneID == nil {
			st := ""
			if ok {
				st = r.Status
			}
			return nil, &MisplacedError{Code: c, Status: st}
		}
		zones[c] = *r.ZoneID
		statuses[i], fromZones[i] = r.Status, *r.ZoneID
	}
	if err := q.SetMarksStatus(ctx, store.SetMarksStatusParams{Codes: codes, Status: Sold, DocumentID: &ev.DocumentID}); err != nil {
		return nil, err
	}
	return zones, q.InsertMarkEvents(ctx, store.InsertMarkEventsParams{
		Codes: codes, FromStatuses: statuses, FromZoneIds: fromZones,
		DocumentID: &ev.DocumentID, UserID: &ev.UserID, ToStatus: Sold,
	})
}

// Unsell возвращает проданные марки на остаток в зону zone — возврат покупателя.
func Unsell(ctx context.Context, tx pgx.Tx, ev Event, location, zone uuid.UUID, codes []string) error {
	q := store.New(tx)
	rows, err := q.LockMarks(ctx, codes)
	if err != nil {
		return err
	}
	found := make(map[string]store.LockMarksRow, len(rows))
	for _, r := range rows {
		found[r.Code] = r
	}
	statuses := make([]string, len(codes))
	for i, c := range codes {
		r, ok := found[c]
		if !ok || r.Status != Sold || r.LocationID != location {
			return &MisplacedError{Code: c, Status: r.Status}
		}
		statuses[i] = r.Status
	}
	if err := q.SetMarksStatus(ctx, store.SetMarksStatusParams{Codes: codes, Status: InStock, ZoneID: &zone, DocumentID: &ev.DocumentID}); err != nil {
		return err
	}
	return q.InsertMarkEvents(ctx, store.InsertMarkEventsParams{
		Codes: codes, FromStatuses: statuses, FromZoneIds: make([]uuid.UUID, len(codes)),
		DocumentID: &ev.DocumentID, UserID: &ev.UserID, ToStatus: InStock, ToZoneID: &zone,
	})
}

// Ship отправляет марки из зоны from в другую точку: до приёмки они «в пути» у получателя (СО-4).
func Ship(ctx context.Context, tx pgx.Tx, ev Event, from, toLocation uuid.UUID, codes []string) error {
	q := store.New(tx)
	rows, err := q.LockMarks(ctx, codes)
	if err != nil {
		return err
	}
	found := make(map[string]store.LockMarksRow, len(rows))
	for _, r := range rows {
		found[r.Code] = r
	}
	statuses := make([]string, len(codes))
	fromZones := make([]uuid.UUID, len(codes))
	for i, c := range codes {
		r, ok := found[c]
		if !ok || r.Status != InStock || r.ZoneID == nil || *r.ZoneID != from {
			return &MisplacedError{Code: c, Status: r.Status}
		}
		statuses[i], fromZones[i] = r.Status, from
	}
	if err := q.MoveMarks(ctx, store.MoveMarksParams{
		Codes: codes, LocationID: toLocation, Status: InTransit, DocumentID: &ev.DocumentID,
	}); err != nil {
		return err
	}
	return q.InsertMarkEvents(ctx, store.InsertMarkEventsParams{
		Codes: codes, FromStatuses: statuses, FromZoneIds: fromZones,
		DocumentID: &ev.DocumentID, UserID: &ev.UserID, ToStatus: InTransit,
	})
}

// Arrive ставит пришедшие марки в зону zone точки-получателя.
func Arrive(ctx context.Context, tx pgx.Tx, ev Event, location, zone uuid.UUID, codes []string) error {
	q := store.New(tx)
	rows, err := q.LockMarks(ctx, codes)
	if err != nil {
		return err
	}
	found := make(map[string]store.LockMarksRow, len(rows))
	for _, r := range rows {
		found[r.Code] = r
	}
	statuses := make([]string, len(codes))
	for i, c := range codes {
		r, ok := found[c]
		if !ok || r.Status != InTransit || r.LocationID != location {
			return &MisplacedError{Code: c, Status: r.Status}
		}
		statuses[i] = r.Status
	}
	if err := q.MoveMarks(ctx, store.MoveMarksParams{
		Codes: codes, LocationID: location, ZoneID: &zone, Status: InStock, DocumentID: &ev.DocumentID,
	}); err != nil {
		return err
	}
	return q.InsertMarkEvents(ctx, store.InsertMarkEventsParams{
		Codes: codes, FromStatuses: statuses, FromZoneIds: make([]uuid.UUID, len(codes)),
		DocumentID: &ev.DocumentID, UserID: &ev.UserID, ToStatus: InStock, ToZoneID: &zone,
	})
}
