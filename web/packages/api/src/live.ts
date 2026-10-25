// События реального времени (УВ-1): сервер сообщает, что изменилось, а данные экран перезапрашивает
// обычным запросом. Соединение одно на вкладку; после обрыва оно восстанавливается, и все
// экраны перезапрашивают данные — события за время обрыва могли пропасть.

export interface LiveEvent {
  type: 'document' | 'scan' | 'stock' | 'exchange' | 'receipt'
  id: string
  topics: string[]
}

type Listener = (event: LiveEvent) => void

export interface Live {
  subscribe(topic: string, listener: Listener): () => void
  onReconnect(listener: () => void): () => void
}

export function createLive(url: () => string, socket = (u: string) => new WebSocket(u)): Live {
  const listeners = new Map<string, Set<Listener>>()
  const reconnects = new Set<() => void>()
  let ws: WebSocket | undefined
  let delay = 1000
  let opened = false
  let timer: ReturnType<typeof setTimeout> | undefined

  const send = (msg: object) => {
    if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg))
  }

  function connect() {
    timer = undefined
    ws = socket(url())
    ws.onopen = () => {
      delay = 1000
      send({ subscribe: [...listeners.keys()] })
      if (opened) reconnects.forEach((r) => r())
      opened = true
    }
    ws.onmessage = (m) => {
      const event = JSON.parse(String(m.data)) as LiveEvent
      const seen = new Set<Listener>()
      for (const topic of event.topics) {
        for (const l of listeners.get(topic) ?? []) {
          if (!seen.has(l)) {
            seen.add(l)
            l(event)
          }
        }
      }
    }
    ws.onclose = () => {
      ws = undefined
      if (listeners.size === 0) return
      timer = setTimeout(connect, delay)
      delay = Math.min(delay * 2, 10_000)
    }
  }

  return {
    subscribe(topic, listener) {
      let set = listeners.get(topic)
      if (!set) {
        set = new Set()
        listeners.set(topic, set)
        send({ subscribe: [topic] })
      }
      set.add(listener)
      if (!ws && !timer) connect()
      return () => {
        set.delete(listener)
        if (set.size === 0) {
          listeners.delete(topic)
          send({ unsubscribe: [topic] })
        }
      }
    },
    onReconnect(listener) {
      reconnects.add(listener)
      return () => reconnects.delete(listener)
    },
  }
}

export const live = createLive(
  () => `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/v1/ws`,
)
