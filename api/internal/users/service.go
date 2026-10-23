// Package users — сотрудники: учётные записи, роли и привязка к точкам.
package users

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth"
	"github.com/ADEXITUM/pikni/api/internal/auth/password"
	"github.com/ADEXITUM/pikni/api/internal/platform/db"
	"github.com/ADEXITUM/pikni/api/internal/users/store"
)

var (
	ErrNotFound          = errors.New("user not found")
	ErrLoginTaken        = errors.New("login is already taken")
	ErrInvalidLogin      = errors.New("invalid login")
	ErrEmptyName         = errors.New("full name is required")
	ErrInvalidRole       = errors.New("unknown role")
	ErrLocationsRequired = errors.New("role requires at least one location")
	ErrNetworkLocations  = errors.New("network roles have no locations")
	ErrUnknownLocation   = errors.New("unknown location")
	ErrConflict          = errors.New("user was changed by someone else")
	ErrSelfLockout       = errors.New("cannot block yourself or drop your own admin role")
)

var loginPattern = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

type User = store.GetUserRow

// Profile — то, что администратор задаёт сотруднику.
type Profile struct {
	FullName    string
	Role        apispec.Role
	LocationIDs []uuid.UUID
}

type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

func (s *Service) List(ctx context.Context) ([]User, error) {
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	users := make([]User, len(rows))
	for i, r := range rows {
		users[i] = User(r)
	}
	return users, nil
}

func (s *Service) Create(ctx context.Context, login string, p Profile) (User, string, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if !loginPattern.MatchString(login) {
		return User{}, "", ErrInvalidLogin
	}
	p, err := p.normalize()
	if err != nil {
		return User{}, "", err
	}

	temporary := password.Temporary()
	hash, err := password.Hash(temporary)
	if err != nil {
		return User{}, "", err
	}

	var user User
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		id, err := q.CreateUser(ctx, store.CreateUserParams{Login: login, FullName: p.FullName, Role: string(p.Role), PasswordHash: hash})
		if db.IsUniqueViolation(err) {
			return ErrLoginTaken
		}
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		if err := addLocations(ctx, q, id, p.LocationIDs); err != nil {
			return err
		}
		err = audit.Record(ctx, tx, audit.Event{
			Action:     apispec.AuditActionUserCreated,
			EntityType: audit.EntityUser,
			EntityID:   id.String(),
			Data:       map[string]any{"login": login, "role": p.Role, "locationIds": p.LocationIDs},
		})
		if err != nil {
			return err
		}
		user, err = q.GetUser(ctx, id)
		return err
	})
	if err != nil {
		return User{}, "", err
	}
	return user, temporary, nil
}

func (s *Service) CreateAdmin(ctx context.Context, login, fullName string) (string, error) {
	_, temporary, err := s.Create(ctx, login, Profile{FullName: fullName, Role: apispec.RoleAdmin})
	return temporary, err
}

// Update меняет ФИО, роль и точки, если карточку не изменили после того,
// как администратор открыл её с версией version.
func (s *Service) Update(ctx context.Context, actor, id uuid.UUID, p Profile, version int32) (User, error) {
	p, err := p.normalize()
	if err != nil {
		return User{}, err
	}

	var user User
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.LockUser(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock user: %w", err)
		}
		before := User(row)
		if before.Version != version {
			return ErrConflict
		}
		if id == actor && before.Role == string(apispec.RoleAdmin) && p.Role != apispec.RoleAdmin {
			return ErrSelfLockout
		}

		if err := q.UpdateUser(ctx, store.UpdateUserParams{ID: id, FullName: p.FullName, Role: string(p.Role)}); err != nil {
			return fmt.Errorf("update user: %w", err)
		}
		if err := q.DeleteUserLocations(ctx, id); err != nil {
			return fmt.Errorf("delete user locations: %w", err)
		}
		if err := addLocations(ctx, q, id, p.LocationIDs); err != nil {
			return err
		}
		if changes := diff(before, p); len(changes) > 0 {
			err := audit.Record(ctx, tx, audit.Event{
				Action:     apispec.AuditActionUserUpdated,
				EntityType: audit.EntityUser,
				EntityID:   id.String(),
				Data:       map[string]any{"login": before.Login, "changes": changes},
			})
			if err != nil {
				return err
			}
		}
		user, err = q.GetUser(ctx, id)
		return err
	})
	return user, err
}

// SetBlocked блокирует или разблокирует сотрудника. Блокировка сразу завершает его сеансы.
func (s *Service) SetBlocked(ctx context.Context, actor, id uuid.UUID, blocked bool) (User, error) {
	if blocked && id == actor {
		return User{}, ErrSelfLockout
	}
	action := apispec.AuditActionUserUnblocked
	if blocked {
		action = apispec.AuditActionUserBlocked
	}

	var user User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.LockUser(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock user: %w", err)
		}
		if err := q.SetUserBlocked(ctx, store.SetUserBlockedParams{ID: id, IsBlocked: blocked}); err != nil {
			return fmt.Errorf("set blocked: %w", err)
		}
		if blocked {
			if _, err := q.DeleteUserSessions(ctx, id); err != nil {
				return fmt.Errorf("delete sessions: %w", err)
			}
		}
		err = audit.Record(ctx, tx, audit.Event{
			Action:     action,
			EntityType: audit.EntityUser,
			EntityID:   id.String(),
			Data:       map[string]any{"login": row.Login},
		})
		if err != nil {
			return err
		}
		user, err = q.GetUser(ctx, id)
		return err
	})
	return user, err
}

// ResetPassword выдаёт временный пароль, снимает блокировку за ошибки входа
// и завершает все сеансы сотрудника.
func (s *Service) ResetPassword(ctx context.Context, id uuid.UUID) (string, error) {
	temporary := password.Temporary()
	hash, err := password.Hash(temporary)
	if err != nil {
		return "", err
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		login, err := q.ResetPassword(ctx, store.ResetPasswordParams{ID: id, PasswordHash: hash})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("reset password: %w", err)
		}
		if _, err := q.DeleteUserSessions(ctx, id); err != nil {
			return fmt.Errorf("delete sessions: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{
			Action:     apispec.AuditActionUserPasswordReset,
			EntityType: audit.EntityUser,
			EntityID:   id.String(),
			Data:       map[string]any{"login": login},
		})
	})
	if err != nil {
		return "", err
	}
	return temporary, nil
}

func (s *Service) ResetPasswordByLogin(ctx context.Context, login string) (string, error) {
	id, err := s.q.FindUserIDByLogin(ctx, strings.TrimSpace(login))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find user: %w", err)
	}
	return s.ResetPassword(ctx, id)
}

func (s *Service) EndSessions(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		user, err := q.GetUser(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}
		n, err := q.DeleteUserSessions(ctx, id)
		if err != nil {
			return fmt.Errorf("delete sessions: %w", err)
		}
		return audit.Record(ctx, tx, audit.Event{
			Action:     apispec.AuditActionAuthSessionEnded,
			EntityType: audit.EntityUser,
			EntityID:   id.String(),
			Data:       map[string]any{"login": user.Login, "count": n},
		})
	})
}

// normalize проверяет профиль и упорядочивает точки так же, как их возвращает база.
func (p Profile) normalize() (Profile, error) {
	p.FullName = strings.TrimSpace(p.FullName)
	network := auth.NetworkRole(p.Role)
	switch {
	case p.FullName == "":
		return p, ErrEmptyName
	case !p.Role.Valid():
		return p, ErrInvalidRole
	case network && len(p.LocationIDs) > 0:
		return p, ErrNetworkLocations
	case !network && len(p.LocationIDs) == 0:
		return p, ErrLocationsRequired
	}

	ids := slices.Clone(p.LocationIDs)
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	p.LocationIDs = slices.Compact(ids)
	return p, nil
}

func addLocations(ctx context.Context, q *store.Queries, id uuid.UUID, locations []uuid.UUID) error {
	if len(locations) == 0 {
		return nil
	}
	err := q.AddUserLocations(ctx, store.AddUserLocationsParams{UserID: id, LocationIDs: locations})
	if db.IsForeignKeyViolation(err) {
		return ErrUnknownLocation
	}
	if err != nil {
		return fmt.Errorf("add user locations: %w", err)
	}
	return nil
}

func diff(before User, after Profile) map[string]any {
	changes := map[string]any{}
	if before.FullName != after.FullName {
		changes["fullName"] = []string{before.FullName, after.FullName}
	}
	if before.Role != string(after.Role) {
		changes["role"] = []string{before.Role, string(after.Role)}
	}
	if !slices.Equal(before.LocationIDs, after.LocationIDs) {
		changes["locationIds"] = [][]uuid.UUID{before.LocationIDs, after.LocationIDs}
	}
	return changes
}
