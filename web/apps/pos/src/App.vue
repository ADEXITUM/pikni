<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'

import LoginScreen from './components/LoginScreen.vue'
import SaleScreen from './components/SaleScreen.vue'
import SetupScreen from './components/SetupScreen.vue'
import ShiftReportDialog from './components/ShiftReportDialog.vue'
import ShiftScreen from './components/ShiftScreen.vue'
import { keyFromHash } from './device'
import { flushQueue, loadShift, refreshCatalog, refreshTime, setKey, till } from './till'

// Ссылка регистрации из QR-кода настраивает кассу; ключ сразу убирается из адресной строки.
const fromLink = keyFromHash(location.hash)
if (fromLink) {
  setKey(fromLink)
  history.replaceState(null, '', location.pathname)
}

let timer: number | undefined
const online = () => {
  till.online = true
  void flushQueue()
}
const offline = () => (till.online = false)

onMounted(() => {
  if (till.session) {
    void loadShift().catch(() => undefined)
    void refreshTime().catch(() => undefined)
    void refreshCatalog().catch(() => undefined)
  }
  // Раз в минуту — время и окна продажи, отправка отложенных чеков; каталог — раз в 10 минут.
  let tick = 0
  timer = window.setInterval(() => {
    if (!till.session) return
    tick++
    void refreshTime().catch(() => undefined)
    void flushQueue()
    if (tick % 10 === 0) void refreshCatalog().catch(() => undefined)
  }, 60_000)
  window.addEventListener('online', online)
  window.addEventListener('offline', offline)
})

onUnmounted(() => {
  window.clearInterval(timer)
  window.removeEventListener('online', online)
  window.removeEventListener('offline', offline)
})
</script>

<template>
  <v-app>
    <SetupScreen v-if="!till.key" />
    <LoginScreen v-else-if="!till.session" />
    <ShiftScreen v-else-if="!till.shift" />
    <SaleScreen v-else />
    <ShiftReportDialog v-model="till.report" />
  </v-app>
</template>
