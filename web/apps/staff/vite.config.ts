import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import vueDevTools from 'vite-plugin-vue-devtools'
import vuetify from 'vite-plugin-vuetify'

export default defineConfig({
  plugins: [vue(), vuetify(), vueDevTools()],
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
  server: {
    proxy: {
      '/api': process.env.PIKNI_API_URL ?? 'http://localhost:8080',
    },
  },
})
