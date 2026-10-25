import { api, ApiError, live, type LiveEvent } from '@pikni/api'
import { ref } from 'vue'

import { load, save } from '@/preferences'

// Смартфон как беспроводной сканер для ПК (СК-6). ПК открывает сопряжение и показывает QR-код,
// смартфон того же сотрудника сканирует его и дальше пересылает каждый скан. На ПК код приходит
// событием и попадает в открытый экран так же, как со сканера-«клавиатуры».

export interface PcPairing {
  id: string
  code: string
  status: 'waiting' | 'active'
}

const PC_KEY = 'pikni.pairing.pc'
const PHONE_KEY = 'pikni.pairing.phone'

function restore<T>(key: string): T | undefined {
  try {
    const raw = load(key)
    return raw ? (JSON.parse(raw) as T) : undefined
  } catch {
    return undefined
  }
}

export const pcPairing = ref<PcPairing | undefined>(restore(PC_KEY))
export const phonePairing = ref<string | undefined>(restore(PHONE_KEY))

// Экран, открытый последним, получает коды; если экрана со сканированием нет — общий обработчик.
type Sink = (raw: string) => void
const sinks: Sink[] = []
const seen = new Set<string>()

export function onPairedScan(sink: Sink): () => void {
  sinks.push(sink)
  return () => {
    const i = sinks.lastIndexOf(sink)
    if (i >= 0) sinks.splice(i, 1)
  }
}

function setPc(p: PcPairing | undefined) {
  pcPairing.value = p
  save(PC_KEY, p ? JSON.stringify(p) : '')
}

let stop: (() => void) | undefined
function listen(id: string) {
  stop?.()
  stop = live.subscribe(`pair:${id}`, (e: LiveEvent) => {
    const p = pcPairing.value
    if (!p || p.id !== id) return
    if (e.type === 'paired') setPc({ ...p, status: 'active' })
    if (e.type === 'unpaired') {
      stop?.()
      stop = undefined
      setPc(undefined)
    }
    if (e.type === 'paired_scan' && e.raw && e.scan && !seen.has(e.scan)) {
      seen.add(e.scan)
      sinks[sinks.length - 1]?.(e.raw)
    }
  })
}

// После перезагрузки страницы ПК снова слушает своё сопряжение, если оно ещё живо.
export async function resumePairing() {
  const p = pcPairing.value
  if (!p) return
  const { data } = await api.GET('/pairings/{id}', { params: { path: { id: p.id } } })
  if (!data || data.status === 'closed') return setPc(undefined)
  setPc({ ...p, status: data.status })
  listen(p.id)
}

export async function openPairing() {
  const { data, error } = await api.POST('/pairings')
  if (error) throw new ApiError(error)
  setPc({ id: data.id, code: data.code ?? '', status: 'waiting' })
  listen(data.id)
}

export async function closePairing() {
  const p = pcPairing.value
  stop?.()
  stop = undefined
  setPc(undefined)
  if (p) await api.DELETE('/pairings/{id}', { params: { path: { id: p.id } } })
}

export async function joinPairing(code: string) {
  const { data, error } = await api.POST('/pairings/join', { body: { code } })
  if (error) throw new ApiError(error)
  phonePairing.value = data.id
  save(PHONE_KEY, JSON.stringify(data.id))
}

export async function leavePairing() {
  const id = phonePairing.value
  phonePairing.value = undefined
  save(PHONE_KEY, '')
  if (id) await api.DELETE('/pairings/{id}', { params: { path: { id } } })
}

// sendToPc пересылает скан смартфона на ПК. Сопряжение закрыто — смартфон выходит из режима.
export async function sendToPc(raw: string, scanId: string) {
  const id = phonePairing.value
  if (!id) return
  const { error, response } = await api.POST('/pairings/{id}/scans', {
    params: { path: { id } },
    body: { scanId, raw },
  })
  if (response.status === 410 || response.status === 404) {
    phonePairing.value = undefined
    save(PHONE_KEY, '')
  }
  if (error) throw new ApiError(error)
}
