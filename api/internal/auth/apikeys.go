package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
)

// Ключи API внешних систем (ИН-2). Ключ — pk_<префикс>_<секрет>, в базе только хеш. Обращение по
// ключу открывает операции своей области и ничего больше, каждое пишется в журнал ключа.

const apiKeyRole = "api_key"

// scopeOperations — какие операции открывает область ключа.
var scopeOperations = map[string][]string{
	"availability": {"searchavailability", "getavailability", "getsimilar", "listlocations", "getlocation"},
	"catalog": {"listproducts", "getproduct", "listproductvariants", "listcategories", "listbrands", "listproducers",
		"listproductlines", "listproductkinds", "listvolumes", "listcountries", "resolvecode", "listretailprices"},
	"import": {"getimporttemplate", "previewimport", "runimport"},
}

// Права ключа в матрице: точки — вся сеть, но список операций ограничен областью.
func init() {
	matrix[apiKeyRole] = map[apispec.Permission]scope{
		apispec.PermissionAvailabilityView: scopeAll,
		apispec.PermissionRefsView:         scopeAll,
		apispec.PermissionRefsEdit:         scopeAll,
	}
}

var ErrKeyNotFound = errors.New("api key not found")

// keySession проверяет ключ из заголовка Authorization и разрешена ли ему операция.
func (s *Service) keySession(ctx context.Context, c *echo.Context, header, operation string) (Session, error) {
	token, _ := strings.CutPrefix(header, "Bearer ")
	parts := strings.SplitN(strings.TrimSpace(token), "_", 3)
	if len(parts) != 3 || parts[0] != "pk" {
		return Session{}, echo.ErrUnauthorized
	}
	var id, author uuid.UUID
	var hash []byte
	var scopes []string
	var revoked *time.Time
	err := s.pool.QueryRow(ctx, `select id, key_hash, scopes, revoked_at, created_by from api_keys where prefix = $1`, parts[1]).
		Scan(&id, &hash, &scopes, &revoked, &author)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (revoked != nil || subtle.ConstantTimeCompare(hash, hashToken(parts[2])) != 1) {
		return Session{}, echo.ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	allowed := false
	for _, sc := range scopes {
		allowed = allowed || slices.Contains(scopeOperations[sc], operation)
	}
	_, _ = s.pool.Exec(ctx, `with l as (insert into api_key_requests (key_id, method, operation, ip, ok)
		values ($1, $2, $3, nullif($4, '')::inet, $5))
		update api_keys set last_used_at = now() where id = $1`, id, c.Request().Method, operation, c.RealIP(), allowed)
	if !allowed {
		return Session{}, echo.NewHTTPError(403, "Операция не входит в права ключа API")
	}
	// Импорт пишет в журнал действий от имени того, кто выпустил ключ.
	return Session{UserID: author, Login: "api:" + parts[1], Role: apiKeyRole, TOTPEnabled: true}, nil
}

// APIKey — выпущенный ключ без секрета.
type APIKey struct {
	ID                    uuid.UUID
	Name, Prefix, Author  string
	Scopes                []string
	CreatedAt             time.Time
	LastUsedAt, RevokedAt *time.Time
}

func (s *Service) APIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.pool.Query(ctx, `select k.id, k.name, k.prefix, u.full_name, k.scopes, k.created_at, k.last_used_at, k.revoked_at
		from api_keys k join users u on u.id = k.created_by order by k.created_at desc`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (APIKey, error) {
		var k APIKey
		return k, r.Scan(&k.ID, &k.Name, &k.Prefix, &k.Author, &k.Scopes, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	})
}

// CreateAPIKey выпускает ключ; сам ключ возвращается один раз.
func (s *Service) CreateAPIKey(ctx context.Context, name string, scopes []string) (string, APIKey, error) {
	sess, _ := FromContext(ctx)
	prefix := strings.ToLower(rand.Text()[:8])
	secret := rand.Text()
	var k APIKey
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `insert into api_keys (name, prefix, key_hash, scopes, created_by) values ($1, $2, $3, $4, $5)
			returning id, name, prefix, scopes, created_at`, strings.TrimSpace(name), prefix, hashToken(secret), scopes, sess.UserID).
			Scan(&k.ID, &k.Name, &k.Prefix, &k.Scopes, &k.CreatedAt)
		if err != nil {
			return err
		}
		k.Author = sess.FullName
		return audit.Record(ctx, tx, audit.Event{Action: apispec.AuditActionAPIKeyCreated, Data: map[string]any{"name": k.Name, "prefix": prefix, "scopes": scopes}})
	})
	return "pk_" + prefix + "_" + secret, k, err
}

func (s *Service) RevokeAPIKey(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var prefix string
		err := tx.QueryRow(ctx, `update api_keys set revoked_at = coalesce(revoked_at, now()) where id = $1 returning prefix`, id).Scan(&prefix)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrKeyNotFound
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{Action: apispec.AuditActionAPIKeyRevoked, Data: map[string]any{"prefix": prefix}})
	})
}

type APIKeyRequest struct {
	At                time.Time
	Method, Operation string
	IP                *string
	OK                bool
}

func (s *Service) APIKeyRequests(ctx context.Context, id uuid.UUID) ([]APIKeyRequest, error) {
	rows, err := s.pool.Query(ctx, `select at, method, operation, host(ip), ok from api_key_requests where key_id = $1
		order by id desc limit 200`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (APIKeyRequest, error) {
		var q APIKeyRequest
		return q, r.Scan(&q.At, &q.Method, &q.Operation, &q.IP, &q.OK)
	})
}
