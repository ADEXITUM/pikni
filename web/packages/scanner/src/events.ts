export type ScanSource = 'camera' | 'wedge' | 'manual' | 'paired'

export type ScanFormat =
  'ean_13' | 'ean_8' | 'upc_a' | 'code_128' | 'qr_code' | 'data_matrix' | 'pdf417' | 'unknown'

// ScanEvent — одно считывание. id создаётся на устройстве: при повторной отправке после
// потери связи сервер узнает скан и не учтёт его дважды.
export interface ScanEvent {
  id: string
  raw: string
  format: ScanFormat
  source: ScanSource
  scannedAt: string
}

export function scanEvent(
  raw: string,
  source: ScanSource,
  format: ScanFormat = 'unknown',
): ScanEvent {
  return { id: crypto.randomUUID(), raw, format, source, scannedAt: new Date().toISOString() }
}

// createDeduper гасит повторное чтение того же кода в течение окна (СК-2): камера видит код
// в каждом кадре. Пока код остаётся в кадре, окно продлевается, поэтому повтор не проскочит.
// forget снимает код с учёта: после отмены ошибочного скана его можно сразу считать снова.
export function createDeduper(windowMs = 2000) {
  const seen = new Map<string, number>()
  const fresh = (raw: string, now = Date.now()): boolean => {
    for (const [code, at] of seen) if (now - at > windowMs) seen.delete(code)
    const last = seen.get(raw)
    seen.set(raw, now)
    return last === undefined
  }
  return Object.assign(fresh, { forget: (raw: string) => seen.delete(raw) })
}
