import { resolve } from 'node:path'

import { defineConfig, devices } from '@playwright/test'

// Сквозные сценарии ТЗ (раздел 7) в настоящем браузере. Нужен запущенный сервер с базой:
// локально — task dev, в CI — шаг перед тестами. Фронтенд для тестов собирается и
// поднимается сам.
const port = 5199
const camera = resolve(import.meta.dirname, 'e2e/.cache/camera.y4m')

export default defineConfig({
  testDir: 'e2e',
  globalSetup: './e2e/global-setup.ts',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: `http://localhost:${port}`,
    storageState: 'e2e/.auth/admin.json',
    locale: 'ru-RU',
    testIdAttribute: 'data-test',
    trace: 'retain-on-failure',
    // Камера подменяется видеофайлом с кодами, который готовит global-setup.
    launchOptions: {
      args: [
        '--use-fake-ui-for-media-stream',
        '--use-fake-device-for-media-stream',
        `--use-file-for-fake-video-capture=${camera}`,
      ],
    },
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    // Собранная версия, а не сервер разработки: тот при первом запуске перезагружает страницу,
    // когда оптимизирует зависимости, и тест может попасть на перезагрузку.
    command: `npx vite build && npx vite preview --port ${port} --strictPort`,
    timeout: 180_000,
    url: `http://localhost:${port}`,
    reuseExistingServer: !process.env.CI,
    env: { PIKNI_API_URL: process.env.PIKNI_API_URL ?? 'http://localhost:8080' },
  },
})
