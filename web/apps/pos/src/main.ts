import { createPikniVuetify } from '@pikni/ui'
import { createApp } from 'vue'

import App from './App.vue'

createApp(App).use(createPikniVuetify()).mount('#app')

// Оболочка кассы кешируется, чтобы касса открывалась и без связи (КС-4).
if ('serviceWorker' in navigator && import.meta.env.PROD) {
  void navigator.serviceWorker.register(`${import.meta.env.BASE_URL}sw.js`)
}
