import { api, type ImportReport } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { VFileInput } from 'vuetify/components'

import ImportDialog from '../ImportDialog.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { POST: vi.fn<ApiModule['api']['POST']>() },
}))

const DialogStub = defineComponent({
  props: { modelValue: Boolean },
  setup:
    (props, { slots }) =>
    () =>
      props.modelValue ? slots.default?.() : null,
})

const report = (r: Partial<ImportReport>): ImportReport => ({
  committed: false,
  rows: 2,
  products: 1,
  variants: 2,
  newProducers: ['ООО «Тайга»'],
  newBrands: [],
  newLines: [],
  errors: [],
  ...r,
})

async function openWithFile() {
  const wrapper = mount(ImportDialog, {
    props: { modelValue: false },
    global: {
      plugins: [createPikniVuetify(), [VueQueryPlugin, { queryClient: new QueryClient() }]],
      stubs: { VDialog: DialogStub },
    },
  })
  await wrapper.setProps({ modelValue: true })
  wrapper.findComponent(VFileInput).vm.$emit('update:modelValue', new File(['x'], 'товары.xlsx'))
  await flushPromises()
  return wrapper
}

describe('ImportDialog', () => {
  beforeEach(() => vi.mocked(api.POST).mockReset())

  it('ошибки по строкам не дают загрузить', async () => {
    vi.mocked(api.POST).mockResolvedValue({
      data: report({
        products: 0,
        errors: [{ row: 3, message: 'нет категории «Пивные напитки»' }],
      }),
      response: new Response(),
    } as never)
    const wrapper = await openWithFile()

    expect(api.POST).toHaveBeenCalledWith('/catalog/import/preview', expect.anything())
    expect(wrapper.find('[data-test="import-errors"]').text()).toContain('нет категории')
    expect(wrapper.find('[data-test="import-run"]').attributes('disabled')).toBeDefined()
  })

  it('без ошибок загружает тот же файл и сообщает итог', async () => {
    vi.mocked(api.POST)
      .mockResolvedValueOnce({ data: report({}), response: new Response() } as never)
      .mockResolvedValueOnce({
        data: report({ committed: true }),
        response: new Response(),
      } as never)
    const wrapper = await openWithFile()

    expect(wrapper.text()).toContain('производители: ООО «Тайга»')
    await wrapper.find('[data-test="import-run"]').trigger('click')
    await flushPromises()

    expect(api.POST).toHaveBeenLastCalledWith('/catalog/import', expect.anything())
    expect(wrapper.find('[data-test="import-done"]').text()).toContain('Загружено позиций: 1')
    expect(wrapper.emitted('imported')).toHaveLength(1)
  })
})
