import type { Me } from '@pikni/api'
import { QueryClient } from '@tanstack/vue-query'
import { describe, expect, it, vi } from 'vitest'
import type { RouteLocationNormalized, Router } from 'vue-router'

import { meQuery, safeRedirect, sessionGuard, sessionMiddleware } from '../session'

const me: Me = {
  id: '01a16948-042f-7e7d-bec4-42c1e6861f9d',
  login: 'admin',
  fullName: 'Администратор',
  role: 'admin',
  mustChangePassword: false,
  csrfToken: 'csrf-1',
  permissions: ['users.manage'],
  locationIds: [],
}

function clientWith(value: Me | null) {
  const client = new QueryClient()
  client.setQueryData(meQuery.queryKey, value)
  return client
}

function to(fullPath: string, extra: Partial<RouteLocationNormalized> = {}) {
  return { fullPath, path: fullPath, query: {}, meta: {}, ...extra } as RouteLocationNormalized
}

describe('safeRedirect', () => {
  it('пропускает только пути внутри приложения', () => {
    expect(safeRedirect('/audit?page=2')).toBe('/audit?page=2')
    expect(safeRedirect('//evil.example')).toBe('/')
    expect(safeRedirect('/\\evil.example')).toBe('/')
    expect(safeRedirect('https://evil.example')).toBe('/')
    expect(safeRedirect(undefined)).toBe('/')
  })
})

describe('sessionGuard', () => {
  it('без сессии отправляет на вход и запоминает страницу', async () => {
    expect(await sessionGuard(clientWith(null), to('/audit?page=2'))).toEqual({
      name: 'login',
      query: { redirect: '/audit?page=2' },
    })
  })

  it('с временным паролем пускает только на смену пароля', async () => {
    const client = clientWith({ ...me, mustChangePassword: true })
    expect(await sessionGuard(client, to('/audit'))).toEqual({
      name: 'change-password',
      query: { redirect: '/audit' },
    })
    expect(await sessionGuard(client, to('/change-password', { name: 'change-password' }))).toBe(
      true,
    )
  })

  it('вошедшего со страницы входа возвращает туда, откуда он пришёл', async () => {
    const login = to('/login', { meta: { public: true }, query: { redirect: '/audit' } })
    expect(await sessionGuard(clientWith(me), login)).toBe('/audit')
    expect(await sessionGuard(clientWith(null), login)).toBe(true)
  })
})

describe('sessionMiddleware', () => {
  function setup() {
    const client = clientWith(me)
    const replace = vi.fn<Router['replace']>()
    const router = { replace, currentRoute: { value: { fullPath: '/audit' } } } as unknown as Router
    return { client, replace, middleware: sessionMiddleware(client, router) }
  }

  it('добавляет CSRF-токен только к изменяющим запросам', async () => {
    const { middleware } = setup()
    const post = new Request('http://localhost/api/v1/auth/logout', { method: 'POST' })
    const get = new Request('http://localhost/api/v1/locations')

    await middleware.onRequest?.({ request: post } as never)
    await middleware.onRequest?.({ request: get } as never)

    expect(post.headers.get('X-CSRF-Token')).toBe('csrf-1')
    expect(get.headers.get('X-CSRF-Token')).toBeNull()
  })

  it('при 401 забывает сессию и ведёт на вход', async () => {
    const { client, replace, middleware } = setup()

    await middleware.onResponse?.({
      response: new Response(null, { status: 401 }),
      schemaPath: '/locations',
    } as never)

    expect(client.getQueryData(meQuery.queryKey)).toBeUndefined()
    expect(replace).toHaveBeenCalledWith({ name: 'login', query: { redirect: '/audit' } })
  })

  it('не трогает 401 от формы входа: это неверный пароль', async () => {
    const { client, replace, middleware } = setup()

    await middleware.onResponse?.({
      response: new Response(null, { status: 401 }),
      schemaPath: '/auth/login',
    } as never)

    expect(client.getQueryData(meQuery.queryKey)).toEqual(me)
    expect(replace).not.toHaveBeenCalled()
  })
})
