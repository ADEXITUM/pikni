import { createLive } from '@pikni/api'
import { afterEach, describe, expect, it, vi } from 'vitest'

class FakeSocket {
  static OPEN = 1
  readyState = 0
  sent: unknown[] = []
  onopen?: () => void
  onmessage?: (m: { data: string }) => void
  onclose?: () => void
  send(data: string) {
    this.sent.push(JSON.parse(data))
  }
  open() {
    this.readyState = FakeSocket.OPEN
    this.onopen?.()
  }
  drop() {
    this.readyState = 3
    this.onclose?.()
  }
  event(topics: string[]) {
    this.onmessage?.({ data: JSON.stringify({ type: 'document', id: 'x', topics }) })
  }
}

describe('live', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('подписывается на темы, доставляет события и после обрыва переподключается', () => {
    vi.useFakeTimers()
    vi.stubGlobal('WebSocket', FakeSocket)
    const sockets: FakeSocket[] = []
    const live = createLive(
      () => 'ws://test/api/v1/ws',
      () => {
        const s = new FakeSocket()
        sockets.push(s)
        return s as unknown as WebSocket
      },
    )
    const doc = vi.fn<(e: unknown) => void>()
    const both = vi.fn<(e: unknown) => void>()
    const reconnected = vi.fn<() => void>()
    live.onReconnect(reconnected)
    live.subscribe('doc:1', doc)
    live.subscribe('doc:1', both)
    live.subscribe('location:2', both)

    sockets[0]!.open()
    expect(sockets[0]!.sent).toEqual([{ subscribe: ['doc:1', 'location:2'] }])
    sockets[0]!.event(['doc:1', 'location:2'])
    expect(doc).toHaveBeenCalledTimes(1)
    expect(both).toHaveBeenCalledTimes(1)

    sockets[0]!.drop()
    vi.advanceTimersByTime(1000)
    sockets[1]!.open()
    expect(sockets[1]!.sent).toEqual([{ subscribe: ['doc:1', 'location:2'] }])
    expect(reconnected).toHaveBeenCalledTimes(1)
  })
})
