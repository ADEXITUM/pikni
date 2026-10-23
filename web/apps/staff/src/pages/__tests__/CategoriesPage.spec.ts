import { api, type CategoryTree, type Permission } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

import { makeAttribute, makeCategory, makeMe } from '@/__tests__/fixtures'
import { meQuery } from '@/session'

import CategoriesPage from '../CategoriesPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { GET: vi.fn<ApiModule['api']['GET']>() },
}))

const DialogStub = defineComponent({
  props: { modelValue: Boolean },
  setup:
    (props, { slots }) =>
    () =>
      props.modelValue ? slots.default?.() : null,
})

const tree: CategoryTree = {
  categories: [
    makeCategory({ id: 'wine', name: 'Вино' }),
    makeCategory({ id: 'beer', name: 'Пиво и сидр', sort: 20 }),
    makeCategory({ id: 'sparkling', name: 'Игристое', parentId: 'wine', depth: 2 }),
    makeCategory({ id: 'still', name: 'Тихое', parentId: 'wine', depth: 2, sort: 20 }),
    makeCategory({ id: 'brut', name: 'Брют', parentId: 'sparkling', depth: 3 }),
  ],
  attributes: [
    makeAttribute({
      id: 'color',
      categoryId: 'wine',
      code: 'color',
      name: 'Цвет',
      type: 'enum',
      filterable: true,
      options: [
        { value: 'красное', archived: false },
        { value: 'оранжевое', archived: true },
      ],
    }),
    makeAttribute({ id: 'method', categoryId: 'sparkling', code: 'method', name: 'Метод' }),
  ],
}

async function mountPage(permissions: Permission[]) {
  const queryClient = new QueryClient()
  queryClient.setQueryData(meQuery.queryKey, makeMe({ permissions }))
  const wrapper = mount(CategoriesPage, {
    global: {
      plugins: [createPikniVuetify(), [VueQueryPlugin, { queryClient }]],
      stubs: { VDialog: DialogStub },
    },
  })
  await flushPromises()
  return wrapper
}

describe('CategoriesPage', () => {
  beforeEach(() => {
    vi.mocked(api.GET).mockResolvedValue({ data: tree, response: new Response() } as never)
  })

  it('дерево по порядку, у подкатегории — унаследованные характеристики с источником', async () => {
    const wrapper = await mountPage(['refs.view'])

    expect(wrapper.findAll('.v-list-item').map((i) => i.text())).toEqual([
      'Вино',
      'Игристое',
      'Брют',
      'Тихое',
      'Пиво и сидр',
    ])
    const card = () => wrapper.find('[data-test="category-card"]')
    expect(card().text()).toContain('красное')
    expect(card().text()).not.toContain('оранжевое')

    await wrapper.find('[data-test="category-Игристое"]').trigger('click')
    expect(card().text()).toContain('Вино')
    expect(wrapper.find('[data-test="attr-color"]').text()).toContain('«Вино»')
    expect(wrapper.find('[data-test="attr-method"]').text()).not.toContain('«')
    expect(wrapper.find('[data-test="attr-add"]').exists()).toBe(false)

    await wrapper.find('[data-test="attr-color"] .pk-link').trigger('click')
    expect(card().find('.v-card-title').text()).toBe('Вино')
  })

  it('правят только свои характеристики, четвёртый уровень не предлагается', async () => {
    const wrapper = await mountPage(['refs.view', 'refs.edit'])

    await wrapper.find('[data-test="category-Брют"]').trigger('click')
    expect(wrapper.find('[data-test="category-add-child"]').exists()).toBe(false)

    await wrapper.find('[data-test="category-Игристое"]').trigger('click')
    expect(wrapper.find('[data-test="category-add-child"]').exists()).toBe(true)

    await wrapper.find('[data-test="attr-color"]').trigger('click')
    expect(wrapper.text()).not.toContain('Изменить характеристику')

    await wrapper.find('[data-test="attr-method"]').trigger('click')
    expect(wrapper.text()).toContain('Изменить характеристику')
  })
})
