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
      path: '/locations/:id/assortment',
      name: 'location-assortment',
      component: () => import('@/pages/AssortmentPage.vue'),
      meta: { title: 'Ассортиментная матрица', permission: 'refs.view' },
    },
    {
      path: '/sale-rules',
      name: 'sale-rules',
      component: () => import('@/pages/SaleRulesPage.vue'),
      meta: { title: 'Правила продажи алкоголя', permission: 'refs.view' },
    },
    {
      path: '/scan',
      name: 'scan',
      component: () => import('@/pages/ScanPage.vue'),
      meta: { title: 'Сканер', permission: 'refs.view' },
    },
    {
      path: '/products',
      name: 'products',
      component: () => import('@/pages/ProductsPage.vue'),
      meta: { title: 'Товары', permission: 'refs.view' },
    },
    {
      path: '/products/new',
      name: 'product-new',
      component: () => import('@/pages/ProductPage.vue'),
      meta: { title: 'Новая позиция', permission: 'refs.edit' },
    },
    {
      path: '/products/:id',
      name: 'product',
      component: () => import('@/pages/ProductPage.vue'),
      meta: { title: 'Позиция', permission: 'refs.view' },
    },
    {
      path: '/categories',
      name: 'categories',
      component: () => import('@/pages/CategoriesPage.vue'),
      meta: { title: 'Категории товаров', permission: 'refs.view' },
    },
    {
      path: '/refs',
      name: 'refs',
      component: () => import('@/pages/RefsPage.vue'),
      meta: { title: 'Справочники', permission: 'refs.view' },
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
