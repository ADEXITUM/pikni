import { api } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import { createApp } from 'vue'

import App from './App.vue'
import { queryClient } from './query'
import router from './router'
import { sessionMiddleware } from './session'

api.use(sessionMiddleware(queryClient, router))

createApp(App)
  .use(createPinia())
  .use(router)
  .use(createPikniVuetify())
  .use(VueQueryPlugin, { queryClient })
  .mount('#app')
