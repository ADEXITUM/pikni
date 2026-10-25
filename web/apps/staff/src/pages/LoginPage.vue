<script setup lang="ts">
import { mdiEye, mdiEyeOff, mdiQrcodeScan } from '@mdi/js'
import { api, ApiError, type Me, type Role } from '@pikni/api'
import mascotError from '@pikni/ui/assets/mascot/error.svg'
import mascotIdle from '@pikni/ui/assets/mascot/idle.svg'
import ScanCamera from '@pikni/ui/ScanCamera.vue'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import AuthCard from '@/components/AuthCard.vue'
import { roleLabels } from '@/labels'
import { meQuery, safeRedirect } from '@/session'

const route = useRoute()
const router = useRouter()
const queryClient = useQueryClient()

const login = ref('')
const password = ref('')
const showPassword = ref(false)
// Второй шаг входа: пароль верный, нужен код из приложения или резервный код (ПБ-3).
const challenge = ref<string>()
const code = ref('')
const scanning = ref(false)

async function signedIn(me: Me) {
  queryClient.setQueryData(meQuery.queryKey, me)
  const redirect = safeRedirect(route.query.redirect)
  await router.replace(
    me.mustChangePassword ? { name: 'change-password', query: { redirect } } : redirect,
  )
}

const { mutate, isPending, error } = useMutation({
  mutationFn: async () => {
    if (challenge.value) {
      const { data, error } = await api.POST('/auth/login/totp', {
        body: { challenge: challenge.value, code: code.value },
      })
      if (error) throw new ApiError(error)
      return data
    }
    const { data, error, response } = await api.POST('/auth/login', {
      body: { login: login.value, password: password.value },
    })
    if (error) throw new ApiError(error)
    if (response.status === 202) {
      challenge.value = (data as unknown as { challenge: string }).challenge
      return undefined
    }
    return data as Me
  },
  onSuccess: async (me) => {
    if (me) await signedIn(me)
  },
})

// Вход по QR-коду, который показывает ПК с выполненным входом (ПБ-2).
const qrError = ref<string>()
async function redeem(raw: string) {
  if (!raw.startsWith('pikni:login:')) {
    qrError.value = 'Это не код входа — откройте «Профиль» на компьютере и нажмите «Показать код»'
    return
  }
  scanning.value = false
  const { data, error } = await api.POST('/auth/qr/redeem', { body: { code: raw } })
  if (error) {
    qrError.value = new ApiError(error).message
    return
  }
  await signedIn(data)
}

// Демонстрационный стенд: вход под любой ролью одной кнопкой (п. 4.1.4).
const { data: demo } = useQuery({
  queryKey: ['demo-roles'],
  retry: false,
  queryFn: async () => {
    const { data } = await api.GET('/auth/demo')
    return data?.roles ?? []
  },
})
async function demoLogin(role: Role) {
  const { data, error } = await api.POST('/auth/demo/{role}', { params: { path: { role } } })
  if (error) {
    qrError.value = new ApiError(error).message
    return
  }
  await signedIn(data)
}

const errorText = computed(() =>
  error.value instanceof ApiError ? error.value.message : 'Нет связи с сервером',
)
</script>

<template>
  <AuthCard title="Вход" :mascot="error || qrError ? mascotError : mascotIdle">
    <template v-if="scanning">
      <p class="text-body-medium mb-3">
        Наведите камеру на QR-код в «Профиле» на компьютере, где вы уже вошли.
      </p>
      <ScanCamera class="mb-3" @scan="(c) => redeem(c.raw)" @close="scanning = false" />
      <v-btn block variant="text" @click="scanning = false">Ввести логин и пароль</v-btn>
    </template>

    <v-form v-else-if="challenge" @submit.prevent="mutate()">
      <p class="text-body-medium mb-3">
        Введите шесть цифр из приложения-аутентификатора или резервный код.
      </p>
      <v-text-field
        v-model="code"
        label="Код"
        inputmode="numeric"
        autocomplete="one-time-code"
        autofocus
        class="mb-4"
        data-test="totp"
      />
      <v-alert v-if="error" type="error" class="mb-4" data-test="error">{{ errorText }}</v-alert>
      <v-btn type="submit" color="primary" size="large" block :loading="isPending" :disabled="!code"
        >Войти</v-btn
      >
      <v-btn block variant="text" class="mt-2" @click="challenge = undefined">Назад</v-btn>
    </v-form>

    <v-form v-else @submit.prevent="mutate()">
      <v-text-field
        v-model="login"
        label="Логин"
        name="username"
        autocomplete="username"
        autofocus
        class="mb-3"
      />
      <v-text-field
        v-model="password"
        label="Пароль"
        name="password"
        :type="showPassword ? 'text' : 'password'"
        autocomplete="current-password"
        :append-inner-icon="showPassword ? mdiEyeOff : mdiEye"
        class="mb-4"
        @click:append-inner="showPassword = !showPassword"
      />

      <v-alert v-if="error" type="error" class="mb-4" data-test="error">{{ errorText }}</v-alert>
      <v-alert v-if="qrError" type="error" class="mb-4">{{ qrError }}</v-alert>

      <v-btn
        type="submit"
        color="primary"
        size="large"
        block
        :loading="isPending"
        :disabled="!login || !password"
      >
        Войти
      </v-btn>
      <v-btn
        :prepend-icon="mdiQrcodeScan"
        block
        variant="text"
        class="mt-2"
        data-test="qr-login"
        @click="((qrError = undefined), (scanning = true))"
        >Войти по QR-коду с компьютера</v-btn
      >
      <template v-if="demo?.length">
        <v-divider class="my-4" />
        <p class="text-body-medium text-medium-emphasis mb-2">
          Демонстрационный стенд — войти как:
        </p>
        <div class="d-flex flex-wrap ga-2" data-test="demo-roles">
          <v-chip v-for="r in demo" :key="r" @click="demoLogin(r)">{{ roleLabels[r] }}</v-chip>
        </div>
      </template>
    </v-form>
  </AuthCard>
</template>
