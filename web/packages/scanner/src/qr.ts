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
  return scalable(svg)
}

// scalable делает SVG генератора масштабируемым: у него фиксированные width и height без viewBox,
// поэтому заданный стилями размер обрезал или сдвигал рисунок. Модули рисуются без сглаживания.
export function scalable(svg: string): string {
  const body = svg.slice(svg.indexOf('<svg'))
  const size = /<svg[^>]*\swidth="(\d+)"[^>]*\sheight="(\d+)"/.exec(body)
  if (!size) return body
  return body.replace(
    /<svg[^>]*?>/,
    `<svg viewBox="0 0 ${size[1]} ${size[2]}" xmlns="http://www.w3.org/2000/svg" shape-rendering="crispEdges" role="img">`,
  )
}
