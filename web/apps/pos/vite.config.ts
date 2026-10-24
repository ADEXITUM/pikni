import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import vuetify from 'vite-plugin-vuetify'

// Касса открывается по адресу /pos/ того же сервера, что и основное приложение: в разработке
// сервер Vite основного приложения проксирует /pos/ сюда.
export default defineConfig({
  base: '/pos/',
  plugins: [vue(), vuetify()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  optimizeDeps: {
    exclude: ['vuetify'],
  },
  worker: {
    format: 'es',
  },
  server: {
    port: 5174,
    strictPort: true,
    proxy: {
      '/api': process.env.PIKNI_API_URL ?? 'http://localhost:8080',
    },
  },
})
