import type { AuditAction, AuditEntry, LocationKind, OpeningHours, Role } from '@pikni/api'

import { formatDateTime } from '@/format'
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
  'sale_rule.created': 'Новая редакция правила продажи',
  'sale_rule.deleted': 'Удаление редакции правила продажи',
  'sale_ban.created': 'Новый день запрета продажи',
  'sale_ban.deleted': 'Удаление дня запрета продажи',
  'ref.created': 'Новая запись справочника',
  'ref.updated': 'Изменение записи справочника',
  'product.created': 'Создание позиции',
  'product.updated': 'Изменение позиции',
  'variant.created': 'Новый вариант позиции',
  'variant.updated': 'Изменение варианта',
  'barcode.created': 'Привязка штрихкода',
  'barcode.deleted': 'Отвязка штрихкода',
  'price.created': 'Назначение цены',
  'price.deleted': 'Отмена назначенной цены',
  'min_price.created': 'Новая редакция МРЦ',
  'min_price.deleted': 'Удаление редакции МРЦ',
  'assortment.set': 'Изменение ассортиментной матрицы',
  'assortment.removed': 'Удаление из ассортиментной матрицы',
  'assortment.copied': 'Копирование ассортиментной матрицы',
}

export const auditActionColors: Partial<Record<AuditAction, string>> = {
  'auth.login_failed': 'error',
  'user.blocked': 'error',
  'user.created': 'success',
  'location.created': 'success',
  'product.created': 'success',
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
  isActive: 'Действует',
  sort: 'Порядок',
  unit: 'Единица',
  options: 'Значения',
  archivedOptions: 'Значения в архиве',
  filterable: 'В фильтрах',
  usedForAnalogs: 'Для подбора аналогов',
  category: 'Категория',
  kindCode: 'Вид продукции',
  abv: 'Крепость',
  crptMarked: 'Честный знак',
  producer: 'Производитель',
  brand: 'Бренд',
  line: 'Линейка',
  variety: 'Разновидность',
  country: 'Страна',
  region: 'Регион происхождения',
  attributes: 'Характеристики',
  alcocode: 'Алкокод',
  gtin: 'GTIN',
  inn: 'ИНН',
  receivingMode: 'Режим приёмки',
  contactPerson: 'Контактное лицо',
  phone: 'Телефон',
  email: 'Эл. почта',
  minQty: 'Мин. остаток',
  alcohol: 'Алкогольная продукция',
}

const refEntities: Record<string, string> = {
  producer: 'производитель',
  brand: 'бренд',
  origin_region: 'регион',
  volume: 'объём',
  writeoff_reason: 'причина списания',
  product_line: 'линейка',
  supplier: 'поставщик',
  category: 'категория',
  category_attribute: 'характеристика',
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
  // Записи до решения 0031 хранят артикул позиции, новые — только наименование.
  if (e.entityType === 'product')
    return [e.data.article, `«${String(e.data.name)}»`].filter(Boolean).join(' ')
  if (e.entityType === 'variant') return `${e.data.article} «${e.data.name}», ${e.data.variant}`
  if (e.entityType === 'assortment') return 'матрица ' + String(e.data.location)
  if (e.entityType === 'min_price') return 'МРЦ: ' + String(e.data.group)
  if (e.entityType === 'sale_ban') return typeof e.data.name === 'string' ? e.data.name : ''
  if (e.entityType && refEntities[e.entityType] && typeof e.data.name === 'string') {
    return `${refEntities[e.entityType]} «${e.data.name}»`
  }
  if (e.entityType === 'sale_rule') return `регион ${e.data.subjectCode ?? 'РФ'}`
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
    if (Array.isArray(value)) return value.join(', ') || '—'
    return String(value)
  }
  const take = (key: string) => {
    const value = data[key]
    delete data[key]
    return value
  }

  take('login')
  take('code')
  if (/^(ref|product|variant|barcode)\./.test(e.action)) take('name')
  take('article')
  take('variant')
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
    case 'assortment.set':
    case 'assortment.removed':
      take('location')
      take('name')
      details.push({
        label: 'Позиция',
        value: `${String(e.data.article)} ${String(e.data.name)}, ${String(e.data.variant)}`,
      })
      if (data.minQty !== undefined)
        details.push({ label: 'Мин. остаток', value: String(take('minQty')) })
      break
    case 'assortment.copied':
      take('location')
      details.push({ label: 'Из точки', value: String(take('from')) })
      details.push({ label: 'Добавлено вариантов', value: String(take('added')) })
      break
    case 'price.created':
    case 'price.deleted':
      details.push({
        label: take('kind') === 'purchase' ? 'Закупочная цена' : 'Розничная цена',
        value: String(take('price')),
      })
      details.push({ label: 'Действует с', value: formatDateTime(String(take('validFrom'))) })
      break
    case 'min_price.created':
    case 'min_price.deleted':
      take('group')
      details.push({ label: 'Крепость свыше, %', value: String(take('abvFrom')) })
      details.push({ label: 'До, %', value: String(take('abvTo') ?? 'без границы') })
      details.push({
        label: 'Цена',
        value: String(take('price')) + ' за ' + String(take('volumeMl')) + ' мл',
      })
      details.push({ label: 'Действует с', value: String(take('validFrom')) })
      if (data.source) details.push({ label: 'Основание', value: String(take('source')) })
      break
    case 'barcode.created':
    case 'barcode.deleted':
      details.push({ label: 'Штрихкод', value: String(take('code')) })
      break
    case 'product.created':
      details.push({ label: 'Категория', value: String(take('category')) })
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
