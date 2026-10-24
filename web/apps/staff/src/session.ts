import { api, ApiError, type Me, type Middleware, type Permission } from '@pikni/api'
import { queryOptions, useQuery, useQueryClient, type QueryClient } from '@tanstack/vue-query'
import {
  useRouter,
  type RouteLocationNormalized,
  type RouteLocationRaw,
  type RouteMeta,
  type Router,
} from 'vue-router'

import { networkRoles } from '@/labels'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    // Страница доступна без входа.
    public?: boolean
    // Страница без шапки приложения.
    bare?: boolean
    // Право, без которого раздел не открывается и не виден в меню.
    permission?: Permission
  }
}

// Кто вошёл — это данные сервера, поэтому они живут в кеше запросов, как любые другие.
// null — сессии нет. Перезапрашиваются только явно: при входе, выходе и смене пароля.
export const meQuery = queryOptions({
  queryKey: ['me'],
  queryFn: async (): Promise<Me | null> => {
    const { data, error, response } = await api.GET('/auth/me')
    if (response.status === 401) return null
    if (error) throw new ApiError(error)
    return data
  },
  staleTime: Infinity,
})

export function useMe() {
  return useQuery(meQuery)
}

export function useLogout() {
  const client = useQueryClient()
  const router = useRouter()
  return async () => {
    await api.POST('/auth/logout')
    client.clear()
    await router.replace({ name: 'login' })
  }
}

// homeRoute — стартовый экран: у сотрудников магазина — сканер, у ролей сети — каталог.
// Без сессии guard и так уведёт на вход.
export function homeRoute(me: Me | null): 'scan' | 'products' {
  return me && networkRoles.has(me.role) ? 'products' : 'scan'
}

// Только путь внутри приложения: ссылка вида /login?redirect=//evil.example не уведёт на чужой сайт.
export function safeRedirect(value: unknown): string {
  const ok =
    typeof value === 'string' &&
    value.startsWith('/') &&
    !value.startsWith('//') &&
    !value.startsWith('/\\')
  return ok ? value : '/'
}

export function canOpen(me: Me, to: { meta: RouteMeta }): boolean {
  return !to.meta.permission || me.permissions.includes(to.meta.permission)
}

export async function sessionGuard(
  client: QueryClient,
  to: RouteLocationNormalized,
): Promise<RouteLocationRaw | true> {
  const me = await client.ensureQueryData(meQuery).catch(() => null)

  if (to.meta.public) {
    return me ? safeRedirect(to.query.redirect) : true
  }
  if (!me) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (me.mustChangePassword && to.name !== 'change-password') {
    return { name: 'change-password', query: { redirect: to.fullPath } }
  }
  if (!canOpen(me, to)) {
    return { name: 'forbidden' }
  }
  return true
}

const safeMethods = new Set(['GET', 'HEAD', 'OPTIONS'])
const sessionlessPaths = new Set(['/auth/login', '/auth/me'])

export function sessionMiddleware(client: QueryClient, router: Router): Middleware {
  return {
    onRequest({ request }) {
      const token = client.getQueryData(meQuery.queryKey)?.csrfToken
      if (token && !safeMethods.has(request.method)) {
        request.headers.set('X-CSRF-Token', token)
      }
      return request
    },
    async onResponse({ response, schemaPath }) {
      if (response.status === 401 && !sessionlessPaths.has(schemaPath)) {
        client.clear()
        await router.replace({
          name: 'login',
          query: { redirect: router.currentRoute.value.fullPath },
        })
      }
      return response
    },
  }
}
