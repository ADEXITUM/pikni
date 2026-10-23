import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    { path: '/', redirect: '/locations' },
    {
      path: '/locations',
      component: () => import('@/pages/LocationsPage.vue'),
      meta: { title: 'Точки сети' },
    },
  ],
})

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} — Пикни` : 'Пикни'
})

export default router
