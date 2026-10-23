import 'vuetify/styles'
import '@fontsource-variable/manrope'
import '@fontsource/jetbrains-mono/500.css'
import './brand.css'

import { createVuetify } from 'vuetify'
import { aliases, mdi } from 'vuetify/iconsets/mdi-svg'
import { en, ru } from 'vuetify/locale'

import { defaults, theme } from './theme'

export { brand } from './theme'

export function createPikniVuetify() {
  return createVuetify({
    theme,
    defaults,
    icons: { defaultSet: 'mdi', aliases, sets: { mdi } },
    locale: { locale: 'ru', fallback: 'en', messages: { ru, en } },
  })
}
