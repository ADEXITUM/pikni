<script setup lang="ts">
import { mdiContentCopy } from '@mdi/js'
import mascotIdle from '@pikni/ui/assets/mascot/idle.svg'
import { onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import AuthCard from '@/components/AuthCard.vue'
import QrCode from '@/components/QrCode.vue'
import { safeRedirect, useLogout } from '@/session'
import { useTotpSetup } from '@/totp'

// Обязательное подключение второго фактора (ПБ-3): у администратора без него остальные разделы
// закрыты, поэтому экран отдельный, без меню — нечего нажимать, кроме шагов подключения.
const route = useRoute()
const router = useRouter()
const logout = useLogout()
const { setup, code, recovery, error, busy, start, enable, copyRecovery } = useTotpSetup()

onMounted(start)

function proceed() {
  void router.replace(safeRedirect(route.query.redirect))
}
</script>

<template>
  <AuthCard title="Защитите вход администратора" :mascot="mascotIdle">
    <template v-if="recovery">
      <p class="text-body-medium mb-3">
        Второй фактор подключён. Сохраните резервные коды: каждый подходит один раз, если телефон
        потеряется. Больше они не покажутся.
      </p>
      <div class="pk-codes mb-2" data-test="recovery">
        <code v-for="c in recovery" :key="c">{{ c }}</code>
      </div>
      <v-btn
        :prepend-icon="mdiContentCopy"
        variant="text"
        size="small"
        class="mb-4"
        @click="copyRecovery"
        >Скопировать</v-btn
      >
      <v-btn color="primary" size="large" block data-test="totp-continue" @click="proceed"
        >Коды сохранены — продолжить</v-btn
      >
    </template>

    <template v-else>
      <p class="text-body-medium text-medium-emphasis mb-4">
        У администратора полный доступ к системе, поэтому вход защищается кодом с телефона. Это
        займёт минуту.
      </p>
      <ol class="pk-steps mb-4">
        <li>
          Установите приложение-аутентификатор: «Яндекс Ключ», Google Authenticator или любое
          похожее.
        </li>
        <li>Отсканируйте в нём код:</li>
      </ol>
      <div class="d-flex justify-center mb-2"><QrCode :svg="setup?.qr" data-test="totp-qr" /></div>
      <p v-if="setup" class="text-body-small text-medium-emphasis text-center mb-4">
        Не сканируется — введите ключ вручную:<br /><code>{{ setup.secret }}</code>
      </p>
      <ol class="pk-steps mb-3" start="3">
        <li>Введите шесть цифр, которые покажет приложение:</li>
      </ol>
      <v-form @submit.prevent="enable">
        <v-text-field
          v-model="code"
          label="Код из приложения"
          inputmode="numeric"
          autocomplete="one-time-code"
          maxlength="6"
          class="mb-3"
          data-test="totp-code"
        />
        <v-alert v-if="error" type="error" class="mb-3">{{ error }}</v-alert>
        <v-btn
          type="submit"
          color="primary"
          size="large"
          block
          :loading="busy"
          :disabled="code.length !== 6"
          data-test="totp-enable"
          >Подключить</v-btn
        >
      </v-form>
    </template>
    <v-btn variant="text" block class="mt-2" @click="logout()">Выйти</v-btn>
  </AuthCard>
</template>

<style scoped>
.pk-steps {
  padding-left: 20px;
}
.pk-steps li {
  margin-bottom: 6px;
}
.pk-codes {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 6px;
}
</style>
