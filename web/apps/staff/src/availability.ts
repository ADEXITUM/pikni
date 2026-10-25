import type { AttributeType, CategoryAttribute } from '@pikni/api'
import type { LocationQuery, LocationQueryRaw } from 'vue-router'

// Фильтры поиска наличия живут в адресе страницы (НЛ-1): ссылкой можно поделиться, а «назад»
// возвращает к тем же результатам.
export interface AvailabilityFilters {
  q: string
  categoryId?: string
  kindCode?: string
  brandId?: string
  countryCode?: string
  volumeMl?: number
  abvFrom?: number
  abvTo?: number
  priceFrom?: number
  priceTo?: number
  city?: string
  locationId?: string
  locationKind?: 'store' | 'warehouse'
  // Вся сеть вместо своего города: сотруднику точки поиск по умолчанию идёт в его городе.
  network: boolean
  inStock: boolean
  sort: 'name' | 'price' | 'stock' | 'new'
  // Характеристики категории: значение или диапазон числа.
  attrs: Record<string, string | boolean | { min?: number; max?: number }>
}

const text = (v: unknown) => (typeof v === 'string' && v !== '' ? v : undefined)
const num = (v: unknown) => {
  const n = typeof v === 'string' && v !== '' ? Number(v.replace(',', '.')) : NaN
  return Number.isFinite(n) ? n : undefined
}

export function filtersFromQuery(query: LocationQuery): AvailabilityFilters {
  let attrs: AvailabilityFilters['attrs'] = {}
  try {
    const parsed: unknown = JSON.parse(text(query.attrs) ?? '{}')
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed))
      attrs = parsed as typeof attrs
  } catch {
    // испорченный адрес — без фильтра характеристик
  }
  const sort = text(query.sort)
  const kind = text(query.locationKind)
  return {
    q: text(query.q) ?? '',
    categoryId: text(query.categoryId),
    kindCode: text(query.kindCode),
    brandId: text(query.brandId),
    countryCode: text(query.countryCode),
    volumeMl: num(query.volumeMl),
    abvFrom: num(query.abvFrom),
    abvTo: num(query.abvTo),
    priceFrom: num(query.priceFrom),
    priceTo: num(query.priceTo),
    city: text(query.city),
    locationId: text(query.locationId),
    locationKind: kind === 'store' || kind === 'warehouse' ? kind : undefined,
    network: query.network === '1',
    inStock: query.inStock === '1',
    sort: sort === 'price' || sort === 'stock' || sort === 'new' ? sort : 'name',
    attrs,
  }
}

export function queryFromFilters(f: AvailabilityFilters): LocationQueryRaw {
  const q: LocationQueryRaw = {}
  for (const [k, v] of Object.entries(f)) {
    if (k === 'attrs' || v === undefined || v === '' || v === false) continue
    if (k === 'sort' && v === 'name') continue
    q[k] = v === true ? '1' : String(v)
  }
  const attrs = cleanAttrs(f.attrs)
  if (Object.keys(attrs).length) q.attrs = JSON.stringify(attrs)
  return q
}

// cleanAttrs убирает пустые значения: незаполненный диапазон фильтром не считается.
export function cleanAttrs(attrs: AvailabilityFilters['attrs']): AvailabilityFilters['attrs'] {
  const out: AvailabilityFilters['attrs'] = {}
  for (const [k, v] of Object.entries(attrs)) {
    if (typeof v === 'object' && v !== null) {
      const r: { min?: number; max?: number } = {}
      if (typeof v.min === 'number') r.min = v.min
      if (typeof v.max === 'number') r.max = v.max
      if (r.min !== undefined || r.max !== undefined) out[k] = r
    } else if (v !== '' && v !== undefined && v !== null) {
      out[k] = v
    }
  }
  return out
}

// Фильтруемые характеристики выбранной категории — свои и унаследованные от предков.
export function filterableAttributes(
  attributes: CategoryAttribute[],
  categoryPath: string[],
): CategoryAttribute[] {
  const path = new Set(categoryPath)
  return attributes
    .filter((a) => a.filterable && a.isActive && path.has(a.categoryId))
    .sort((a, b) => a.sort - b.sort || a.name.localeCompare(b.name))
}

export const rangeTypes: AttributeType[] = ['number']
