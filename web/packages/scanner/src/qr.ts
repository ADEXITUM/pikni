import { prepareZXingModule, writeBarcode } from 'zxing-wasm/writer'
import wasmUrl from 'zxing-wasm/writer/zxing_writer.wasm?url'

prepareZXingModule({
  overrides: {
    locateFile: (path: string, prefix: string) =>
      path.endsWith('.wasm') ? wasmUrl : prefix + path,
  },
})

// qrSvg рисует QR-код SVG-разметкой: ключ кассы в карточке регистрации, ссылка ЕГАИС на чеке.
export async function qrSvg(text: string): Promise<string> {
  const { svg, error } = await writeBarcode(text, {
    format: 'QRCode',
    options: 'ecLevel=M',
    scale: 4,
  })
  if (error) throw new Error(error)
  return svg
}
