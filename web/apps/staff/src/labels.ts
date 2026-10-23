import type { Role } from '@pikni/api'

export const roleLabels: Record<Role, string> = {
  admin: 'Администратор',
  manager: 'Руководитель',
  accountant: 'Бухгалтер',
  store_manager: 'Управляющий магазином',
  storekeeper: 'Кладовщик',
  cashier: 'Продавец-кассир',
}

// Повторяет auth.networkRoles на сервере: этим ролям точки не назначаются.
export const networkRoles: ReadonlySet<Role> = new Set<Role>(['admin', 'manager', 'accountant'])
