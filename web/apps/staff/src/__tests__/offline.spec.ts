import type * as ApiModule from '@pikni/api'
import { describe, expect, it, vi } from 'vitest'

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<typeof ApiModule>()),
  api: { POST: vi.fn<typeof ApiModule.api.POST>() },
}))

const { api } = await import('@pikni/api')
const offline = await import('@/offline')

const body = (id: string) => ({
  id,
  raw: '4600000000016',
  source: 'wedge' as const,
  scannedAt: '2026-10-24T10:00:00Z',
})

describe('сканы без связи', () => {
  it('копит сканы, пока нет связи, и отправляет их по порядку, когда связь появилась', async () => {
    offline.enqueue('d1', body('s1'))
    offline.enqueue('d1', body('s2'))
    offline.enqueue('d2', body('s3'))
    const pending = offline.pendingFor(() => 'd1')
    expect(pending.value).toBe(2)
    expect(JSON.parse(localStorage.getItem('pikni.scans.queue')!)).toHaveLength(3)

    // Связи всё ещё нет: очередь не трогается.
    vi.mocked(api.POST).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    await offline.flush()
    expect(offline.queue.value).toHaveLength(3)

    // Связь есть: второй скан сервер отклонил — он уходит из очереди в список отклонённых.
    vi.mocked(api.POST)
      .mockResolvedValueOnce({ data: {} } as never)
      .mockResolvedValueOnce({ error: { title: 'Зона закрыта', status: 409 } } as never)
      .mockResolvedValueOnce({ data: {} } as never)
    await offline.flush()
    expect(offline.queue.value).toHaveLength(0)
    expect(pending.value).toBe(0)
    expect(offline.rejected.value).toEqual([{ raw: '4600000000016', reason: 'Зона закрыта' }])
    expect(
      vi.mocked(api.POST).mock.calls.map((c) => (c[1] as { body: { id: string } }).body.id),
    ).toEqual(['s1', 's1', 's2', 's3'])
  })
})
