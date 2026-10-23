import type { CategoryAttribute, Container, EgaisAccounting, ScanKind, Variant } from '@pikni/api'

import { formatVolume } from '@/refs'

export type AttributeValues = Record<string, unknown>

export const accountingLabels: Record<EgaisAccounting | 'none', string> = {
  marked: 'помарочный учёт ЕГАИС',
  quantity: 'количественный учёт ЕГАИС',
  none: 'без ЕГАИС',
}

export const formatAbv = (abv: number) => `${abv.toLocaleString('ru')} %`

export const formatPrice = (rubles: number) =>
  rubles.toLocaleString('ru', { style: 'currency', currency: 'RUB' })

// Как сервер понял поисковую строку.
export const scanLabels: Record<ScanKind, string> = {
  barcode: 'штрихкод',
  alcocode: 'алкокод',
  egais_mark: 'марка ЕГАИС, алкокод',
  egais_mark_new: 'марка нового образца',
  crpt_code: 'код «Честного знака», GTIN',
}

export const containerLabels: Record<Container, string> = {
  glass: 'стекло',
  pet: 'ПЭТ',
  can: 'банка',
}

export function variantLabel(v: Pick<Variant, 'volumeMl' | 'container' | 'giftPack'>): string {
  const parts: string[] = []
  if (v.volumeMl) parts.push(formatVolume(v.volumeMl))
  if (v.container) parts.push(containerLabels[v.container])
  if (v.giftPack) parts.push('подарочная упаковка')
  return parts.join(', ') || 'без объёма и тары'
}

export const isEmpty = (v: unknown) => v === '' || v === null || v === undefined

// fitAttributes оставляет значения, которые есть в схеме новой категории, и возвращает
// названия убранных: сервер не примет характеристику, которой у категории нет.
export function fitAttributes(
  values: AttributeValues,
  from: CategoryAttribute[],
  to: CategoryAttribute[],
): { values: AttributeValues; dropped: string[] } {
  const allowed = new Set(to.map((a) => a.code))
  const kept: AttributeValues = {}
  const dropped: string[] = []
  for (const [code, v] of Object.entries(values)) {
    if (isEmpty(v)) continue
    if (allowed.has(code)) kept[code] = v
    else dropped.push(from.find((a) => a.code === code)?.name ?? code)
  }
  return { values: kept, dropped }
}

// attributeValues готовит значения к отправке: пустые убирает, число из поля ввода
// (строку с запятой или точкой) переводит в number.
export function attributeValues(values: AttributeValues, schema: CategoryAttribute[]) {
  const result: AttributeValues = {}
  for (const a of schema) {
    const v = values[a.code]
    if (isEmpty(v)) continue
    result[a.code] = a.type === 'number' ? Number(String(v).replace(',', '.')) : v
  }
  return result
}
