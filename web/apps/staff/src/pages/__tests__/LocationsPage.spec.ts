import { api, type Location } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { makeLocation, makeMe } from '@/__tests__/fixtures'
import type { Permission } from '@pikni/api'
import { meQuery } from '@/session'

import LocationsPage from '../LocationsPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { GET: vi.fn<ApiModule['api']['GET']>() },
}))

const locations: Location[] = [
  makeLocation({
    id: '01a165f5-5fe6-75dd-8c1b-658bffe94376',
    code: 'МГ-01',
    name: 'Магазин на ул. Ленина',
    kind: 'store',
    city: 'Благовещенск',
    timezone: 'Asia/Yakutsk',
    openingHours: {
      mon: { open: '09:00', close: '23:00' },
      tue: { open: '09:00', close: '23:00' },
    },
  }),
  makeLocation({
    id: '01a165f5-5fe5-7a10-9b2e-1f0c3d4e5f60',
    code: 'СК-01',
    name: 'Центральный склад',
    kind: 'warehouse',
    city: 'Благовещенск',
    timezone: 'Asia/Yakutsk',
  }),
]

const Stub = { render: () => null }

async function mountPage(permissions: Permission[] = ['refs.view']) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(meQuery.queryKey, makeMe({ permissions }))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/locations', name: 'locations', component: LocationsPage },
      { path: '/locations/new', name: 'location-new', component: Stub },
      { path: '/locations/:id', name: 'location', component: Stub },
    ],
  })
  await router.push('/locations')
  const wrapper = mount(LocationsPage, {
    global: { plugins: [createPikniVuetify(), router, [VueQueryPlugin, { queryClient }]] },
  })
  return { wrapper, router }
}

describe('LocationsPage', () => {
  afterEach(() => {
    vi.mocked(api.GET).mockReset()
  })

  it('показывает точки из API', async () => {
    vi.mocked(api.GET).mockResolvedValue({
      data: { items: locations },
      error: undefined,
      response: new Response(),
    } as never)

    const { wrapper } = await mountPage()
    await flushPromises()

    expect(wrapper.text()).toContain('Магазин на ул. Ленина')
    expect(wrapper.text()).toContain('Склад')
    expect(wrapper.text()).toContain('пн–вт 09:00–23:00, ср–вс выходной')
    expect(wrapper.find('[data-test="add-location"]').exists()).toBe(false)
  })

  it('с правом refs.edit показывает «Добавить точку», по строке открывает карточку', async () => {
    vi.mocked(api.GET).mockResolvedValue({
      data: { items: locations },
      response: new Response(),
    } as never)

    const { wrapper, router } = await mountPage(['refs.view', 'refs.edit'])
    await flushPromises()

    expect(wrapper.find('[data-test="add-location"]').exists()).toBe(true)
    await wrapper.findAll('tbody tr')[0]!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe(`/locations/${locations[0]!.id}`)
  })

  it('показывает ошибку из ответа API', async () => {
    vi.mocked(api.GET).mockResolvedValue({
      data: undefined,
      error: { type: 'about:blank', title: 'Сервис временно недоступен', status: 503 },
      response: new Response(null, { status: 503 }),
    } as never)

    const { wrapper } = await mountPage()
    await flushPromises()

    expect(wrapper.text()).toContain('Не удалось загрузить точки')
    expect(wrapper.text()).toContain('Сервис временно недоступен')
  })
})
