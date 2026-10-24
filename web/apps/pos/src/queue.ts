import type { PosNewReceipt } from '@pikni/api/pos'

import { readJSON, type Store } from './storage'

const KEY = 'pikni.pos.queue'
const REJECTED = 'pikni.pos.rejected'

export interface Rejected {
  receipt: PosNewReceipt
  reason: string
}

export type SendResult = 'sent' | 'offline' | { refused: string }

// Очередь чеков, пробитых без связи (КС-7). Идентификатор чека задаёт касса, поэтому повторная
// отправка безопасна: сервер вернёт уже принятый чек, а не проведёт продажу второй раз.
export function createQueue(store: Store) {
  const pending = () => readJSON<PosNewReceipt[]>(store, KEY, [])
  const rejected = () => readJSON<Rejected[]>(store, REJECTED, [])
  const save = (items: PosNewReceipt[]) =>
    store.set(KEY, items.length ? JSON.stringify(items) : null)

  return {
    pending,
    rejected,
    push(receipt: PosNewReceipt) {
      save([...pending(), { ...receipt, offline: true }])
    },
    // flush отправляет чеки по порядку и останавливается на первом, для которого нет связи.
    // Чек, который сервер отклонил, откладывается: его разбирает управляющий.
    async flush(send: (r: PosNewReceipt) => Promise<SendResult>): Promise<number> {
      let sent = 0
      for (const receipt of pending()) {
        const result = await send(receipt)
        if (result === 'offline') break
        if (result !== 'sent') {
          store.set(REJECTED, JSON.stringify([...rejected(), { receipt, reason: result.refused }]))
        }
        save(pending().filter((r) => r.id !== receipt.id))
        sent++
      }
      return sent
    },
    clearRejected() {
      store.set(REJECTED, null)
    },
  }
}

export type Queue = ReturnType<typeof createQueue>
