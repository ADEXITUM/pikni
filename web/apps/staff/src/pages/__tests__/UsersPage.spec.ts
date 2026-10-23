import { api, type Location, type User } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import { makeLocation } from '@/__tests__/fixtures'

import UsersPage from '../UsersPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { GET: vi.fn<ApiModule['api']['GET']>() },
}))

const locations: Location[] = [
  makeLocation({
    id: 'mg-1',
    code: 'МГ-01',
    name: 'Магазин',
    kind: 'store',
    city: 'Благовещенск',
    timezone: 'Asia/Yakutsk',
  }),
  makeLocation({
    id: 'sk-1',
    code: 'СК-01',
    name: 'Склад',
    kind: 'warehouse',
    city: 'Благовещенск',
    timezone: 'Asia/Yakutsk',
  }),
]

const base = {
  mustChangePassword: false,
  isBlocked: false,
  version: 1,
  createdAt: '2026-10-01T00:00:00Z',
}
const users: User[] = [
  { ...base, id: '1', login: 'admin', fullName: 'Администратор', role: 'admin', locationIds: [] },
  {
    ...base,
    id: '2',
    login: 'kassir',
    fullName: 'Кассир',
    role: 'cashier',
    locationIds: ['mg-1'],
    mustChangePassword: true,
  },
  {
    ...base,
    id: '3',
    login: 'sklad',
    fullName: 'Кладовщик',
    role: 'storekeeper',
    locationIds: ['mg-1', 'sk-1'],
    isBlocked: true,
  },
]

describe('UsersPage', () => {
  it('показывает сотрудников с ролями, точками и статусом', async () => {
    vi.mocked(api.GET).mockImplementation(((path: string) =>
      Promise.resolve({
        data: { items: path === '/users' ? users : locations },
        response: new Response(),
      })) as never)

    const wrapper = mount(UsersPage, {
      global: {
        plugins: [createPikniVuetify(), [VueQueryPlugin, { queryClient: new QueryClient() }]],
      },
    })
    await flushPromises()

    const rows = wrapper.findAll('tbody tr').map((r) => r.text())
    expect(rows[0]).toContain('Администратор')
    expect(rows[0]).toContain('вся сеть')
    expect(rows[0]).toContain('Активен')
    expect(rows[1]).toContain('Продавец-кассир')
    expect(rows[1]).toContain('Временный пароль')
    expect(rows[2]).toContain('МГ-01, СК-01')
    expect(rows[2]).toContain('Заблокирован')
  })
})
