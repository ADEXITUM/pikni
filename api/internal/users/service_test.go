package users_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
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
	if ok, _ := password.Check(temp, hash); !ok || role != "admin" || !mustChange {
		t.Errorf("stored user: role=%q must_change=%v password_ok=%v", role, mustChange, ok)
	}

	if _, err := svc.CreateAdmin(ctx, "ADMIN", "Второй"); !errors.Is(err, users.ErrLoginTaken) {
		t.Errorf("second CreateAdmin() error = %v, want ErrLoginTaken", err)
	}
	if _, err := svc.CreateAdmin(ctx, "boss", " "); !errors.Is(err, users.ErrEmptyName) {
		t.Errorf("CreateAdmin() with empty name error = %v, want ErrEmptyName", err)
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

	temp, err := svc.ResetPasswordByLogin(ctx, "Admin")
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

	if _, err := svc.ResetPasswordByLogin(ctx, "nobody"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("ResetPassword(unknown) error = %v, want ErrNotFound", err)
	}
	if n := auditCount(t, db, "user.password_reset"); n != 1 {
		t.Errorf("user.password_reset entries = %d, want 1", n)
	}
}

func createLocations(t *testing.T, db *dbtest.DB) (uuid.UUID, uuid.UUID) {
	t.Helper()
	rows, err := db.App.Query(t.Context(), `
		insert into locations (code, name, kind, city, timezone) values
			('МГ-01', 'Магазин 1', 'store', 'Благовещенск', 'Asia/Yakutsk'),
			('МГ-02', 'Магазин 2', 'store', 'Хабаровск', 'Asia/Vladivostok')
		returning id`)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		t.Fatal(err)
	}
	return ids[0], ids[1]
}

func TestCreateValidatesProfile(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := users.NewService(db.App)
	store1, _ := createLocations(t, db)
	cashier := func(ids ...uuid.UUID) users.Profile {
		return users.Profile{FullName: "Кассир", Role: apispec.RoleCashier, LocationIDs: ids}
	}
	if _, _, err := svc.Create(t.Context(), "kassir", cashier(store1)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		login   string
		profile users.Profile
		want    error
	}{
		{"короткий логин", "ab", cashier(store1), users.ErrInvalidLogin},
		{"кириллица в логине", "петрова", cashier(store1), users.ErrInvalidLogin},
		{"пробел в логине", "a petrova", cashier(store1), users.ErrInvalidLogin},
		{"занятый логин в другом регистре", "KASSIR", cashier(store1), users.ErrLoginTaken},
		{"без ФИО", "kassir2", users.Profile{FullName: " ", Role: apispec.RoleCashier, LocationIDs: []uuid.UUID{store1}}, users.ErrEmptyName},
		{"неизвестная роль", "kassir2", users.Profile{FullName: "Кассир", Role: "boss"}, users.ErrInvalidRole},
		{"кассир без точки", "kassir2", cashier(), users.ErrLocationsRequired},
		{"руководитель с точкой", "boss", users.Profile{FullName: "Руководитель", Role: apispec.RoleManager, LocationIDs: []uuid.UUID{store1}}, users.ErrNetworkLocations},
		{"несуществующая точка", "kassir2", cashier(uuid.New()), users.ErrUnknownLocation},
	}
	for _, tt := range tests {
		if _, _, err := svc.Create(t.Context(), tt.login, tt.profile); !errors.Is(err, tt.want) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.want)
		}
	}
}

func TestCreateCashier(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := users.NewService(db.App)
	store1, store2 := createLocations(t, db)

	user, temp, err := svc.Create(t.Context(), " A.Petrova ", users.Profile{
		FullName: "Петрова А. В.", Role: apispec.RoleCashier, LocationIDs: []uuid.UUID{store2, store1, store2},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if user.Login != "a.petrova" || !user.MustChangePassword || len(user.LocationIDs) != 2 || user.Version != 1 {
		t.Errorf("user = %+v", user)
	}

	var hash string
	if err := db.App.QueryRow(t.Context(), `select password_hash from users where id = $1`, user.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if ok, _ := password.Check(temp, hash); !ok {
		t.Error("temporary password does not match the stored hash")
	}
	if n := auditCount(t, db, "user.created"); n != 1 {
		t.Errorf("user.created entries = %d, want 1", n)
	}
}

func TestUpdateUsesVersion(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := users.NewService(db.App)
	store1, store2 := createLocations(t, db)
	admin := uuid.New()

	user, _, err := svc.Create(t.Context(), "kassir", users.Profile{FullName: "Кассир", Role: apispec.RoleCashier, LocationIDs: []uuid.UUID{store1}})
	if err != nil {
		t.Fatal(err)
	}

	moved := users.Profile{FullName: "Кассир Иванова", Role: apispec.RoleStorekeeper, LocationIDs: []uuid.UUID{store2}}
	updated, err := svc.Update(t.Context(), admin, user.ID, moved, user.Version)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Version != 2 || updated.Role != "storekeeper" || updated.LocationIDs[0] != store2 {
		t.Errorf("updated = %+v", updated)
	}

	// Второй администратор открыл карточку раньше и сохраняет старую версию.
	if _, err := svc.Update(t.Context(), admin, user.ID, moved, user.Version); !errors.Is(err, users.ErrConflict) {
		t.Errorf("stale update error = %v, want ErrConflict", err)
	}
	if _, err := svc.Update(t.Context(), admin, uuid.New(), moved, 1); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("unknown user error = %v, want ErrNotFound", err)
	}

	var changes string
	err = db.App.QueryRow(t.Context(), `select data->'changes' from audit_log where action = 'user.updated'`).Scan(&changes)
	if err != nil || !strings.Contains(changes, "fullName") || !strings.Contains(changes, "locationIds") {
		t.Errorf("user.updated changes = %s (%v)", changes, err)
	}
}

func TestAdminCannotLockThemselvesOut(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := users.NewService(db.App)
	if _, err := svc.CreateAdmin(t.Context(), "admin", "Администратор"); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	me := list[0]

	if _, err := svc.SetBlocked(t.Context(), me.ID, me.ID, true); !errors.Is(err, users.ErrSelfLockout) {
		t.Errorf("blocking yourself: error = %v, want ErrSelfLockout", err)
	}
	demoted := users.Profile{FullName: me.FullName, Role: apispec.RoleManager}
	if _, err := svc.Update(t.Context(), me.ID, me.ID, demoted, me.Version); !errors.Is(err, users.ErrSelfLockout) {
		t.Errorf("dropping own admin role: error = %v, want ErrSelfLockout", err)
	}
	renamed := users.Profile{FullName: "Синдилеев К. А.", Role: apispec.RoleAdmin}
	if _, err := svc.Update(t.Context(), me.ID, me.ID, renamed, me.Version); err != nil {
		t.Errorf("renaming yourself: %v", err)
	}
}

func TestBlockEndsSessions(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc := users.NewService(db.App)
	ctx := t.Context()
	store1, _ := createLocations(t, db)
	user, _, err := svc.Create(ctx, "kassir", users.Profile{FullName: "Кассир", Role: apispec.RoleCashier, LocationIDs: []uuid.UUID{store1}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.App.Exec(ctx, `insert into sessions (user_id, token_hash, csrf_token, expires_at) values ($1, 'token', 'csrf', now() + interval '1 day')`, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	blocked, err := svc.SetBlocked(ctx, uuid.New(), user.ID, true)
	if err != nil {
		t.Fatalf("SetBlocked() error = %v", err)
	}
	var sessions int
	if err := db.App.QueryRow(ctx, `select count(*) from sessions`).Scan(&sessions); err != nil || sessions != 0 || !blocked.IsBlocked {
		t.Errorf("after block: sessions=%d blocked=%v err=%v", sessions, blocked.IsBlocked, err)
	}

	unblocked, err := svc.SetBlocked(ctx, uuid.New(), user.ID, false)
	if err != nil || unblocked.IsBlocked {
		t.Errorf("after unblock: blocked=%v err=%v", unblocked.IsBlocked, err)
	}
	if auditCount(t, db, "user.blocked") != 1 || auditCount(t, db, "user.unblocked") != 1 {
		t.Error("block and unblock are not in the audit log")
	}
}
