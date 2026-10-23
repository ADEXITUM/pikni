import type { AuditAction, AuditEntry, Role } from '@pikni/api'

import { roleLabels } from '@/labels'

// Тот же словарь есть на сервере для выгрузки в Excel.
export const auditActionLabels: Record<AuditAction, string> = {
  'auth.login': 'Вход',
  'auth.login_failed': 'Неудачный вход',
  'auth.logout': 'Выход',
  'auth.password_changed': 'Смена пароля',
  'auth.session_ended': 'Завершение сеанса',
  'user.created': 'Создание сотрудника',
  'user.updated': 'Изменение сотрудника',
  'user.blocked': 'Блокировка сотрудника',
  'user.unblocked': 'Разблокировка сотрудника',
  'user.password_reset': 'Сброс пароля',
}

export const auditActionColors: Partial<Record<AuditAction, string>> = {
  'auth.login_failed': 'error',
  'user.blocked': 'error',
  'user.created': 'success',
}

const failureReasons: Record<string, string> = {
  unknown_login: 'такого логина нет',
  password: 'неверный пароль',
  locked: 'вход временно закрыт после неудачных попыток',
  blocked: 'учётная запись заблокирована',
}

export interface Detail {
  label: string
  value: string
}

export function auditActor(e: AuditEntry): string {
  if (e.user) return e.user.fullName
  return e.action === 'auth.login_failed' ? '—' : 'Система'
}

export function auditObject(e: AuditEntry): string {
  const login = typeof e.data.login === 'string' ? e.data.login : undefined
  if (e.entityType === 'user') return login ?? e.entityId ?? ''
  if (e.entityType === 'session') return 'сеанс'
  return ''
}

// auditDetails переводит данные события в строки «поле: значение». Поля, которых
// словарь не знает, показываются как есть, чтобы ничего не терялось.
export function auditDetails(e: AuditEntry, locationCodes: Map<string, string>): Detail[] {
  const data: Record<string, unknown> = { ...e.data }
  const details: Detail[] = []
  const role = (r: unknown) => roleLabels[r as Role] ?? String(r)
  const places = (ids: unknown) => {
    const list = Array.isArray(ids) ? ids.map((id) => locationCodes.get(id) ?? String(id)) : []
    return list.join(', ') || 'вся сеть'
  }
  const take = (key: string) => {
    const value = data[key]
    delete data[key]
    return value
  }

  take('login')
  switch (e.action) {
    case 'auth.login_failed': {
      const reason = String(take('reason'))
      details.push({ label: 'Причина', value: failureReasons[reason] ?? reason })
      if (take('locked') === true) details.push({ label: 'Итог', value: 'вход закрыт на 15 минут' })
      break
    }
    case 'user.created':
      details.push({ label: 'Роль', value: role(take('role')) })
      details.push({ label: 'Точки', value: places(take('locationIds')) })
      break
    case 'user.updated': {
      const changes = (take('changes') ?? {}) as Record<string, [unknown, unknown]>
      if (changes.fullName) details.push({ label: 'ФИО', value: changes.fullName.join(' → ') })
      if (changes.role) details.push({ label: 'Роль', value: changes.role.map(role).join(' → ') })
      if (changes.locationIds) {
        details.push({ label: 'Точки', value: changes.locationIds.map(places).join(' → ') })
      }
      break
    }
    case 'auth.session_ended': {
      const count = take('count')
      if (count !== undefined) details.push({ label: 'Завершено сеансов', value: String(count) })
      break
    }
  }

  for (const [key, value] of Object.entries(data)) {
    details.push({ label: key, value: JSON.stringify(value) })
  }
  if (e.ip) details.push({ label: 'IP', value: e.ip })
  return details
}
