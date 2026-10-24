import { expect, test } from '@playwright/test'

const taiga = 'Водка «Тайга» классическая'

test('администратор после входа попадает в каталог', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/products$/)
})

test('скан ручным сканером на любом экране открывает «Сканер» с товаром (П-19)', async ({
  page,
}) => {
  await page.goto('/products')
  await expect(page.getByRole('heading', { name: 'Товары' })).toBeVisible()
  // Сканер «печатает» быстрее человека и заканчивает Enter; фокуса в поле нет. Нажатия
  // отправляются из страницы одной задачей: через протокол отладки паузы между ними
  // плавают и иногда длиннее, чем у настоящего сканера.
  await page.evaluate((code) => {
    for (const key of [...code, 'Enter']) {
      document.body.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }))
    }
  }, '2900000000025')

  await expect(page).toHaveURL(/\/scan$/)
  await expect(page.getByTestId('raw')).toHaveText('2900000000025')
  await expect(page.getByTestId('outcome')).toHaveText('Найден')
  await expect(page.getByTestId('name')).toHaveText(taiga)
})

test('камера читает код без нажатий, пока код в кадре — повтора нет (П-27)', async ({ page }) => {
  await page.goto('/scan')
  await page.getByTestId('camera-on').click()

  await expect(page.getByTestId('name')).toHaveText(taiga, { timeout: 15_000 })
  await expect(page.getByTestId('kind')).toContainText('камера')
  // Видео повторяет один и тот же кадр: окно подавления повторов продлевается.
  await page.waitForTimeout(2_500)
  await expect(page.getByTestId('recent')).toHaveCount(0)
})

test('правка карточки: панель «Есть несохранённые изменения» и «было — станет» перед сохранением', async ({
  page,
}) => {
  await page.goto('/products')
  await page.getByText(taiga).first().click()
  await expect(page.getByRole('heading', { name: taiga })).toBeVisible()

  await page.getByTestId('name').locator('input').fill(`${taiga} (правка)`)
  await expect(page.getByTestId('unsaved')).toBeVisible()
  await page.getByTestId('unsaved').getByTestId('save').click()

  const dialog = page.getByTestId('confirm-dialog')
  await expect(dialog.getByTestId('confirm-changes')).toContainText(`${taiga} (правка)`)
  await dialog.getByTestId('confirm-cancel').click()

  // Уйти без сохранения тоже спросят.
  await page.getByRole('link', { name: 'Сканер' }).click()
  await expect(page.getByTestId('confirm-dialog')).toContainText('Уйти без сохранения?')
  await page.getByTestId('confirm-cancel').click()
  await expect(page).toHaveURL(/\/products\//)
  await page.getByTestId('discard').click()
  await expect(page.getByTestId('unsaved')).toHaveCount(0)
})
