import type { AuditAction, AuditEntry, LocationKind, OpeningHours, Role } from '@pikni/api'

import { formatHours } from '@/hours'
import { locationKindLabels, roleLabels } from '@/labels'

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
  'location.created': 'Создание точки',
  'location.updated': 'Изменение точки',
}

export const auditActionColors: Partial<Record<AuditAction, string>> = {
  'auth.login_failed': 'error',
  'user.blocked': 'error',
  'user.created': 'success',
  'location.created': 'success',
}

const failureReasons: Record<string, string> = {
  unknown_login: 'такого логина нет',
  password: 'неверный пароль',
  locked: 'вход временно закрыт после неудачных попыток',
  blocked: 'учётная запись заблокирована',
}

// Названия полей в изменениях «было → стало».
const fieldLabels: Record<string, string> = {
  fullName: 'ФИО',
  role: 'Роль',
  locationIds: 'Точки',
  name: 'Название',
  city: 'Город',
  timezone: 'Часовой пояс',
  subjectCode: 'Регион',
  address: 'Адрес',
  lat: 'Широта',
  lon: 'Долгота',
  fsrarId: 'ФСРАР ID',
  kpp: 'КПП',
  licenseNo: 'Лицензия',
  licenseValidTo: 'Лицензия до',
  openingHours: 'Режим работы',
  isPublic: 'На публичной странице',
  isActive: 'Действует',
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
  if (e.entityType === 'location') return typeof e.data.code === 'string' ? e.data.code : ''
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
  const show = (key: string, value: unknown): string => {
    if (value === null || value === undefined || value === '') return '—'
    if (typeof value === 'boolean') return value ? 'да' : 'нет'
    if (key === 'role') return role(value)
    if (key === 'locationIds') return places(value)
    if (key === 'openingHours') return formatHours(value as OpeningHours)
    return String(value)
  }
  const take = (key: string) => {
    const value = data[key]
    delete data[key]
    return value
  }

  take('login')
  take('code')
  const changes = take('changes') as Record<string, [unknown, unknown]> | undefined
  for (const [key, [was, now]] of Object.entries(changes ?? {})) {
    details.push({ label: fieldLabels[key] ?? key, value: `${show(key, was)} → ${show(key, now)}` })
  }

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
    case 'location.created':
      details.push({ label: 'Название', value: String(take('name')) })
      details.push({ label: 'Тип', value: locationKindLabels[take('kind') as LocationKind] })
      break
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
