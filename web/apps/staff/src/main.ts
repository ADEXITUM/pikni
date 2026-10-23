import { api } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import { createApp } from 'vue'

import App from './App.vue'
import { loadTheme } from './preferences'
import { queryClient } from './query'
import router from './router'
import { sessionMiddleware } from './session'

api.use(sessionMiddleware(queryClient, router))

const vuetify = createPikniVuetify()
void vuetify.theme.change(loadTheme())

const app = createApp(App)
  .use(createPinia())
  .use(router)
  .use(vuetify)
  .use(VueQueryPlugin, { queryClient })

// Пока не известно, вошёл ли пользователь, показывать нечего: иначе мелькнёт оболочка
// и уйдут запросы, на которые придёт 401.
void router.isReady().then(() => app.mount('#app'))
