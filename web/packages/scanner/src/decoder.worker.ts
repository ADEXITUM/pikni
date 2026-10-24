import { prepareZXingModule, readBarcodes } from 'zxing-wasm/reader'
import wasmUrl from 'zxing-wasm/reader/zxing_reader.wasm?url'

import { readerOptions, toDecoded } from './decode'

// Модуль WebAssembly раздаёт наш сервер, а не CDN по умолчанию: строгая CSP, работа без
// внешних сетей и PWA без связи.
prepareZXingModule({
  overrides: {
    locateFile: (path: string, prefix: string) =>
      path.endsWith('.wasm') ? wasmUrl : prefix + path,
  },
})

self.onmessage = async (e: MessageEvent<{ id: number; image: ImageData }>) => {
  const { id, image } = e.data
  try {
    self.postMessage({ id, codes: toDecoded(await readBarcodes(image, readerOptions)) })
  } catch (err) {
    self.postMessage({ id, error: String(err) })
  }
}
