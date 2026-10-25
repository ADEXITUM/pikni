<script setup lang="ts">
import { mdiCellphoneKey } from '@mdi/js'
import { api, ApiError } from '@pikni/api'
import { qrSvg } from '@pikni/scanner'
import { computed, onBeforeUnmount, ref } from 'vue'
import QrCode from '@/components/QrCode.vue'

// Вход на смартфоне по QR-коду с ПК (ПБ-2): код одноразовый и живёт 60 секунд.
const qr = ref('')
const expires = ref(0)
const now = ref(Date.now())
const error = ref<string>()
let timer: ReturnType<typeof setInterval> | undefined

const left = computed(() => Math.max(0, Math.ceil((expires.value - now.value) / 1000)))

async function show() {
  error.value = undefined
  const { data, error: problem } = await api.POST('/auth/qr')
  if (problem) return (error.value = new ApiError(problem).message)
  qr.value = await qrSvg(data.code)
  expires.value = new Date(data.expiresAt).getTime()
  clearInterval(timer)
  timer = setInterval(() => {
    now.value = Date.now()
    if (left.value === 0) {
      clearInterval(timer)
      qr.value = ''
    }
  }, 500)
}
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <v-card class="mb-4" data-test="phone-login">
    <v-card-item
      title="Вход на смартфоне"
      subtitle="Без пароля: откройте «Пикни» на телефоне и отсканируйте код"
    >
      <template #prepend><v-icon :icon="mdiCellphoneKey" /></template>
    </v-card-item>
    <v-card-text v-if="qr">
      <!-- eslint-disable-next-line vue/no-v-html -- SVG собран нашим кодом -->
      <QrCode :svg="qr" class="mb-3" data-test="login-qr" />
      <p class="text-body-medium">Код действует ещё {{ left }} с и подходит один раз.</p>
    </v-card-text>
    <v-alert v-if="error" type="error" class="mx-4 mb-4">{{ error }}</v-alert>
    <v-card-actions>
      <v-btn variant="tonal" data-test="show-login-qr" @click="show">{{
        qr ? 'Новый код' : 'Показать код'
      }}</v-btn>
    </v-card-actions>
  </v-card>
</template>
