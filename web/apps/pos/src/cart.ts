import type { PosItem, PosNewReceipt } from '@pikni/api/pos'

export interface Line {
  item: PosItem
  qty: number
  markCode?: string
  crptCode?: string
  // Цена строки, если она не равна цене каталога: пачка сигарет продаётся не дороже МРЦ с пачки.
  price?: number
  // МРЦ из кода пачки, ₽ — показывается в строке чека.
  maxPrice?: number
}

export const linePrice = (l: Line) => l.price ?? l.item.price

export type Payment = PosNewReceipt['payment']

// Бутылка с акцизной маркой и пачка с кодом «Честного знака» — каждая отдельной строкой:
// код идёт в ЕГАИС и ГИС МТ поштучно. Остальное складывается в одну строку.
export function addLine(lines: Line[], line: Line): Line[] {
  if (line.markCode || line.crptCode) {
    const dup = lines.some(
      (l) =>
        (line.markCode && l.markCode === line.markCode) ||
        (line.crptCode && l.crptCode === line.crptCode),
    )
    return dup ? lines : [...lines, { ...line, qty: 1 }]
  }
  const i = lines.findIndex(
    (l) => l.item.variantId === line.item.variantId && !l.markCode && !l.crptCode,
  )
  if (i < 0) return [...lines, line]
  return lines.map((l, j) => (j === i ? { ...l, qty: l.qty + line.qty } : l))
}

export function isDuplicateCode(lines: Line[], markCode?: string, crptCode?: string): boolean {
  return lines.some(
    (l) => (markCode && l.markCode === markCode) || (crptCode && l.crptCode === crptCode),
  )
}

export function setQty(lines: Line[], index: number, qty: number): Line[] {
  const l = lines[index]
  if (!l || l.markCode || l.crptCode || !Number.isInteger(qty) || qty < 1) return lines
  return lines.map((x, j) => (j === index ? { ...x, qty } : x))
}

export function removeLine(lines: Line[], index: number): Line[] {
  return lines.filter((_, j) => j !== index)
}

// Сумма в копейках: в рублях с плавающей точкой 0,1 + 0,2 ≠ 0,3.
export function totalKopecks(lines: Line[]): number {
  return lines.reduce((sum, l) => sum + Math.round(linePrice(l) * 100) * l.qty, 0)
}

export const needsAge = (lines: Line[]) =>
  lines.some((l) => l.item.alcohol || l.item.tobacco === true)

// Без связи пробивается только товар без марок и кодов: их проверяет ЕГАИС и «Честный знак».
export const canSellOffline = (lines: Line[]) =>
  lines.every(
    (l) => !l.markCode && !l.crptCode && l.item.accounting !== 'marked' && !l.item.crptMarked,
  )

export function toReceipt(
  lines: Line[],
  opts: {
    id: string
    payment: Payment
    ageConfirmed: boolean
    now: Date
    offline?: boolean
    returnOf?: string
    reservationId?: string
  },
): PosNewReceipt {
  return {
    id: opts.id,
    kind: opts.returnOf ? 'return' : 'sale',
    returnOf: opts.returnOf,
    payment: opts.payment,
    ageConfirmed: opts.ageConfirmed,
    createdOnDeviceAt: opts.now.toISOString(),
    offline: opts.offline || undefined,
    reservationId: opts.reservationId,
    lines: lines.map((l) => ({
      variantId: l.item.variantId,
      qty: l.qty,
      price: linePrice(l),
      markCode: l.markCode,
      crptCode: l.crptCode,
    })),
  }
}

export function rubles(kopecks: number): string {
  return (
    (kopecks / 100).toLocaleString('ru', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) +
    ' ₽'
  )
}
