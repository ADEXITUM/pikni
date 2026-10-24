import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

import { autoConfirm } from '@/__tests__/confirm'

import { formatVolume, type RefConfig } from '@/refs'

import RefTable from '../RefTable.vue'

interface Row {
  id: string
  name: string
  isActive: boolean
}

const DialogStub = defineComponent({
  props: { modelValue: Boolean },
  setup:
    (props, { slots }) =>
    () =>
      props.modelValue ? slots.default?.() : null,
})

function config(over: Partial<RefConfig<Row>> = {}): RefConfig<Row> {
  return {
    key: 'test',
    title: 'Тест',
    rowKey: (r) => r.id,
    load: async () => [{ id: '1', name: 'Тайга', isActive: true }],
    columns: [{ key: 'name', title: 'Название' }],
    fields: [{ key: 'name', label: 'Название' }],
    hasActive: true,
    toForm: (r) => ({ name: r.name }),
    create: vi.fn<NonNullable<RefConfig<Row>['create']>>().mockResolvedValue(undefined),
    update: vi.fn<NonNullable<RefConfig<Row>['update']>>().mockResolvedValue(undefined),
    ...over,
  }
}

async function mountTable(c: RefConfig<Row>, canEdit = true) {
  const wrapper = mount(RefTable<Row>, {
    props: { config: c, canEdit },
    global: {
      plugins: [createPikniVuetify(), [VueQueryPlugin, { queryClient: new QueryClient() }]],
      stubs: { VDialog: DialogStub },
    },
  })
  await flushPromises()
  return wrapper
}

describe('RefTable', () => {
  let confirmed: ReturnType<typeof autoConfirm>
  beforeEach(() => {
    confirmed = autoConfirm()
  })
  afterEach(() => confirmed.stop())

  it('справочник без полей или без права — только для чтения', async () => {
    const readOnly = await mountTable(config({ fields: undefined }))
    expect(readOnly.text()).toContain('Тайга')
    expect(readOnly.find('[data-test="ref-add"]').exists()).toBe(false)

    const noRight = await mountTable(config(), false)
    expect(noRight.find('[data-test="ref-add"]').exists()).toBe(false)
  })

  it('добавляет запись', async () => {
    const c = config()
    const wrapper = await mountTable(c)

    await wrapper.find('[data-test="ref-add"]').trigger('click')
    await wrapper.find('[data-test="field-name"] input').setValue('Белуга')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(c.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'Белуга' }))
  })

  it('по строке открывает правку с заполненными полями', async () => {
    const c = config()
    const wrapper = await mountTable(c)

    await wrapper.find('tbody tr').trigger('click')
    const input = wrapper.find('[data-test="field-name"] input')
    expect((input.element as HTMLInputElement).value).toBe('Тайга')

    await input.setValue('Тайга Премиум')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(c.update).toHaveBeenCalledWith(
      expect.objectContaining({ id: '1' }),
      expect.objectContaining({ name: 'Тайга Премиум', isActive: true }),
    )
  })
})

describe('RefTable: значения по умолчанию и удаление', () => {
  let confirmed: ReturnType<typeof autoConfirm>
  beforeEach(() => {
    confirmed = autoConfirm()
  })
  afterEach(() => confirmed.stop())

  it('новая запись получает initial, удалить можно только разрешённое', async () => {
    const remove = vi.fn<NonNullable<RefConfig<Row>['remove']>>().mockResolvedValue(undefined)
    const c = config({
      load: async () => [
        { id: '1', name: 'Действует', isActive: true },
        { id: '2', name: 'Будущая', isActive: true },
      ],
      fields: [{ key: 'name', label: 'Название', initial: 'по умолчанию' }],
      remove,
      canRemove: (r) => r.id === '2',
    })
    const wrapper = await mountTable(c)

    const buttons = wrapper.findAll('[data-test="ref-remove"]')
    expect(buttons).toHaveLength(1)
    await buttons[0]!.trigger('click')
    await flushPromises()
    expect(remove).toHaveBeenCalledWith(expect.objectContaining({ id: '2' }))
    expect(confirmed.asked[0]?.text).toContain('«Будущая»')

    await wrapper.find('[data-test="ref-add"]').trigger('click')
    const input = wrapper.find('[data-test="field-name"] input')
    expect((input.element as HTMLInputElement).value).toBe('по умолчанию')
  })
})

describe('formatVolume', () => {
  it.each([
    [250, '0,25 л'],
    [700, '0,7 л'],
    [1750, '1,75 л'],
  ])('%i мл → %s', (ml, text) => {
    expect(formatVolume(ml)).toBe(text)
  })
})
