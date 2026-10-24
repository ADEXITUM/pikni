/**
 * Тема «Пикни» для Vuetify 4.
 *
 * Палитра «склад и накладная»: бланк накладной, штемпельные чернила, сургуч,
 * дерево ящика. Обоснование каждого цвета — в brandbook.md, раздел 6.
 * Контраст проверяется скриптом check-contrast.mjs (результат — contrast.md).
 *
 * Подключение (src/app/plugins/vuetify.ts):
 *
 *   import { createVuetify } from 'vuetify'
 *   import { theme, defaults } from '@/shared/brand/vuetify-theme'
 *   import '@/shared/brand/brand.css'
 *   export default createVuetify({ theme, defaults })
 */
import type { ThemeDefinition, VuetifyOptions } from 'vuetify'

/** Сырые цвета бренда. В компонентах используются только роли темы, не эти значения. */
export const brand = {
  /** Бланк: фон приложения — холодная серая бумага, как копия накладной. */
  blank: '#EEF1F5',
  /** Лист: карточки, таблицы, поля ввода — белая бумага. */
  paper: '#FFFFFF',
  /** Серый бланк: заливки при наведении, фон «своей точки» в матрице. */
  blank200: '#E2E7EE',
  /** Чернила: основной текст. Сине-чёрный, мягче чистого #000. */
  ink: '#1A2029',
  /** Штемпельная синь: основной цвет действий. Цвет печати на документе. */
  stamp: '#2D3A8C',
  /** Сургуч: ошибки, «стоп», отмена (как «красное сторно» в бухгалтерии). */
  wax: '#A3302B',
  /** Дерево ящика: иллюстрации (веник, планшет, ноги персонажа), акцент «своей точки». */
  wood: '#E6BF88',
  woodDark: '#7A4A1F',
  /** Ночная смена: фон тёмной темы, холодный тёмно-синий. */
  night: '#0E1218',
} as const

export const pikniLight: ThemeDefinition = {
  dark: false,
  colors: {
    background: brand.blank,
    surface: brand.paper,
    'surface-bright': '#FFFFFF',
    'surface-light': brand.blank200,
    'surface-variant': '#2A313C',
    'on-surface-variant': brand.blank,
    'on-background': brand.ink,
    'on-surface': brand.ink,

    primary: brand.stamp,
    'primary-darken-1': '#232E70',
    'on-primary': '#FFFFFF',
    secondary: brand.woodDark,
    'on-secondary': '#FFFFFF',

    success: '#1E6A3A',
    'on-success': '#FFFFFF',
    warning: '#875B00',
    'on-warning': '#FFFFFF',
    error: brand.wax,
    'on-error': '#FFFFFF',
    info: '#0C6170',
    'on-info': '#FFFFFF',

    // Статусы документов (п. 4.4.5 ТЗ: единые цвет + значок + слово)
    'status-draft': '#4B5563',
    'on-status-draft': '#FFFFFF',
    'status-progress': brand.stamp,
    'on-status-progress': '#FFFFFF',
    'status-pending': '#875B00',
    'on-status-pending': '#FFFFFF',
    'status-posted': '#1E6A3A',
    'on-status-posted': '#FFFFFF',
    'status-cancelled': brand.wax,
    'on-status-cancelled': '#FFFFFF',
    'status-transit': '#0C6170',
    'on-status-transit': '#FFFFFF',

    // Алкогольная специфика: акцизные марки и ограничения продажи
    'mark-ok': '#1E6A3A',
    'on-mark-ok': '#FFFFFF',
    'mark-off-balance': brand.wax,
    'on-mark-off-balance': '#FFFFFF',
    'sale-blocked': brand.wax,
    'on-sale-blocked': '#FFFFFF',

    // Бренд для иллюстраций и персонажа
    wood: brand.wood,
    'on-wood': brand.ink,
  },
  variables: {
    'border-color': '#2E3A4A',
    'border-opacity': 0.2,
    'high-emphasis-opacity': 0.92,
    'medium-emphasis-opacity': 0.74,
    'disabled-opacity': 0.42,
    'hover-opacity': 0.06,
    'focus-opacity': 0.14,
    'selected-opacity': 0.1,
    'activated-opacity': 0.12,
    'pressed-opacity': 0.16,
    'theme-code': brand.blank200,
    'theme-on-code': brand.ink,
    'theme-kbd': brand.blank200,
    'theme-on-kbd': brand.ink,
    'shadow-color': '#1A2029',
  },
}

export const pikniDark: ThemeDefinition = {
  dark: true,
  colors: {
    background: brand.night,
    surface: '#161B23',
    'surface-bright': '#2A313C',
    'surface-light': '#1F2630',
    'surface-variant': '#E4E8EE',
    'on-surface-variant': '#161B23',
    'on-background': '#E8ECF1',
    'on-surface': '#E8ECF1',

    primary: '#AEB8FF',
    'primary-darken-1': '#8F9BF0',
    'on-primary': '#12174A',
    secondary: '#E2B47E',
    'on-secondary': '#2B1A08',

    success: '#8BD6A2',
    'on-success': '#0A2E17',
    warning: '#F1C462',
    'on-warning': '#2E2000',
    error: '#FF9B8F',
    'on-error': '#4D0D08',
    info: '#7DD1DD',
    'on-info': '#05292F',

    'status-draft': '#C3CAD4',
    'on-status-draft': '#161B23',
    'status-progress': '#AEB8FF',
    'on-status-progress': '#12174A',
    'status-pending': '#F1C462',
    'on-status-pending': '#2E2000',
    'status-posted': '#8BD6A2',
    'on-status-posted': '#0A2E17',
    'status-cancelled': '#FF9B8F',
    'on-status-cancelled': '#4D0D08',
    'status-transit': '#7DD1DD',
    'on-status-transit': '#05292F',

    'mark-ok': '#8BD6A2',
    'on-mark-ok': '#0A2E17',
    'mark-off-balance': '#FF9B8F',
    'on-mark-off-balance': '#4D0D08',
    'sale-blocked': '#FF9B8F',
    'on-sale-blocked': '#4D0D08',

    wood: brand.wood,
    'on-wood': brand.ink,
  },
  variables: {
    'border-color': '#E8ECF1',
    'border-opacity': 0.18,
    'high-emphasis-opacity': 0.96,
    'medium-emphasis-opacity': 0.76,
    'disabled-opacity': 0.48,
    'hover-opacity': 0.08,
    'focus-opacity': 0.16,
    'selected-opacity': 0.12,
    'activated-opacity': 0.16,
    'pressed-opacity': 0.2,
    'theme-code': '#1F2630',
    'theme-on-code': '#E8ECF1',
    'theme-kbd': '#1F2630',
    'theme-on-kbd': '#E8ECF1',
    'shadow-color': '#000000',
  },
}

/**
 * Темы называются light и dark: так их находит режим 'system'
 * (в Vuetify 4 он включён по умолчанию и следует настройке ОС).
 * Переключатель в профиле: theme.change('light' | 'dark' | 'system').
 */
export const theme: VuetifyOptions['theme'] = {
  defaultTheme: 'system',
  variations: false,
  themes: {
    light: pikniLight,
    dark: pikniDark,
  },
}

/**
 * Глобальные умолчания компонентов.
 * Принципы: без теней и ряби (это подпись Material), рамка вместо тени,
 * крупные цели касания, спокойная плотность таблиц (пользователь просил
 * не делать мелкий текст и плотные таблицы).
 */
export const defaults: VuetifyOptions['defaults'] = {
  global: {
    ripple: false,
  },
  VBtn: {
    variant: 'flat',
    rounded: 'lg',
    class: 'pk-btn',
  },
  VCard: {
    flat: true,
    border: true,
    rounded: 'lg',
  },
  VSheet: {
    rounded: 'lg',
  },
  VTextField: { variant: 'outlined', density: 'comfortable', rounded: 'lg', color: 'primary', hideDetails: 'auto' },
  VTextarea: { variant: 'outlined', density: 'comfortable', rounded: 'lg', color: 'primary', hideDetails: 'auto' },
  VSelect: { variant: 'outlined', density: 'comfortable', rounded: 'lg', color: 'primary', hideDetails: 'auto' },
  VAutocomplete: { variant: 'outlined', density: 'comfortable', rounded: 'lg', color: 'primary', hideDetails: 'auto' },
  VCombobox: { variant: 'outlined', density: 'comfortable', rounded: 'lg', color: 'primary', hideDetails: 'auto' },
  VNumberInput: { variant: 'outlined', density: 'comfortable', rounded: 'lg', color: 'primary', hideDetails: 'auto', controlVariant: 'split' },
  VCheckbox: { color: 'primary', density: 'comfortable', hideDetails: 'auto' },
  VSwitch: { color: 'primary', density: 'comfortable', hideDetails: 'auto' },
  VRadioGroup: { color: 'primary', density: 'comfortable', hideDetails: 'auto' },
  VChip: {
    rounded: 'md',
    variant: 'tonal',
  },
  VDataTable: { density: 'comfortable', hover: true },
  VDataTableServer: { density: 'comfortable', hover: true },
  VList: { density: 'comfortable' },
  VAlert: { variant: 'tonal', border: 'start', rounded: 'lg' },
  VAppBar: { flat: true, border: 'b', color: 'surface' },
  VNavigationDrawer: { border: 'e', color: 'surface' },
  VBottomNavigation: { grow: true, height: 64, border: 't', elevation: 0, color: 'primary' },
  VTabs: { color: 'primary' },
  VProgressLinear: { color: 'primary', height: 6, rounded: true },
  VSnackbar: { rounded: 'lg', location: 'top' },
  VDialog: { maxWidth: 560 },
  VTooltip: { location: 'top' },
}

/** Как показывать статус: цвет + значок + слово (никогда только цвет). */
export interface StatusMeta {
  label: string
  icon: string
  color: string
}

/** Семейства статусов документов. Конкретные статусы из ТЗ (п. 4.2.3) сводятся к ним. */
export const documentStatus = {
  draft: { label: 'Черновик', icon: 'mdi-pencil-outline', color: 'status-draft' },
  progress: { label: 'В работе', icon: 'mdi-progress-clock', color: 'status-progress' },
  pending: { label: 'На утверждении', icon: 'mdi-timer-sand', color: 'status-pending' },
  posted: { label: 'Проведён', icon: 'mdi-check-decagram-outline', color: 'status-posted' },
  cancelled: { label: 'Отменён', icon: 'mdi-cancel', color: 'status-cancelled' },
  transit: { label: 'В пути', icon: 'mdi-truck-fast-outline', color: 'status-transit' },
} as const satisfies Record<string, StatusMeta>

/** Статусы акцизных марок и ограничений продажи. */
export const markStatus = {
  ok: { label: 'Марка проверена', icon: 'mdi-shield-check-outline', color: 'mark-ok' },
  offBalance: { label: 'Марка не на балансе', icon: 'mdi-shield-alert-outline', color: 'mark-off-balance' },
  saleBlocked: { label: 'Продажа запрещена по времени', icon: 'mdi-clock-alert-outline', color: 'sale-blocked' },
} as const satisfies Record<string, StatusMeta>

/**
 * Карточка последнего скана. Тяжесть передаётся ещё и вариантом:
 * tonal — можно продолжать, flat (сплошная заливка) — «стоп», нужно действие.
 * Звук и вибрация — в sounds/pikni-sounds.js (события с теми же ключами).
 */
export const scanFeedback = {
  accepted: { label: 'Принято', icon: 'mdi-check-circle-outline', color: 'success', variant: 'tonal' },
  notInPlan: { label: 'Не по плану', icon: 'mdi-plus-circle-outline', color: 'warning', variant: 'tonal' },
  unknown: { label: 'Код не найден', icon: 'mdi-help-circle-outline', color: 'error', variant: 'tonal' },
  stop: { label: 'Стоп', icon: 'mdi-octagon-outline', color: 'error', variant: 'flat' },
} as const
