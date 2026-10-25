import { createRouter, createWebHistory } from 'vue-router'

import { queryClient } from '@/query'
import { homeRoute, meQuery, sessionGuard } from '@/session'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    // Стартовый экран по роли: в магазине — сканер, в офисе сети — каталог.
    {
      path: '/',
      name: 'home',
      component: { render: () => null },
      beforeEnter: async () => ({ name: homeRoute(await queryClient.ensureQueryData(meQuery)) }),
    },
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
      meta: { title: 'Сканер', permission: 'refs.view', scans: true },
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
      path: '/documents',
      name: 'documents',
      component: () => import('@/pages/DocumentsPage.vue'),
      meta: { title: 'Документы', permission: 'availability.view' },
    },
    {
      path: '/documents/transfer/new',
      name: 'transfer-new',
      component: () => import('@/pages/TransferNewPage.vue'),
      meta: { title: 'Заявка на перемещение', permission: 'transfer.request', scans: true },
    },
    {
      path: '/documents/:id',
      name: 'document',
      component: () => import('@/pages/DocumentPage.vue'),
      meta: { title: 'Документ', permission: 'availability.view', scans: true },
    },
    {
      path: '/availability',
      name: 'availability',
      component: () => import('@/pages/AvailabilityPage.vue'),
      meta: { title: 'Наличие по сети', permission: 'availability.view', scans: true },
    },
    {
      path: '/availability/:productId',
      name: 'availability-card',
      component: () => import('@/pages/AvailabilityCardPage.vue'),
      meta: { title: 'Наличие', permission: 'availability.view' },
    },
    {
      path: '/reservations',
      name: 'reservations',
      component: () => import('@/pages/ReservationsPage.vue'),
      meta: { title: 'Брони', permission: 'reservations.manage', scans: true },
    },
    {
      path: '/reservations/new',
      name: 'reservation-new',
      component: () => import('@/pages/ReservationNewPage.vue'),
      meta: { title: 'Отложить для покупателя', permission: 'reservations.manage', scans: true },
    },
    {
      path: '/egais/reconciliation',
      name: 'reconciliation',
      component: () => import('@/pages/ReconciliationPage.vue'),
      meta: { title: 'Сверка с ЕГАИС', permission: 'egais.reconcile' },
    },
    {
      path: '/price-tags',
      name: 'price-tags',
      component: () => import('@/pages/PriceTagsPage.vue'),
      meta: { title: 'Ценники', permission: 'floor.view', scans: true },
    },
    {
      path: '/assets',
      name: 'assets',
      component: () => import('@/pages/AssetsPage.vue'),
      meta: { title: 'Имущество', permission: 'assets.view' },
    },
    {
      path: '/assets/:id',
      name: 'asset',
      component: () => import('@/pages/AssetPage.vue'),
      meta: { title: 'Объект имущества', permission: 'assets.view' },
    },
    {
      path: '/asset-audits/:id',
      name: 'asset-audit',
      component: () => import('@/pages/AssetAuditPage.vue'),
      meta: { title: 'Инвентаризация имущества', permission: 'assets.view', scans: true },
    },
    {
      path: '/stock',
      name: 'stock',
      component: () => import('@/pages/StockPage.vue'),
      meta: { title: 'Остатки', permission: 'availability.view' },
    },
    {
      path: '/stock/moves',
      name: 'stock-moves',
      component: () => import('@/pages/StockMovesPage.vue'),
      meta: { title: 'Карточка движения', permission: 'availability.view' },
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
      path: '/pos-devices',
      name: 'pos-devices',
      component: () => import('@/pages/PosDevicesPage.vue'),
      meta: { title: 'Кассы', permission: 'pos.devices' },
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
