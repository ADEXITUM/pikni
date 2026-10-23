package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
)

// Выборочные клетки таблицы ПБ-6 ТЗ (сценарий испытаний П-02).
func TestMatrixFollowsSpecification(t *testing.T) {
	tests := []struct {
		role apispec.Role
		perm apispec.Permission
		want scope
	}{
		{apispec.RoleAdmin, apispec.PermissionUsersManage, scopeAll},
		{apispec.RoleManager, apispec.PermissionUsersManage, scopeNone},
		{apispec.RoleManager, apispec.PermissionUsersView, scopeAll},
		{apispec.RoleAccountant, apispec.PermissionUsersView, scopeNone},
		{apispec.RoleAdmin, apispec.PermissionPosSell, scopeNone},
		{apispec.RoleAdmin, apispec.PermissionReceiptEdit, scopeNone},
		{apispec.RoleManager, apispec.PermissionPosView, scopeAll},
		{apispec.RoleManager, apispec.PermissionPosSell, scopeNone},
		{apispec.RoleManager, apispec.PermissionDocsApprove, scopeAll},
		{apispec.RoleStoreManager, apispec.PermissionDocsApprove, scopeNone},
		{apispec.RoleAccountant, apispec.PermissionCostView, scopeAll},
		{apispec.RoleStoreManager, apispec.PermissionCostView, scopeNone},
		{apispec.RoleAccountant, apispec.PermissionAuditView, scopeNone},
		{apispec.RoleStorekeeper, apispec.PermissionReceiptEdit, scopeOwn},
		{apispec.RoleCashier, apispec.PermissionPosSell, scopeOwn},
		{apispec.RoleCashier, apispec.PermissionReceiptView, scopeNone},
		{apispec.RoleCashier, apispec.PermissionAvailabilityView, scopeAll},
		{apispec.RoleCashier, apispec.PermissionStocktakeCount, scopeOwn},
		{apispec.RoleManager, apispec.PermissionStocktakeCount, scopeNone},
	}
	for _, tt := range tests {
		if got := scopeOf(string(tt.role), tt.perm); got != tt.want {
			t.Errorf("scopeOf(%s, %s) = %d, want %d", tt.role, tt.perm, got, tt.want)
		}
	}
}

func TestMatrixInvariants(t *testing.T) {
	spec, err := apispec.GetSpec()
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range spec.Components.Schemas["Role"].Value.Enum {
		role := apispec.Role(r.(string))
		if len(matrix[role]) == 0 {
			t.Errorf("role %s has no permissions", role)
		}
		for p, s := range matrix[role] {
			if networkRoles[role] && s == scopeOwn {
				t.Errorf("network role %s has location-scoped %s", role, p)
			}
			if view, ok := strings.CutSuffix(string(p), ".edit"); ok && matrix[role][apispec.Permission(view+".view")] < s {
				t.Errorf("role %s can edit but not view %s", role, view)
			}
		}
	}

	granted := map[apispec.Permission]bool{}
	for _, perms := range matrix {
		for p := range perms {
			granted[p] = true
		}
	}
	for _, p := range spec.Components.Schemas["Permission"].Value.Enum {
		if !granted[apispec.Permission(p.(string))] {
			t.Errorf("permission %s is granted to nobody", p)
		}
	}
}

func TestAllowedLocations(t *testing.T) {
	store := uuid.New()
	cashier := context.WithValue(t.Context(), sessionKey, Session{Role: "cashier", LocationIDs: []uuid.UUID{store}})
	unassigned := context.WithValue(t.Context(), sessionKey, Session{Role: "cashier"})

	if got, err := AllowedLocations(cashier, apispec.PermissionPosSell); err != nil || len(got) != 1 || got[0] != store {
		t.Errorf("own scope = %v, %v; want [%s]", got, err, store)
	}
	if got, err := AllowedLocations(cashier, apispec.PermissionAvailabilityView); err != nil || got != nil {
		t.Errorf("network scope = %v, %v; want nil", got, err)
	}
	if got, err := AllowedLocations(unassigned, apispec.PermissionPosSell); err != nil || got == nil || len(got) != 0 {
		t.Errorf("own scope without locations = %#v, %v; want empty non-nil", got, err)
	}
	if _, err := AllowedLocations(cashier, apispec.PermissionUsersManage); !errors.Is(err, ErrForbidden) {
		t.Errorf("missing permission error = %v, want ErrForbidden", err)
	}
	if _, err := AllowedLocations(t.Context(), apispec.PermissionRefsView); !errors.Is(err, ErrForbidden) {
		t.Errorf("no session error = %v, want ErrForbidden", err)
	}
}

func TestCheckLocation(t *testing.T) {
	own, other := uuid.New(), uuid.New()
	cashier := context.WithValue(t.Context(), sessionKey, Session{Role: "cashier", LocationIDs: []uuid.UUID{own}})
	manager := context.WithValue(t.Context(), sessionKey, Session{Role: "manager"})

	tests := []struct {
		name     string
		ctx      context.Context
		perm     apispec.Permission
		location uuid.UUID
		allowed  bool
	}{
		{"кассир в своём магазине", cashier, apispec.PermissionPosSell, own, true},
		{"кассир в чужом магазине", cashier, apispec.PermissionPosSell, other, false},
		{"кассир смотрит наличие в чужом", cashier, apispec.PermissionAvailabilityView, other, true},
		{"руководитель в любой точке", manager, apispec.PermissionPosView, other, true},
		{"руководитель не продаёт", manager, apispec.PermissionPosSell, own, false},
	}
	for _, tt := range tests {
		err := CheckLocation(tt.ctx, tt.perm, tt.location)
		if (err == nil) != tt.allowed {
			t.Errorf("%s: error = %v, want allowed=%v", tt.name, err, tt.allowed)
		}
	}
}

func TestAuthorize(t *testing.T) {
	cashier := Session{Role: "cashier"}
	temporary := Session{Role: "admin", MustChangePassword: true}
	adminOnly := operation{permission: apispec.PermissionUsersManage}
	anyone := operation{}

	tests := []struct {
		name string
		sess Session
		op   operation
		want int
	}{
		{"cashier, refs.view", cashier, operation{permission: apispec.PermissionRefsView}, 0},
		{"cashier, users.manage", cashier, adminOnly, http.StatusForbidden},
		{"temporary password, users.manage", temporary, adminOnly, http.StatusForbidden},
		{"temporary password, authenticated", temporary, anyone, 0},
	}
	for _, tt := range tests {
		err := authorize(tt.sess, tt.op)
		if got := echo.StatusCode(err); got != tt.want {
			t.Errorf("%s: status = %d (%v), want %d", tt.name, got, err, tt.want)
		}
	}
}

func TestLoadOperations(t *testing.T) {
	ops, err := loadOperations()
	if err != nil {
		t.Fatalf("loadOperations() error = %v", err)
	}
	want := map[string]operation{
		"login":         {public: true},
		"getme":         {},
		"listlocations": {permission: apispec.PermissionRefsView},
	}
	for name, op := range want {
		if ops[name] != op {
			t.Errorf("operation %s = %+v, want %+v", name, ops[name], op)
		}
	}
}
