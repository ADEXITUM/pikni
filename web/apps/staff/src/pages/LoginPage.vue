<script setup lang="ts">
import { mdiEye, mdiEyeOff } from '@mdi/js'
import { api, ApiError } from '@pikni/api'
import mascotError from '@pikni/ui/assets/mascot/error.svg'
import mascotIdle from '@pikni/ui/assets/mascot/idle.svg'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import AuthCard from '@/components/AuthCard.vue'
import { meQuery, safeRedirect } from '@/session'

const route = useRoute()
const router = useRouter()
const queryClient = useQueryClient()

const login = ref('')
const password = ref('')
const showPassword = ref(false)

const { mutate, isPending, error } = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/auth/login', {
      body: { login: login.value, password: password.value },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: async (me) => {
    queryClient.setQueryData(meQuery.queryKey, me)
    const redirect = safeRedirect(route.query.redirect)
    await router.replace(
      me.mustChangePassword ? { name: 'change-password', query: { redirect } } : redirect,
    )
  },
})

const errorText = computed(() =>
  error.value instanceof ApiError ? error.value.message : 'Нет связи с сервером',
)
</script>

<template>
  <AuthCard title="Вход" :mascot="error ? mascotError : mascotIdle">
    <v-form @submit.prevent="mutate()">
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
    </v-form>
  </AuthCard>
</template>
