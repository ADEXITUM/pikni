// Package pos — учебная касса (КС-1 – КС-7): зарегистрированные кассы, вход кассира по PIN,
// смены, проверки продажи и чеки. Касса работает только через кассовый API (openapi/pos.yaml).
package pos

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/auth/password"
	"github.com/ADEXITUM/pikni/api/internal/catalog"
	"github.com/ADEXITUM/pikni/api/internal/crpt"
	"github.com/ADEXITUM/pikni/api/internal/documents"
	"github.com/ADEXITUM/pikni/api/internal/egais"
	"github.com/ADEXITUM/pikni/api/internal/locations"
	"github.com/ADEXITUM/pikni/api/internal/marks"
	"github.com/ADEXITUM/pikni/api/internal/pos/store"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrDeviceKey     = errors.New("unknown or revoked device key")
	ErrBadPIN        = errors.New("wrong login or PIN")
	ErrLocked        = errors.New("cashier is locked for 15 minutes")
	ErrNotCashier    = errors.New("user cannot sell at this location")
	ErrSession       = errors.New("cashier session is missing or expired")
	ErrInvalidPIN    = errors.New("PIN must be 4 to 6 digits")
	ErrNoShift       = errors.New("no open shift")
	ErrShiftOpen     = errors.New("shift is already open")
	ErrInactivePoint = errors.New("location is archived")
	ErrNotStore      = errors.New("tills are registered only in stores")
)

// sessionTTL — смена кассира не длиннее суток; после — вход заново.
const sessionTTL = 24 * time.Hour

var pinPattern = regexp.MustCompile(`^[0-9]{4,6}$`)

type Service struct {
	pool      *pgxpool.Pool
	q         *store.Queries
	catalog   *catalog.Service
	marks     *marks.Service
	docs      *documents.Service
	locations *locations.Service
	egais     egais.Gateway
	crpt      crpt.Gateway
	now       func() time.Time
}

func NewService(pool *pgxpool.Pool, cat *catalog.Service, mk *marks.Service, docs *documents.Service,
	locs *locations.Service, eg egais.Gateway, cz crpt.Gateway) *Service {
	return &Service{pool: pool, q: store.New(pool), catalog: cat, marks: mk, docs: docs, locations: locs,
		egais: eg, crpt: cz, now: time.Now}
}

func hash(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

func random(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// Device — касса, её точка и торговый зал, где она продаёт.
type Device struct {
	ID, LocationID, FloorZone uuid.UUID
	Name                      string
	LocationCode, Location    string
	Timezone                  string
	FSRARID                   *string
}

// RegisterDevice регистрирует кассу магазина (ПБ-4) и возвращает её ключ — он показывается один раз,
// в базе хранится только хеш. Администратор регистрирует кассы любого магазина, управляющий — своего.
func (s *Service) RegisterDevice(ctx context.Context, location uuid.UUID, name string) (store.ListDevicesRow, string, error) {
	if err := auth.CheckLocation(ctx, apispec.PermissionPosDevices, location); err != nil {
		return store.ListDevicesRow{}, "", err
	}
	prefix := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random(5)))
	secret := base64.RawURLEncoding.EncodeToString(random(24))
	name = strings.TrimSpace(name)
	var createdBy *uuid.UUID
	if sess, ok := auth.FromContext(ctx); ok {
		createdBy = &sess.UserID
	}
	var row store.ListDevicesRow
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		loc, err := q.DeviceLocation(ctx, location)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if loc.Kind != "store" {
			return ErrNotStore
		}
		if !loc.IsActive {
			return ErrInactivePoint
		}
		id, err := q.CreateDevice(ctx, store.CreateDeviceParams{
			LocationID: location, Name: name, KeyPrefix: prefix, KeyHash: hash(secret), CreatedBy: createdBy,
		})
		if err != nil {
			return err
		}
		rows, err := q.ListDevices(ctx, store.ListDevicesParams{ID: &id})
		if err != nil {
			return err
		}
		row = rows[0]
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionPosDeviceCreated, EntityType: audit.EntityPosDevice, EntityID: id.String(),
			Data: map[string]any{"name": name, "location": row.LocationCode, "keyPrefix": prefix},
		})
	})
	return row, "pos_" + prefix + "_" + secret, err
}

func (s *Service) Devices(ctx context.Context) ([]store.ListDevicesRow, error) {
	allowed, err := auth.AllowedLocations(ctx, apispec.PermissionPosDevices)
	if err != nil {
		return nil, err
	}
	return s.q.ListDevices(ctx, store.ListDevicesParams{LocationIDs: allowed})
}

func (s *Service) RevokeDevice(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		location, err := q.DeviceLocationID(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := auth.CheckLocation(ctx, apispec.PermissionPosDevices, location); err != nil {
			return err
		}
		name, err := q.RevokeDevice(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionPosDeviceRevoked, EntityType: audit.EntityPosDevice, EntityID: id.String(),
			Data: map[string]any{"name": name},
		})
	})
}

// SetPIN задаёт PIN кассира для входа на кассе: 4–6 цифр, хранится хешем, как пароль.
func (s *Service) SetPIN(ctx context.Context, user uuid.UUID, pin string) error {
	if !pinPattern.MatchString(pin) {
		return ErrInvalidPIN
	}
	h, err := password.Hash(pin)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		login, err := s.q.WithTx(tx).SetPIN(ctx, store.SetPINParams{ID: user, PinHash: &h})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionUserPinSet, EntityType: audit.EntityUser, EntityID: user.String(),
			Data: map[string]any{"login": login},
		})
	})
}

// Device находит кассу по ключу из заголовка «Authorization: Device pos_<префикс>_<секрет>».
func (s *Service) Device(ctx context.Context, header string) (Device, error) {
	key := strings.TrimSpace(strings.TrimPrefix(header, "Device "))
	parts := strings.SplitN(key, "_", 3)
	if len(parts) != 3 || parts[0] != "pos" {
		return Device{}, ErrDeviceKey
	}
	d, err := s.q.DeviceByPrefix(ctx, parts[1])
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (d.RevokedAt != nil || subtle.ConstantTimeCompare(d.KeyHash, hash(parts[2])) != 1) {
		return Device{}, ErrDeviceKey
	}
	if err != nil {
		return Device{}, err
	}
	if !d.LocationActive {
		return Device{}, ErrInactivePoint
	}
	_ = s.q.TouchDevice(ctx, d.ID)
	return Device{
		ID: d.ID, LocationID: d.LocationID, FloorZone: d.FloorZoneID, Name: d.Name, LocationCode: d.LocationCode,
		Location: d.LocationName, Timezone: d.Timezone, FSRARID: d.FsrarID,
	}, nil
}

// Session — вход кассира на кассе.
type Session struct {
	Token     string
	Cashier   Cashier
	ExpiresAt time.Time
}

type Cashier struct {
	ID   uuid.UUID
	Name string
}

// Login — вход кассира по логину и PIN. Пять ошибок подряд блокируют вход на 15 минут.
// Продавать может только тот, у кого есть право продажи в точке кассы.
func (s *Service) Login(ctx context.Context, d Device, login, pin string) (Session, error) {
	u, err := s.q.CashierByLogin(ctx, login)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrBadPIN
	}
	if err != nil {
		return Session{}, err
	}
	if u.LockedUntil != nil && u.LockedUntil.After(s.now()) {
		return Session{}, ErrLocked
	}
	if u.IsBlocked || u.PinHash == nil {
		return Session{}, ErrBadPIN
	}
	if ok, err := password.Check(pin, *u.PinHash); err != nil || !ok {
		_ = s.q.PinFailed(ctx, u.ID)
		return Session{}, ErrBadPIN
	}
	if !auth.RoleCan(u.Role, apispec.PermissionPosSell, d.LocationID, u.LocationIDs) {
		return Session{}, ErrNotCashier
	}
	if err := s.q.PinSucceeded(ctx, u.ID); err != nil {
		return Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(random(32))
	sess := Session{Token: token, Cashier: Cashier{ID: u.ID, Name: u.FullName}, ExpiresAt: s.now().Add(sessionTTL)}
	err = s.q.CreatePosSession(ctx, store.CreatePosSessionParams{
		DeviceID: d.ID, UserID: u.ID, TokenHash: hash(token), ExpiresAt: sess.ExpiresAt,
	})
	return sess, err
}

// Context — кто и на какой кассе работает: передаётся в каждый метод кассового API.
type Context struct {
	Device    Device
	Cashier   Cashier
	SessionID uuid.UUID
}

func (s *Service) Authenticate(ctx context.Context, d Device, token string) (Context, error) {
	if token == "" {
		return Context{}, ErrSession
	}
	row, err := s.q.PosSessionByToken(ctx, hash(token))
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (row.DeviceID != d.ID || row.RevokedAt != nil ||
		row.ExpiresAt.Before(s.now()) || row.IsBlocked) {
		return Context{}, ErrSession
	}
	if err != nil {
		return Context{}, err
	}
	return Context{Device: d, Cashier: Cashier{ID: row.UserID, Name: row.FullName}, SessionID: row.ID}, nil
}

func (s *Service) Logout(ctx context.Context, pc Context) error {
	return s.q.RevokePosSession(ctx, pc.SessionID)
}
