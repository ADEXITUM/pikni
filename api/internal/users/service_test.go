package users_test

import (
	"errors"
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/auth/password"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
	"github.com/ADEXITUM/pikni/api/internal/users"
)

func TestCreateAdmin(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := users.NewService(db.App)
	ctx := t.Context()

	temp, err := svc.CreateAdmin(ctx, " admin ", "Администратор")
	if err != nil {
		t.Fatalf("CreateAdmin() error = %v", err)
	}

	var role, hash string
	var mustChange bool
	err = db.App.QueryRow(ctx, `select role, password_hash, must_change_password from users where login = 'admin'`).
		Scan(&role, &hash, &mustChange)
	if err != nil {
		t.Fatalf("read user: %v", err)
	}
	if ok, _ := password.Check(temp, hash); !ok || role != users.RoleAdmin || !mustChange {
		t.Errorf("stored user: role=%q must_change=%v password_ok=%v", role, mustChange, ok)
	}

	if _, err := svc.CreateAdmin(ctx, "ADMIN", "Второй"); !errors.Is(err, users.ErrLoginTaken) {
		t.Errorf("second CreateAdmin() error = %v, want ErrLoginTaken", err)
	}
	if _, err := svc.CreateAdmin(ctx, "boss", " "); !errors.Is(err, users.ErrEmptyFields) {
		t.Errorf("CreateAdmin() with empty name error = %v, want ErrEmptyFields", err)
	}

	// Неудачная попытка откатила транзакцию вместе с записью журнала.
	if n := auditCount(t, db, "user.created"); n != 1 {
		t.Errorf("user.created entries = %d, want 1", n)
	}
}

func auditCount(t *testing.T, db *dbtest.DB, action string) int {
	t.Helper()
	var n int
	if err := db.App.QueryRow(t.Context(), `select count(*) from audit_log where action = $1`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestResetPassword(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := users.NewService(db.App)
	ctx := t.Context()

	old, err := svc.CreateAdmin(ctx, "admin", "Администратор")
	if err != nil {
		t.Fatalf("CreateAdmin() error = %v", err)
	}
	_, err = db.App.Exec(ctx, `
		update users set failed_logins = 5, locked_until = now() + interval '15 minutes', must_change_password = false;
		insert into sessions (user_id, token_hash, csrf_token, expires_at)
		select id, 'token', 'csrf', now() + interval '7 days' from users`)
	if err != nil {
		t.Fatalf("prepare locked user with a session: %v", err)
	}

	temp, err := svc.ResetPassword(ctx, "Admin")
	if err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}

	var hash string
	var failed, sessions int
	var locked, mustChange bool
	err = db.App.QueryRow(ctx, `
		select password_hash, failed_logins, locked_until is not null, must_change_password,
		       (select count(*) from sessions)
		from users`).Scan(&hash, &failed, &locked, &mustChange, &sessions)
	if err != nil {
		t.Fatalf("read user: %v", err)
	}
	newOK, _ := password.Check(temp, hash)
	oldOK, _ := password.Check(old, hash)
	if !newOK || oldOK || failed != 0 || locked || !mustChange || sessions != 0 {
		t.Errorf("after reset: new_ok=%v old_ok=%v failed=%d locked=%v must_change=%v sessions=%d",
			newOK, oldOK, failed, locked, mustChange, sessions)
	}

	if _, err := svc.ResetPassword(ctx, "nobody"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("ResetPassword(unknown) error = %v, want ErrNotFound", err)
	}
	if n := auditCount(t, db, "user.password_reset"); n != 1 {
		t.Errorf("user.password_reset entries = %d, want 1", n)
	}
}
