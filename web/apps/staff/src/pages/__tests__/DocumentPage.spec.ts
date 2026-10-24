import { api, type Document, type DocumentScanResult } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { autoConfirm } from '@/__tests__/confirm'
import { makeMe } from '@/__tests__/fixtures'
import ConfirmHost from '@/components/ConfirmHost.vue'
import { playFeedback } from '@/feedback'
import { meQuery } from '@/session'

import DocumentPage from '../DocumentPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@/feedback', () => ({ playFeedback: vi.fn<(event: string) => void>() }))

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: {
    GET: vi.fn<ApiModule['api']['GET']>(),
    POST: vi.fn<ApiModule['api']['POST']>(),
    PUT: vi.fn<ApiModule['api']['PUT']>(),
    DELETE: vi.fn<ApiModule['api']['DELETE']>(),
  },
}))

const draft: Document = {
  id: 'd1',
  kind: 'opening',
  number: 'ВО-2026-000001',
  status: 'draft',
  docDate: '2026-10-24',
  locationId: 'l1',
  locationName: 'Магазин на ул. Ленина',
  zoneId: 'z1',
  zoneKind: 'backroom',
  authorName: 'Кладовщик',
  lineCount: 0,
  totalQty: 0,
  createdAt: '2026-10-24T03:00:00Z',
  version: 1,
  comment: '',
  lines: [],
  scans: [],
  canEdit: true,
  canPost: false,
}

const vodka = {
  variantId: 'v1',
  productId: 'p1',
  article: 'ЧК-000001',
  name: 'Тайга',
  giftPack: false,
}

function scanResult(
  id: string,
  status: DocumentScanResult['scan']['status'],
  qty?: number,
): DocumentScanResult {
  return {
    scan: {
      id,
      raw: id,
      status,
      qty: 1,
      scannedAt: '2026-10-24T03:01:00Z',
      variantId: 'v1',
      name: 'Тайга',
    },
    line:
      qty === undefined ? undefined : { ...vodka, volumeMl: 500, egaisAccounting: 'marked', qty },
  }
}

async function mountPage() {
  const queryClient = new QueryClient()
  queryClient.setQueryData(
    meQuery.queryKey,
    makeMe({ permissions: ['availability.view', 'stocktake.manage'] }),
  )
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/documents/:id', name: 'document', component: DocumentPage },
      { path: '/documents', name: 'documents', component: { render: () => null } },
    ],
  })
  await router.push('/documents/d1')
  const wrapper = mount(
    { components: { DocumentPage, ConfirmHost }, template: '<DocumentPage /><ConfirmHost />' },
    {
      global: {
        plugins: [createPikniVuetify(), router, [VueQueryPlugin, { queryClient }]],
        stubs: { ScanCamera: { template: '<div />', emits: ['scan', 'close'] } },
      },
      attachTo: document.body,
    },
  )
  await flushPromises()
  return { wrapper, queryClient }
}

const manual = async (wrapper: Awaited<ReturnType<typeof mountPage>>['wrapper'], code: string) => {
  await wrapper.find('[data-test="manual"] input').setValue(code)
  await wrapper.find('form').trigger('submit')
  await flushPromises()
}

describe('DocumentPage', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.mocked(playFeedback).mockClear()
    vi.mocked(api.GET).mockResolvedValue({ data: draft, response: new Response() } as never)
    vi.mocked(api.POST).mockReset()
  })

  it('штрихкод выбирает товар, марки нового образца относятся к нему и наращивают строку', async () => {
    const { wrapper } = await mountPage()
    vi.mocked(api.POST)
      .mockResolvedValueOnce({
        data: scanResult('4600000000017', 'selected'),
        response: new Response(),
      } as never)
      .mockResolvedValueOnce({
        data: scanResult('PKN0000000000001', 'accepted', 1),
        response: new Response(),
      } as never)

    await manual(wrapper, '4600000000017')
    expect(wrapper.find('[data-test="selected"]').text()).toContain('Тайга')

    await manual(wrapper, 'PKN0000000000001')
    const body = vi.mocked(api.POST).mock.calls[1]![1] as {
      body: { variantId?: string; raw: string }
    }
    expect(body.body).toMatchObject({ raw: 'PKN0000000000001', variantId: 'v1' })
    expect(wrapper.find('[data-test="lines"]').text()).toContain('ЧК-000001')
    expect(wrapper.findAll('[data-test="scan"]')).toHaveLength(2)
    // У строки с марками количество не правится вручную — его считают марки.
    expect(wrapper.find('[data-test="qty"]').exists()).toBe(false)
    expect(playFeedback).toHaveBeenLastCalledWith('accepted')
    wrapper.unmount()
  })

  it('чужая марка — красный исход с причиной и сигнал ошибки', async () => {
    const { wrapper } = await mountPage()
    const res = scanResult('PKN0000000000009', 'foreign')
    res.scan.reason = 'Марки нет в учёте точки'
    vi.mocked(api.POST).mockResolvedValueOnce({ data: res, response: new Response() } as never)

    await manual(wrapper, 'PKN0000000000009')
    expect(wrapper.find('[data-test="last-scan"]').text()).toContain('Марки нет в учёте точки')
    expect(playFeedback).toHaveBeenLastCalledWith('unknown')
    wrapper.unmount()
  })

  it('проведение спрашивает подтверждение с итогом документа', async () => {
    vi.mocked(api.GET).mockResolvedValue({
      data: {
        ...draft,
        canPost: true,
        lineCount: 1,
        totalQty: 2,
        lines: [{ ...vodka, egaisAccounting: 'marked', qty: 2 }],
      },
      response: new Response(),
    } as never)
    const { wrapper } = await mountPage()
    vi.mocked(api.POST).mockResolvedValueOnce({
      data: { ...draft, status: 'posted', canEdit: false },
      response: new Response(),
    } as never)

    const { asked, stop } = autoConfirm(true)
    await wrapper.find('[data-test="post"]').trigger('click')
    await flushPromises()
    stop()

    expect(asked[0]?.text).toContain('всего 2 шт.')
    expect(api.POST).toHaveBeenCalledWith('/documents/{id}/post', {
      params: { path: { id: 'd1' } },
      body: { version: 1 },
    })
    expect(wrapper.find('[data-test="status"]').text()).toBe('Проведён')
    expect(playFeedback).toHaveBeenLastCalledWith('posted')
    wrapper.unmount()
  })
})
