import type { Location, Me } from '@pikni/api'

export function makeLocation(
  l: Partial<Location> & Pick<Location, 'id' | 'code' | 'name'>,
): Location {
  return {
    kind: 'store',
    city: 'Благовещенск',
    timezone: 'Asia/Yakutsk',
    subjectCode: '28',
    openingHours: {},
    isPublic: false,
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
