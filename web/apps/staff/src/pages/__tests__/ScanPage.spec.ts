import { api, type CodeResolution, type Permission } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { makeMe } from '@/__tests__/fixtures'
import { playFeedback } from '@/feedback'
import { meQuery } from '@/session'

import ScanPage from '../ScanPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@/feedback', () => ({ playFeedback: vi.fn<(event: string) => void>() }))

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { GET: vi.fn<ApiModule['api']['GET']>() },
}))

const found: CodeResolution = {
  scan: { kind: 'barcode', code: '2900000000025' },
  product: {
    id: 'p1',
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
    article: 'ЧК-000002',
    volumeMl: 500,
    container: 'glass',
    giftPack: false,
    isActive: true,
    barcodes: [],
    retailPrice: 459,
    minPrice: 409,
  },
}

const CameraStub = {
  name: 'ScanCamera',
  template: '<div data-test="camera" />',
  emits: ['scan', 'close'],
}

async function mountPage(permissions: Permission[] = ['refs.view', 'refs.edit'], path = '/scan') {
  const queryClient = new QueryClient()
  queryClient.setQueryData(meQuery.queryKey, makeMe({ permissions }))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/scan', name: 'scan', component: ScanPage },
      { path: '/products/:id', name: 'product', component: { render: () => null } },
    ],
  })
  await router.push(path)
  const wrapper = mount(ScanPage, {
    global: {
      plugins: [createPikniVuetify(), router, [VueQueryPlugin, { queryClient }]],
      stubs: { ScanCamera: CameraStub },
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
    localStorage.clear()
    vi.mocked(playFeedback).mockClear()
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

  it('камера включается кнопкой и запоминается; повтор кода из кадра гасится', async () => {
    const wrapper = await mountPage()
    expect(wrapper.find('[data-test="camera"]').exists()).toBe(false)

    await wrapper.find('[data-test="camera-on"]').trigger('click')
    expect(localStorage.getItem('pikni.scan.camera')).toBe('1')
    const camera = wrapper.findComponent(CameraStub)
    camera.vm.$emit('scan', { raw: '2900000000025', format: 'ean_13' })
    camera.vm.$emit('scan', { raw: '2900000000025', format: 'ean_13' })
    await flushPromises()

    expect(api.GET).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-test="result"]').text()).toContain('камера')

    camera.vm.$emit('close')
    await flushPromises()
    expect(wrapper.find('[data-test="camera"]').exists()).toBe(false)
    expect(localStorage.getItem('pikni.scan.camera')).toBe('0')
    wrapper.unmount()
  })

  it('у каждого исхода свой сигнал и слово; отменённый скан можно сразу повторить', async () => {
    const wrapper = await mountPage()
    await wrapper.find('[data-test="camera-on"]').trigger('click')
    const camera = wrapper.findComponent(CameraStub)
    const scan = async (raw: string) => {
      camera.vm.$emit('scan', { raw, format: 'ean_13' })
      await flushPromises()
    }
    const outcome = () => wrapper.find('[data-test="outcome"]').text()

    await scan('2900000000025')
    expect(playFeedback).toHaveBeenLastCalledWith('accepted')
    expect(outcome()).toContain('Найден')

    await scan('4006381333931')
    expect(playFeedback).toHaveBeenLastCalledWith('unknown')
    expect(outcome()).toContain('Нет в каталоге')

    await scan('00046000000000000015')
    expect(playFeedback).toHaveBeenLastCalledWith('notInPlan')

    await wrapper.find('[data-test="undo"]').trigger('click')
    expect(playFeedback).toHaveBeenLastCalledWith('undone')
    expect(wrapper.find('[data-test="recent"]').findAll('.v-list-item')).toHaveLength(2)

    await scan('00046000000000000015')
    expect(wrapper.find('[data-test="local"]').text()).toContain('SSCC')
    wrapper.unmount()
  })

  it('артикул ищется на сервере; код стоит первой строкой, его вид — под ним', async () => {
    const wrapper = await mountPage()
    await wrapper.find('[data-test="manual"] input').setValue('чк-1')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.GET).toHaveBeenCalledWith('/codes/resolve', { params: { query: { code: 'чк-1' } } })
    const card = wrapper.find('[data-test="result"]')
    expect(card.find('[data-test="raw"]').text()).toBe('чк-1')
    expect(card.find('[data-test="kind"]').text()).toContain('Артикул')
    expect(card.find('[data-test="missing"]').text()).toContain('ЧК-000001')
    wrapper.unmount()
  })

  it('продавцу — артикул варианта и данные товара без перехода в каталог; поле ввода над результатом', async () => {
    const wrapper = await mountPage(['refs.view'])
    scanWithWedge('2900000000025')
    await flushPromises()

    const html = wrapper.html()
    expect(html.indexOf('data-test="manual"')).toBeLessThan(html.indexOf('data-test="result"'))
    const found = wrapper.find('[data-test="found"]').text()
    expect(found).toContain('ЧК-000002')
    expect(found).toContain('ООО «Тайга», Российская Федерация')
    expect(wrapper.find('[data-test="edit-product"]').exists()).toBe(false)

    await wrapper.find('[data-test="manual"] input').setValue('4006381333931')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="missing"]').text()).toContain('Сообщите руководителю')
    wrapper.unmount()
  })

  it('код из адреса — скан на другом экране — разбирается сразу, адрес очищается', async () => {
    const wrapper = await mountPage(undefined, '/scan?code=2900000000025')
    expect(api.GET).toHaveBeenCalledWith('/codes/resolve', {
      params: { query: { code: '2900000000025' } },
    })
    expect(wrapper.find('[data-test="kind"]').text()).toContain('сканер')
    wrapper.unmount()
  })
})
