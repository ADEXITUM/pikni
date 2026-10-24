import { api, type CodeResolution } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import ScanPage from '../ScanPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { GET: vi.fn<ApiModule['api']['GET']>() },
}))

const found: CodeResolution = {
  scan: { kind: 'barcode', code: '2900000000025' },
  product: {
    id: 'p1',
    article: 'ЧК-000001',
    name: 'Водка «Тайга» классическая',
    categoryId: 'vodka',
    categoryName: 'Водка',
    crptMarked: false,
    producerId: 'taiga',
    producerName: 'ООО «Тайга»',
    countryCode: '643',
    countryName: 'Российская Федерация',
    attributes: {},
    isActive: true,
    version: 1,
  },
  variant: {
    id: 'v1',
    productId: 'p1',
    volumeMl: 500,
    container: 'glass',
    giftPack: false,
    isActive: true,
    barcodes: [],
    retailPrice: 459,
    minPrice: 409,
  },
}

async function mountPage() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/scan', component: ScanPage },
      { path: '/products/:id', name: 'product', component: { render: () => null } },
    ],
  })
  const wrapper = mount(ScanPage, {
    global: {
      plugins: [createPikniVuetify(), router, [VueQueryPlugin, { queryClient: new QueryClient() }]],
    },
    attachTo: document.body,
  })
  await flushPromises()
  return wrapper
}

const scanWithWedge = (code: string) => {
  for (const key of [...code, 'Enter']) window.dispatchEvent(new KeyboardEvent('keydown', { key }))
}

describe('ScanPage', () => {
  beforeEach(() => {
    vi.mocked(api.GET).mockClear()
    vi.mocked(api.GET).mockImplementation(((
      _: string,
      init: { params: { query: { code: string } } },
    ) =>
      Promise.resolve({
        data:
          init.params.query.code === '2900000000025'
            ? found
            : { scan: { kind: 'barcode', code: init.params.query.code } },
        response: new Response(),
      })) as never)
  })

  it('скан ручным сканером без фокуса показывает позицию, вариант и цену', async () => {
    const wrapper = await mountPage()

    scanWithWedge('2900000000025')
    await flushPromises()

    expect(api.GET).toHaveBeenCalledWith('/codes/resolve', {
      params: { query: { code: '2900000000025' } },
    })
    const result = wrapper.find('[data-test="result"]').text()
    expect(result).toContain('Водка «Тайга» классическая')
    expect(result).toContain('0,5 л, стекло')
    expect(result).toContain('МРЦ')
    wrapper.unmount()
  })

  it('незнакомый код, марка нового образца и короб — понятные ответы', async () => {
    const wrapper = await mountPage()
    const manual = async (code: string) => {
      await wrapper.find('[data-test="manual"] input').setValue(code)
      await wrapper.find('form').trigger('submit')
      await flushPromises()
    }

    await manual('4006381333931')
    expect(wrapper.find('[data-test="missing"]').exists()).toBe(true)

    await manual('10110026167968' + '0118001' + 'A'.repeat(129))
    expect(wrapper.find('[data-test="local"]').text()).toContain('нет алкокода')

    await manual('00046000000000000015')
    expect(wrapper.find('[data-test="local"]').text()).toContain('SSCC')
    expect(api.GET).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-test="recent"]').findAll('.v-list-item')).toHaveLength(3)
    wrapper.unmount()
  })
})
