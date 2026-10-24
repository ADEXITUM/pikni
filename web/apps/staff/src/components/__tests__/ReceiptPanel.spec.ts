import { api, type Document } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import ReceiptPanel from '../ReceiptPanel.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { PUT: vi.fn<ApiModule['api']['PUT']>() },
}))

const receipt = (over: Partial<NonNullable<Document['receipt']>> = {}): Document => ({
  id: 'd1',
  kind: 'receipt',
  number: 'ПТ-2026-000001',
  status: 'draft',
  docDate: '2026-10-24',
  locationId: 'l1',
  locationName: 'Магазин',
  zoneId: 'z1',
  zoneKind: 'backroom',
  authorName: 'ЕГАИС',
  lineCount: 1,
  totalQty: 5,
  createdAt: '2026-10-24T03:00:00Z',
  version: 1,
  comment: '',
  canEdit: true,
  canPost: true,
  scans: [],
  lines: [
    {
      variantId: 'v1',
      productId: 'p1',
      article: 'ЧК-000001',
      name: 'Тайга',
      giftPack: false,
      egaisAccounting: 'marked',
      qty: 5,
      qtyPlan: 6,
      qtyFact: 5,
      qtyAccepted: 5,
    },
  ],
  receipt: {
    mode: 'sample',
    packages: [{ code: '046000000000000017', qty: 6, toOpen: true, scanned: true, counted: 5 }],
    unmapped: [],
    shortage: 1,
    surplus: 0,
    foreign: 0,
    differences: true,
    ...over,
  },
})

function mountPanel(doc: Document) {
  return mount(ReceiptPanel, {
    props: { doc },
    global: {
      plugins: [createPikniVuetify(), [VueQueryPlugin, { queryClient: new QueryClient() }]],
    },
  })
}

describe('ReceiptPanel', () => {
  it('для продукции с марками режимы «по документу» и «по коробам» недоступны', () => {
    const wrapper = mountPanel(receipt())
    expect(wrapper.find('[data-test="mode-document"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="mode-boxes"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="mode-piece"]').attributes('disabled')).toBeUndefined()
  })

  it('расхождение во вскрытом коробе предлагает перейти к поштучной приёмке', async () => {
    vi.mocked(api.PUT).mockResolvedValueOnce({
      data: receipt({ mode: 'piece' }),
      response: new Response(),
    } as never)
    const wrapper = mountPanel(receipt())
    expect(wrapper.find('[data-test="summary"]').text()).toContain('акт расхождения')
    await wrapper.find('[data-test="to-piece"]').trigger('click')
    await flushPromises()
    expect(api.PUT).toHaveBeenCalledWith('/documents/{id}/mode', {
      params: { path: { id: 'd1' } },
      body: { mode: 'piece' },
    })
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('без расхождений — акт подтверждения и никаких подсказок', () => {
    const wrapper = mountPanel(
      receipt({
        shortage: 0,
        differences: false,
        packages: [{ code: '046000000000000017', qty: 6, toOpen: true, scanned: true, counted: 6 }],
      }),
    )
    expect(wrapper.find('[data-test="summary"]').text()).toContain('акт подтверждения')
    expect(wrapper.find('[data-test="sample-problem"]').exists()).toBe(false)
  })
})
