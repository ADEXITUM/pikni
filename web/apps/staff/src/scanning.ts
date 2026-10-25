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

import { onPairedScan } from '@/pairing'

export const codeKindLabels: Record<CodeKind, string> = {
  excise_mark: 'Марка ЕГАИС',
  crpt: 'Код «Честного знака»',
  package: 'Короб или палета',
  product: 'Штрихкод товара',
  article: 'Артикул',
  asset: 'Инвентарный номер',
  service_qr: 'Служебный QR-код',
  unknown: 'Неизвестный код',
}

export interface Scan {
  event: ScanEvent
  code: RecognizedCode
}

// useScanner подключает экран к сканам. Аппаратный сканер слушается, пока экран открыт, —
// без фокуса в поле ввода, как и сопряжённый смартфон. Ручной ввод и камера передают код через
// submit. Повтор того же кода за 2 с гасится (СК-2), кроме ручного ввода — его человек повторяет
// намеренно — и смартфона: повторы он гасит сам.
export function useScanner(onScan: (scan: Scan) => void, dedupeMs = 2000) {
  const fresh = createDeduper(dedupeMs)

  function submit(raw: string, source: ScanSource, format?: ScanFormat) {
    if (source !== 'manual' && source !== 'paired' && !fresh(raw)) return
    onScan({ event: scanEvent(raw, source, format), code: recognize(raw) })
  }

  let stops: (() => void)[] = []
  onMounted(
    () =>
      (stops = [
        listenWedge({ onScan: (raw) => submit(raw, 'wedge') }),
        onPairedScan((raw) => submit(raw, 'paired')),
      ]),
  )
  onBeforeUnmount(() => stops.forEach((s) => s()))

  return { submit, forget: fresh.forget }
}
