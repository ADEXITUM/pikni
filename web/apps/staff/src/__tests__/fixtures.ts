import type { Category, CategoryAttribute, Location, Me } from '@pikni/api'

export function makeCategory(c: Partial<Category> & Pick<Category, 'id' | 'name'>): Category {
  return { depth: 1, sort: 10, isActive: true, alcohol: true, ...c }
}

export function makeAttribute(
  a: Partial<CategoryAttribute> & Pick<CategoryAttribute, 'id' | 'categoryId' | 'code'>,
): CategoryAttribute {
  return {
    name: a.code,
    type: 'text',
    options: [],
    filterable: false,
    usedForAnalogs: false,
    sort: 10,
    isActive: true,
    ...a,
  }
}

export function makeLocation(
  l: Partial<Location> & Pick<Location, 'id' | 'code' | 'name'>,
): Location {
  return {
    kind: 'store',
    city: 'Благовещенск',
    timezone: 'Asia/Yakutsk',
    subjectCode: '28',
    openingHours: {},
    isActive: true,
    version: 1,
    zones: [],
    ...l,
  }
}

export function makeMe(m: Partial<Me> = {}): Me {
  return {
    id: 'u-admin',
    login: 'admin',
    fullName: 'Администратор',
    role: 'admin',
    mustChangePassword: false,
    csrfToken: 'csrf',
    permissions: [],
    locationIds: [],
    ...m,
  }
}
