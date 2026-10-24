// @vitest-environment node
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'

import { beforeAll, describe, expect, it } from 'vitest'
import { prepareZXingModule, readBarcodes, writeBarcode, type WriterOptions } from 'zxing-wasm/full'

import { GS, recognize } from '../codes'
import { readerOptions, toDecoded } from '../decode'

// Коды рисует тот же ZXing, а читает — с настройками камеры. Так проверяется, что текст кода
// доходит до разбора без искажений: разделители GS, длинная марка PDF417, SSCC.
beforeAll(async () => {
  const wasm = readFileSync(
    createRequire(import.meta.url).resolve('zxing-wasm/full/zxing_full.wasm'),
  )
  await prepareZXingModule({
    overrides: {
      wasmBinary: wasm.buffer.slice(wasm.byteOffset, wasm.byteOffset + wasm.byteLength),
    },
    fireImmediately: true,
  })
})

async function scan(text: string, format: WriterOptions['format'], options = '') {
  const { image, error } = await writeBarcode(text, { format, options, scale: 3 })
  if (!image) throw new Error(error)
  return toDecoded(await readBarcodes(image, readerOptions))
}

const mark = '20N00001CGUMZYCB99J1NKN31105001000056NQQMS5VP4HTF5SB46ZSQQJD8BNJP891'

describe('распознавание изображений кодов', () => {
  it('штрихкод EAN-13', async () => {
    const [code] = await scan('4602124002104', 'EAN13')
    expect(code).toEqual({ raw: '4602124002104', format: 'ean_13' })
    expect(recognize(code!.raw).kind).toBe('product')
  })

  it('SSCC короба в GS1-128', async () => {
    const [code] = await scan('[00]046000000000000015', 'Code128', 'gs1')
    expect(code).toEqual({ raw: '00046000000000000015', format: 'code_128' })
    expect(recognize(code!.raw)).toMatchObject({ kind: 'package', code: '046000000000000015' })
  })

  it('код «Честного знака» в GS1 DataMatrix сохраняет разделитель GS', async () => {
    const [code] = await scan('[01]04602124002104[21]5i.7D(U[93]33kC', 'DataMatrix', 'gs1')
    expect(code).toEqual({ raw: `0104602124002104215i.7D(U${GS}9333kC`, format: 'data_matrix' })
    expect(recognize(code!.raw)).toMatchObject({
      kind: 'crpt',
      code: '04602124002104',
      serial: '5i.7D(U',
    })
  })

  it('марка ЕГАИС старого образца в PDF417', async () => {
    const [code] = await scan(mark, 'PDF417')
    expect(code).toEqual({ raw: mark, format: 'pdf417' })
    expect(recognize(code!.raw)).toMatchObject({
      kind: 'excise_mark',
      alcocode: '0177207000002463655',
    })
  })

  it('служебный QR-код', async () => {
    const [code] = await scan('pikni:pair/7f3a', 'QRCode')
    expect(code).toEqual({ raw: 'pikni:pair/7f3a', format: 'qr_code' })
    expect(recognize(code!.raw).kind).toBe('service_qr')
  })
})
