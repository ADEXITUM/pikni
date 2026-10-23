package audit_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
	"github.com/ADEXITUM/pikni/api/internal/audit"
	"github.com/ADEXITUM/pikni/api/internal/platform/dbtest"
)

func list(t *testing.T, db audit.DB, p apispec.ListAuditLogParams) apispec.AuditPage {
	t.Helper()
	resp, err := audit.NewHandler(db).ListAuditLog(t.Context(), apispec.ListAuditLogRequestObject{Params: p})
	if err != nil {
		t.Fatalf("ListAuditLog() error = %v", err)
	}
	return apispec.AuditPage(resp.(apispec.ListAuditLog200JSONResponse))
}

func TestRecordIsPartOfTransaction(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := audit.WithActor(t.Context(), audit.Actor{IP: "203.0.113.7"})

	errRollback := errors.New("rollback")
	err := pgx.BeginFunc(ctx, db.App, func(tx pgx.Tx) error {
		if err := audit.Record(ctx, tx, audit.Event{Action: apispec.AuditActionAuthLogout}); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("transaction error = %v", err)
	}

	err = audit.Record(ctx, db.App, audit.Event{
		Action:     apispec.AuditActionUserCreated,
		EntityType: audit.EntityUser,
		EntityID:   "42",
		Data:       map[string]any{"login": "petrova"},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	page := list(t, db.App, apispec.ListAuditLogParams{})
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want only the committed entry", len(page.Items))
	}
	e := page.Items[0]
	if e.Action != apispec.AuditActionUserCreated || e.IP == nil || *e.IP != "203.0.113.7" ||
		e.Data["login"] != "petrova" || e.User != nil || *e.EntityID != "42" {
		t.Errorf("entry = %+v", e)
	}
}

func TestListFiltersAndPages(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	for i := range 5 {
		action := apispec.AuditActionAuthLogin
		if i%2 == 1 {
			action = apispec.AuditActionAuthLogout
		}
		if err := audit.Record(t.Context(), db.App, audit.Event{Action: action}); err != nil {
			t.Fatal(err)
		}
	}

	limit := 2
	var ids []int64
	var before *int64
	for _, want := range []int{2, 2, 1} {
		page := list(t, db.App, apispec.ListAuditLogParams{Limit: &limit, Before: before})
		if len(page.Items) != want {
			t.Fatalf("page size = %d, want %d", len(page.Items), want)
		}
		for _, e := range page.Items {
			ids = append(ids, e.ID)
		}
		before = page.NextBefore
	}
	if before != nil {
		t.Errorf("last page has nextBefore = %d", *before)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] >= ids[i-1] {
			t.Fatalf("ids are not strictly descending: %v", ids)
		}
	}

	logout := apispec.AuditActionAuthLogout
	if page := list(t, db.App, apispec.ListAuditLogParams{Action: &logout}); len(page.Items) != 2 {
		t.Errorf("logout entries = %d, want 2", len(page.Items))
	}
}
