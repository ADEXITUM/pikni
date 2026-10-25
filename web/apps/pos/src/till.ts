import {
  posApi,
  type PosCatalog,
  type PosCheck,
  type PosNewReceipt,
  type PosReceipt,
  type PosSession,
  type PosShift,
  type PosTime,
} from '@pikni/api/pos'
import { reactive } from 'vue'

import { createQueue, type SendResult } from './queue'
import { local, readJSON } from './storage'

export class TillError extends Error {}

// Состояние кассы. Ключ кассы и каталог переживают перезагрузку; сессия кассира тоже — до
// истечения срока, иначе случайное обновление страницы посреди чека выкидывало бы кассира.
export const till = reactive({
  key: local.get('pikni.pos.key'),
  session: readJSON<PosSession | null>(local, 'pikni.pos.session', null),
  shift: readJSON<PosShift | null>(local, 'pikni.pos.shift', null),
  catalog: readJSON<PosCatalog | null>(local, 'pikni.pos.catalog', null),
  time: null as PosTime | null,
  online: navigator.onLine,
  queued: 0,
  rejected: 0,
  // Отчёт закрытой смены: показывается и после перехода на экран открытия смены.
  report: null as PosShift | null,
})

export const queue = createQueue(local)
till.queued = queue.pending().length
till.rejected = queue.rejected().length

if (till.session && new Date(till.session.expiresAt) < new Date()) till.session = null

export function setKey(key: string | null) {
  till.key = key
  local.set('pikni.pos.key', key)
  setSession(null)
}

function setSession(session: PosSession | null) {
  till.session = session
  local.set('pikni.pos.session', session ? JSON.stringify(session) : null)
  if (!session) setShift(null)
}

// Смена тоже хранится на кассе: без связи после перезагрузки касса продолжает продавать.
function setShift(shift: PosShift | null) {
  till.shift = shift
  local.set('pikni.pos.shift', shift ? JSON.stringify(shift) : null)
}

posApi.use({
  onRequest({ request }) {
    if (till.key) request.headers.set('Authorization', `Device ${till.key}`)
    if (till.session) request.headers.set('X-Cashier-Session', till.session.token)
    return request
  },
  onResponse({ response }) {
    till.online = true
    // Ключ кассы отключили в «Кассах» или сессия кассира истекла — заново настроить кассу или войти.
    if (response.status === 401) {
      if (!till.session || response.url.endsWith('/session')) return response
      setSession(null)
    }
    return response
  },
})

// call выполняет запрос; без связи — TillError с понятным текстом и till.online = false.
async function call<T>(
  request: () => Promise<{ data?: T; error?: { detail?: string; title: string; status: number } }>,
): Promise<T> {
  let res
  try {
    res = await request()
  } catch {
    till.online = false
    throw new TillError('Нет связи с сервером')
  }
  if (res.error) {
    if (res.error.status === 401 && res.error.detail?.startsWith('Касса')) setKey(null)
    throw new TillError(res.error.detail ?? res.error.title)
  }
  return res.data as T
}

export async function login(login: string, pin: string) {
  setSession(await call(() => posApi.POST('/session', { body: { login, pin } })))
  await Promise.all([loadShift(), refreshCatalog(), refreshTime()])
}

export async function logout() {
  await call(() => posApi.DELETE('/session')).catch(() => undefined)
  setSession(null)
}

export async function loadShift() {
  const state = await call(() => posApi.GET('/shift'))
  setShift(state.shift ?? null)
}

export async function openShift() {
  setShift(await call(() => posApi.POST('/shift')))
}

export async function closeShift() {
  till.report = await call(() => posApi.POST('/shift/close'))
  setShift(null)
}

export async function refreshCatalog() {
  const catalog = await call(() => posApi.GET('/catalog'))
  till.catalog = catalog
  local.set('pikni.pos.catalog', JSON.stringify(catalog))
}

export async function refreshTime() {
  till.time = await call(() => posApi.GET('/time'))
}

export function check(raw: string): Promise<PosCheck> {
  return call(() => posApi.POST('/checks', { body: { raw } }))
}

export function reservations() {
  return call(() => posApi.GET('/reservations')).then((r) => r.items)
}

export function findReceipts(query: { number?: number; mark?: string }): Promise<PosReceipt[]> {
  return call(() => posApi.GET('/receipts', { params: { query } })).then((r) => r.items)
}

export type ReceiptResult =
  { receipt: PosReceipt } | { refusal: { code: string; message: string } } | { offline: true }

// sendReceipt отправляет чек. Отказ (422) — не ошибка, а причина для кассира.
export async function sendReceipt(r: PosNewReceipt): Promise<ReceiptResult> {
  let res
  try {
    res = await posApi.POST('/receipts', { body: r })
  } catch {
    till.online = false
    return { offline: true }
  }
  if (res.data) {
    await loadShift().catch(() => undefined)
    return { receipt: res.data }
  }
  if (res.response.status === 422)
    return { refusal: res.error as { code: string; message: string } }
  const problem = res.error as { detail?: string; title?: string }
  throw new TillError(problem.detail ?? problem.title ?? 'Чек не принят')
}

export function enqueue(r: PosNewReceipt) {
  queue.push(r)
  till.queued = queue.pending().length
}

export async function flushQueue() {
  if (!till.session || queue.pending().length === 0) return
  await queue.flush(async (r): Promise<SendResult> => {
    // Ошибка сервера, а не отказ продажи (например, смена ещё не открыта), — повторить позже.
    const res = await sendReceipt(r).catch(() => ({ offline: true as const }))
    if ('offline' in res) return 'offline'
    if ('refusal' in res) return { refused: res.refusal.message }
    return 'sent'
  })
  till.queued = queue.pending().length
  till.rejected = queue.rejected().length
}
