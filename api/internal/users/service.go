// Package users — сотрудники: учётные записи, роли и привязка к точкам.
package users

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth/password"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/users/store"
)

const RoleAdmin = "admin"

var (
	ErrNotFound    = errors.New("user not found")
	ErrLoginTaken  = errors.New("login is already taken")
	ErrEmptyFields = errors.New("login and full name are required")
)

type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

func (s *Service) CreateAdmin(ctx context.Context, login, fullName string) (temporary string, err error) {
	login, fullName = strings.TrimSpace(login), strings.TrimSpace(fullName)
	if login == "" || fullName == "" {
		return "", ErrEmptyFields
	}

	temporary = password.Temporary()
	hash, err := password.Hash(temporary)
	if err != nil {
		return "", err
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		id, err := s.q.WithTx(tx).CreateUser(ctx, store.CreateUserParams{Login: login, FullName: fullName, Role: RoleAdmin, PasswordHash: hash})
		if db.IsUniqueViolation(err) {
			return ErrLoginTaken
		}
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{
			Action:     apispec.AuditActionUserCreated,
			EntityType: audit.EntityUser,
			EntityID:   id.String(),
			Data:       map[string]any{"login": login, "role": RoleAdmin},
		})
	})
	if err != nil {
		return "", err
	}
	return temporary, nil
}

// ResetPassword выдаёт временный пароль, снимает блокировку за ошибки входа
// и завершает все сессии пользователя.
func (s *Service) ResetPassword(ctx context.Context, login string) (temporary string, err error) {
	temporary = password.Temporary()
	hash, err := password.Hash(temporary)
	if err != nil {
		return "", err
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		user, err := q.ResetPassword(ctx, store.ResetPasswordParams{Login: strings.TrimSpace(login), PasswordHash: hash})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("reset password: %w", err)
		}
		if err := q.DeleteUserSessions(ctx, user.ID); err != nil {
			return fmt.Errorf("delete sessions: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{
			Action:     apispec.AuditActionUserPasswordReset,
			EntityType: audit.EntityUser,
			EntityID:   user.ID.String(),
			Data:       map[string]any{"login": user.Login},
		})
	})
	if err != nil {
		return "", err
	}
	return temporary, nil
}
