import { api, type Location } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import LocationsPage from '../LocationsPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { GET: vi.fn<ApiModule['api']['GET']>() },
}))

const locations: Location[] = [
  {
    id: '01a165f5-5fe6-75dd-8c1b-658bffe94376',
    code: 'МГ-01',
    name: 'Магазин на ул. Ленина',
    kind: 'store',
    city: 'Благовещенск',
    timezone: 'Asia/Yakutsk',
  },
  {
    id: '01a165f5-5fe5-7a10-9b2e-1f0c3d4e5f60',
    code: 'СК-01',
    name: 'Центральный склад',
    kind: 'warehouse',
    city: 'Благовещенск',
    timezone: 'Asia/Yakutsk',
  },
]

function mountPage() {
  return mount(LocationsPage, {
    global: {
      plugins: [
        createPikniVuetify(),
        [VueQueryPlugin, { queryClientConfig: { defaultOptions: { queries: { retry: false } } } }],
      ],
    },
  })
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

    const wrapper = mountPage()
    await flushPromises()

    expect(wrapper.text()).toContain('Магазин на ул. Ленина')
    expect(wrapper.text()).toContain('Магазин')
    expect(wrapper.text()).toContain('Склад')
  })

  it('показывает ошибку из ответа API', async () => {
    vi.mocked(api.GET).mockResolvedValue({
      data: undefined,
      error: { type: 'about:blank', title: 'Сервис временно недоступен', status: 503 },
      response: new Response(null, { status: 503 }),
    } as never)

    const wrapper = mountPage()
    await flushPromises()

    expect(wrapper.text()).toContain('Не удалось загрузить точки')
    expect(wrapper.text()).toContain('Сервис временно недоступен')
  })
})
