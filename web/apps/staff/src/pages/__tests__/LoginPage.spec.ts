import { api, type Me } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import mascotError from '@pikni/ui/assets/mascot/error.svg'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { meQuery } from '@/session'

import LoginPage from '../LoginPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { POST: vi.fn<ApiModule['api']['POST']>() },
}))

const me: Me = {
  id: '01a16948-042f-7e7d-bec4-42c1e6861f9d',
  login: 'admin',
  fullName: 'Администратор',
  role: 'admin',
  mustChangePassword: false,
  csrfToken: 'csrf-1',
  permissions: ['users.manage'],
  locationIds: [],
}

const Stub = { render: () => null }

async function setup(redirect?: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Stub },
      { path: '/login', name: 'login', component: LoginPage },
      { path: '/change-password', name: 'change-password', component: Stub },
      { path: '/audit', component: Stub },
    ],
  })
  await router.push({ path: '/login', query: redirect ? { redirect } : {} })

  const queryClient = new QueryClient()
  const wrapper = mount(LoginPage, {
    global: { plugins: [createPikniVuetify(), router, [VueQueryPlugin, { queryClient }]] },
  })
  return { wrapper, router, queryClient }
}

async function submit(wrapper: VueWrapper, login: string, password: string) {
  await wrapper.find('input[name="username"]').setValue(login)
  await wrapper.find('input[name="password"]').setValue(password)
  await wrapper.find('form').trigger('submit')
  await flushPromises()
}

describe('LoginPage', () => {
  afterEach(() => {
    vi.mocked(api.POST).mockReset()
  })

  it('после входа возвращает на страницу, которую открывали', async () => {
    vi.mocked(api.POST).mockResolvedValue({ data: me, response: new Response() } as never)
    const { wrapper, router, queryClient } = await setup('/audit')

    await submit(wrapper, 'admin', 'верный-пароль')

    expect(api.POST).toHaveBeenCalledWith('/auth/login', {
      body: { login: 'admin', password: 'верный-пароль' },
    })
    expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(me)
    expect(router.currentRoute.value.fullPath).toBe('/audit')
  })

  it('с временным паролем ведёт на смену пароля', async () => {
    vi.mocked(api.POST).mockResolvedValue({
      data: { ...me, mustChangePassword: true },
      response: new Response(),
    } as never)
    const { wrapper, router } = await setup('/audit')

    await submit(wrapper, 'admin', 'временный')

    expect(router.currentRoute.value.name).toBe('change-password')
    expect(router.currentRoute.value.query.redirect).toBe('/audit')
  })

  it('показывает ошибку сервера и грустного маскота', async () => {
    vi.mocked(api.POST).mockResolvedValue({
      error: {
        type: 'about:blank',
        title: 'Требуется вход',
        status: 401,
        detail: 'Неверный логин или пароль',
      },
      response: new Response(null, { status: 401 }),
    } as never)
    const { wrapper, router } = await setup()

    await submit(wrapper, 'admin', 'неверный')

    expect(wrapper.find('[data-test="error"]').text()).toBe('Неверный логин или пароль')
    expect(wrapper.find('[data-test="mascot"]').attributes('src')).toBe(mascotError)
    expect(router.currentRoute.value.name).toBe('login')
  })
})
