// Package pairing — смартфон как беспроводной сканер для ПК (СК-6). ПК открывает сопряжение и
// показывает QR-код, смартфон того же пользователя его сканирует; коды со смартфона приходят на ПК
// событием по WebSocket не позже чем через секунду.
package pairing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/auth"
)

var (
	ErrNotFound = errors.New("pairing not found")
	ErrExpired  = errors.New("pairing expired or closed")
	ErrBadCode  = errors.New("not a pairing code")
)

// ttl — сколько ждёт QR-код и живёт сопряжение: смена, а не навсегда.
const ttl = 12 * time.Hour

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func user(ctx context.Context) uuid.UUID {
	sess, _ := auth.FromContext(ctx)
	return sess.UserID
}

// Open — сопряжение на ПК: идентификатор и текст QR-кода «pikni:pair:<id>:<секрет>».
func (s *Service) Open(ctx context.Context) (uuid.UUID, string, time.Time, error) {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	secret := base64.RawURLEncoding.EncodeToString(b)
	expires := time.Now().Add(ttl)
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `insert into pairings (user_id, secret_hash, expires_at) values ($1, $2, $3) returning id`,
		user(ctx), hash(secret), expires).Scan(&id)
	return id, "pikni:pair:" + id.String() + ":" + secret, expires, err
}

// Join — смартфон сканирует QR-код. Сопрячься можно только под тем же пользователем.
func (s *Service) Join(ctx context.Context, code string) (uuid.UUID, time.Time, error) {
	parts := strings.Split(strings.TrimSpace(code), ":")
	if len(parts) != 4 || parts[0] != "pikni" || parts[1] != "pair" {
		return uuid.Nil, time.Time{}, ErrBadCode
	}
	id, err := uuid.Parse(parts[2])
	if err != nil {
		return uuid.Nil, time.Time{}, ErrBadCode
	}
	var stored []byte
	var status string
	var expires time.Time
	err = s.pool.QueryRow(ctx, `select secret_hash, status, expires_at from pairings where id = $1 and user_id = $2`,
		id, user(ctx)).Scan(&stored, &status, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, time.Time{}, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	if subtle.ConstantTimeCompare(stored, hash(parts[3])) != 1 {
		return uuid.Nil, time.Time{}, ErrNotFound
	}
	if status == "closed" || expires.Before(time.Now()) {
		return uuid.Nil, time.Time{}, ErrExpired
	}
	return id, expires, pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update pairings set status = 'active', joined_at = now() where id = $1`, id); err != nil {
			return err
		}
		return publish(ctx, tx, "paired", id, "")
	})
}

func (s *Service) check(ctx context.Context, id uuid.UUID, statuses ...string) error {
	var status string
	var expires time.Time
	err := s.pool.QueryRow(ctx, `select status, expires_at from pairings where id = $1 and user_id = $2`, id, user(ctx)).
		Scan(&status, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	for _, st := range statuses {
		if st == status && expires.After(time.Now()) {
			return nil
		}
	}
	return ErrExpired
}

// Scan пересылает код со смартфона на ПК. Идентификатор скана задаёт смартфон — ПК не засчитает
// повтор дважды.
func (s *Service) Scan(ctx context.Context, id uuid.UUID, scan uuid.UUID, raw string) error {
	if err := s.check(ctx, id, "active"); err != nil {
		return err
	}
	return publish(ctx, s.pool, "paired_scan", id, raw, scan)
}

// Close разрывает сопряжение с любой стороны.
func (s *Service) Close(ctx context.Context, id uuid.UUID) error {
	if err := s.check(ctx, id, "waiting", "active"); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update pairings set status = 'closed', closed_at = now() where id = $1`, id); err != nil {
			return err
		}
		return publish(ctx, tx, "unpaired", id, "")
	})
}

func (s *Service) Status(ctx context.Context, id uuid.UUID) (string, time.Time, error) {
	var status string
	var expires time.Time
	err := s.pool.QueryRow(ctx, `select status, expires_at from pairings where id = $1 and user_id = $2`, id, user(ctx)).
		Scan(&status, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", expires, ErrNotFound
	}
	if err == nil && status != "closed" && expires.Before(time.Now()) {
		status = "closed"
	}
	return status, expires, err
}

// publish объявляет событие сопряжения в канал событий. В отличие от изменений в базе, событие
// несёт сам код: ПК должен получить его сразу, перезапрашивать нечего.
func publish(ctx context.Context, db interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, kind string, id uuid.UUID, raw string, scan ...uuid.UUID) error {
	ev := map[string]any{"type": kind, "id": id.String(), "topics": []string{"pair:" + id.String()}}
	if raw != "" {
		ev["raw"] = raw
	}
	if len(scan) > 0 {
		ev["scan"] = scan[0].String()
	}
	payload, _ := json.Marshal(ev)
	var n int64
	return db.QueryRow(ctx, `insert into realtime_events (payload) values ($1::jsonb) returning id`, string(payload)).Scan(&n)
}
