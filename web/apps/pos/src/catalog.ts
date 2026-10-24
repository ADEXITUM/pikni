import type { PosCatalog, PosItem } from '@pikni/api/pos'
import { recognize } from '@pikni/scanner'

// Каталог хранится на кассе: без связи товар без марок находится по штрихкоду.
export function catalogIndex(catalog: PosCatalog | null): Map<string, PosItem> {
  const index = new Map<string, PosItem>()
  for (const item of catalog?.items ?? []) {
    for (const code of item.barcodes) index.set(code, item)
    if (item.gtin) index.set(item.gtin, item)
    index.set(item.article, item)
  }
  return index
}

// findOffline ищет товар по скану без сервера. Марки ЕГАИС и коды «Честного знака» без связи
// не проверить — для них причина отказа.
export function findOffline(
  index: Map<string, PosItem>,
  raw: string,
): { item: PosItem } | { refusal: string } {
  const code = recognize(raw)
  if (code.kind === 'excise_mark' || code.kind === 'crpt') {
    return { refusal: 'Нет связи — товар с маркой продать нельзя' }
  }
  // GTIN-14 с ведущим нулём — тот же EAN-13.
  const item = index.get(code.code) ?? index.get(code.code.replace(/^0(?=\d{13}$)/, ''))
  if (!item) return { refusal: 'Товара с таким кодом нет в каталоге кассы' }
  if (item.accounting === 'marked' || item.crptMarked) {
    return { refusal: 'Нет связи — товар с маркой продать нельзя' }
  }
  return { item }
}
