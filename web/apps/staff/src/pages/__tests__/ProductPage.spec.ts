import { api, type CategoryTree, type Permission, type Product } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { VAutocomplete } from 'vuetify/components'

import { autoConfirm } from '@/__tests__/confirm'

import { makeAttribute, makeCategory, makeMe } from '@/__tests__/fixtures'
import { meQuery } from '@/session'

import ProductPage from '../ProductPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: {
    GET: vi.fn<ApiModule['api']['GET']>(),
    POST: vi.fn<ApiModule['api']['POST']>(),
    PUT: vi.fn<ApiModule['api']['PUT']>(),
  },
}))

const tree: CategoryTree = {
  categories: [
    makeCategory({ id: 'strong', name: 'Крепкий алкоголь' }),
    makeCategory({ id: 'vodka', name: 'Водка', parentId: 'strong', depth: 2 }),
    makeCategory({ id: 'whisky', name: 'Виски', parentId: 'strong', depth: 2, sort: 20 }),
    makeCategory({ id: 'tobacco', name: 'Табачная продукция', alcohol: false, sort: 30 }),
  ],
  attributes: [
    makeAttribute({
      id: 'raw',
      categoryId: 'vodka',
      code: 'raw_material',
      name: 'Сырьё',
      type: 'enum',
      options: [
        { value: 'пшеница', archived: true },
        { value: 'рожь', archived: false },
      ],
    }),
    makeAttribute({
      id: 'aging',
      categoryId: 'whisky',
      code: 'aging',
      name: 'Выдержка',
      type: 'number',
      unit: 'лет',
    }),
  ],
}

const product: Product = {
  id: 'p1',
  name: 'Тайга',
  categoryId: 'vodka',
  categoryName: 'Водка',
  kindCode: '200',
  egaisAccounting: 'marked',
  abv: 40,
  crptMarked: false,
  producerId: 'taiga',
  producerName: 'ООО «Тайга»',
  countryCode: '643',
  countryName: 'Российская Федерация',
  attributes: { raw_material: 'пшеница' },
  isActive: true,
  version: 3,
}

const responses: Record<string, unknown> = {
  '/products/{id}': product,
  '/categories': tree,
  '/product-kinds': { items: [{ code: '200', name: 'Водка', egaisAccounting: 'marked' }] },
  '/producers': {
    items: [{ id: 'taiga', name: 'ООО «Тайга»', countryCode: '643', isActive: true }],
  },
  '/brands': { items: [] },
  '/product-lines': { items: [] },
  '/countries': { items: [{ code: '643', alpha2: 'RU', name: 'Российская Федерация' }] },
  '/origin-regions': { items: [] },
  '/products/{id}/variants': { items: [] },
  '/volumes': { items: [] },
}

async function mountAt(path: string, permissions: Permission[] = ['refs.view', 'refs.edit']) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(meQuery.queryKey, makeMe({ permissions }))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/products', name: 'products', component: { render: () => null } },
      { path: '/products/new', name: 'product-new', component: ProductPage },
      { path: '/products/:id', name: 'product', component: ProductPage },
    ],
  })
  await router.push(path)
  const wrapper = mount(ProductPage, {
    global: { plugins: [createPikniVuetify(), router, [VueQueryPlugin, { queryClient }]] },
  })
  await flushPromises()
  return { wrapper, router }
}

// Выбор в v-autocomplete — как будто пользователь выбрал пункт списка.
async function choose(wrapper: VueWrapper, field: string, value: string) {
  const input = wrapper
    .findAllComponents(VAutocomplete)
    .find((c) => c.attributes('data-test') === field)!
  input.vm.$emit('update:modelValue', value)
  await flushPromises()
}

describe('ProductPage', () => {
  let confirmed: ReturnType<typeof autoConfirm>
  beforeEach(() => {
    confirmed = autoConfirm()
  })
  afterEach(() => confirmed.stop())

  beforeEach(() => {
    vi.mocked(api.GET).mockImplementation(((path: string) =>
      Promise.resolve({ data: responses[path], response: new Response() })) as never)
    vi.mocked(api.POST).mockReset()
    vi.mocked(api.PUT).mockReset()
  })

  it('карточка: способ учёта из вида продукции, архивное значение у позиции сохраняется', async () => {
    const { wrapper } = await mountAt('/products/p1')

    expect(wrapper.find('h1').text()).toBe('Тайга')
    expect(wrapper.find('[data-test="accounting"]').text()).toBe('помарочный учёт ЕГАИС')
    expect(wrapper.find('[data-test="attr-raw_material"]').text()).toContain('пшеница (в архиве)')
  })

  it('смена категории убирает чужие характеристики и предупреждает; числа уходят числами', async () => {
    vi.mocked(api.PUT).mockResolvedValue({ data: product, response: new Response() } as never)
    const { wrapper } = await mountAt('/products/p1')

    await choose(wrapper, 'category', 'whisky')
    expect(wrapper.find('[data-test="dropped"]').text()).toContain('Сырьё')
    await wrapper.find('[data-test="attr-aging"] input').setValue('5,5')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.PUT).toHaveBeenCalledWith('/products/{id}', {
      params: { path: { id: 'p1' } },
      body: expect.objectContaining({
        categoryId: 'whisky',
        attributes: { aging: 5.5 },
        abv: 40,
        version: 3,
      }),
    })
  })

  it('в неалкогольной категории нет вида продукции и крепости, и они не уходят на сервер', async () => {
    vi.mocked(api.PUT).mockResolvedValue({ data: product, response: new Response() } as never)
    const { wrapper } = await mountAt('/products/p1')
    expect(wrapper.find('[data-test="abv"]').exists()).toBe(true)

    await choose(wrapper, 'category', 'tobacco')
    expect(wrapper.find('[data-test="abv"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="kind"]').exists()).toBe(false)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(api.PUT).toHaveBeenCalledWith('/products/{id}', {
      params: { path: { id: 'p1' } },
      body: expect.objectContaining({ categoryId: 'tobacco', kindCode: undefined, abv: undefined }),
    })
  })

  it('если позицию уже изменили, предлагает загрузить свежую версию', async () => {
    vi.mocked(api.PUT).mockResolvedValue({
      error: {
        type: 'about:blank',
        title: 'Conflict',
        status: 409,
        detail: 'Позицию уже изменил другой пользователь',
      },
      response: new Response(),
    } as never)
    const { wrapper } = await mountAt('/products/p1')

    await wrapper.find('[data-test="name"] input').setValue('Тайга Премиум')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(confirmed.asked[0]?.changes).toEqual([
      { label: 'Наименование', old: 'Тайга', new: 'Тайга Премиум' },
    ])
    const alert = wrapper.find('[data-test="save-error"]').text()
    expect(alert).toContain('Позицию уже изменил другой пользователь')
    expect(alert).toContain('Загрузить свежую версию')
  })

  it('новая позиция: для алкоголя без крепости не отправляет', async () => {
    const { wrapper } = await mountAt('/products/new')

    await wrapper.find('[data-test="name"] input').setValue('Тайга')
    await choose(wrapper, 'category', 'vodka')
    await choose(wrapper, 'producer', 'taiga')
    await choose(wrapper, 'kind', '200')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.POST).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Для алкогольной продукции укажите крепость')
    expect(wrapper.find('[data-test="attr-raw_material"]').exists()).toBe(true)
  })
})
