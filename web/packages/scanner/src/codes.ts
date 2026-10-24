// Распознавание вида кода на устройстве. Повторяет серверный разбор (api/internal/catalog/scan.go),
// чтобы экран сразу показал, что считано, даже без связи; решение всё равно принимает сервер.

export type CodeKind =
  'excise_mark' | 'crpt' | 'package' | 'product' | 'asset' | 'service_qr' | 'unknown'

export interface RecognizedCode {
  kind: CodeKind
  // Что искать: штрихкод, GTIN-14, номер марки, SSCC, инвентарный номер или ссылка.
  code: string
  // Алкокод из марки ЕГАИС старого образца.
  alcocode?: string
  // Марка нового образца: алкокода в ней нет, товар находится только по учёту марок.
  newMark?: boolean
  // Серийный номер кода «Честного знака».
  serial?: string
}

export const GS = '\u001d'

// Сканер в русской раскладке печатает буквы с тех же клавиш: «Й» вместо «Q».
const RU = 'ЙЦУКЕНГШЩЗФЫВАПРОЛДЯЧСМИТЬ'
const EN = 'QWERTYUIOPASDFGHJKLZXCVBNM'
const layout = new Map([...RU].map((r, i) => [r, EN[i]!]))

export function fixLayout(s: string): string {
  return [...s].map((c) => layout.get(c.toUpperCase()) ?? c).join('')
}

// normalizeScan снимает то, что добавляют сканеры: префикс символики (]d2 у GS1 DataMatrix),
// служебный FNC1 или GS в начале и символ «␝», которым некоторые сканеры заменяют GS.
export function normalizeScan(raw: string): string {
  let s = raw.trim()
  if (/^\][A-Za-z][0-9A-Za-z]/.test(s)) s = s.slice(3)
  return s.replace(/^[\xe8\u001d]+/, '').split('\u241d').join(GS)
}

// gs1Valid проверяет контрольную цифру EAN, UPC, GTIN и SSCC: веса 3 и 1 справа налево.
export function gs1Valid(code: string): boolean {
  if (!/^\d{2,}$/.test(code)) return false
  let sum = 0
  for (let i = code.length - 2, w = 3; i >= 0; i--, w = 4 - w) sum += Number(code[i]) * w
  return (10 - (sum % 10)) % 10 === Number(code[code.length - 1])
}

// В марке старого образца символы 8–19 — алкокод по основанию 36. Число больше 2^53,
// поэтому считается в BigInt: parseInt потерял бы последние цифры.
function alcocodeFromMark(mark: string): string {
  let n = 0n
  for (const c of mark.slice(7, 19)) n = n * 36n + BigInt(parseInt(c, 36))
  return n.toString().padStart(19, '0')
}

// Код «Честного знака»: (01) + GTIN, (21) + серийный номер, после GS — криптохвост (91–93).
// Если сканер потерял GS, длину серийного номера подсказывает товарная группа: пиво — 7, напитки — 13.
function parseCrpt(s: string): { gtin: string; serial: string } | undefined {
  if (!/^01\d{14}21/.test(s) || !gs1Valid(s.slice(2, 16))) return undefined
  const gtin = s.slice(2, 16)
  const rest = s.slice(18)
  const gs = rest.indexOf(GS)
  if (gs >= 0) return { gtin, serial: rest.slice(0, gs) }
  const len = [7, 13].find((n) => /^9[123]/.test(rest.slice(n)))
  return { gtin, serial: len ? rest.slice(0, len) : rest }
}

export function recognize(raw: string): RecognizedCode {
  const s = normalizeScan(raw)
  if (!s) return { kind: 'unknown', code: '' }

  if (/^pikni:/i.test(s) || /^https?:\/\/[^/]+\/(pair|pos\/register)(\/|\?|$)/.test(s)) {
    return { kind: 'service_qr', code: s }
  }
  if (/^INV-[0-9A-Z-]{3,}$/i.test(s)) return { kind: 'asset', code: s.toUpperCase() }

  const latin = fixLayout(s).toUpperCase()
  if (/^[0-9A-Z]+$/.test(latin)) {
    if (latin.length === 68)
      return { kind: 'excise_mark', code: latin, alcocode: alcocodeFromMark(latin) }
    if (latin.length === 150) return { kind: 'excise_mark', code: latin, newMark: true }
    // Учебные марки имитатора ЕГАИС начинаются с PKN и с настоящими не совпадают.
    if (/^PKN[0-9A-Z]{10,}$/.test(latin)) return { kind: 'excise_mark', code: latin }
  }

  const crpt = parseCrpt(s)
  if (crpt) return { kind: 'crpt', code: crpt.gtin, serial: crpt.serial }

  if (/^00\d{18}$/.test(s) && gs1Valid(s.slice(2))) return { kind: 'package', code: s.slice(2) }

  if (/^\d+$/.test(s)) {
    if (s.length === 19) return { kind: 'product', code: s }
    if ([8, 12, 13, 14].includes(s.length) && gs1Valid(s)) return { kind: 'product', code: s }
    return { kind: 'unknown', code: s }
  }
  // Code 128 на коробах и своих этикетках: латиница с цифрами без пробелов.
  if (s.length >= 4 && /\d/.test(s) && /^[\x21-\x7e]+$/.test(s)) return { kind: 'product', code: s }
  return { kind: 'unknown', code: s }
}
