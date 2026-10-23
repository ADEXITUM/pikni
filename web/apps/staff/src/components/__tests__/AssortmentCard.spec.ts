import { api, type AssortmentItem } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { VSelect } from 'vuetify/components'

import { makeLocation } from '@/__tests__/fixtures'

import AssortmentCard from '../AssortmentCard.vue'

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

async function mountCard(canEdit = true) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/products/:id', name: 'product', component: { render: () => null } }],
  })
  const wrapper = mount(AssortmentCard, {
    props: { locationId: 'mg-1', canEdit },
    global: {
      plugins: [createPikniVuetify(), router, [VueQueryPlugin, { queryClient: new QueryClient() }]],
      stubs: { VDialog: DialogStub },
    },
  })
  await flushPromises()
  return wrapper
}

describe('AssortmentCard', () => {
  beforeEach(() => {
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
            : { items: [half] },
        response: new Response(),
      })) as never)
    const ok = Promise.resolve({ data: { added: 5 }, response: new Response() })
    vi.mocked(api.PUT).mockReturnValue(ok as never)
    vi.mocked(api.POST).mockReturnValue(ok as never)
    vi.mocked(api.DELETE).mockReturnValue(ok as never)
  })

  it('без права — только просмотр', async () => {
    const wrapper = await mountCard(false)
    const row = wrapper.find('[data-test="assortment-v1"]').text()
    expect(row).toContain('ЧК-000001')
    expect(row).toContain('0,5 л, стекло')
    expect(row).toContain('12')
    expect(wrapper.find('[data-test="assortment-add"]').exists()).toBe(false)
  })

  it('минимум меняется в таблице, строка удаляется, матрица копируется', async () => {
    const wrapper = await mountCard()

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
