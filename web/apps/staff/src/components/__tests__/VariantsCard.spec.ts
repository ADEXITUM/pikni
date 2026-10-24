import { api, type Variant } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

import VariantsCard from '../VariantsCard.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: {
    GET: vi.fn<ApiModule['api']['GET']>(),
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

const half: Variant = {
  id: 'v1',
  productId: 'p1',
  article: 'ЧК-000002',
  volumeMl: 500,
  container: 'glass',
  giftPack: false,
  alcocode: '0177207000002463655',
  isActive: true,
  barcodes: [{ code: '4006381333931', kind: 'ean13' }],
  retailPrice: 299,
  minPrice: 349,
  purchasePrice: 210,
}

async function mountCard(canEdit = true, showCost = false) {
  const wrapper = mount(VariantsCard, {
    props: { productId: 'p1', isEgais: true, canEdit, showCost },
    global: {
      plugins: [createPikniVuetify(), [VueQueryPlugin, { queryClient: new QueryClient() }]],
      stubs: { VDialog: DialogStub },
    },
  })
  await flushPromises()
  return wrapper
}

describe('VariantsCard', () => {
  beforeEach(() => {
    vi.mocked(api.GET).mockImplementation(((path: string) =>
      Promise.resolve({
        data:
          path === '/volumes'
            ? {
                items: [
                  { ml: 500, isActive: true },
                  { ml: 700, isActive: true },
                ],
              }
            : path.endsWith('-prices')
              ? { items: [] }
              : { items: [half] },
        response: new Response(),
      })) as never)
    vi.mocked(api.POST).mockResolvedValue({
      data: { items: [], code: '2000000000015', kind: 'internal' },
      response: new Response(),
    } as never)
    vi.mocked(api.DELETE).mockResolvedValue({ response: new Response() } as never)
  })

  it('показывает варианты со штрихкодами; без права — без кнопок', async () => {
    const readOnly = await mountCard(false)
    const row = readOnly.find('[data-test="variant-v1"]').text()
    expect(row).toContain('0,5 л, стекло')
    expect(row).toContain('0177207000002463655')
    expect(row).toContain('4006381333931')
    expect(readOnly.find('[data-test="variant-add"]').exists()).toBe(false)
    expect(readOnly.find('[data-test="barcode-4006381333931"] .v-chip__close').exists()).toBe(false)
  })

  it('создаёт варианты по списку объёмов и тары', async () => {
    const wrapper = await mountCard()

    await wrapper.find('[data-test="variants-generate"]').trigger('click')
    await flushPromises()
    const volumes = wrapper.findAll('[data-test="generate-volumes"] .v-chip')
    await volumes[0]!.trigger('click')
    await volumes[1]!.trigger('click')
    await wrapper.findAll('[data-test="generate-containers"] .v-chip')[1]!.trigger('click')
    await wrapper.find('[data-test="generate-save"]').trigger('click')
    await flushPromises()

    expect(api.POST).toHaveBeenCalledWith('/products/{id}/variants/generate', {
      params: { path: { id: 'p1' } },
      body: { volumes: [500, 700], containers: ['pet'], giftPack: false },
    })
  })

  it('внутренний штрихкод — без кода; крестик отвязывает штрихкод', async () => {
    const wrapper = await mountCard()

    await wrapper.find('[data-test="barcode-add"]').trigger('click')
    await wrapper.find('[data-test="barcode-internal"]').trigger('click')
    await flushPromises()
    expect(api.POST).toHaveBeenCalledWith('/variants/{id}/barcodes', {
      params: { path: { id: 'v1' } },
      body: {},
    })

    await wrapper.find('[data-test="barcode-4006381333931"] .v-chip__close').trigger('click')
    await flushPromises()
    expect(api.DELETE).toHaveBeenCalledWith('/barcodes/{code}', {
      params: { path: { code: '4006381333931' } },
    })
  })

  it('цена ниже МРЦ подсвечена, закупка — только с правом cost.view', async () => {
    const seller = await mountCard(false)
    const row = seller.find('[data-test="variant-v1"]')
    expect(row.find('[data-test="below-min"]').text()).toContain('ниже МРЦ')
    expect(row.text()).not.toContain('210')

    const manager = await mountCard(true, true)
    expect(manager.find('[data-test="variant-v1"]').text()).toContain('210')
  })

  it('назначает розничную цену из окна цен', async () => {
    const wrapper = await mountCard()

    await wrapper.find('[data-test="variant-price"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="min-price"]').text()).toContain('МРЦ сейчас')
    await wrapper.find('[data-test="price-value"] input').setValue('449,90')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.POST).toHaveBeenCalledWith('/variants/{id}/retail-prices', {
      params: { path: { id: 'v1' } },
      body: { price: 449.9, validFrom: undefined },
    })
  })
})
