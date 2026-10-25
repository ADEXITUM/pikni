import { defineConfig, devices } from '@playwright/test'

// Сценарии на демо-данных (решение 0063): база с демо-сетью, учётными записями (seed-users) и
// применёнными сценариями (pikni-admin scenario all). Каждый тест входит под своей ролью и
// продолжает сценарий так, как это сделал бы сотрудник.
const port = 5197

export default defineConfig({
  testDir: 'e2e-scenarios',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: `http://localhost:${port}`,
    locale: 'ru-RU',
    testIdAttribute: 'data-test',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `npx vite build && npx vite preview --port ${port} --strictPort`,
    timeout: 180_000,
    url: `http://localhost:${port}`,
    reuseExistingServer: !process.env.CI,
    env: { PIKNI_API_URL: process.env.PIKNI_API_URL ?? 'http://localhost:8080' },
  },
})
