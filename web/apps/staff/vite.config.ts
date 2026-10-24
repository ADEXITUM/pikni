import { fileURLToPath, URL } from 'node:url'

import basicSsl from '@vitejs/plugin-basic-ssl'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import vueDevTools from 'vite-plugin-vue-devtools'
import vuetify from 'vite-plugin-vuetify'

// PIKNI_HTTPS=1 — HTTPS с самоподписанным сертификатом и доступ из локальной сети: камера
// в браузере телефона работает только в защищённом контексте.
const https = process.env.PIKNI_HTTPS === '1'

export default defineConfig({
  plugins: [vue(), vuetify(), vueDevTools(), ...(https ? [basicSsl()] : [])],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  // Компоненты Vuetify подключаются по мере использования. Если их предварительно собирать,
  // Vite находит новый компонент только при первом открытии страницы и перезагружает её.
  optimizeDeps: {
    exclude: ['vuetify'],
  },
  // Распознавание кодов работает в модульном потоке (Web Worker) пакета сканера.
  worker: {
    format: 'es',
  },
  server: {
    host: https || undefined,
    proxy: {
      '/api': process.env.PIKNI_API_URL ?? 'http://localhost:8080',
      // Учебная касса (apps/pos) — на том же адресе, что и основное приложение: ссылка из QR-кода
      // регистрации кассы открывает её без настройки.
      '^/pos(/|$)': { target: process.env.PIKNI_POS_URL ?? 'http://localhost:5174', ws: true },
    },
  },
})
