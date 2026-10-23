package migrations_test

import (
	"testing"

	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

const (
	insufficientPrivilege = "42501"
	uniqueViolation       = "23505"
	raiseException        = "P0001"
)

func TestAuditLogIsAppendOnly(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := t.Context()

	if _, err := db.App.Exec(ctx, `insert into audit_log (action) values ('test')`); err != nil {
		t.Fatalf("insert as app: %v", err)
	}

	for _, stmt := range []string{
		`update audit_log set action = 'changed'`,
		`delete from audit_log`,
		`truncate audit_log`,
	} {
		t.Run(stmt, func(t *testing.T) {
			_, err := db.App.Exec(ctx, stmt)
			dbtest.WantCode(t, err, insufficientPrivilege)

			_, err = db.Owner.Exec(ctx, stmt)
			dbtest.WantCode(t, err, raiseException)
		})
	}
}

func TestAppCannotChangeSchema(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)

	for _, stmt := range []string{
		`create table scratch (id int)`,
		`alter table audit_log disable trigger audit_log_append_only`,
		`drop table audit_log`,
	} {
		t.Run(stmt, func(t *testing.T) {
			_, err := db.App.Exec(t.Context(), stmt)
			dbtest.WantCode(t, err, insufficientPrivilege)
		})
	}
}

func TestUserLoginIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := t.Context()

	const insert = `insert into users (login, full_name, role, password_hash) values ($1, 'Петрова А. В.', 'cashier', 'x')`
	if _, err := db.App.Exec(ctx, insert, "Petrova"); err != nil {
		t.Fatalf("insert first user: %v", err)
	}

	_, err := db.App.Exec(ctx, insert, "petrova")
	dbtest.WantCode(t, err, uniqueViolation)
}
