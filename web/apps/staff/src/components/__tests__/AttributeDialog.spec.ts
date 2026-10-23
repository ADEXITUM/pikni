import { api } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

import { makeAttribute } from '@/__tests__/fixtures'

import AttributeDialog from '../AttributeDialog.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { POST: vi.fn<ApiModule['api']['POST']>(), PUT: vi.fn<ApiModule['api']['PUT']>() },
}))

const DialogStub = defineComponent({
  props: { modelValue: Boolean },
  setup:
    (props, { slots }) =>
    () =>
      props.modelValue ? slots.default?.() : null,
})

async function openDialog(props: InstanceType<typeof AttributeDialog>['$props']) {
  const wrapper = mount(AttributeDialog, {
    props,
    global: {
      plugins: [createPikniVuetify(), [VueQueryPlugin, { queryClient: new QueryClient() }]],
      stubs: { VDialog: DialogStub },
    },
  })
  await wrapper.setProps({ modelValue: true })
  return wrapper
}

describe('AttributeDialog', () => {
  beforeEach(() => {
    const ok = Promise.resolve({ data: {}, response: new Response() })
    vi.mocked(api.POST).mockReturnValue(ok as never)
    vi.mocked(api.PUT).mockReturnValue(ok as never)
  })

  it('новая характеристика-список: повтор не добавляет, набранное значение не теряется', async () => {
    const wrapper = await openDialog({ modelValue: false, categoryId: 'vodka', nextSort: 30 })

    await wrapper.find('[data-test="attr-name"] input').setValue('Регион')
    await wrapper.find('[data-test="attr-code"] input').setValue('region')
    const option = wrapper.find('[data-test="attr-new-option"] input')
    await option.setValue('Сибирь')
    await option.trigger('keydown', { key: 'Enter' })
    await option.setValue('сибирь')
    await option.trigger('keydown', { key: 'Enter' })
    expect(wrapper.text()).toContain('Такое значение уже есть')

    await option.setValue('Алтай')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.POST).toHaveBeenCalledWith('/categories/{id}/attributes', {
      params: { path: { id: 'vodka' } },
      body: expect.objectContaining({
        name: 'Регион',
        code: 'region',
        type: 'enum',
        options: ['Сибирь', 'Алтай'],
        sort: 30,
      }),
    })
  })

  it('правка: код и тип закрыты, сохранённое значение только архивируется', async () => {
    const attribute = makeAttribute({
      id: 'a1',
      categoryId: 'vodka',
      code: 'region',
      name: 'Регион',
      type: 'enum',
      options: [
        { value: 'Сибирь', archived: false },
        { value: 'Алтай', archived: false },
      ],
    })
    const wrapper = await openDialog({
      modelValue: false,
      categoryId: 'vodka',
      attribute,
      nextSort: 0,
    })

    expect(wrapper.find('[data-test="attr-code"] input').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="option-Сибирь"] .v-chip__close').exists()).toBe(false)

    await wrapper.find('[data-test="option-Алтай"] [data-test="option-archive"]').trigger('click')
    const option = wrapper.find('[data-test="attr-new-option"] input')
    await option.setValue('Урал')
    await option.trigger('keydown', { key: 'Enter' })
    expect(wrapper.find('[data-test="option-Урал"] .v-chip__close').exists()).toBe(true)

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.PUT).toHaveBeenCalledWith('/category-attributes/{id}', {
      params: { path: { id: 'a1' } },
      body: expect.objectContaining({
        options: [
          { value: 'Сибирь', archived: false },
          { value: 'Алтай', archived: true },
          { value: 'Урал', archived: false },
        ],
        isActive: true,
      }),
    })
  })
})
