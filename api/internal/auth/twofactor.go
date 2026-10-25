package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/auth/store"
)

const (
	challengeLifetime = 5 * time.Minute
	qrLifetime        = 60 * time.Second
	recoveryCodes     = 10
	issuer            = "Пикни"
)

var (
	ErrBadCode         = errors.New("wrong or expired one-time code")
	ErrTOTPRequiredFor = errors.New("two-factor authentication is mandatory for this role")
	ErrNoPendingTOTP   = errors.New("two-factor setup was not started")
)

// ChallengeError — пароль верный, нужен одноразовый код: Token передаётся вторым шагом входа.
type ChallengeError struct {
	Token     string
	ExpiresAt time.Time
}

func (e *ChallengeError) Error() string { return "one-time code required" }

func (s *Service) challenge(ctx context.Context, user uuid.UUID) error {
	token := rand.Text()
	expires := s.now().Add(challengeLifetime)
	if err := s.q.CreateLoginChallenge(ctx, store.CreateLoginChallengeParams{
		TokenHash: hashToken(token), UserID: user, ExpiresAt: expires,
	}); err != nil {
		return err
	}
	return &ChallengeError{Token: token, ExpiresAt: expires}
}

// verify проверяет код из приложения или резервный код.
func (s *Service) verify(ctx context.Context, q *store.Queries, user uuid.UUID, secret, code string) (bool, error) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) == 6 {
		ok, _ := totp.ValidateCustom(code, secret, s.now(), totp.ValidateOpts{Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		return ok, nil
	}
	n, err := q.UseRecoveryCode(ctx, store.UseRecoveryCodeParams{UserID: user, CodeHash: hashToken(strings.ToUpper(code))})
	return n == 1, err
}

// LoginTOTP — второй шаг входа: вызов из первого шага и код. Неверных кодов не больше пяти.
func (s *Service) LoginTOTP(ctx context.Context, token, code string, client Client) (Session, string, error) {
	ctx = audit.WithActor(ctx, audit.Actor{IP: client.IP})
	ch, err := s.q.TakeLoginChallenge(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, "", ErrBadCode
	}
	if err != nil {
		return Session{}, "", err
	}
	u, err := s.q.GetUserByID(ctx, ch.UserID)
	if err != nil {
		return Session{}, "", err
	}
	t, err := s.q.GetTOTP(ctx, ch.UserID)
	if err != nil || t.TotpSecret == nil {
		return Session{}, "", ErrBadCode
	}
	ok, err := s.verify(ctx, s.q, ch.UserID, *t.TotpSecret, code)
	if err != nil {
		return Session{}, "", err
	}
	row := store.GetUserByLoginRow(u)
	if !ok {
		return Session{}, "", failLogin(ctx, s.pool, &row, map[string]any{"reason": "totp"}, ErrBadCode)
	}
	if u.IsBlocked {
		return Session{}, "", ErrBlocked
	}
	_ = s.q.DeleteLoginChallenge(ctx, hashToken(token))
	return s.startSession(ctx, row, client, apispec.AuditActionAuthLogin)
}

// SetupTOTP начинает подключение: новый секрет и ссылка otpauth для QR-кода приложения.
func (s *Service) SetupTOTP(ctx context.Context, sess Session) (secret, url string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: issuer, AccountName: sess.Login})
	if err != nil {
		return "", "", err
	}
	if err := s.q.SetTOTPPending(ctx, store.SetTOTPPendingParams{ID: sess.UserID, Secret: ptr(key.Secret())}); err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

func ptr[T any](v T) *T { return &v }

// EnableTOTP подтверждает подключение кодом из приложения и выдаёт резервные коды — их
// показывают один раз.
func (s *Service) EnableTOTP(ctx context.Context, sess Session, code string) ([]string, error) {
	t, err := s.q.GetTOTP(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	if t.TotpPending == nil {
		return nil, ErrNoPendingTOTP
	}
	if ok, _ := s.verify(ctx, s.q, sess.UserID, *t.TotpPending, code); !ok || len(strings.TrimSpace(code)) != 6 {
		return nil, ErrBadCode
	}
	codes := make([]string, recoveryCodes)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.EnableTOTP(ctx, sess.UserID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, sess.UserID); err != nil {
			return err
		}
		for i := range codes {
			c := strings.ToUpper(rand.Text()[:10])
			codes[i] = c[:5] + "-" + c[5:]
			if err := q.InsertRecoveryCode(ctx, store.InsertRecoveryCodeParams{UserID: sess.UserID, CodeHash: hashToken(codes[i])}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Event{Action: apispec.AuditActionAuthTotpEnabled, EntityType: audit.EntityUser, EntityID: sess.UserID.String()})
	})
	return codes, err
}

// DisableTOTP отключает второй фактор по действующему коду. Администратору — нельзя.
func (s *Service) DisableTOTP(ctx context.Context, sess Session, code string) error {
	if sess.Role == string(apispec.RoleAdmin) {
		return ErrTOTPRequiredFor
	}
	t, err := s.q.GetTOTP(ctx, sess.UserID)
	if err != nil {
		return err
	}
	if !t.TotpEnabled || t.TotpSecret == nil {
		return nil
	}
	if ok, err := s.verify(ctx, s.q, sess.UserID, *t.TotpSecret, code); err != nil || !ok {
		return ErrBadCode
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.DisableTOTP(ctx, sess.UserID); err != nil {
			return err
		}
		if err := q.DeleteRecoveryCodes(ctx, sess.UserID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{Action: apispec.AuditActionAuthTotpDisabled, EntityType: audit.EntityUser, EntityID: sess.UserID.String()})
	})
}

// IssueQR — одноразовый код входа на смартфоне для пользователя, который уже вошёл на ПК.
func (s *Service) IssueQR(ctx context.Context, sess Session) (string, time.Time, error) {
	code := rand.Text()
	expires := s.now().Add(qrLifetime)
	err := s.q.CreateQRLogin(ctx, store.CreateQRLoginParams{CodeHash: hashToken(code), UserID: sess.UserID, ExpiresAt: expires})
	return "pikni:login:" + code, expires, err
}

// RedeemQR — вход по QR-коду. Сеанс на ПК уже прошёл второй фактор, поэтому код его заменяет.
func (s *Service) RedeemQR(ctx context.Context, raw string, client Client) (Session, string, error) {
	ctx = audit.WithActor(ctx, audit.Actor{IP: client.IP})
	code, ok := strings.CutPrefix(strings.TrimSpace(raw), "pikni:login:")
	if !ok {
		return Session{}, "", ErrBadCode
	}
	user, err := s.q.TakeQRLogin(ctx, hashToken(code))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, "", ErrBadCode
	}
	if err != nil {
		return Session{}, "", fmt.Errorf("take qr login: %w", err)
	}
	u, err := s.q.GetUserByID(ctx, user)
	if err != nil {
		return Session{}, "", err
	}
	if u.IsBlocked {
		return Session{}, "", ErrBlocked
	}
	return s.startSession(ctx, store.GetUserByLoginRow(u), client, apispec.AuditActionAuthLoginQr)
}
