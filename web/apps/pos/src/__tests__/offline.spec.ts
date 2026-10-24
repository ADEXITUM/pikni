import type { PosItem, PosNewReceipt } from '@pikni/api/pos'
import { describe, expect, it } from 'vitest'

import { catalogIndex, findOffline } from '../catalog'
import { keyFromHash, validKey } from '../device'
import { createQueue } from '../queue'
import { memoryStore } from '../storage'

const oldMark = '20N00001CGUMZYCB99J1NKN31105001000056NQQMS5VP4HTF5SB46ZSQQJD8BNJP891'

const item = (over: Partial<PosItem>): PosItem => ({
  variantId: 'v',
  article: 'ЧК-000001',
  name: 'x',
  label: '',
  barcodes: [],
  alcohol: false,
  crptMarked: false,
  price: 79,
  ...over,
})

describe('findOffline', () => {
  const index = catalogIndex({
    version: '2026-10-24T00:00:00Z',
    items: [
      item({ variantId: 'snack', article: 'ЧК-000003', barcodes: ['4600000000039'] }),
      item({
        variantId: 'vodka',
        article: 'ЧК-000001',
        barcodes: ['4600000000015'],
        accounting: 'marked',
      }),
    ],
  })

  it('находит товар по штрихкоду, GTIN и артикулу', () => {
    expect(findOffline(index, '4600000000039')).toMatchObject({ item: { variantId: 'snack' } })
    expect(findOffline(index, '04600000000039')).toMatchObject({ item: { variantId: 'snack' } })
    expect(findOffline(index, 'чк-3')).toMatchObject({ item: { variantId: 'snack' } })
  })

  it('не продаёт без связи товар с маркой и незнакомый код', () => {
    expect(findOffline(index, oldMark)).toHaveProperty('refusal')
    expect(findOffline(index, '4600000000015')).toHaveProperty('refusal')
    expect(findOffline(index, '4600000000022')).toHaveProperty('refusal')
  })
})

describe('device key', () => {
  it('берёт ключ из ссылки регистрации', () => {
    expect(keyFromHash('#key=pos_abcd2345_Zx-9_q')).toBe('pos_abcd2345_Zx-9_q')
    expect(keyFromHash('#key=garbage')).toBeNull()
    expect(validKey(' pos_ab_cd ')).toBe(true)
  })
})

describe('offline queue', () => {
  const receipt = (id: string): PosNewReceipt => ({
    id,
    kind: 'sale',
    payment: 'cash',
    ageConfirmed: false,
    createdOnDeviceAt: '2026-10-24T05:00:00Z',
    lines: [{ variantId: 'snack', qty: 1, price: 79 }],
  })

  it('отправляет по порядку, откладывает отклонённые и ждёт связи', async () => {
    const q = createQueue(memoryStore())
    q.push(receipt('a'))
    q.push(receipt('b'))
    q.push(receipt('c'))
    expect(q.pending().every((r) => r.offline)).toBe(true)

    const seen: string[] = []
    const sent = await q.flush(async (r) => {
      seen.push(r.id)
      if (r.id === 'a') return 'sent'
      if (r.id === 'b') return { refused: 'Продажа алкоголя сейчас запрещена' }
      return 'offline'
    })
    expect(seen).toEqual(['a', 'b', 'c'])
    expect(sent).toBe(2)
    expect(q.pending().map((r) => r.id)).toEqual(['c'])
    expect(q.rejected()).toMatchObject([
      { receipt: { id: 'b' }, reason: 'Продажа алкоголя сейчас запрещена' },
    ])

    await q.flush(async () => 'sent')
    expect(q.pending()).toEqual([])
  })
})
