import type { DayHours, OpeningHours } from '@pikni/api'

export const weekdays = [
  { key: 'mon', short: 'пн', title: 'Понедельник' },
  { key: 'tue', short: 'вт', title: 'Вторник' },
  { key: 'wed', short: 'ср', title: 'Среда' },
  { key: 'thu', short: 'чт', title: 'Четверг' },
  { key: 'fri', short: 'пт', title: 'Пятница' },
  { key: 'sat', short: 'сб', title: 'Суббота' },
  { key: 'sun', short: 'вс', title: 'Воскресенье' },
] as const

export type Weekday = (typeof weekdays)[number]['key']

// formatHours сворачивает одинаковые соседние дни: «пн–пт 09:00–23:00, сб 10:00–23:00, вс выходной».
export function formatHours(hours: OpeningHours): string {
  const text = (d?: DayHours) => (d ? `${d.open}–${d.close}` : 'выходной')
  const groups: { from: string; to: string; text: string }[] = []

  for (const day of weekdays) {
    const value = text(hours[day.key])
    const last = groups[groups.length - 1]
    if (last && last.text === value) {
      last.to = day.short
    } else {
      groups.push({ from: day.short, to: day.short, text: value })
    }
  }

  if (groups.length === 1 && groups[0]!.text !== 'выходной') return `ежедневно ${groups[0]!.text}`
  return groups.map((g) => `${g.from === g.to ? g.from : `${g.from}–${g.to}`} ${g.text}`).join(', ')
}
