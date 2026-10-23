import { createPikniVuetify } from '@pikni/ui'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { createPinia } from 'pinia'
import { createApp } from 'vue'

import App from './App.vue'
import router from './router'

createApp(App)
  .use(createPinia())
  .use(router)
  .use(createPikniVuetify())
  .use(VueQueryPlugin)
  .mount('#app')
