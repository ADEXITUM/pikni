package auth

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/auth/password"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

// Часы обрезаны до микросекунд, как timestamptz, чтобы время из базы сравнивалось точно.
func newTestService(t *testing.T) (*Service, *dbtest.DB, *time.Time) {
	t.Helper()
	db := dbtest.New(t)
	svc := NewService(db.App)
	now := time.Now().Truncate(time.Microsecond)
	svc.now = func() time.Time { return now }
	return svc, db, &now
}

func createUser(t *testing.T, db *dbtest.DB, login, plain string) {
	t.Helper()
	hash, err := password.Hash(plain)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.App.Exec(t.Context(),
		`insert into users (login, full_name, role, password_hash) values ($1, 'Петрова А. В.', 'cashier', $2)`, login, hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
}

func TestLoginLocksAfterFailedAttempts(t *testing.T) {
	t.Parallel()
	svc, db, clock := newTestService(t)
	ctx := t.Context()
	createUser(t, db, "petrova", "верный-пароль")

	for i := range maxFailedLogins {
		if _, _, err := svc.Login(ctx, "petrova", "неверный", Client{}); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: error = %v, want ErrInvalidCredentials", i+1, err)
		}
	}

	_, _, err := svc.Login(ctx, "petrova", "верный-пароль", Client{})
	var locked *LockedError
	if !errors.As(err, &locked) || locked.RetryAfter != lockoutDuration {
		t.Fatalf("login during lockout: error = %v, want LockedError for %s", err, lockoutDuration)
	}

	*clock = clock.Add(lockoutDuration + time.Second)
	if _, _, err := svc.Login(ctx, "Petrova", "верный-пароль", Client{}); err != nil {
		t.Fatalf("login after lockout: %v", err)
	}

	var failed int
	if err := db.App.QueryRow(ctx, `select failed_logins from users`).Scan(&failed); err != nil || failed != 0 {
		t.Errorf("failed_logins = %d (%v), want 0 after successful login", failed, err)
	}
}

func TestLoginUnknownAndBlockedUser(t *testing.T) {
	t.Parallel()
	svc, db, _ := newTestService(t)
	ctx := t.Context()
	createUser(t, db, "ivanov", "верный-пароль")
	if _, err := db.App.Exec(ctx, `update users set is_blocked = true`); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		login, password string
		want            error
	}{
		{"nobody", "верный-пароль", ErrInvalidCredentials},
		{"ivanov", "неверный", ErrInvalidCredentials},
		{"ivanov", "верный-пароль", ErrBlocked},
	}
	for _, tt := range tests {
		if _, _, err := svc.Login(ctx, tt.login, tt.password, Client{}); !errors.Is(err, tt.want) {
			t.Errorf("Login(%q, %q) error = %v, want %v", tt.login, tt.password, err, tt.want)
		}
	}
}

func TestLoginStoresTokenHash(t *testing.T) {
	t.Parallel()
	svc, db, _ := newTestService(t)
	createUser(t, db, "petrova", "верный-пароль")

	sess, token, err := svc.Login(t.Context(), "petrova", "верный-пароль", Client{IP: "203.0.113.7", UserAgent: "Zebra TC21"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	var hash []byte
	var ip, ua string
	err = db.App.QueryRow(t.Context(), `select token_hash, host(ip), user_agent from sessions where id = $1`, sess.ID).
		Scan(&hash, &ip, &ua)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	if !bytes.Equal(hash, hashToken(token)) || bytes.Contains(hash, []byte(token)) || ip != "203.0.113.7" || ua != "Zebra TC21" {
		t.Errorf("stored session: hash ok=%v ip=%q ua=%q", bytes.Equal(hash, hashToken(token)), ip, ua)
	}
}

func TestSessionIdleTimeout(t *testing.T) {
	t.Parallel()
	svc, db, clock := newTestService(t)
	ctx := t.Context()
	createUser(t, db, "petrova", "верный-пароль")
	_, token, err := svc.Login(ctx, "petrova", "верный-пароль", Client{})
	if err != nil {
		t.Fatal(err)
	}
	loginAt := *clock

	lastSeen := func() time.Time {
		t.Helper()
		var at time.Time
		if err := db.App.QueryRow(ctx, `select last_seen_at from sessions`).Scan(&at); err != nil {
			t.Fatalf("read last_seen_at: %v", err)
		}
		return at
	}

	*clock = loginAt.Add(touchInterval - time.Minute)
	if _, err := svc.Authenticate(ctx, token); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !lastSeen().Equal(loginAt) {
		t.Error("last_seen_at updated before touchInterval passed")
	}

	*clock = loginAt.Add(touchInterval + time.Minute)
	if _, err := svc.Authenticate(ctx, token); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !lastSeen().Equal(*clock) {
		t.Error("last_seen_at not updated after touchInterval")
	}

	*clock = clock.Add(idleTimeout + time.Minute)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Authenticate() after idle timeout error = %v, want ErrNoSession", err)
	}
	var sessions int
	if err := db.App.QueryRow(ctx, `select count(*) from sessions`).Scan(&sessions); err != nil || sessions != 0 {
		t.Errorf("sessions = %d (%v), want expired session deleted", sessions, err)
	}
}

func TestSessionAbsoluteLifetime(t *testing.T) {
	t.Parallel()
	svc, db, clock := newTestService(t)
	ctx := t.Context()
	createUser(t, db, "petrova", "верный-пароль")
	_, token, err := svc.Login(ctx, "petrova", "верный-пароль", Client{})
	if err != nil {
		t.Fatal(err)
	}

	// Сотрудник заходит каждые 11 часов, поэтому срок бездействия не истекает,
	// но через 7 суток после входа сессия всё равно заканчивается.
	step := 11 * time.Hour
	for elapsed := step; elapsed <= sessionLifetime; elapsed += step {
		*clock = clock.Add(step)
		if _, err := svc.Authenticate(ctx, token); err != nil {
			t.Fatalf("Authenticate() after %s error = %v", elapsed, err)
		}
	}
	*clock = clock.Add(step)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Authenticate() after lifetime error = %v, want ErrNoSession", err)
	}
}

func TestChangePassword(t *testing.T) {
	t.Parallel()
	svc, db, _ := newTestService(t)
	ctx := t.Context()
	createUser(t, db, "petrova", "старый-пароль")
	current, currentToken, err := svc.Login(ctx, "petrova", "старый-пароль", Client{})
	if err != nil {
		t.Fatal(err)
	}
	_, otherToken, err := svc.Login(ctx, "petrova", "старый-пароль", Client{})
	if err != nil {
		t.Fatal(err)
	}

	rejected := []struct {
		current, next string
		want          error
	}{
		{"неверный", "новый-пароль", ErrWrongPassword},
		{"старый-пароль", "старый-пароль", ErrSamePassword},
		{"старый-пароль", "коротко", password.ErrTooShort},
	}
	for _, tt := range rejected {
		if err := svc.ChangePassword(ctx, current, tt.current, tt.next); !errors.Is(err, tt.want) {
			t.Errorf("ChangePassword(%q, %q) error = %v, want %v", tt.current, tt.next, err, tt.want)
		}
	}

	if err := svc.ChangePassword(ctx, current, "старый-пароль", "новый-пароль"); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}

	sess, err := svc.Authenticate(ctx, currentToken)
	if err != nil || sess.MustChangePassword {
		t.Errorf("current session: err=%v must_change=%v, want valid and false", err, sess.MustChangePassword)
	}
	if _, err := svc.Authenticate(ctx, otherToken); !errors.Is(err, ErrNoSession) {
		t.Errorf("other session error = %v, want ErrNoSession", err)
	}
	if _, _, err := svc.Login(ctx, "petrova", "старый-пароль", Client{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("login with old password error = %v, want ErrInvalidCredentials", err)
	}
	if _, _, err := svc.Login(ctx, "petrova", "новый-пароль", Client{}); err != nil {
		t.Errorf("login with new password: %v", err)
	}
}

func TestLoginWritesAudit(t *testing.T) {
	t.Parallel()
	svc, db, clock := newTestService(t)
	ctx := t.Context()
	createUser(t, db, "petrova", "верный-пароль")
	client := Client{IP: "203.0.113.7"}

	// Пароль, по ошибке введённый в поле логина, не должен попасть в журнал.
	_, _, _ = svc.Login(ctx, "мой-секретный-пароль", "x", client)
	for range maxFailedLogins {
		_, _, _ = svc.Login(ctx, "petrova", "неверный", client)
	}
	_, _, _ = svc.Login(ctx, "petrova", "верный-пароль", client)
	*clock = clock.Add(lockoutDuration + time.Second)
	if _, _, err := svc.Login(ctx, "petrova", "верный-пароль", client); err != nil {
		t.Fatal(err)
	}

	rows, err := db.App.Query(ctx, `
		select action || coalesce(':' || (data->>'reason'), '')
		       || case when (data->>'locked')::bool then ':locked' else '' end
		from audit_log order by id`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"auth.login_failed:unknown_login",
		"auth.login_failed:password",
		"auth.login_failed:password",
		"auth.login_failed:password",
		"auth.login_failed:password",
		"auth.login_failed:password:locked",
		"auth.login_failed:locked",
		"auth.login",
	}
	if !slices.Equal(got, want) {
		t.Errorf("audit log:\n got %q\nwant %q", got, want)
	}

	var leaked, login int
	err = db.App.QueryRow(ctx, `
		select count(*) filter (where data::text like '%мой-секретный-пароль%'),
		       count(*) filter (where action = 'auth.login' and user_id is not null
		                          and session_id is not null and host(ip) = '203.0.113.7')
		from audit_log`).Scan(&leaked, &login)
	if err != nil || leaked != 0 || login != 1 {
		t.Errorf("leaked=%d login_with_actor=%d err=%v", leaked, login, err)
	}
}
