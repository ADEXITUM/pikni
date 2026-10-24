import { reactive } from 'vue'

export interface ConfirmChange {
  label: string
  old: string
  new: string
}

export interface ConfirmRequest {
  title: string
  text?: string
  // changes — что было и что станет: изменение показывается до сохранения.
  changes?: ConfirmChange[]
  confirmText?: string
  danger?: boolean
}

// Одно окно подтверждения на всё приложение (ConfirmHost в App.vue). Удаление и изменение
// сохраняются только после него: случайный щелчок по крестику ничего не ломает.
export const confirmState = reactive({
  open: false,
  request: undefined as ConfirmRequest | undefined,
  resolve: undefined as ((ok: boolean) => void) | undefined,
})

export function confirm(request: ConfirmRequest): Promise<boolean> {
  confirmState.resolve?.(false)
  return new Promise((resolve) => {
    Object.assign(confirmState, { request, resolve, open: true })
  })
}

export function settleConfirm(ok: boolean) {
  confirmState.resolve?.(ok)
  Object.assign(confirmState, { open: false, resolve: undefined })
}

export function display(v: unknown): string {
  if (v === null || v === undefined || v === '') return '—'
  if (typeof v === 'boolean') return v ? 'да' : 'нет'
  if (Array.isArray(v)) return v.length ? v.map(display).join(', ') : '—'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

// diffFields — изменённые поля для окна подтверждения. Поля без подписи не показываются.
export function diffFields(
  before: Record<string, unknown>,
  after: Record<string, unknown>,
  labels: Record<string, string>,
): ConfirmChange[] {
  return Object.entries(labels)
    .map(([key, label]) => ({ label, old: display(before[key]), new: display(after[key]) }))
    .filter((c) => c.old !== c.new)
}

// confirmChanges спрашивает про изменения; без изменений сохранять нечего — возвращает false.
export async function confirmChanges(title: string, changes: ConfirmChange[]): Promise<boolean> {
  if (!changes.length) return false
  return confirm({ title, changes, confirmText: 'Сохранить' })
}
