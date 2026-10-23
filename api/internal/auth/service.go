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
	now := s.now()
	u, err := s.q.GetUserByLogin(ctx, strings.TrimSpace(login))
	if errors.Is(err, pgx.ErrNoRows) {
		// Хеш считается и здесь, иначе по времени ответа видно, существует ли логин.
		_, _ = password.Check(plain, dummyHash())
		return Session{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, "", fmt.Errorf("get user: %w", err)
	}

	if u.LockedUntil != nil && now.Before(*u.LockedUntil) {
		return Session{}, "", &LockedError{RetryAfter: u.LockedUntil.Sub(now)}
	}

	ok, err := password.Check(plain, u.PasswordHash)
	if err != nil {
		return Session{}, "", fmt.Errorf("check password: %w", err)
	}
	if !ok {
		err := s.q.RecordFailedLogin(ctx, store.RecordFailedLoginParams{
			ID: u.ID, MaxAttempts: maxFailedLogins, LockUntil: now.Add(lockoutDuration),
		})
		if err != nil {
			return Session{}, "", fmt.Errorf("record failed login: %w", err)
		}
		return Session{}, "", ErrInvalidCredentials
	}
	if u.IsBlocked {
		return Session{}, "", ErrBlocked
	}

	if err := s.q.ResetFailedLogins(ctx, u.ID); err != nil {
		return Session{}, "", fmt.Errorf("reset failed logins: %w", err)
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
	sess.ID, err = s.q.CreateSession(ctx, store.CreateSessionParams{
		UserID:     u.ID,
		TokenHash:  hashToken(token),
		CSRFToken:  sess.CSRFToken,
		IP:         client.IP,
		UserAgent:  client.UserAgent,
		LastSeenAt: now,
		ExpiresAt:  now.Add(sessionLifetime),
	})
	if err != nil {
		return Session{}, "", fmt.Errorf("create session: %w", err)
	}
	return sess, token, nil
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
	return s.q.DeleteSession(ctx, sess.ID)
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
		return q.DeleteOtherSessions(ctx, store.DeleteOtherSessionsParams{UserID: sess.UserID, KeepID: sess.ID})
	})
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
