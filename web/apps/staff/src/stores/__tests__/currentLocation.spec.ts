import type { Me } from '@pikni/api'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'

import { ALL_LOCATIONS, useCurrentLocation } from '../currentLocation'

function user(id: string, locationIds: string[]): Me {
  return {
    id,
    login: id,
    fullName: id,
    role: 'store_manager',
    mustChangePassword: false,
    csrfToken: 'csrf',
    permissions: [],
    locationIds,
  }
}

describe('useCurrentLocation', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('по умолчанию все точки, если их несколько, и единственную, если одна', () => {
    const store = useCurrentLocation()

    store.restore(user('boss', ['mg-1', 'mg-2']))
    expect(store.selected).toBe(ALL_LOCATIONS)

    store.restore(user('kassir', ['mg-1']))
    expect(store.selected).toBe('mg-1')

    store.restore(user('new', []))
    expect(store.selected).toBeNull()
  })

  it('запоминает выбор для каждого пользователя отдельно', () => {
    const store = useCurrentLocation()
    store.restore(user('boss', ['mg-1', 'mg-2']))
    store.select('mg-2')

    store.restore(user('other', ['mg-1', 'mg-2']))
    expect(store.selected).toBe(ALL_LOCATIONS)

    store.restore(user('boss', ['mg-1', 'mg-2']))
    expect(store.selected).toBe('mg-2')
  })

  it('не берёт сохранённую точку, если она больше не доступна', () => {
    const store = useCurrentLocation()
    store.restore(user('boss', ['mg-1', 'mg-2']))
    store.select('mg-2')

    store.restore(user('boss', ['mg-1', 'mg-3']))
    expect(store.selected).toBe(ALL_LOCATIONS)
  })
})
