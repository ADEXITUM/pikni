import type { ReaderOptions, ReadResult } from 'zxing-wasm/reader'

import type { ScanFormat } from './events'

export interface DecodedCode {
  raw: string
  format: ScanFormat
}

export type Decoder = (image: ImageData) => Promise<DecodedCode[]>

// Форматы СК-1. Plain отдаёт текст кода как есть, с разделителями GS внутри кода «Честного
// знака»; режим по умолчанию (HRI) превратил бы его в «(01)…(21)…», и разбор марок не сработал бы.
export const readerOptions: ReaderOptions = {
  formats: ['EAN13', 'EAN8', 'UPCA', 'Code128', 'QRCode', 'DataMatrix', 'PDF417'],
  textMode: 'Plain',
  tryHarder: true,
  maxNumberOfSymbols: 1,
}

export function toScanFormat(format: string): ScanFormat {
  if (format === 'EAN13') return 'ean_13'
  if (format === 'EAN8') return 'ean_8'
  if (format === 'UPCA') return 'upc_a'
  if (format === 'Code128') return 'code_128'
  if (format.startsWith('QRCode')) return 'qr_code'
  if (format === 'DataMatrix') return 'data_matrix'
  if (format.endsWith('PDF417')) return 'pdf417'
  return 'unknown'
}

export function toDecoded(results: ReadResult[]): DecodedCode[] {
  return results
    .filter((r) => r.isValid && r.text !== '')
    .map((r) => ({ raw: r.text, format: toScanFormat(r.format) }))
}
