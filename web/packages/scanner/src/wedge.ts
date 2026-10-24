import { GS } from './codes'

// Аппаратный сканер в режиме эмуляции клавиатуры (СК-7) «печатает» код за миллисекунды
// и завершает его Enter. Человек набирает в 3–5 раз медленнее, поэтому скан узнаётся по
// интервалу между символами и принимается без фокуса в поле ввода.
export interface WedgeOptions {
  onScan(raw: string): void
  // Наибольший интервал между символами скана, мс.
  maxInterval?: number
  minLength?: number
  // ignoreFields — скан в поле ввода остаётся обычным вводом: так работает глобальный
  // перехват в шапке, а поиск, куда навели фокус, получает код как набранный текст.
  ignoreFields?: boolean
}

type Field = HTMLInputElement | HTMLTextAreaElement

const isField = (t: EventTarget | null): t is Field =>
  t instanceof HTMLInputElement || t instanceof HTMLTextAreaElement

export function createWedgeDetector({
  onScan,
  maxInterval = 35,
  minLength = 6,
  ignoreFields = false,
}: WedgeOptions) {
  let buffer = ''
  let last = -Infinity
  // Поле, в которое успели попасть символы скана, и его значение до скана.
  let field: Field | null = null
  let before = ''

  return (e: KeyboardEvent, now = performance.now()) => {
    if (ignoreFields && isField(e.target)) return
    const fast = now - last <= maxInterval

    if (e.key === 'Enter') {
      if (fast && buffer.length >= minLength) {
        e.preventDefault()
        e.stopPropagation()
        if (field) {
          field.value = before
          field.dispatchEvent(new Event('input', { bubbles: true }))
        }
        onScan(buffer)
      }
      buffer = ''
      last = -Infinity
      return
    }

    // Разделитель GS сканеры передают сочетанием Ctrl+].
    const char = e.ctrlKey && (e.key === ']' || e.code === 'BracketRight') ? GS : e.key
    if (char.length !== 1 || ((e.ctrlKey || e.altKey || e.metaKey) && char !== GS)) return
    if (char === GS) e.preventDefault()

    if (!fast || buffer === '') {
      buffer = ''
      field = isField(e.target) ? e.target : null
      before = field?.value ?? ''
    }
    buffer += char
    last = now
  }
}

// listenWedge слушает клавиатуру на этапе перехвата: скан доходит до экрана раньше, чем
// поле ввода успеет обработать Enter.
export function listenWedge(options: WedgeOptions, target: Window = window): () => void {
  const detector = createWedgeDetector(options)
  const handler = (e: KeyboardEvent) => detector(e)
  target.addEventListener('keydown', handler, { capture: true })
  return () => target.removeEventListener('keydown', handler, { capture: true })
}
