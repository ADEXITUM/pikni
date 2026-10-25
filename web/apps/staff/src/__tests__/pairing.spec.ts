import type * as ApiModule from '@pikni/api'
import type { LiveEvent } from '@pikni/api'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const listeners = new Map<string, (e: LiveEvent) => void>()

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<typeof ApiModule>()),
  api: {
    GET: vi.fn<typeof ApiModule.api.GET>(),
    POST: vi.fn<typeof ApiModule.api.POST>(),
    DELETE: vi.fn<typeof ApiModule.api.DELETE>(
      async () => ({ response: new Response(null, { status: 204 }) }) as never,
    ),
  },
  live: {
    subscribe: (topic: string, l: (e: LiveEvent) => void) => {
      listeners.set(topic, l)
      return () => listeners.delete(topic)
    },
    onReconnect: () => () => {},
  },
}))

const { api } = await import('@pikni/api')
const pairing = await import('@/pairing')

describe('сопряжение смартфона с ПК', () => {
  beforeEach(() => {
    localStorage.clear()
    listeners.clear()
    vi.mocked(api.POST).mockReset()
  })

  it('ПК получает коды смартфона в последний открытый экран, повтор скана — один раз', async () => {
    vi.mocked(api.POST).mockResolvedValueOnce({
      data: { id: 'p1', code: 'pikni:pair:p1:secret', status: 'waiting', expiresAt: '' },
      response: new Response(),
    } as never)
    await pairing.openPairing()
    expect(pairing.pcPairing.value).toMatchObject({ id: 'p1', status: 'waiting' })

    const send = listeners.get('pair:p1')!
    send({ type: 'paired', id: 'p1', topics: ['pair:p1'] })
    expect(pairing.pcPairing.value?.status).toBe('active')

    const shell = vi.fn<(raw: string) => void>()
    const screen = vi.fn<(raw: string) => void>()
    pairing.onPairedScan(shell)
    const leave = pairing.onPairedScan(screen)
    const scan = (id: string) =>
      send({ type: 'paired_scan', id: 'p1', topics: ['pair:p1'], raw: '4600000000016', scan: id })
    scan('s1')
    scan('s1')
    expect(screen).toHaveBeenCalledTimes(1)
    expect(shell).not.toHaveBeenCalled()

    leave()
    scan('s2')
    expect(shell).toHaveBeenCalledWith('4600000000016')

    send({ type: 'unpaired', id: 'p1', topics: ['pair:p1'] })
    expect(pairing.pcPairing.value).toBeUndefined()
  })

  it('смартфон пересылает сканы, пока сопряжение живо', async () => {
    vi.mocked(api.POST).mockResolvedValueOnce({
      data: { id: 'p2', status: 'active', expiresAt: '' },
      response: new Response(),
    } as never)
    await pairing.joinPairing('pikni:pair:p2:secret')
    expect(pairing.phonePairing.value).toBe('p2')

    vi.mocked(api.POST).mockResolvedValueOnce({
      error: { title: 'Сопряжение закрыто', status: 410 },
      response: new Response(null, { status: 410 }),
    } as never)
    await expect(pairing.sendToPc('4600000000016', 's1')).rejects.toThrow('Сопряжение закрыто')
    expect(pairing.phonePairing.value).toBeUndefined()
  })
})
