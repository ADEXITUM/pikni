import { api, type AssortmentItem, type Permission } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { VSelect } from 'vuetify/components'

import { makeLocation, makeMe } from '@/__tests__/fixtures'
import { meQuery } from '@/session'

import AssortmentPage from '../AssortmentPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: {
    GET: vi.fn<ApiModule['api']['GET']>(),
    PUT: vi.fn<ApiModule['api']['PUT']>(),
    POST: vi.fn<ApiModule['api']['POST']>(),
    DELETE: vi.fn<ApiModule['api']['DELETE']>(),
  },
}))

const DialogStub = defineComponent({
  props: { modelValue: Boolean },
  setup:
    (props, { slots }) =>
    () =>
      props.modelValue ? slots.default?.() : null,
})

const half: AssortmentItem = {
  variantId: 'v1',
  productId: 'p1',
  article: 'ЧК-000001',
  name: 'Водка «Тайга»',
  volumeMl: 500,
  container: 'glass',
  giftPack: false,
  isActive: true,
  minQty: 12,
}

async function mountPage(permissions: Permission[] = ['refs.view', 'refs.edit']) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/locations/:id/assortment', component: AssortmentPage },
      { path: '/locations/:id', name: 'location', component: { render: () => null } },
      { path: '/products/:id', name: 'product', component: { render: () => null } },
    ],
  })
  await router.push('/locations/mg-1/assortment')
  const queryClient = new QueryClient()
  queryClient.setQueryData(meQuery.queryKey, makeMe({ permissions }))
  const wrapper = mount(AssortmentPage, {
    global: {
      plugins: [createPikniVuetify(), router, [VueQueryPlugin, { queryClient }]],
      stubs: { VDialog: DialogStub },
    },
  })
  await flushPromises()
  return wrapper
}

describe('AssortmentPage', () => {
  beforeEach(() => {
    vi.mocked(api.GET).mockReset()
    vi.mocked(api.GET).mockImplementation(((path: string) =>
      Promise.resolve({
        data:
          path === '/locations'
            ? {
                items: [
                  makeLocation({ id: 'mg-1', code: 'МГ-01', name: 'Ленина' }),
                  makeLocation({ id: 'mg-2', code: 'МГ-02', name: '50 лет Октября' }),
                ],
              }
            : { items: [half], total: 1 },
        response: new Response(),
      })) as never)
    const ok = Promise.resolve({ data: { added: 5 }, response: new Response() })
    vi.mocked(api.PUT).mockReturnValue(ok as never)
    vi.mocked(api.POST).mockReturnValue(ok as never)
    vi.mocked(api.DELETE).mockReturnValue(ok as never)
  })

  it('без права — только просмотр; видно точку и число вариантов', async () => {
    const wrapper = await mountPage(['refs.view'])
    const row = wrapper.find('[data-test="assortment-v1"]').text()
    expect(row).toContain('ЧК-000001')
    expect(row).toContain('0,5 л, стекло')
    expect(row).toContain('12')
    expect(wrapper.text()).toContain('МГ-01 · Ленина')
    expect(wrapper.find('[data-test="assortment-total"]').text()).toBe('Вариантов: 1')
    expect(wrapper.find('[data-test="assortment-add"]').exists()).toBe(false)
  })

  it('поиск уходит на сервер, по Enter — сразу', async () => {
    const wrapper = await mountPage()
    const input = wrapper.find('[data-test="assortment-search"] input')
    await input.setValue('тайга')
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(api.GET).toHaveBeenCalledWith('/locations/{id}/assortment', {
      params: { path: { id: 'mg-1' }, query: { q: 'тайга', after: undefined } },
    })
    expect(wrapper.find('[data-test="assortment-total"]').text()).toBe('Найдено: 1')
  })

  it('минимум меняется в таблице, строка удаляется, матрица копируется', async () => {
    const wrapper = await mountPage()

    const qty = wrapper.find('[data-test="assortment-min"] input')
    await qty.setValue('24')
    await qty.trigger('change')
    await flushPromises()
    expect(api.PUT).toHaveBeenCalledWith('/locations/{id}/assortment/{variantId}', {
      params: { path: { id: 'mg-1', variantId: 'v1' } },
      body: { minQty: 24 },
    })

    await wrapper.find('[data-test="assortment-remove"]').trigger('click')
    await flushPromises()
    expect(api.DELETE).toHaveBeenCalledWith('/locations/{id}/assortment/{variantId}', {
      params: { path: { id: 'mg-1', variantId: 'v1' } },
    })

    await wrapper.find('[data-test="assortment-copy"]').trigger('click')
    const select = wrapper
      .findAllComponents(VSelect)
      .find((c) => c.attributes('data-test') === 'assortment-copy-from')!
    expect(select.props('items')).toEqual([{ value: 'mg-2', title: 'МГ-02 · 50 лет Октября' }])
    select.vm.$emit('update:modelValue', 'mg-2')
    await flushPromises()
    await wrapper.find('[data-test="assortment-copy-run"]').trigger('click')
    await flushPromises()
    expect(api.POST).toHaveBeenCalledWith('/locations/{id}/assortment/copy', {
      params: { path: { id: 'mg-1' } },
      body: { fromLocationId: 'mg-2' },
    })
    expect(wrapper.text()).toContain('Добавлено вариантов: 5')
  })
})
