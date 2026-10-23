import type { SaleBan, SaleRule, SaleVerdict } from '@pikni/api'

const months = [
  'января',
  'февраля',
  'марта',
  'апреля',
  'мая',
  'июня',
  'июля',
  'августа',
  'сентября',
  'октября',
  'ноября',
  'декабря',
]

export function describeVerdict(v: SaleVerdict): string {
  if (v.banDay) return `нельзя весь день: ${v.banDay}`
  if (v.allowed) return `можно до ${v.to}`
  return `нельзя, продажа с ${v.from} до ${v.to}`
}

export function banDate(b: SaleBan): string {
  if (b.monthDay) {
    const [month, day] = b.monthDay.split('-').map(Number)
    return `ежегодно ${day} ${months[month! - 1]}`
  }
  return b.date ? new Date(`${b.date}T00:00`).toLocaleDateString('ru') : ''
}

export type EditionStatus = 'current' | 'planned' | 'past'

// editionStatuses помечает редакции каждого правила: действующую (последнюю наступившую),
// будущие и прежние. Ключ — регион и вид продукции.
export function editionStatuses(rules: SaleRule[], today: string): Map<string, EditionStatus> {
  const statuses = new Map<string, EditionStatus>()
  const groups = new Map<string, SaleRule[]>()
  for (const r of rules) {
    const key = `${r.subjectCode ?? ''}/${r.scope}`
    groups.set(key, [...(groups.get(key) ?? []), r])
  }
  for (const editions of groups.values()) {
    const started = editions.filter((r) => r.validFrom <= today)
    const current = started.reduce<SaleRule | undefined>(
      (best, r) => (!best || r.validFrom > best.validFrom ? r : best),
      undefined,
    )
    for (const r of editions) {
      statuses.set(r.id, r === current ? 'current' : r.validFrom > today ? 'planned' : 'past')
    }
  }
  return statuses
}
