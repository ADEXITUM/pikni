import { describe, expect, it } from 'vitest'

import { GS, fixLayout, gs1Valid, recognize } from '../codes'
import { createDeduper } from '../events'

// Те же примеры, что в серверных тестах (api/internal/catalog/scan_test.go).
const oldMark = '20N00001CGUMZYCB99J1NKN31105001000056NQQMS5VP4HTF5SB46ZSQQJD8BNJP891'
const beerCode = `0104602124002104215i.7D(U${GS}9333kC`

describe('recognize', () => {
  it.each([
    [oldMark, { kind: 'excise_mark', code: oldMark, alcocode: '0177207000002463655' }],
    ['10110026167968' + '0118001' + 'A'.repeat(129), { kind: 'excise_mark', newMark: true }],
    ['PKN0000000001234', { kind: 'excise_mark', code: 'PKN0000000001234' }],
    [beerCode, { kind: 'crpt', code: '04602124002104', serial: '5i.7D(U' }],
    [']d2\xe8' + beerCode, { kind: 'crpt', code: '04602124002104' }],
    [beerCode.replace(GS, ''), { kind: 'crpt', serial: '5i.7D(U' }],
    ['00046000000000000015', { kind: 'package', code: '046000000000000015' }],
    ['4006381333931', { kind: 'product', code: '4006381333931' }],
    ['0177207000002463655', { kind: 'product', code: '0177207000002463655' }],
    ['PK-BOX-0001', { kind: 'product' }],
    ['inv-0042', { kind: 'asset', code: 'INV-0042' }],
    ['https://pikni.example/pair?token=abc', { kind: 'service_qr' }],
    ['4006381333932', { kind: 'unknown' }],
    ['тайга', { kind: 'unknown' }],
  ])('%s', (raw, want) => {
    expect(recognize(raw)).toMatchObject(want)
  })

  it('марку, набранную сканером в русской раскладке, переводит обратно', () => {
    const cyrillic = [...oldMark]
      .map((c) => 'ЙЦУКЕНГШЩЗФЫВАПРОЛДЯЧСМИТЬ'['QWERTYUIOPASDFGHJKLZXCVBNM'.indexOf(c)] ?? c)
      .join('')
    expect(fixLayout(cyrillic)).toBe(oldMark)
    expect(recognize(cyrillic).alcocode).toBe('0177207000002463655')
  })

  it('gs1Valid', () => {
    expect(gs1Valid('73513537')).toBe(true)
    expect(gs1Valid('036000291452')).toBe(true)
    expect(gs1Valid('04602124002104')).toBe(true)
    expect(gs1Valid('04602124002105')).toBe(false)
  })
})

describe('createDeduper', () => {
  it('повтор в течение окна гасится, пока код остаётся в кадре', () => {
    const fresh = createDeduper(2000)
    expect(fresh('A', 0)).toBe(true)
    expect(fresh('A', 1500)).toBe(false)
    expect(fresh('A', 3000)).toBe(false) // окно продлилось на 1500
    expect(fresh('B', 3100)).toBe(true)
    expect(fresh('A', 5100)).toBe(true)
  })
})
