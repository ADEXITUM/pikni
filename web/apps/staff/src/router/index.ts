import { createRouter, createWebHistory } from 'vue-router'

import { queryClient } from '@/query'
import { sessionGuard } from '@/session'

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
      meta: { title: 'Точки сети', permission: 'refs.view' },
    },
    {
      path: '/locations/new',
      name: 'location-new',
      component: () => import('@/pages/LocationPage.vue'),
      meta: { title: 'Новая точка', permission: 'refs.edit' },
    },
    {
      path: '/locations/:id',
      name: 'location',
      component: () => import('@/pages/LocationPage.vue'),
      meta: { title: 'Точка', permission: 'refs.view' },
    },
    {
      path: '/users',
      name: 'users',
      component: () => import('@/pages/UsersPage.vue'),
      meta: { title: 'Сотрудники', permission: 'users.manage' },
    },
    {
      path: '/audit',
      name: 'audit',
      component: () => import('@/pages/AuditPage.vue'),
      meta: { title: 'Журнал действий', permission: 'audit.view' },
    },
    {
      path: '/profile',
      name: 'profile',
      component: () => import('@/pages/ProfilePage.vue'),
      meta: { title: 'Профиль' },
    },
    {
      path: '/forbidden',
      name: 'forbidden',
      component: () => import('@/pages/ForbiddenPage.vue'),
      meta: { title: 'Нет доступа' },
    },
  ],
})

router.beforeEach((to) => sessionGuard(queryClient, to))

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} — Пикни` : 'Пикни'
})

export default router
