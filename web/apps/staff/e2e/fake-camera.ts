import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { dirname } from 'node:path'

import { prepareZXingModule, writeBarcode } from 'zxing-wasm/full'

// writeFakeCamera готовит видео для подменённой камеры Chromium: кадры в формате Y4M
// (яркость и пустой цвет), на каждом — штрихкод. ffmpeg не нужен: код рисует тот же ZXing.
export async function writeFakeCamera(file: string, code: string, format: 'EAN13' | 'PDF417') {
  const wasm = await readFile(
    createRequire(import.meta.url).resolve('zxing-wasm/full/zxing_full.wasm'),
  )
  await prepareZXingModule({
    overrides: {
      wasmBinary: wasm.buffer.slice(wasm.byteOffset, wasm.byteOffset + wasm.byteLength),
    },
    fireImmediately: true,
  })
  const { symbol } = await writeBarcode(code, { format, scale: 1 })

  const width = 640
  const height = 480
  const scale = Math.floor(Math.min((width * 0.5) / symbol.width, (height * 0.3) / symbol.height))
  const left = Math.floor((width - symbol.width * scale) / 2)
  const top = Math.floor((height - symbol.height * scale) / 2)

  const luma = new Uint8Array(width * height).fill(235)
  for (let y = 0; y < symbol.height * scale; y++) {
    for (let x = 0; x < symbol.width * scale; x++) {
      const module = symbol.data[Math.floor(y / scale) * symbol.width + Math.floor(x / scale)]!
      luma[(top + y) * width + left + x] = module ? 235 : 16
    }
  }
  const chroma = new Uint8Array((width / 2) * (height / 2)).fill(128)
  const frame = Buffer.concat([Buffer.from('FRAME\n'), luma, chroma, chroma])
  const header = Buffer.from(`YUV4MPEG2 W${width} H${height} F10:1 Ip A1:1 C420jpeg\n`)

  await mkdir(dirname(file), { recursive: true })
  await writeFile(file, Buffer.concat([header, ...Array.from({ length: 10 }, () => frame)]))
}
