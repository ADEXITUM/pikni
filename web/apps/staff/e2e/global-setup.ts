import { createHmac } from 'node:crypto'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'

import { writeFakeCamera } from './fake-camera'

// Перед тестами: видео для камеры и вход под администратором стенда. Первый вход по
// временному паролю требует сменить пароль, а администратору — подключить второй фактор: это
// делает setup, а не каждый тест. E2E_TEMP_PASSWORD — временный пароль из
// «pikni-admin create-admin», если вход первый.
export default async function globalSetup() {
  const here = import.meta.dirname
  // Штрихкод 0,5 л «Тайги» из демо-каталога (api/db/seed/catalog.csv).
  await writeFakeCamera(resolve(here, '.cache/camera.y4m'), '2900000000025', 'EAN13')

  const api = process.env.PIKNI_API_URL ?? 'http://localhost:8080'
  const login = process.env.E2E_LOGIN ?? 'e2e'
  const password = process.env.E2E_PASSWORD ?? 'e2e-пароль-надёжный'
  const secretFile = resolve(here, '.auth/totp-secret')

  const post = (path: string, body: unknown, headers: Record<string, string> = {}) =>
    fetch(`${api}/api/v1${path}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...headers },
      body: JSON.stringify(body),
    })
  const signIn = (pass: string) => post('/auth/login', { login, password: pass })

  let response = await signIn(password)
  if (response.status === 401 && process.env.E2E_TEMP_PASSWORD) {
    const first = await signIn(process.env.E2E_TEMP_PASSWORD)
    if (!first.ok) throw new Error(`e2e: temporary password rejected: ${first.status}`)
    const me = (await first.json()) as { csrfToken: string }
    const changed = await post(
      '/auth/password',
      { currentPassword: process.env.E2E_TEMP_PASSWORD, newPassword: password },
      { 'X-CSRF-Token': me.csrfToken, Cookie: cookieOf(first) },
    )
    if (!changed.ok) throw new Error(`e2e: password change failed: ${changed.status}`)
    response = await signIn(password)
  }
  if (response.status === 202) {
    const { challenge } = (await response.json()) as { challenge: string }
    const secret = (await readFile(secretFile, 'utf8')).trim()
    response = await post('/auth/login/totp', { challenge, code: totp(secret) })
  }
  if (!response.ok)
    throw new Error(`e2e: sign-in failed: ${response.status}. Set E2E_TEMP_PASSWORD`)

  const me = (await response.clone().json()) as { csrfToken: string; mustEnrollTotp?: boolean }
  if (me.mustEnrollTotp) {
    const headers = { 'X-CSRF-Token': me.csrfToken, Cookie: cookieOf(response) }
    const setup = (await (await post('/auth/totp/setup', {}, headers)).json()) as { secret: string }
    const enabled = await post('/auth/totp/enable', { code: totp(setup.secret) }, headers)
    if (!enabled.ok) throw new Error(`e2e: two-factor setup failed: ${enabled.status}`)
    await mkdir(resolve(here, '.auth'), { recursive: true })
    await writeFile(secretFile, setup.secret)
  }

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

// totp — одноразовый код RFC 6238 (30 с, 6 цифр, SHA-1) из секрета в base32.
function totp(secret: string, now = Date.now()): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const c of secret.replace(/=+$/, '').toUpperCase())
    bits += alphabet.indexOf(c).toString(2).padStart(5, '0')
  const key = Buffer.from(bits.match(/.{8}/g)!.map((b) => parseInt(b, 2)))
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(now / 30000)))
  const h = createHmac('sha1', key).update(counter).digest()
  const off = h[h.length - 1]! & 0xf
  return String((h.readUInt32BE(off) & 0x7fffffff) % 1_000_000).padStart(6, '0')
}
