import { api, type AuditEntry } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import AuditPage from '../AuditPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { GET: vi.fn<ApiModule['api']['GET']>() },
}))

const user = { id: 'u-1', login: 'admin', fullName: 'Синдилеев К. А.' }
const firstPage: AuditEntry[] = [
  { id: 12, at: '2026-10-23T10:30:00Z', action: 'auth.login', user, data: {} },
  {
    id: 11,
    at: '2026-10-23T10:29:00Z',
    action: 'auth.login_failed',
    data: { reason: 'unknown_login' },
  },
]
const secondPage: AuditEntry[] = [
  { id: 10, at: '2026-10-23T10:00:00Z', action: 'user.created', data: { login: 'kassir' } },
]

describe('AuditPage', () => {
  it('показывает события и подгружает следующую страницу по курсору', async () => {
    vi.mocked(api.GET).mockImplementation(((
      path: string,
      init?: { params?: { query?: { before?: number } } },
    ) => {
      const body =
        path === '/audit'
          ? init?.params?.query?.before === 11
            ? { items: secondPage }
            : { items: firstPage, nextBefore: 11 }
          : { items: [] }
      return Promise.resolve({ data: body, response: new Response() })
    }) as never)

    const wrapper = mount(AuditPage, {
      global: {
        plugins: [createPikniVuetify(), [VueQueryPlugin, { queryClient: new QueryClient() }]],
      },
    })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('Вход')
    expect(text).toContain('Неудачный вход')
    expect(text).toContain('Синдилеев К. А.')
    expect(text).not.toContain('Создание сотрудника')
    expect(wrapper.find('[data-test="export"]').attributes('href')).toMatch(
      /^\/api\/v1\/audit\/export\?tz=/,
    )

    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Показать ещё')!
      .trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Создание сотрудника')
    expect(wrapper.findAll('button').some((b) => b.text() === 'Показать ещё')).toBe(false)
  })
})
