import { mkdir, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'

import { writeFakeCamera } from './fake-camera'

// Перед тестами: видео для камеры и вход под администратором стенда. Первый вход по
// временному паролю требует сменить пароль — это делает setup, а не каждый тест.
// E2E_TEMP_PASSWORD — временный пароль из «pikni-admin create-admin», если вход первый.
export default async function globalSetup() {
  const here = import.meta.dirname
  // Штрихкод 0,5 л «Тайги» из демо-каталога (api/db/seed/catalog.csv).
  await writeFakeCamera(resolve(here, '.cache/camera.y4m'), '2900000000025', 'EAN13')

  const api = process.env.PIKNI_API_URL ?? 'http://localhost:8080'
  const login = process.env.E2E_LOGIN ?? 'e2e'
  const password = process.env.E2E_PASSWORD ?? 'e2e-пароль-надёжный'

  const signIn = (pass: string) =>
    fetch(`${api}/api/v1/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ login, password: pass }),
    })

  let response = await signIn(password)
  if (response.status === 401 && process.env.E2E_TEMP_PASSWORD) {
    const first = await signIn(process.env.E2E_TEMP_PASSWORD)
    if (!first.ok) throw new Error(`e2e: temporary password rejected: ${first.status}`)
    const me = (await first.json()) as { csrfToken: string }
    const changed = await fetch(`${api}/api/v1/auth/password`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': me.csrfToken,
        Cookie: cookieOf(first),
      },
      body: JSON.stringify({
        currentPassword: process.env.E2E_TEMP_PASSWORD,
        newPassword: password,
      }),
    })
    if (!changed.ok) throw new Error(`e2e: password change failed: ${changed.status}`)
    response = await signIn(password)
  }
  if (!response.ok)
    throw new Error(`e2e: sign-in failed: ${response.status}. Set E2E_TEMP_PASSWORD`)

  // Cookie сессии ставится на localhost без порта, поэтому подходит и для Vite на другом порту;
  // флаг Secure Chrome принимает и на http://localhost.
  const [name, value] = cookieOf(response).split('=') as [string, string]
  const state = {
    cookies: [
      {
        name,
        value,
        domain: 'localhost',
        path: '/api',
        expires: -1,
        httpOnly: true,
        secure: true,
        sameSite: 'Lax',
      },
    ],
    origins: [],
  }
  await mkdir(resolve(here, '.auth'), { recursive: true })
  await writeFile(resolve(here, '.auth/admin.json'), JSON.stringify(state))
}

function cookieOf(response: Response): string {
  const header = response.headers.getSetCookie()[0]
  if (!header) throw new Error('e2e: no session cookie')
  return header.split(';')[0]!
}
