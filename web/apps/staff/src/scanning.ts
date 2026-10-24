import {
  type CodeKind,
  createDeduper,
  listenWedge,
  recognize,
  scanEvent,
  type RecognizedCode,
  type ScanEvent,
  type ScanFormat,
  type ScanSource,
} from '@pikni/scanner'
import { onBeforeUnmount, onMounted } from 'vue'

export const codeKindLabels: Record<CodeKind, string> = {
  excise_mark: 'Марка ЕГАИС',
  crpt: 'Код «Честного знака»',
  package: 'Короб или палета',
  product: 'Штрихкод товара',
  asset: 'Инвентарный номер',
  service_qr: 'Служебный QR-код',
  unknown: 'Неизвестный код',
}

export interface Scan {
  event: ScanEvent
  code: RecognizedCode
}

// useScanner подключает экран к сканам. Аппаратный сканер слушается, пока экран открыт, —
// без фокуса в поле ввода. Ручной ввод и камера передают код через submit. Повтор того же
// кода за 2 с гасится (СК-2), кроме ручного ввода: его человек повторяет намеренно.
export function useScanner(onScan: (scan: Scan) => void, dedupeMs = 2000) {
  const fresh = createDeduper(dedupeMs)

  function submit(raw: string, source: ScanSource, format?: ScanFormat) {
    if (source !== 'manual' && !fresh(raw)) return
    onScan({ event: scanEvent(raw, source, format), code: recognize(raw) })
  }

  let stop: (() => void) | undefined
  onMounted(() => (stop = listenWedge({ onScan: (raw) => submit(raw, 'wedge') })))
  onBeforeUnmount(() => stop?.())

  return { submit, forget: fresh.forget }
}
