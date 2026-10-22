// Проверка контраста темы «Пикни» по WCAG 2.1 (уровень AA).
// Запуск: node check-contrast.mjs        — печать результата
//         node check-contrast.mjs --write — пересобрать contrast.md
// Нужен Node 22.18+ или 23.6+: он сам снимает типы при импорте .ts.
import { writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { pikniLight, pikniDark } from './vuetify-theme.ts'

/** Непрозрачность рамки поля в покое. Должна совпадать с brand.css (.v-field__outline). */
const FIELD_OUTLINE_OPACITY = 0.6

const hexToRgb = hex => {
  const h = hex.replace('#', '')
  const full = h.length === 3 ? [...h].map(c => c + c).join('') : h
  return [0, 2, 4].map(i => parseInt(full.slice(i, i + 2), 16))
}
const rgbToHex = rgb => '#' + rgb.map(v => Math.round(v).toString(16).padStart(2, '0')).join('').toUpperCase()
const blend = (fg, bg, alpha) => {
  const f = hexToRgb(fg), b = hexToRgb(bg)
  return rgbToHex(f.map((v, i) => v * alpha + b[i] * (1 - alpha)))
}
const luminance = hex => {
  const [r, g, b] = hexToRgb(hex).map(v => {
    const c = v / 255
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}
const ratio = (a, b) => {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

const ROLES = [
  ['primary', 'основной (действия)'],
  ['secondary', 'вторичный (дерево)'],
  ['success', 'успех'],
  ['warning', 'внимание'],
  ['error', 'ошибка / стоп'],
  ['info', 'справка'],
  ['status-draft', 'Черновик'],
  ['status-progress', 'В работе'],
  ['status-pending', 'На утверждении'],
  ['status-posted', 'Проведён'],
  ['status-cancelled', 'Отменён'],
  ['status-transit', 'В пути'],
  ['mark-ok', 'Марка проверена'],
  ['mark-off-balance', 'Марка не на балансе'],
  ['sale-blocked', 'Продажа запрещена по времени'],
]

function checkTheme (name, t) {
  const c = t.colors, v = t.variables
  const S = c.surface, B = c.background, F = c['on-surface']
  const rows = []
  const add = (group, what, fg, bg, need) => {
    const r = ratio(fg, bg)
    rows.push({ group, what, fg, bg, r, need, ok: need ? r >= need : null })
  }

  add('Текст', 'Основной текст на карточке', F, S, 4.5)
  add('Текст', 'Основной текст на фоне', c['on-background'], B, 4.5)
  const mo = v['medium-emphasis-opacity']
  add('Текст', `Вторичный текст на карточке (непрозрачность ${mo})`, blend(F, S, mo), S, 4.5)
  add('Текст', `Вторичный текст на фоне (непрозрачность ${mo})`, blend(c['on-background'], B, mo), B, 4.5)
  add('Текст', 'Текст на заливке surface-light (наведение, своя точка)', F, c['surface-light'], 4.5)
  add('Текст', 'Подсказка (surface-variant)', c['on-surface-variant'], c['surface-variant'], 4.5)

  const ao = v['activated-opacity']
  for (const [key, label] of ROLES) {
    add(label, 'Надпись на сплошной заливке', c[`on-${key}`], c[key], 4.5)
    add(label, 'Цвет как текст на карточке', c[key], S, 4.5)
    add(label, 'Цвет как текст на фоне', c[key], B, 4.5)
    add(label, `Тональный чип (заливка ${ao})`, c[key], blend(c[key], S, ao), 4.5)
  }

  add('Элементы', `Рамка поля ввода (непрозрачность ${FIELD_OUTLINE_OPACITY})`, blend(F, S, FIELD_OUTLINE_OPACITY), S, 3)
  add('Элементы', 'Кольцо фокуса (primary) на карточке', c.primary, S, 3)
  add('Элементы', 'Кольцо фокуса (primary) на фоне', c.primary, B, 3)
  add('Элементы', 'Рамка карточки (декоративная, требования нет)', blend(v['border-color'], S, v['border-opacity']), B, null)
  add('Элементы', 'Неактивный текст (по WCAG не нормируется)', blend(F, S, v['disabled-opacity']), S, null)
  return { name, rows }
}

const results = [checkTheme('Светлая тема', pikniLight), checkTheme('Тёмная тема', pikniDark)]

const fmt = n => n.toFixed(2).replace('.', ',')
let failed = 0
const md = [
  '# Контраст темы «Пикни»',
  '',
  'Таблица собрана скриптом `check-contrast.mjs` из файла `vuetify-theme.ts`, руками её не правят.',
  'Пересборка: `node check-contrast.mjs --write`.',
  '',
  'Метод — WCAG 2.1: относительная яркость цветов sRGB, отношение (L1 + 0,05) / (L2 + 0,05).',
  'Полупрозрачные слои (вторичный текст, тональные чипы, рамки) сначала накладываются на подложку, как это делает браузер, и только потом считаются.',
  '',
  'Пороги уровня AA:',
  '- обычный текст — 4,5 : 1 (критерий 1.4.3);',
  '- границы элементов управления и индикатор фокуса — 3 : 1 (критерий 1.4.11).',
  '',
  'Цвет статуса никогда не работает один: рядом всегда значок и слово (критерий 1.4.1, п. 4.4.5 ТЗ).',
  '',
]
for (const { name, rows } of results) {
  md.push(`## ${name}`, '', '| Роль | Пара | Передний план | Подложка | Контраст | Порог | Итог |', '|---|---|---|---|---|---|---|')
  for (const row of rows) {
    if (row.ok === false) failed++
    const verdict = row.ok === null ? '—' : row.ok ? 'да' : '**нет**'
    md.push(`| ${row.group} | ${row.what} | \`${row.fg}\` | \`${row.bg}\` | ${fmt(row.r)} | ${row.need ? fmt(row.need) : '—'} | ${verdict} |`)
  }
  md.push('')
}
md.push(failed ? `**Не прошли проверку: ${failed}.**` : 'Все нормируемые пары проходят уровень AA.', '')

if (process.argv.includes('--write')) {
  const out = fileURLToPath(new URL('./contrast.md', import.meta.url))
  writeFileSync(out, md.join('\n'), 'utf8')
  console.log('Записано:', out)
}
for (const { name, rows } of results) {
  for (const r of rows) if (r.ok === false) console.log(`НЕ ПРОШЛО  ${name} | ${r.group} | ${r.what}: ${fmt(r.r)} (нужно ${r.need})  ${r.fg} на ${r.bg}`)
}
console.log(failed ? `Провалов: ${failed}` : 'Все пары проходят AA')
process.exitCode = failed ? 1 : 0
