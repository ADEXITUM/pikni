package auth

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/ADEXITUM/pikni/api/internal/apispec"
)

type scope int

const (
	scopeNone scope = iota
	scopeOwn
	scopeAll
)

var ErrForbidden = errors.New("permission denied")

// Роли уровня сети работают со всеми точками, точки им не назначаются.
var networkRoles = map[apispec.Role]bool{
	apispec.RoleAdmin:      true,
	apispec.RoleManager:    true,
	apispec.RoleAccountant: true,
}

// matrix повторяет таблицу ПБ-6 ТЗ: «+» и «П» — scopeAll, «Т» и «ТП» — scopeOwn.
// users.view у руководителя — список сотрудников для фильтра журнала действий.
// Просмотр и изменение — разные права, поэтому «П» и «+» различаются набором прав.
var matrix = map[apispec.Role]map[apispec.Permission]scope{
	apispec.RoleAdmin: {
		apispec.PermissionUsersManage:      scopeAll,
		apispec.PermissionUsersView:        scopeAll,
		apispec.PermissionRefsView:         scopeAll,
		apispec.PermissionRefsEdit:         scopeAll,
		apispec.PermissionCostView:         scopeAll,
		apispec.PermissionReceiptView:      scopeAll,
		apispec.PermissionAvailabilityView: scopeAll,
		apispec.PermissionAssetsView:       scopeAll,
		apispec.PermissionAuditView:        scopeAll,
	},
	apispec.RoleManager: {
		apispec.PermissionUsersView:        scopeAll,
		apispec.PermissionRefsView:         scopeAll,
		apispec.PermissionRefsEdit:         scopeAll,
		apispec.PermissionCostView:         scopeAll,
		apispec.PermissionReceiptView:      scopeAll,
		apispec.PermissionFloorView:        scopeAll,
		apispec.PermissionTransferRequest:  scopeAll,
		apispec.PermissionTransferView:     scopeAll,
		apispec.PermissionPosView:          scopeAll,
		apispec.PermissionWriteoffCreate:   scopeAll,
		apispec.PermissionDocsApprove:      scopeAll,
		apispec.PermissionStocktakeManage:  scopeAll,
		apispec.PermissionOpeningPost:      scopeAll,
		apispec.PermissionAvailabilityView: scopeAll,
		apispec.PermissionEgaisReconcile:   scopeAll,
		apispec.PermissionAssetsView:       scopeAll,
		apispec.PermissionReportsView:      scopeAll,
		apispec.PermissionAuditView:        scopeAll,
	},
	apispec.RoleAccountant: {
		apispec.PermissionRefsView:         scopeAll,
		apispec.PermissionCostView:         scopeAll,
		apispec.PermissionReceiptView:      scopeAll,
		apispec.PermissionFloorView:        scopeAll,
		apispec.PermissionTransferView:     scopeAll,
		apispec.PermissionPosView:          scopeAll,
		apispec.PermissionAvailabilityView: scopeAll,
		apispec.PermissionEgaisReconcile:   scopeAll,
		apispec.PermissionOpeningPost:      scopeAll,
		apispec.PermissionAssetsView:       scopeAll,
		apispec.PermissionAssetsEdit:       scopeAll,
		apispec.PermissionReportsView:      scopeAll,
	},
	apispec.RoleStoreManager: {
		apispec.PermissionRefsView:           scopeAll,
		apispec.PermissionReceiptView:        scopeOwn,
		apispec.PermissionReceiptEdit:        scopeOwn,
		apispec.PermissionFloorView:          scopeOwn,
		apispec.PermissionFloorEdit:          scopeOwn,
		apispec.PermissionTransferRequest:    scopeOwn,
		apispec.PermissionTransferView:       scopeOwn,
		apispec.PermissionTransferEdit:       scopeOwn,
		apispec.PermissionPosView:            scopeOwn,
		apispec.PermissionPosSell:            scopeOwn,
		apispec.PermissionWriteoffCreate:     scopeOwn,
		apispec.PermissionStocktakeManage:    scopeOwn,
		apispec.PermissionStocktakeCount:     scopeOwn,
		apispec.PermissionReservationsManage: scopeOwn,
		apispec.PermissionAvailabilityView:   scopeAll,
		apispec.PermissionEgaisReconcile:     scopeOwn,
		apispec.PermissionAssetsView:         scopeOwn,
		apispec.PermissionReportsView:        scopeOwn,
	},
	apispec.RoleStorekeeper: {
		apispec.PermissionRefsView:         scopeAll,
		apispec.PermissionReceiptView:      scopeOwn,
		apispec.PermissionReceiptEdit:      scopeOwn,
		apispec.PermissionFloorView:        scopeOwn,
		apispec.PermissionFloorEdit:        scopeOwn,
		apispec.PermissionTransferRequest:  scopeOwn,
		apispec.PermissionTransferView:     scopeOwn,
		apispec.PermissionTransferEdit:     scopeOwn,
		apispec.PermissionWriteoffCreate:   scopeOwn,
		apispec.PermissionStocktakeManage:  scopeOwn,
		apispec.PermissionStocktakeCount:   scopeOwn,
		apispec.PermissionAvailabilityView: scopeAll,
		apispec.PermissionEgaisReconcile:   scopeOwn,
		apispec.PermissionAssetsView:       scopeOwn,
		apispec.PermissionReportsView:      scopeOwn,
	},
	apispec.RoleCashier: {
		apispec.PermissionRefsView:           scopeAll,
		apispec.PermissionFloorView:          scopeOwn,
		apispec.PermissionFloorEdit:          scopeOwn,
		apispec.PermissionTransferRequest:    scopeOwn,
		apispec.PermissionPosView:            scopeOwn,
		apispec.PermissionPosSell:            scopeOwn,
		apispec.PermissionStocktakeCount:     scopeOwn,
		apispec.PermissionReservationsManage: scopeOwn,
		apispec.PermissionAvailabilityView:   scopeAll,
		apispec.PermissionReportsView:        scopeOwn,
	},
}

func NetworkRole(r apispec.Role) bool {
	return networkRoles[r]
}

func scopeOf(role string, p apispec.Permission) scope {
	return matrix[apispec.Role(role)][p]
}

func permissionsOf(role string) []apispec.Permission {
	return slices.Sorted(maps.Keys(matrix[apispec.Role(role)]))
}

// Has сообщает, есть ли у текущего пользователя право хотя бы в одной точке. Нужна там, где
// от права зависит не доступ к операции, а состав ответа: закупочную цену видит не каждый.
func Has(ctx context.Context, p apispec.Permission) bool {
	sess, ok := FromContext(ctx)
	return ok && scopeOf(sess.Role, p) != scopeNone
}

// AllowedLocations — точки, в пределах которых у текущего пользователя есть право.
// nil означает всю сеть: сервис передаёт результат прямо в SQL-фильтр.
func AllowedLocations(ctx context.Context, p apispec.Permission) ([]uuid.UUID, error) {
	sess, ok := FromContext(ctx)
	if !ok {
		return nil, ErrForbidden
	}
	switch scopeOf(sess.Role, p) {
	case scopeAll:
		return nil, nil
	case scopeOwn:
		// Пустой, но не nil, если точки не назначены: иначе фильтр пропустил бы всю сеть.
		return append([]uuid.UUID{}, sess.LocationIDs...), nil
	}
	return nil, ErrForbidden
}

// RoleCan — есть ли у роли право в точке. Нужна там, где пользователь входит не через сессию
// основного приложения, а, например, по PIN на кассе.
func RoleCan(role string, p apispec.Permission, location uuid.UUID, userLocations []uuid.UUID) bool {
	switch scopeOf(role, p) {
	case scopeAll:
		return true
	case scopeOwn:
		return slices.Contains(userLocations, location)
	}
	return false
}

// CheckLocation проверяет точку, которую прислал клиент. Выбор точки в интерфейсе —
// только подсказка, поэтому её проверяет каждый метод, принимающий точку.
func CheckLocation(ctx context.Context, p apispec.Permission, location uuid.UUID) error {
	allowed, err := AllowedLocations(ctx, p)
	if err != nil {
		return err
	}
	if allowed != nil && !slices.Contains(allowed, location) {
		return ErrForbidden
	}
	return nil
}
