// Package auth — вход сотрудников: сессии в cookie, защита от подбора пароля и от CSRF.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth/password"
	"github.com/ADEXITUM/pikni/api/internal/auth/store"
)

const (
	maxFailedLogins = 5
	lockoutDuration = 15 * time.Minute
	idleTimeout     = 12 * time.Hour
	sessionLifetime = 7 * 24 * time.Hour
	touchInterval   = 5 * time.Minute
)

var (
	ErrInvalidCredentials = errors.New("invalid login or password")
	ErrBlocked            = errors.New("user is blocked")
	ErrNoSession          = errors.New("no valid session")
	ErrWrongPassword      = errors.New("current password is wrong")
	ErrSamePassword       = errors.New("new password equals the current one")
)

type LockedError struct {
	RetryAfter time.Duration
}

func (e *LockedError) Error() string {
	return fmt.Sprintf("login is locked for %s", e.RetryAfter.Round(time.Second))
}

type Session struct {
	ID                 uuid.UUID
	UserID             uuid.UUID
	Login              string
	FullName           string
	Role               string
	MustChangePassword bool
	CSRFToken          string
	LocationIDs        []uuid.UUID
}

type Client struct {
	IP        string
	UserAgent string
}

type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool), now: time.Now}
}

var dummyHash = sync.OnceValue(func() string {
	h, _ := password.Hash("no-such-user")
	return h
})

// Login возвращает сессию и токен для cookie. В базе хранится только SHA-256 токена.
func (s *Service) Login(ctx context.Context, login, plain string, client Client) (Session, string, error) {
	ctx = audit.WithActor(ctx, audit.Actor{IP: client.IP})
	now := s.now()

	u, err := s.q.GetUserByLogin(ctx, strings.TrimSpace(login))
	if errors.Is(err, pgx.ErrNoRows) {
		// Хеш считается и здесь, иначе по времени ответа видно, существует ли логин.
		// Введённый текст в журнал не пишется: в поле логина иногда по ошибке вводят пароль.
		_, _ = password.Check(plain, dummyHash())
		return Session{}, "", failLogin(ctx, s.pool, nil, map[string]any{"reason": "unknown_login"}, ErrInvalidCredentials)
	}
	if err != nil {
		return Session{}, "", fmt.Errorf("get user: %w", err)
	}

	if u.LockedUntil != nil && now.Before(*u.LockedUntil) {
		locked := &LockedError{RetryAfter: u.LockedUntil.Sub(now)}
		return Session{}, "", failLogin(ctx, s.pool, &u, map[string]any{"reason": "locked"}, locked)
	}

	ok, err := password.Check(plain, u.PasswordHash)
	if err != nil {
		return Session{}, "", fmt.Errorf("check password: %w", err)
	}
	if !ok {
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			locked, err := s.q.WithTx(tx).RecordFailedLogin(ctx, store.RecordFailedLoginParams{
				ID: u.ID, MaxAttempts: maxFailedLogins, LockUntil: now.Add(lockoutDuration),
			})
			if err != nil {
				return fmt.Errorf("record failed login: %w", err)
			}
			return failLogin(ctx, tx, &u, map[string]any{"reason": "password", "locked": locked}, nil)
		})
		if err != nil {
			return Session{}, "", err
		}
		return Session{}, "", ErrInvalidCredentials
	}
	if u.IsBlocked {
		return Session{}, "", failLogin(ctx, s.pool, &u, map[string]any{"reason": "blocked"}, ErrBlocked)
	}

	token := rand.Text()
	sess := Session{
		UserID:             u.ID,
		Login:              u.Login,
		FullName:           u.FullName,
		Role:               u.Role,
		MustChangePassword: u.MustChangePassword,
		CSRFToken:          rand.Text(),
		LocationIDs:        u.LocationIDs,
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.ResetFailedLogins(ctx, u.ID); err != nil {
			return fmt.Errorf("reset failed logins: %w", err)
		}
		var err error
		sess.ID, err = q.CreateSession(ctx, store.CreateSessionParams{
			UserID:     u.ID,
			TokenHash:  hashToken(token),
			CSRFToken:  sess.CSRFToken,
			IP:         client.IP,
			UserAgent:  client.UserAgent,
			LastSeenAt: now,
			ExpiresAt:  now.Add(sessionLifetime),
		})
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		actor := audit.Actor{UserID: u.ID, SessionID: sess.ID, IP: client.IP}
		return audit.Record(audit.WithActor(ctx, actor), tx, audit.Event{Action: apispec.AuditActionAuthLogin})
	})
	if err != nil {
		return Session{}, "", err
	}
	return sess, token, nil
}

// failLogin записывает неудачную попытку входа и возвращает result, если запись удалась.
func failLogin(ctx context.Context, db audit.DB, u *store.GetUserByLoginRow, data map[string]any, result error) error {
	e := audit.Event{Action: apispec.AuditActionAuthLoginFailed, Data: data}
	if u != nil {
		e.EntityType, e.EntityID = audit.EntityUser, u.ID.String()
		data["login"] = u.Login
	}
	if err := audit.Record(ctx, db, e); err != nil {
		return err
	}
	return result
}

func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	row, err := s.q.GetSession(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}

	now := s.now()
	idle := now.Sub(row.LastSeenAt)
	if row.IsBlocked || now.After(row.ExpiresAt) || idle > idleTimeout {
		if err := s.q.DeleteSession(ctx, row.ID); err != nil {
			return Session{}, fmt.Errorf("delete expired session: %w", err)
		}
		return Session{}, ErrNoSession
	}
	if idle > touchInterval {
		if err := s.q.TouchSession(ctx, store.TouchSessionParams{ID: row.ID, LastSeenAt: now}); err != nil {
			return Session{}, fmt.Errorf("touch session: %w", err)
		}
	}

	return Session{
		ID:                 row.ID,
		UserID:             row.UserID,
		Login:              row.Login,
		FullName:           row.FullName,
		Role:               row.Role,
		MustChangePassword: row.MustChangePassword,
		CSRFToken:          row.CSRFToken,
		LocationIDs:        row.LocationIDs,
	}, nil
}

// WorkLocations — точки, между которыми пользователь выбирает в интерфейсе.
func (s *Service) WorkLocations(ctx context.Context, sess Session) ([]uuid.UUID, error) {
	if networkRoles[apispec.Role(sess.Role)] {
		return s.q.ListLocationIDs(ctx)
	}
	return sess.LocationIDs, nil
}

func (s *Service) Logout(ctx context.Context, sess Session) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.q.WithTx(tx).DeleteSession(ctx, sess.ID); err != nil {
			return fmt.Errorf("delete session: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{Action: apispec.AuditActionAuthLogout})
	})
}

// ChangePassword оставляет текущую сессию и завершает остальные сессии пользователя.
func (s *Service) ChangePassword(ctx context.Context, sess Session, current, next string) error {
	hash, err := s.q.GetPasswordHash(ctx, sess.UserID)
	if err != nil {
		return fmt.Errorf("get password hash: %w", err)
	}
	ok, err := password.Check(current, hash)
	if err != nil {
		return fmt.Errorf("check password: %w", err)
	}
	if !ok {
		return ErrWrongPassword
	}
	if next == current {
		return ErrSamePassword
	}
	if err := password.Validate(sess.Login, next); err != nil {
		return err
	}

	newHash, err := password.Hash(next)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.SetPassword(ctx, store.SetPasswordParams{ID: sess.UserID, PasswordHash: newHash}); err != nil {
			return fmt.Errorf("set password: %w", err)
		}
		if _, err := q.DeleteOtherSessions(ctx, store.DeleteOtherSessionsParams{UserID: sess.UserID, KeepID: sess.ID}); err != nil {
			return fmt.Errorf("delete other sessions: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{Action: apispec.AuditActionAuthPasswordChanged})
	})
}

func (s *Service) ListSessions(ctx context.Context, sess Session) ([]store.ListUserSessionsRow, error) {
	now := s.now()
	return s.q.ListUserSessions(ctx, store.ListUserSessionsParams{
		UserID: sess.UserID, Now: now, IdleSince: now.Add(-idleTimeout),
	})
}

// EndSession завершает один из сеансов пользователя. Чужой сеанс для него не существует.
func (s *Service) EndSession(ctx context.Context, sess Session, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		n, err := s.q.WithTx(tx).DeleteUserSession(ctx, store.DeleteUserSessionParams{ID: id, UserID: sess.UserID})
		if err != nil {
			return fmt.Errorf("delete session: %w", err)
		}
		if n == 0 {
			return ErrNoSession
		}
		return audit.Record(ctx, tx, audit.Event{
			Action:     apispec.AuditActionAuthSessionEnded,
			EntityType: audit.EntitySession,
			EntityID:   id.String(),
		})
	})
}

func (s *Service) EndOtherSessions(ctx context.Context, sess Session) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		n, err := s.q.WithTx(tx).DeleteOtherSessions(ctx, store.DeleteOtherSessionsParams{UserID: sess.UserID, KeepID: sess.ID})
		if err != nil {
			return fmt.Errorf("delete other sessions: %w", err)
		}
		if n == 0 {
			return nil
		}
		return audit.Record(ctx, tx, audit.Event{
			Action: apispec.AuditActionAuthSessionEnded,
			Data:   map[string]any{"count": n},
		})
	})
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
