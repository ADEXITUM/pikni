import type { PosItem } from '@pikni/api/pos'
import { describe, expect, it } from 'vitest'

import {
  addLine,
  canSellOffline,
  isDuplicateCode,
  needsAge,
  removeLine,
  setQty,
  toReceipt,
  totalKopecks,
  type Line,
} from '../cart'

const item = (over: Partial<PosItem>): PosItem => ({
  variantId: 'v-' + (over.name ?? 'x'),
  article: 'ЧК-000001',
  name: 'x',
  label: '',
  barcodes: [],
  alcohol: false,
  crptMarked: false,
  price: 0,
  ...over,
})

const vodka = item({ name: 'Тайга', alcohol: true, accounting: 'marked', price: 599 })
const beer = item({ name: 'Амурское', alcohol: true, accounting: 'quantity', price: 119 })
const snack = item({ name: 'Сухарики', price: 0.1 })

describe('cart', () => {
  it('складывает товар без марок в одну строку, а бутылки с марками — по одной', () => {
    let lines: Line[] = []
    lines = addLine(lines, { item: beer, qty: 1 })
    lines = addLine(lines, { item: beer, qty: 2 })
    lines = addLine(lines, { item: vodka, qty: 1, markCode: 'M1' })
    lines = addLine(lines, { item: vodka, qty: 1, markCode: 'M2' })
    expect(lines.map((l) => [l.item.name, l.qty, l.markCode])).toEqual([
      ['Амурское', 3, undefined],
      ['Тайга', 1, 'M1'],
      ['Тайга', 1, 'M2'],
    ])
    expect(addLine(lines, { item: vodka, qty: 1, markCode: 'M1' })).toBe(lines)
    expect(isDuplicateCode(lines, 'M2')).toBe(true)
  })

  it('не меняет количество у строки с маркой', () => {
    const lines: Line[] = [
      { item: vodka, qty: 1, markCode: 'M1' },
      { item: beer, qty: 1 },
    ]
    expect(setQty(lines, 0, 5)).toBe(lines)
    expect(setQty(lines, 1, 0)).toBe(lines)
    expect(setQty(lines, 1, 4)[1]!.qty).toBe(4)
    expect(removeLine(lines, 0)).toEqual([{ item: beer, qty: 1 }])
  })

  it('считает сумму в копейках без ошибок округления', () => {
    expect(totalKopecks([{ item: snack, qty: 3 }])).toBe(30)
    expect(
      totalKopecks([
        { item: beer, qty: 2 },
        { item: snack, qty: 1 },
      ]),
    ).toBe(23810)
  })

  it('требует проверить возраст за алкоголь и табак и не пробивает марки без связи', () => {
    expect(needsAge([{ item: snack, qty: 1 }])).toBe(false)
    expect(needsAge([{ item: item({ tobacco: true }), qty: 1 }])).toBe(true)
    expect(
      canSellOffline([
        { item: beer, qty: 1 },
        { item: snack, qty: 1 },
      ]),
    ).toBe(true)
    expect(canSellOffline([{ item: vodka, qty: 1, markCode: 'M1' }])).toBe(false)
    expect(canSellOffline([{ item: item({ crptMarked: true }), qty: 1 }])).toBe(false)
  })

  it('собирает чек для кассового API', () => {
    const now = new Date('2026-10-24T05:00:00Z')
    const r = toReceipt([{ item: vodka, qty: 1, markCode: 'M1' }], {
      id: 'id-1',
      payment: 'card',
      ageConfirmed: true,
      now,
    })
    expect(r).toEqual({
      id: 'id-1',
      kind: 'sale',
      returnOf: undefined,
      payment: 'card',
      ageConfirmed: true,
      createdOnDeviceAt: '2026-10-24T05:00:00.000Z',
      offline: undefined,
      lines: [
        { variantId: vodka.variantId, qty: 1, price: 599, markCode: 'M1', crptCode: undefined },
      ],
    })
  })
})
