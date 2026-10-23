import { createRouter, createWebHistory } from 'vue-router'

import { queryClient } from '@/query'
import { sessionGuard } from '@/session'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    // Страница доступна без входа.
    public?: boolean
    // Страница без шапки приложения.
    bare?: boolean
  }
}

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    { path: '/', redirect: '/locations' },
    {
      path: '/login',
      name: 'login',
      component: () => import('@/pages/LoginPage.vue'),
      meta: { title: 'Вход', public: true, bare: true },
    },
    {
      path: '/change-password',
      name: 'change-password',
      component: () => import('@/pages/ChangePasswordPage.vue'),
      meta: { title: 'Смена пароля', bare: true },
    },
    {
      path: '/locations',
      name: 'locations',
      component: () => import('@/pages/LocationsPage.vue'),
      meta: { title: 'Точки сети' },
    },
  ],
})

router.beforeEach((to) => sessionGuard(queryClient, to))

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} — Пикни` : 'Пикни'
})

export default router
