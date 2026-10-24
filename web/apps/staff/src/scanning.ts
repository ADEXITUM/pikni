import {
  createDeduper,
  listenWedge,
  recognize,
  scanEvent,
  type RecognizedCode,
  type ScanEvent,
  type ScanSource,
} from '@pikni/scanner'
import { onBeforeUnmount, onMounted } from 'vue'

export interface Scan {
  event: ScanEvent
  code: RecognizedCode
}

// useScanner подключает экран к сканам. Аппаратный сканер слушается, пока экран открыт, —
// без фокуса в поле ввода. Ручной ввод и камера передают код через submit. Повтор того же
// кода за 2 с гасится (СК-2), кроме ручного ввода: его человек повторяет намеренно.
export function useScanner(onScan: (scan: Scan) => void, dedupeMs = 2000) {
  const fresh = createDeduper(dedupeMs)

  function submit(raw: string, source: ScanSource) {
    if (source !== 'manual' && !fresh(raw)) return
    onScan({ event: scanEvent(raw, source), code: recognize(raw) })
  }

  let stop: (() => void) | undefined
  onMounted(() => (stop = listenWedge({ onScan: (raw) => submit(raw, 'wedge') })))
  onBeforeUnmount(() => stop?.())

  return { submit }
}
