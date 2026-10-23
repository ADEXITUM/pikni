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

createApp(App)
  .use(createPinia())
  .use(router)
  .use(vuetify)
  .use(VueQueryPlugin, { queryClient })
  .mount('#app')
