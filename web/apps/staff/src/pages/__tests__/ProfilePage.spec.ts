import { api, type Me, type SessionInfo } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { autoConfirm } from '@/__tests__/confirm'

import { meQuery } from '@/session'

import ProfilePage from '../ProfilePage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { GET: vi.fn<ApiModule['api']['GET']>(), DELETE: vi.fn<ApiModule['api']['DELETE']>() },
}))

const me: Me = {
  id: 'u1',
  login: 'petrova',
  fullName: 'Петрова А. В.',
  role: 'store_manager',
  mustChangePassword: false,
  csrfToken: 'csrf',
  permissions: [],
  locationIds: [],
}

const sessions: SessionInfo[] = [
  {
    id: 's-pc',
    current: true,
    ip: '203.0.113.7',
    userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/141.0.0.0 Safari/537.36',
    createdAt: '2026-10-23T08:00:00Z',
    lastSeenAt: '2026-10-23T08:00:00Z',
  },
  {
    id: 's-phone',
    current: false,
    ip: '198.51.100.4',
    userAgent: 'Mozilla/5.0 (Linux; Android 13; TC21) Chrome/141.0.0.0 Mobile Safari/537.36',
    createdAt: '2026-10-22T08:00:00Z',
    lastSeenAt: '2026-10-22T18:00:00Z',
  },
]

async function mountPage() {
  const queryClient = new QueryClient()
  queryClient.setQueryData(meQuery.queryKey, me)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: ProfilePage },
      { path: '/password', name: 'change-password', component: ProfilePage },
    ],
  })
  const wrapper = mount(ProfilePage, {
    global: { plugins: [createPikniVuetify(), router, [VueQueryPlugin, { queryClient }]] },
  })
  await flushPromises()
  return wrapper
}

describe('ProfilePage', () => {
  let confirmed: ReturnType<typeof autoConfirm>
  beforeEach(() => {
    confirmed = autoConfirm()
  })
  afterEach(() => confirmed.stop())

  beforeEach(() => {
    vi.mocked(api.GET)
      .mockReset()
      .mockResolvedValue({ data: { items: sessions }, response: new Response() } as never)
    vi.mocked(api.DELETE)
      .mockReset()
      .mockResolvedValue({ response: new Response(null, { status: 204 }) } as never)
  })

  it('показывает сеансы с устройством и отмечает текущий', async () => {
    const wrapper = await mountPage()

    const pc = wrapper.find('[data-test="session-s-pc"]')
    expect(pc.text()).toContain('Chrome · Windows')
    expect(pc.text()).toContain('Это устройство')
    expect(wrapper.find('[data-test="session-s-phone"]').text()).toContain('Chrome · Android')
  })

  it('завершает выбранный сеанс и все остальные', async () => {
    const wrapper = await mountPage()

    await wrapper.find('[data-test="session-s-phone"] button').trigger('click')
    await flushPromises()
    expect(api.DELETE).toHaveBeenCalledWith('/auth/sessions/{id}', {
      params: { path: { id: 's-phone' } },
    })

    await wrapper.find('[data-test="end-others"]').trigger('click')
    await flushPromises()
    expect(api.DELETE).toHaveBeenCalledWith('/auth/sessions')
  })
})
