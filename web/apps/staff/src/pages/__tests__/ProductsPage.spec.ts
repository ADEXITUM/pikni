import { api, type Product } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { makeMe } from '@/__tests__/fixtures'
import { meQuery } from '@/session'

import ProductsPage from '../ProductsPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { GET: vi.fn<ApiModule['api']['GET']>() },
}))

const taiga: Product = {
  id: 'p1',
  article: 'ЧК-000001',
  name: 'Тайга',
  categoryId: 'vodka',
  categoryName: 'Водка',
  kindCode: '200',
  egaisAccounting: 'marked',
  abv: 40,
  crptMarked: false,
  producerId: 'taiga',
  producerName: 'ООО «Тайга»',
  brandName: 'Тайга',
  countryCode: '643',
  countryName: 'Российская Федерация',
  attributes: {},
  isActive: true,
  version: 1,
}

async function mountPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(meQuery.queryKey, makeMe({ permissions: ['refs.view'] }))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/products', name: 'products', component: ProductsPage },
      { path: '/products/new', name: 'product-new', component: { render: () => null } },
      { path: '/products/:id', name: 'product', component: { render: () => null } },
    ],
  })
  await router.push('/products')
  const wrapper = mount(ProductsPage, {
    global: { plugins: [createPikniVuetify(), router, [VueQueryPlugin, { queryClient }]] },
  })
  await flushPromises()
  return { wrapper, router }
}

describe('ProductsPage', () => {
  beforeEach(() => {
    vi.mocked(api.GET).mockImplementation(((path: string) =>
      Promise.resolve({
        data:
          path === '/products'
            ? { items: [taiga], nextAfter: 'p1' }
            : path === '/categories'
              ? { categories: [], attributes: [] }
              : { items: [] },
        response: new Response(),
      })) as never)
  })

  it('список с подгрузкой, поиск уходит на сервер после паузы, строка открывает карточку', async () => {
    const { wrapper, router } = await mountPage()

    const row = wrapper.find('tbody tr')
    expect(row.text()).toContain('ЧК-000001')
    expect(row.text()).toContain('40 %')
    expect(row.text()).toContain('помарочный учёт ЕГАИС')
    expect(wrapper.text()).toContain('Показать ещё')
    expect(wrapper.find('[data-test="product-add"]').exists()).toBe(false)

    await wrapper.find('[data-test="search"] input').setValue('тай')
    await vi.waitFor(() =>
      expect(api.GET).toHaveBeenCalledWith('/products', {
        params: { query: expect.objectContaining({ q: 'тай' }) },
      }),
    )

    await wrapper.find('tbody tr').trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe('/products/p1'))
  })

  it('скан со сканера ищется сразу по Enter и показывает, как понят код', async () => {
    const mark = '20N00001CGUMZYCB99J1NKN31105001000056NQQMS5VP4HTF5SB46ZSQQJD8BNJP891'
    vi.mocked(api.GET).mockImplementation(((
      path: string,
      init?: { params?: { query?: { q?: string } } },
    ) =>
      Promise.resolve({
        data:
          path === '/products'
            ? init?.params?.query?.q === mark
              ? { items: [taiga], scan: { kind: 'egais_mark', code: '0177207000002463655' } }
              : { items: [] }
            : { items: [], categories: [], attributes: [] },
        response: new Response(),
      })) as never)
    const { wrapper } = await mountPage()

    const input = wrapper.find('[data-test="search"] input')
    await input.setValue(mark)
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()

    expect(wrapper.find('[data-test="scan"]').text()).toBe(
      'Поиск по коду: марка ЕГАИС, алкокод 0177207000002463655',
    )
    expect(wrapper.find('tbody tr').text()).toContain('ЧК-000001')
  })
})
