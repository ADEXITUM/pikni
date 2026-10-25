import { expect, test, type Page } from '@playwright/test'

// Пароль учётных записей pikni-admin seed-users.
const password = 'Пикни-демо-2026'
const api = process.env.PIKNI_API_URL ?? 'http://localhost:8080'

type Session = { cookie: string; csrf: string }
type Doc = { id: string; kind: string; status: string; number: string; ttnNumber?: string }

async function signIn(page: Page, login: string): Promise<Session> {
  const r = await fetch(`${api}/api/v1/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ login, password }),
  })
  expect(r.status, `вход ${login}`).toBe(200)
  const cookie = r.headers.getSetCookie()[0]!.split(';')[0]!
  const [name, value] = cookie.split('=') as [string, string]
  await page.context().addCookies([
    {
      name,
      value,
      domain: 'localhost',
      path: '/api',
      httpOnly: true,
      secure: true,
      sameSite: 'Lax',
    },
  ])
  const me = (await r.json()) as { csrfToken: string }
  return { cookie, csrf: me.csrfToken }
}

async function get<T>(s: Session, path: string): Promise<T> {
  const r = await fetch(`${api}/api/v1${path}`, { headers: { Cookie: s.cookie } })
  expect(r.ok, path).toBeTruthy()
  return (await r.json()) as T
}

async function findDoc(s: Session, kind: string, status: string): Promise<Doc> {
  const { items } = await get<{ items: Doc[] }>(s, `/documents?kind=${kind}`)
  const doc = items.find((d) => d.status === status)
  expect(doc, `${kind} в статусе ${status}`).toBeTruthy()
  return doc!
}

// confirm — подтверждение в окне «Вы уверены?», если действие его спрашивает.
async function confirm(page: Page) {
  const ok = page.getByTestId('confirm-ok')
  await ok.waitFor({ timeout: 2000 }).catch(() => undefined)
  if (await ok.isVisible()) await ok.click()
}

async function statusOf(s: Session, id: string): Promise<string> {
  return (await get<Doc>(s, `/documents/${id}`)).status
}

test('ревизия в черновике: управляющий начинает подсчёт', async ({ page }) => {
  const s = await signIn(page, 'upr')
  const doc = await findDoc(s, 'stocktake', 'draft')
  await page.goto(`/documents/${doc.id}`)
  await page.getByTestId('start').click()
  await confirm(page)
  await expect.poll(() => statusOf(s, doc.id)).toBe('counting')
  await expect(page.getByTestId('zone')).toHaveCount(2)
})

test('идёт полная ревизия: склад закрыт, зал досчитывается при открытом магазине', async ({
  page,
}) => {
  const s = await signIn(page, 'upr-mg02')
  const doc = await findDoc(s, 'stocktake', 'counting')
  await page.goto(`/documents/${doc.id}`)
  await expect(page.getByTestId('zone')).toHaveCount(2)
  await page.getByTestId('zone').filter({ hasText: 'зал' }).click()
  await expect(page.getByTestId('close-zone')).toBeVisible()
  await page.getByTestId('close-zone').click()
  await confirm(page)
  await page.getByText('Все зоны').click()
  await expect(page.getByTestId('reconcile')).toBeVisible()
})

test('сличение коньяка и виски: не найденные, переместившиеся и чужая марки', async ({ page }) => {
  const s = await signIn(page, 'upr-mg03')
  const doc = await findDoc(s, 'stocktake', 'reconciling')
  await page.goto(`/documents/${doc.id}`)
  await expect(page.getByTestId('sheet-summary')).toBeVisible()
  await expect(page.getByTestId('sheet-marks').locator('.v-list-item')).not.toHaveCount(0)
  await expect(page.getByTestId('submit')).toBeVisible()
})

test('директор утверждает ревизию пива с недостачей', async ({ page }) => {
  const s = await signIn(page, 'director')
  const doc = await findDoc(s, 'stocktake', 'pending_approval')
  await page.goto(`/documents/${doc.id}`)
  await page.getByTestId('approve').click()
  await confirm(page)
  await expect.poll(() => statusOf(s, doc.id)).not.toBe('pending_approval')
})

test('бой ждёт второго ключа: директор утверждает списание', async ({ page }) => {
  const s = await signIn(page, 'director')
  const doc = await findDoc(s, 'writeoff', 'pending_approval')
  await page.goto(`/documents/${doc.id}`)
  await page.getByTestId('approve').click()
  await confirm(page)
  await expect.poll(() => statusOf(s, doc.id)).not.toBe('pending_approval')
})

test('ЕГАИС отклонила ТТН: склад возвращает товар себе', async ({ page }) => {
  const s = await signIn(page, 'keeper-sk02')
  const doc = await findDoc(s, 'transfer', 'egais_rejected')
  await page.goto(`/documents/${doc.id}`)
  await expect(page.getByTestId('transfer-rejected')).toBeVisible()
  await page.getByTestId('return-transfer').click()
  await confirm(page)
  await expect.poll(() => statusOf(s, doc.id)).toBe('cancelled')
})

test('заявка магазина: склад согласует', async ({ page }) => {
  const s = await signIn(page, 'keeper-sk01')
  const doc = await findDoc(s, 'transfer', 'requested')
  await page.goto(`/documents/${doc.id}`)
  await page.getByTestId('approve').click()
  await confirm(page)
  await expect.poll(() => statusOf(s, doc.id)).toBe('draft')
})

test('перемещение в пути: магазин видит поступление к приёмке', async ({ page }) => {
  const s = await signIn(page, 'keeper')
  const { items } = await get<{ items: Doc[] }>(s, '/documents?kind=receipt')
  const receipt = items.find((d) => d.status === 'draft' && d.ttnNumber?.startsWith('ПМ-'))
  expect(receipt, 'поступление по перемещению').toBeTruthy()
  await page.goto(`/documents/${receipt!.id}`)
  await expect(page.getByText(receipt!.ttnNumber!).first()).toBeVisible()
  await expect(page.getByTestId('lines')).toBeVisible()
})

test('бронь покупателя видна кассиру', async ({ page }) => {
  await signIn(page, 'kassir')
  await page.goto('/reservations')
  await expect(page.getByTestId('reservation').filter({ hasText: 'Андрей' })).toBeVisible()
})

test('ТТН с расхождениями ждёт приёмки на складе', async ({ page }) => {
  const s = await signIn(page, 'keeper-sk02')
  const doc = await findDoc(s, 'receipt', 'draft')
  await page.goto(`/documents/${doc.id}`)
  await expect(page.getByTestId('ttn')).toBeVisible()
})

test('сверка с ЕГАИС показывает расхождение по пиву', async ({ page }) => {
  await signIn(page, 'upr')
  await page.goto('/egais/reconciliation')
  await expect(page.getByTestId('snapshot')).toBeVisible()
  await expect(page.getByTestId('products')).toBeVisible()
})
