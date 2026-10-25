// Package reservations — брони покупателей (НЛ-4, НЛ-5): сотрудник откладывает товар по звонку
// или визиту покупателя, точка хранения подтверждает бронь сканом бутылки, бронь выдаётся через
// кассу с обычными проверками продажи. Просроченные брони снимаются сами, данные покупателя
// обезличиваются через 30 дней после закрытия.
package reservations

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/reservations/store"
)

const (
	StatusNew       = "new"
	StatusConfirmed = "confirmed"
	StatusIssued    = "issued"
	StatusCancelled = "cancelled"
	StatusExpired   = "expired"
)

var (
	ErrNotFound      = errors.New("reservation not found")
	ErrConsent       = errors.New("customer consent is required")
	ErrBadCustomer   = errors.New("customer name and phone are required")
	ErrNotOpen       = errors.New("reservation is not open")
	ErrNotConfirmed  = errors.New("reservation is not confirmed")
	ErrWrongItem     = errors.New("scanned item does not match the reservation")
	ErrWrongCount    = errors.New("scanned marks do not match the quantity")
	ErrNotEnough     = errors.New("not enough free stock")
	ErrInactive      = errors.New("location or item is archived")
	ErrNotStore      = errors.New("reservations are held only in stores")
	ErrWrongLocation = errors.New("reservation belongs to another location")
)

var phonePattern = regexp.MustCompile(`^\+?[0-9 ()-]{6,20}$`)

type Service struct {
	pool    *pgxpool.Pool
	q       *store.Queries
	catalog *catalog.Service
	marks   *marks.Service
}

func NewService(pool *pgxpool.Pool, cat *catalog.Service, mk *marks.Service) *Service {
	return &Service{pool: pool, q: store.New(pool), catalog: cat, marks: mk}
}

func userID(ctx context.Context) uuid.UUID {
	sess, _ := auth.FromContext(ctx)
	return sess.UserID
}

type New struct {
	LocationID, VariantID uuid.UUID
	Qty                   int32
	CustomerName, Phone   string
	Consent               bool
	Comment               string
}

// Create оформляет бронь в магазине. Срок — до конца следующего дня по местному времени точки.
func (s *Service) Create(ctx context.Context, n New) (uuid.UUID, error) {
	if !n.Consent {
		return uuid.Nil, ErrConsent
	}
	name, phone := strings.TrimSpace(n.CustomerName), strings.TrimSpace(n.Phone)
	if name == "" || !phonePattern.MatchString(phone) || n.Qty <= 0 {
		return uuid.Nil, ErrBadCustomer
	}
	if err := auth.CheckLocation(ctx, apispec.PermissionReservationsManage, n.LocationID); err != nil {
		return uuid.Nil, err
	}
	place, err := s.q.ReservationPlace(ctx, n.LocationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if place.Kind != "store" {
		return uuid.Nil, ErrNotStore
	}
	v, err := s.q.VariantForReservation(ctx, n.VariantID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (!v.IsActive || !v.ProductActive || !place.IsActive) {
		return uuid.Nil, ErrInactive
	}
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		no, err := q.NextReservationNumber(ctx, int32(place.LocalDate.Year()))
		if err != nil {
			return err
		}
		number := fmt.Sprintf("БР-%d-%06d", place.LocalDate.Year(), no)
		if id, err = q.CreateReservation(ctx, store.CreateReservationParams{
			Number: number, LocationID: n.LocationID, VariantID: n.VariantID, Qty: n.Qty, CustomerName: name,
			CustomerPhone: phone, Consent: true, ExpiresAt: place.ExpiresAt, CreatedBy: userID(ctx),
			Comment: strings.TrimSpace(n.Comment),
		}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionReservationCreated, EntityType: audit.EntityReservation, EntityID: id.String(),
			Data: map[string]any{"number": number, "qty": n.Qty},
		})
	})
	return id, err
}

func (s *Service) List(ctx context.Context, location *uuid.UUID, openOnly bool) ([]store.ListReservationsRow, error) {
	allowed, err := auth.AllowedLocations(ctx, apispec.PermissionReservationsManage)
	if err != nil {
		return nil, err
	}
	return s.q.ListReservations(ctx, store.ListReservationsParams{LocationIDs: allowed, LocationID: location, OpenOnly: openOnly})
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (store.ListReservationsRow, error) {
	allowed, err := auth.AllowedLocations(ctx, apispec.PermissionReservationsManage)
	if err != nil {
		return store.ListReservationsRow{}, err
	}
	rows, err := s.q.ListReservations(ctx, store.ListReservationsParams{ID: &id, LocationIDs: allowed})
	if err != nil {
		return store.ListReservationsRow{}, err
	}
	if len(rows) == 0 {
		return store.ListReservationsRow{}, ErrNotFound
	}
	return rows[0], nil
}

func (s *Service) lock(ctx context.Context, q *store.Queries, id uuid.UUID) (store.LockReservationRow, error) {
	r, err := q.LockReservation(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if err := auth.CheckLocation(ctx, apispec.PermissionReservationsManage, r.LocationID); err != nil {
		return r, ErrNotFound
	}
	return r, nil
}

// Confirm подтверждает бронь сканом: у товара с марками — марка каждой отложенной бутылки,
// у остального — штрихкод товара. Товар уходит в резерв и в продажу не попадёт.
func (s *Service) Confirm(ctx context.Context, id uuid.UUID, codes []string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		r, err := s.lock(ctx, q, id)
		if err != nil {
			return err
		}
		if r.Status != StatusNew {
			return ErrNotOpen
		}
		me := userID(ctx)
		var zone uuid.UUID
		if r.EgaisAccounting != nil && *r.EgaisAccounting == "marked" {
			var marksList []string
			for _, raw := range codes {
				scan, ok := catalog.ParseScan(raw)
				if !ok || scan.Mark == "" {
					return ErrWrongItem
				}
				m, err := s.marks.Find(ctx, scan.Mark)
				if err != nil {
					return err
				}
				if m == nil || m.VariantID != r.VariantID {
					return ErrWrongItem
				}
				marksList = append(marksList, scan.Mark)
			}
			if int32(len(marksList)) != r.Qty {
				return ErrWrongCount
			}
			zones, err := marks.Reserve(ctx, tx, me, r.LocationID, marksList)
			if me, ok := errors.AsType[*marks.MisplacedError](err); ok {
				return fmt.Errorf("%w: %s", ErrNotEnough, me.Code)
			}
			if err != nil {
				return err
			}
			for _, z := range zones {
				zone = z
				n, err := q.ChangeReserved(ctx, store.ChangeReservedParams{VariantID: r.VariantID, ZoneID: z, Delta: 1})
				if err != nil {
					return err
				}
				if n == 0 {
					return ErrNotEnough
				}
			}
			if err := q.AddReservationMarks(ctx, store.AddReservationMarksParams{ReservationID: id, Codes: marksList}); err != nil {
				return err
			}
		} else {
			if len(codes) == 0 {
				return ErrWrongItem
			}
			scan, ok := catalog.ParseScan(codes[0])
			if !ok {
				scan = catalog.Scan{Code: strings.TrimSpace(codes[0])}
			}
			v, err := s.catalog.FindVariant(ctx, scan)
			if err != nil {
				return err
			}
			if v == nil || *v != r.VariantID {
				return ErrWrongItem
			}
			if zone, err = q.FreeZone(ctx, store.FreeZoneParams{VariantID: r.VariantID, LocationID: r.LocationID, Qty: r.Qty}); errors.Is(err, pgx.ErrNoRows) {
				return ErrNotEnough
			} else if err != nil {
				return err
			}
			n, err := q.ChangeReserved(ctx, store.ChangeReservedParams{VariantID: r.VariantID, ZoneID: zone, Delta: r.Qty})
			if err != nil {
				return err
			}
			if n == 0 {
				return ErrNotEnough
			}
		}
		if err := q.SetReservationStatus(ctx, store.SetReservationStatusParams{ID: id, Status: StatusConfirmed, ZoneID: &zone, ConfirmedBy: &me}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionReservationConfirmed, EntityType: audit.EntityReservation, EntityID: id.String(),
			Data: map[string]any{"number": r.Number},
		})
	})
}

// release снимает резерв брони: марки — на остаток, количество — из резерва.
func release(ctx context.Context, tx pgx.Tx, q *store.Queries, r store.LockReservationRow, user uuid.UUID) ([]string, error) {
	if r.Status != StatusConfirmed {
		return nil, nil
	}
	codes, err := q.ReservationMarks(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if len(codes) > 0 {
		zones, err := marks.Release(ctx, tx, user, codes)
		if err != nil {
			return nil, err
		}
		for _, z := range zones {
			if _, err := q.ChangeReserved(ctx, store.ChangeReservedParams{VariantID: r.VariantID, ZoneID: z, Delta: -1}); err != nil {
				return nil, err
			}
		}
		return codes, nil
	}
	if r.ZoneID != nil {
		if _, err := q.ChangeReserved(ctx, store.ChangeReservedParams{VariantID: r.VariantID, ZoneID: *r.ZoneID, Delta: -r.Qty}); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (s *Service) Cancel(ctx context.Context, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		r, err := s.lock(ctx, q, id)
		if err != nil {
			return err
		}
		if r.Status != StatusNew && r.Status != StatusConfirmed {
			return ErrNotOpen
		}
		if _, err := release(ctx, tx, q, r, userID(ctx)); err != nil {
			return err
		}
		if err := q.SetReservationStatus(ctx, store.SetReservationStatusParams{ID: id, Status: StatusCancelled}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionReservationCancelled, EntityType: audit.EntityReservation, EntityID: id.String(),
			Data: map[string]any{"number": r.Number},
		})
	})
}

// Taken — что выдаётся по брони: кассе нужно пробить ровно это.
type Taken struct {
	Number    string
	VariantID uuid.UUID
	Qty       int32
	Marks     []string
}

// TakeForSale снимает резерв подтверждённой брони в транзакции чека: дальше продажа идёт с
// обычными проверками. Отметить бронь выданной — Issued в той же транзакции.
func (s *Service) TakeForSale(ctx context.Context, tx pgx.Tx, id, location, user uuid.UUID) (Taken, error) {
	q := s.q.WithTx(tx)
	r, err := q.LockReservation(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Taken{}, ErrNotFound
	}
	if err != nil {
		return Taken{}, err
	}
	if r.LocationID != location {
		return Taken{}, ErrWrongLocation
	}
	if r.Status != StatusConfirmed {
		return Taken{}, ErrNotConfirmed
	}
	codes, err := release(ctx, tx, q, r, user)
	if err != nil {
		return Taken{}, err
	}
	return Taken{Number: r.Number, VariantID: r.VariantID, Qty: r.Qty, Marks: codes}, nil
}

func (s *Service) Issued(ctx context.Context, tx pgx.Tx, id, receipt uuid.UUID) error {
	return s.q.WithTx(tx).SetReservationStatus(ctx, store.SetReservationStatusParams{ID: id, Status: StatusIssued, ReceiptID: &receipt})
}

// ConfirmedAt — подтверждённые брони точки для выдачи на кассе.
func (s *Service) ConfirmedAt(ctx context.Context, location uuid.UUID) ([]store.ListReservationsRow, error) {
	rows, err := s.q.ListReservations(ctx, store.ListReservationsParams{LocationID: &location, OpenOnly: true})
	if err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, r := range rows {
		if r.Status == StatusConfirmed {
			out = append(out, r)
		}
	}
	return out, nil
}

// Sweep снимает просроченные брони и обезличивает старые (НЛ-5). Запускается раз в минуту.
func (s *Service) Sweep(ctx context.Context) error {
	ids, err := s.q.ExpiredReservations(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			q := s.q.WithTx(tx)
			r, err := q.LockReservation(ctx, id)
			if err != nil {
				return err
			}
			if r.Status != StatusNew && r.Status != StatusConfirmed {
				return nil
			}
			if _, err := release(ctx, tx, q, r, uuid.Nil); err != nil {
				return err
			}
			return q.SetReservationStatus(ctx, store.SetReservationStatusParams{ID: id, Status: StatusExpired})
		}); err != nil {
			return err
		}
	}
	_, err = s.q.AnonymizeReservations(ctx)
	return err
}

// Peek — бронь без проверки прав сотрудника: касса сверяет точку сама.
func (s *Service) Peek(ctx context.Context, id uuid.UUID) (store.ListReservationsRow, error) {
	rows, err := s.q.ListReservations(ctx, store.ListReservationsParams{ID: &id})
	if err != nil {
		return store.ListReservationsRow{}, err
	}
	if len(rows) == 0 {
		return store.ListReservationsRow{}, ErrNotFound
	}
	return rows[0], nil
}
