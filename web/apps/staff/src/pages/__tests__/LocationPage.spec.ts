import { api, type Permission } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { makeLocation, makeMe } from '@/__tests__/fixtures'
import { meQuery } from '@/session'

import LocationPage from '../LocationPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: {
    GET: vi.fn<ApiModule['api']['GET']>(),
    POST: vi.fn<ApiModule['api']['POST']>(),
    PUT: vi.fn<ApiModule['api']['PUT']>(),
  },
}))

const store = makeLocation({
  id: 'mg-1',
  code: 'МГ-01',
  name: 'Магазин на ул. Ленина',
  zones: [
    { id: 'z1', kind: 'backroom', name: 'Склад магазина' },
    { id: 'z2', kind: 'floor', name: 'Торговый зал' },
  ],
})

async function mountAt(path: string, permissions: Permission[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(meQuery.queryKey, makeMe({ permissions }))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/locations', name: 'locations', component: { render: () => null } },
      { path: '/locations/new', name: 'location-new', component: LocationPage },
      { path: '/locations/:id', name: 'location', component: LocationPage },
    ],
  })
  await router.push(path)
  const wrapper = mount(LocationPage, {
    global: { plugins: [createPikniVuetify(), router, [VueQueryPlugin, { queryClient }]] },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('LocationPage', () => {
  beforeEach(() => {
    vi.mocked(api.GET).mockImplementation(((path: string) =>
      Promise.resolve({
        data:
          path === '/ru-subjects'
            ? { items: [{ code: '28', name: 'Амурская область' }] }
            : path === '/locations/{id}/sale-check'
              ? {
                  localTime: '2026-10-23T20:30:00+09:00',
                  strong: { allowed: true, from: '11:00', to: '21:00' },
                  other: { allowed: false, banDay: 'День знаний' },
                }
              : store,
        response: new Response(),
      })) as never)
    vi.mocked(api.POST).mockReset()
  })

  it('без права refs.edit показывает карточку только для чтения', async () => {
    const { wrapper } = await mountAt('/locations/mg-1', ['refs.view'])

    expect(wrapper.text()).toContain('МГ-01 · Магазин на ул. Ленина')
    expect(wrapper.text()).toContain('Торговый зал')
    const sale = wrapper.find('[data-test="sale-now"]').text()
    expect(sale).toContain('местное время 20:30')
    expect(sale).toContain('можно до 21:00')
    expect(sale).toContain('нельзя весь день: День знаний')
    expect(wrapper.find('[data-test="name"] input').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="save"]').exists()).toBe(false)
  })

  it('создаёт точку: пустые поля не отправляет, координаты переводит в числа', async () => {
    vi.mocked(api.POST).mockResolvedValue({
      data: { ...store, id: 'new-id' },
      response: new Response(),
    } as never)
    const { wrapper, router } = await mountAt('/locations/new', ['refs.view', 'refs.edit'])

    await wrapper.find('[data-test="code"] input').setValue('МГ-05')
    await wrapper.find('[data-test="name"] input').setValue('Новый магазин')
    await wrapper.find('[data-test="city"] input').setValue('Благовещенск')
    await wrapper.find('[data-test="lat"] input').setValue('50,25')
    await wrapper.find('[data-test="lon"] input').setValue('127.5')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.POST).toHaveBeenCalledWith('/locations', {
      body: expect.objectContaining({
        code: 'МГ-05',
        kind: 'store',
        name: 'Новый магазин',
        lat: 50.25,
        lon: 127.5,
        address: undefined,
        fsrarId: undefined,
        subjectCode: '28',
      }),
    })
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/locations/new-id'))
  })
})
