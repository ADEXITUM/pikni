// Package assets — учёт имущества сети (ИМ-1 – ИМ-6): карточка объекта с историей, инвентарные
// наклейки, перемещение и смена МОЛ, инвентаризация сканированием, списание «двумя ключами».
package assets

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/assets/store"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/forms"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
)

var (
	ErrNotFound          = errors.New("asset not found")
	ErrConflict          = errors.New("asset changed by someone else")
	ErrInvalid           = errors.New("invalid asset fields")
	ErrWrittenOff        = errors.New("asset is written off")
	ErrPendingWriteoff   = errors.New("write-off already pending")
	ErrNoPendingWriteoff = errors.New("no pending write-off")
	ErrSameApprover      = errors.New("approver must differ from requester")
	ErrBadResponsible    = errors.New("responsible user is blocked or unknown")
	ErrAuditOpen         = errors.New("asset audit already open at location")
	ErrAuditClosed       = errors.New("asset audit is finished")
	ErrNotAsset          = errors.New("not an asset label")
)

const (
	StatusInUse      = "in_use"
	StatusRepair     = "repair"
	StatusWrittenOff = "written_off"
)

type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

func me(ctx context.Context) uuid.UUID {
	s, _ := auth.FromContext(ctx)
	return s.UserID
}

func ptr[T any](v T) *T { return &v }

// Fields — редактируемые поля карточки.
type Fields struct {
	Name, Category, Serial string
	AcceptedAt             time.Time
	CostKopeck             int64
}

func (f *Fields) normalize() error {
	f.Name, f.Serial = strings.TrimSpace(f.Name), strings.TrimSpace(f.Serial)
	if f.Name == "" || len(f.Name) > 600 || f.CostKopeck < 0 || f.Category == "" || f.AcceptedAt.IsZero() ||
		f.AcceptedAt.After(time.Now().Add(24*time.Hour)) {
		return ErrInvalid
	}
	return nil
}

func (s *Service) Categories(ctx context.Context) ([]store.AssetCategory, error) {
	return s.q.ListAssetCategories(ctx)
}

// Filter — отбор ведомости; Limit 0 — без ограничения (выгрузка в Excel).
type Filter struct {
	LocationID, ResponsibleID *uuid.UUID
	Category, Status, Q       *string
	Offset, Limit             int32
}

func (s *Service) List(ctx context.Context, f Filter) ([]store.ListAssetsRow, error) {
	allowed, err := auth.AllowedLocations(ctx, apispec.PermissionAssetsView)
	if err != nil {
		return nil, err
	}
	if allowed != nil && len(allowed) == 0 {
		return nil, nil
	}
	return s.q.ListAssets(ctx, store.ListAssetsParams{
		LocationIDs: allowed, LocationID: f.LocationID, ResponsibleID: f.ResponsibleID,
		CategoryCode: f.Category, Status: f.Status, Q: f.Q, RowOffset: f.Offset, MaxRows: f.Limit,
	})
}

// Totals — число объектов и их стоимость по фильтру без учёта страницы.
func (s *Service) Totals(ctx context.Context, f Filter) (store.CountAssetsRow, error) {
	allowed, err := auth.AllowedLocations(ctx, apispec.PermissionAssetsView)
	if err != nil || (allowed != nil && len(allowed) == 0) {
		return store.CountAssetsRow{}, err
	}
	return s.q.CountAssets(ctx, store.CountAssetsParams{
		LocationIDs: allowed, LocationID: f.LocationID, ResponsibleID: f.ResponsibleID,
		CategoryCode: f.Category, Status: f.Status, Q: f.Q,
	})
}

// Card — карточка: объект, история и списание, если оно оформлялось.
type Card struct {
	Asset   store.GetAssetRow
	Events  []store.AssetEventsRow
	Pending *store.PendingWriteoffRow
	Last    *store.LastWriteoffRow
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Card, error) {
	var c Card
	a, err := s.q.GetAsset(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	approver := auth.CheckLocation(ctx, apispec.PermissionDocsApprove, a.LocationID) == nil
	if auth.CheckLocation(ctx, apispec.PermissionAssetsView, a.LocationID) != nil && !approver {
		return c, ErrNotFound
	}
	c.Asset = a
	if c.Events, err = s.q.AssetEvents(ctx, id); err != nil {
		return c, err
	}
	if p, err := s.q.PendingWriteoff(ctx, id); err == nil {
		c.Pending = &p
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return c, err
	}
	if l, err := s.q.LastWriteoff(ctx, id); err == nil {
		c.Last = &l
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return c, err
	}
	return c, nil
}

func (s *Service) checkResponsible(ctx context.Context, q *store.Queries, user *uuid.UUID) error {
	if user == nil {
		return nil
	}
	ok, err := q.UserActive(ctx, *user)
	if err != nil {
		return err
	}
	if !ok {
		return ErrBadResponsible
	}
	return nil
}

func record(ctx context.Context, tx pgx.Tx, action apispec.AuditAction, id uuid.UUID, data map[string]any) error {
	return audit.Record(ctx, tx, audit.Event{Action: action, EntityType: audit.EntityAsset, EntityID: id.String(), Data: data})
}

func (s *Service) Create(ctx context.Context, location uuid.UUID, responsible *uuid.UUID, f Fields) (uuid.UUID, error) {
	if err := auth.CheckLocation(ctx, apispec.PermissionAssetsEdit, location); err != nil {
		return uuid.Nil, err
	}
	if err := f.normalize(); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := s.checkResponsible(ctx, q, responsible); err != nil {
			return err
		}
		row, err := q.CreateAsset(ctx, store.CreateAssetParams{
			Name: f.Name, CategoryCode: f.Category, Serial: f.Serial, AcceptedAt: f.AcceptedAt,
			CostKopecks: f.CostKopeck, LocationID: location, ResponsibleID: responsible,
		})
		if db.IsForeignKeyViolation(err) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		id = row.ID
		if err := q.InsertAssetEvent(ctx, store.InsertAssetEventParams{
			AssetID: id, Kind: "created", ToLocationID: &location, ToUserID: responsible, UserID: ptr(me(ctx)),
		}); err != nil {
			return err
		}
		return record(ctx, tx, apispec.AuditActionAssetCreated, id, map[string]any{"number": row.Number, "name": f.Name})
	})
	return id, err
}

// lock берёт объект на изменение: право правки в его точке, совпадение версии, не списан.
func (s *Service) lock(ctx context.Context, q *store.Queries, id uuid.UUID, version int32) (store.LockAssetRow, error) {
	a, err := q.LockAsset(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if err := auth.CheckLocation(ctx, apispec.PermissionAssetsEdit, a.LocationID); err != nil {
		return a, err
	}
	if a.Version != version {
		return a, ErrConflict
	}
	if a.Status == StatusWrittenOff {
		return a, ErrWrittenOff
	}
	return a, nil
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, version int32, f Fields) error {
	if err := f.normalize(); err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		a, err := s.lock(ctx, q, id, version)
		if err != nil {
			return err
		}
		err = q.UpdateAsset(ctx, store.UpdateAssetParams{
			ID: id, Name: f.Name, CategoryCode: f.Category, Serial: f.Serial, AcceptedAt: f.AcceptedAt, CostKopecks: f.CostKopeck,
		})
		if db.IsForeignKeyViolation(err) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		return record(ctx, tx, apispec.AuditActionAssetUpdated, id, map[string]any{"number": a.Number, "name": f.Name})
	})
}

// Move переводит объект в другую точку и, если указано, под другого МОЛ — одной записью истории
// на каждое изменение (ИМ-3).
func (s *Service) Move(ctx context.Context, id uuid.UUID, version int32, location uuid.UUID, responsible *uuid.UUID, comment string) error {
	if err := auth.CheckLocation(ctx, apispec.PermissionAssetsEdit, location); err != nil {
		return err
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		a, err := s.lock(ctx, q, id, version)
		if err != nil {
			return err
		}
		if err := s.checkResponsible(ctx, q, responsible); err != nil {
			return err
		}
		comment = strings.TrimSpace(comment)
		if a.LocationID != location {
			if err := q.SetAssetLocation(ctx, store.SetAssetLocationParams{ID: id, LocationID: location}); err != nil {
				return err
			}
			if err := q.InsertAssetEvent(ctx, store.InsertAssetEventParams{
				AssetID: id, Kind: "moved", FromLocationID: &a.LocationID, ToLocationID: &location, Comment: comment, UserID: ptr(me(ctx)),
			}); err != nil {
				return err
			}
		}
		if !sameUser(a.ResponsibleID, responsible) {
			if err := q.SetAssetResponsible(ctx, store.SetAssetResponsibleParams{ID: id, ResponsibleID: responsible}); err != nil {
				return err
			}
			if err := q.InsertAssetEvent(ctx, store.InsertAssetEventParams{
				AssetID: id, Kind: "responsible", FromUserID: a.ResponsibleID, ToUserID: responsible, Comment: comment, UserID: ptr(me(ctx)),
			}); err != nil {
				return err
			}
		}
		return record(ctx, tx, apispec.AuditActionAssetMoved, id, map[string]any{"number": a.Number, "name": a.Name})
	})
}

func sameUser(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// SetStatus — в эксплуатации или в ремонте; списание — только через утверждение.
func (s *Service) SetStatus(ctx context.Context, id uuid.UUID, version int32, status, comment string) error {
	if status != StatusInUse && status != StatusRepair {
		return ErrInvalid
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		a, err := s.lock(ctx, q, id, version)
		if err != nil {
			return err
		}
		if a.Status == status {
			return nil
		}
		if err := q.SetAssetStatus(ctx, store.SetAssetStatusParams{ID: id, Status: status}); err != nil {
			return err
		}
		if err := q.InsertAssetEvent(ctx, store.InsertAssetEventParams{
			AssetID: id, Kind: "status", FromStatus: &a.Status, ToStatus: &status, Comment: strings.TrimSpace(comment), UserID: ptr(me(ctx)),
		}); err != nil {
			return err
		}
		return record(ctx, tx, apispec.AuditActionAssetStatusChanged, id, map[string]any{"number": a.Number, "status": status})
	})
}

func (s *Service) SetPhoto(ctx context.Context, id uuid.UUID, data []byte, contentType string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		a, err := q.LockAsset(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := auth.CheckLocation(ctx, apispec.PermissionAssetsEdit, a.LocationID); err != nil {
			return err
		}
		p := store.SetAssetPhotoParams{ID: id}
		if data != nil {
			p.Photo, p.PhotoType = data, &contentType
		}
		return q.SetAssetPhoto(ctx, p)
	})
}

func (s *Service) Photo(ctx context.Context, id uuid.UUID) ([]byte, string, error) {
	p, err := s.q.AssetPhoto(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if auth.CheckLocation(ctx, apispec.PermissionAssetsView, p.LocationID) != nil &&
		auth.CheckLocation(ctx, apispec.PermissionDocsApprove, p.LocationID) != nil {
		return nil, "", ErrNotFound
	}
	return p.Photo, *p.PhotoType, nil
}

func (s *Service) Responsibles(ctx context.Context, location uuid.UUID) ([]store.ResponsiblesRow, error) {
	if err := auth.CheckLocation(ctx, apispec.PermissionAssetsView, location); err != nil {
		return nil, err
	}
	return s.q.Responsibles(ctx, location)
}

// RequestWriteoff — заявка на списание; объект остаётся в учёте до утверждения (ИМ-5).
func (s *Service) RequestWriteoff(ctx context.Context, id uuid.UUID, version int32, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrInvalid
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		a, err := s.lock(ctx, q, id, version)
		if err != nil {
			return err
		}
		if _, err := q.InsertWriteoff(ctx, store.InsertWriteoffParams{AssetID: id, Reason: reason, RequestedBy: me(ctx)}); err != nil {
			if db.IsUniqueViolation(err) {
				return ErrPendingWriteoff
			}
			return err
		}
		if err := q.InsertAssetEvent(ctx, store.InsertAssetEventParams{
			AssetID: id, Kind: "writeoff_requested", Comment: reason, UserID: ptr(me(ctx)),
		}); err != nil {
			return err
		}
		return record(ctx, tx, apispec.AuditActionAssetWriteoffRequested, id, map[string]any{"number": a.Number, "reason": reason})
	})
}

// DecideWriteoff — утверждение руководителем, который не оформлял заявку (ПБ-8).
func (s *Service) DecideWriteoff(ctx context.Context, id uuid.UUID, approve bool, comment string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		a, err := q.LockAsset(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := auth.CheckLocation(ctx, apispec.PermissionDocsApprove, a.LocationID); err != nil {
			return err
		}
		w, err := q.PendingWriteoff(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoPendingWriteoff
		}
		if err != nil {
			return err
		}
		if w.RequestedBy == me(ctx) {
			return ErrSameApprover
		}
		comment = strings.TrimSpace(comment)
		status, kind, action := "rejected", "writeoff_rejected", apispec.AuditActionAssetWriteoffRejected
		if approve {
			status, kind, action = "approved", "written_off", apispec.AuditActionAssetWrittenOff
			if err := q.SetAssetStatus(ctx, store.SetAssetStatusParams{ID: id, Status: StatusWrittenOff}); err != nil {
				return err
			}
		}
		if err := q.DecideWriteoff(ctx, store.DecideWriteoffParams{ID: w.ID, Status: status, DecidedBy: ptr(me(ctx)), Comment: comment}); err != nil {
			return err
		}
		ev := store.InsertAssetEventParams{AssetID: id, Kind: kind, Comment: comment, UserID: ptr(me(ctx))}
		if approve {
			ev.FromStatus, ev.ToStatus = &a.Status, ptr(StatusWrittenOff)
		}
		if err := q.InsertAssetEvent(ctx, ev); err != nil {
			return err
		}
		return record(ctx, tx, action, id, map[string]any{"number": a.Number, "comment": comment})
	})
}

// WriteoffAct — акт о списании по утверждённой заявке.
func (s *Service) WriteoffAct(ctx context.Context, id uuid.UUID) ([]byte, error) {
	c, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Last == nil || c.Last.Status != "approved" || c.Last.DecidedAt == nil {
		return nil, ErrNoPendingWriteoff
	}
	org, err := s.q.OrganizationName(ctx)
	if err != nil {
		return nil, err
	}
	a := c.Asset
	return forms.AssetWriteoffPDF(forms.AssetWriteoff{
		Organization: org, Location: a.LocationName, Number: a.Number, Name: a.Name, Category: a.CategoryName,
		Serial: a.Serial, Responsible: deref(a.ResponsibleName), AcceptedAt: a.AcceptedAt, CostKopeck: a.CostKopecks,
		Reason: c.Last.Reason, RequestedBy: c.Last.RequestedByName, ApprovedBy: deref(c.Last.DecidedByName),
		RequestedAt: c.Last.RequestedAt, ApprovedAt: *c.Last.DecidedAt,
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Labels — инвентарные наклейки выбранных объектов (ИМ-2).
func (s *Service) Labels(ctx context.Context, ids []uuid.UUID, format forms.Format) ([]byte, error) {
	labels := make([]forms.AssetLabel, 0, len(ids))
	for _, id := range ids {
		a, err := s.q.GetAsset(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if err := auth.CheckLocation(ctx, apispec.PermissionAssetsView, a.LocationID); err != nil {
			return nil, err
		}
		labels = append(labels, forms.AssetLabel{Number: a.Number, Name: a.Name, Location: a.LocationName})
	}
	return forms.AssetLabelsPDF(format, labels)
}

// Register — ведомость имущества в Excel (ИМ-6).
func (s *Service) Register(ctx context.Context, f Filter) ([]byte, error) {
	rows, err := s.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]forms.AssetRow, len(rows))
	for i, r := range rows {
		out[i] = forms.AssetRow{
			Number: r.Number, Name: r.Name, Category: r.CategoryName, Serial: r.Serial, AcceptedAt: r.AcceptedAt,
			CostKopeck: r.CostKopecks, Location: r.LocationCode + " · " + r.LocationName, Responsible: deref(r.ResponsibleName),
			Status: statusNames[r.Status],
		}
	}
	return forms.AssetRegisterXLSX(out)
}

var statusNames = map[string]string{StatusInUse: "в эксплуатации", StatusRepair: "в ремонте", StatusWrittenOff: "списан"}
