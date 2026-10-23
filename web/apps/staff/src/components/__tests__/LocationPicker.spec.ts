import { api, type Location, type Me } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { makeLocation } from '@/__tests__/fixtures'
import { meQuery } from '@/session'
import { useCurrentLocation } from '@/stores/currentLocation'

import LocationPicker from '../LocationPicker.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { GET: vi.fn<ApiModule['api']['GET']>() },
}))

const locations: Location[] = [
  makeLocation({
    id: 'mg-1',
    code: 'МГ-01',
    name: 'Магазин на Ленина',
    kind: 'store',
    city: 'Благовещенск',
    timezone: 'Asia/Yakutsk',
  }),
  makeLocation({
    id: 'mg-2',
    code: 'МГ-02',
    name: 'Магазин на Мира',
    kind: 'store',
    city: 'Хабаровск',
    timezone: 'Asia/Vladivostok',
  }),
]

async function mountFor(locationIds: string[]) {
  const me: Me = {
    id: 'u1',
    login: 'u1',
    fullName: 'Сотрудник',
    role: 'store_manager',
    mustChangePassword: false,
    csrfToken: 'csrf',
    permissions: ['refs.view'],
    locationIds,
  }
  const queryClient = new QueryClient()
  queryClient.setQueryData(meQuery.queryKey, me)
  useCurrentLocation().restore(me)

  const wrapper = mount(LocationPicker, {
    global: { plugins: [createPikniVuetify(), [VueQueryPlugin, { queryClient }]] },
  })
  await flushPromises()
  return wrapper
}

describe('LocationPicker', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
    vi.mocked(api.GET).mockResolvedValue({
      data: { items: locations },
      response: new Response(),
    } as never)
  })

  it('при нескольких точках показывает выбор, по умолчанию все точки', async () => {
    const wrapper = await mountFor(['mg-1', 'mg-2'])
    const picker = wrapper.find('[data-test="location-picker"]')

    expect(picker.element.tagName).toBe('BUTTON')
    expect(picker.text()).toContain('Все точки')

    useCurrentLocation().select('mg-2')
    await flushPromises()
    expect(picker.text()).toContain('МГ-02 · Магазин на Мира')
  })

  it('единственную точку показывает без выбора', async () => {
    const wrapper = await mountFor(['mg-1'])
    const picker = wrapper.find('[data-test="location-picker"]')

    expect(picker.element.tagName).toBe('DIV')
    expect(picker.text()).toContain('МГ-01 · Магазин на Ленина')
  })

  it('предупреждает, если точка не назначена', async () => {
    const wrapper = await mountFor([])
    expect(wrapper.text()).toContain('Точка не назначена')
  })
})
