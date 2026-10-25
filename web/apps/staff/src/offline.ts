import { api, ApiError, type NewDocumentScan } from '@pikni/api'
import { computed, ref } from 'vue'

import { load, save } from '@/preferences'
import { queryClient } from '@/query'

// Сканы без связи (СК-8): скан в документ сохраняется на устройстве и уходит, когда связь
// появится. Идентификатор скана создан на устройстве — повторная отправка не засчитается дважды.

export interface QueuedScan {
  documentId: string
  body: NewDocumentScan
}

const KEY = 'pikni.scans.queue'

function restore(): QueuedScan[] {
  try {
    return JSON.parse(load(KEY) ?? '[]') as QueuedScan[]
  } catch {
    return []
  }
}

export const queue = ref<QueuedScan[]>(restore())
// Сканы, которые сервер отклонил при отправке из очереди, — их показывают сотруднику.
export const rejected = ref<{ raw: string; reason: string }[]>([])

const persist = () => save(KEY, JSON.stringify(queue.value))

export function pendingFor(documentId: () => string) {
  return computed(() => queue.value.filter((q) => q.documentId === documentId()).length)
}

// isOffline — запрос не дошёл до сервера: нет сети или сервер недоступен.
export const isOffline = (e: unknown) => !(e instanceof ApiError)

let timer: ReturnType<typeof setInterval> | undefined

export function enqueue(documentId: string, body: NewDocumentScan) {
  queue.value = [...queue.value, { documentId, body }]
  persist()
  timer ??= setInterval(() => void flush(), 15_000)
}

let flushing = false
export async function flush() {
  if (flushing || !queue.value.length) return
  flushing = true
  const touched = new Set<string>()
  try {
    while (queue.value.length) {
      const item = queue.value[0]!
      try {
        const { error } = await api.POST('/documents/{id}/scans', {
          params: { path: { id: item.documentId } },
          body: item.body,
        })
        if (error)
          rejected.value = [
            ...rejected.value,
            { raw: item.body.raw, reason: new ApiError(error).message },
          ]
      } catch {
        return
      }
      touched.add(item.documentId)
      queue.value = queue.value.slice(1)
      persist()
    }
    clearInterval(timer)
    timer = undefined
  } finally {
    flushing = false
    for (const id of touched) void queryClient.invalidateQueries({ queryKey: ['document', id] })
  }
}

if (typeof window !== 'undefined') {
  window.addEventListener('online', () => void flush())
  if (queue.value.length) timer = setInterval(() => void flush(), 15_000)
}
