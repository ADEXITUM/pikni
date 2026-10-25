<script setup lang="ts">
import { mdiContentCopy, mdiShieldCheckOutline } from '@mdi/js'
import { api, ApiError } from '@pikni/api'
import { useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'

import QrCode from '@/components/QrCode.vue'
import { meQuery, useMe } from '@/session'
import { useTotpSetup } from '@/totp'

// Двухфакторная аутентификация (ПБ-3): приложение-аутентификатор на телефоне выдаёт код на 30 с.
// Администратору обязательна — он подключает её на отдельном экране, остальным — по желанию.
const { data: me } = useMe()
const queryClient = useQueryClient()
const { setup, code, recovery, error, busy, start, enable, copyRecovery: copy } = useTotpSetup()
const disableOpen = ref(false)

async function disable() {
  busy.value = true
  error.value = undefined
  const { error: problem } = await api.POST('/auth/totp/disable', { body: { code: code.value } })
  busy.value = false
  if (problem) return (error.value = new ApiError(problem).message)
  disableOpen.value = false
  code.value = ''
  await queryClient.invalidateQueries({ queryKey: meQuery.queryKey })
}
</script>

<template>
  <v-card class="mb-4" data-test="two-factor">
    <v-card-item
      title="Двухфакторная аутентификация"
      :subtitle="me?.totpEnabled ? 'Включена: при входе нужен код из приложения' : 'Выключена'"
    >
      <template #prepend>
        <v-icon :icon="mdiShieldCheckOutline" :color="me?.totpEnabled ? 'success' : undefined" />
      </template>
    </v-card-item>
    <v-card-text>
      <template v-if="recovery">
        <p class="mb-2">
          Подключено. Сохраните резервные коды — каждый подходит один раз, если телефон потеряется.
          Больше они не покажутся.
        </p>
        <div class="pk-codes mb-2" data-test="recovery">
          <code v-for="c in recovery" :key="c">{{ c }}</code>
        </div>
        <v-btn :prepend-icon="mdiContentCopy" variant="text" size="small" @click="copy"
          >Скопировать</v-btn
        >
      </template>

      <template v-if="setup">
        <p class="mb-3">
          Отсканируйте код приложением-аутентификатором (Google Authenticator, «Яндекс Ключ» и
          подобные) и введите шесть цифр, которые оно покажет.
        </p>
        <QrCode :svg="setup.qr" class="mb-3" />
        <p class="text-body-small text-medium-emphasis mb-3">
          Не сканируется — введите ключ вручную: <code>{{ setup.secret }}</code>
        </p>
        <v-form class="d-flex ga-2" @submit.prevent="enable">
          <v-text-field
            v-model="code"
            label="Код из приложения"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="6"
            density="compact"
            hide-details
            data-test="totp-code"
          />
          <v-btn
            type="submit"
            color="primary"
            :loading="busy"
            :disabled="code.length !== 6"
            data-test="totp-enable"
            >Подключить</v-btn
          >
        </v-form>
      </template>
      <v-alert v-if="error" type="error" class="mt-3">{{ error }}</v-alert>
    </v-card-text>
    <v-card-actions v-if="!setup">
      <v-btn
        v-if="!me?.totpEnabled"
        color="primary"
        variant="tonal"
        data-test="totp-start"
        @click="start"
        >Подключить</v-btn
      >
      <v-btn v-else-if="me?.role !== 'admin'" variant="text" @click="disableOpen = true"
        >Отключить</v-btn
      >
    </v-card-actions>
    <v-dialog v-model="disableOpen" max-width="400">
      <v-card title="Отключить второй фактор">
        <v-card-text>
          <v-text-field v-model="code" label="Код из приложения или резервный код" autofocus />
          <v-alert v-if="error" type="error">{{ error }}</v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="disableOpen = false">Отмена</v-btn>
          <v-btn color="error" :loading="busy" :disabled="!code" @click="disable">Отключить</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-card>
</template>

<style scoped>
.pk-codes {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 6px;
}
</style>
