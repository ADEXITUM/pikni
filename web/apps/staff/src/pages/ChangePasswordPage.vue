<script setup lang="ts">
import { api, ApiError, type Me } from '@pikni/api'
import mascotIdle from '@pikni/ui/assets/mascot/idle.svg'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { SubmitEventPromise } from 'vuetify'

import AuthCard from '@/components/AuthCard.vue'
import { meQuery, safeRedirect, useLogout, useMe } from '@/session'

const route = useRoute()
const router = useRouter()
const queryClient = useQueryClient()
const { data: me } = useMe()
const logout = useLogout()

const current = ref('')
const next = ref('')
const repeat = ref('')

const lengthRule = (v: string) => (v.length >= 8 && v.length <= 128) || 'От 8 до 128 символов'
const sameRule = (v: string) => v === next.value || 'Пароли не совпадают'

const { mutate, isPending, error } = useMutation({
  mutationFn: async () => {
    const { error } = await api.POST('/auth/password', {
      body: { currentPassword: current.value, newPassword: next.value },
    })
    if (error) throw new ApiError(error)
  },
  onSuccess: async () => {
    queryClient.setQueryData<Me | null>(
      meQuery.queryKey,
      (m) => m && { ...m, mustChangePassword: false },
    )
    await router.replace(safeRedirect(route.query.redirect))
  },
})

const errorText = computed(() =>
  error.value instanceof ApiError ? error.value.message : 'Нет связи с сервером',
)

async function submit(event: SubmitEventPromise) {
  if ((await event).valid) mutate()
}
</script>

<template>
  <AuthCard
    :title="me?.mustChangePassword ? 'Придумайте пароль' : 'Смена пароля'"
    :mascot="mascotIdle"
  >
    <p v-if="me?.mustChangePassword" class="text-body-medium text-medium-emphasis mb-4">
      Вы вошли по временному паролю. Придумайте свой, чтобы продолжить.
    </p>

    <v-form @submit.prevent="submit">
      <v-text-field
        v-model="current"
        :label="me?.mustChangePassword ? 'Временный пароль' : 'Текущий пароль'"
        type="password"
        autocomplete="current-password"
        class="mb-3"
      />
      <v-text-field
        v-model="next"
        label="Новый пароль"
        type="password"
        autocomplete="new-password"
        :rules="[lengthRule]"
        class="mb-3"
      />
      <v-text-field
        v-model="repeat"
        label="Новый пароль ещё раз"
        type="password"
        autocomplete="new-password"
        :rules="[sameRule]"
        class="mb-4"
      />

      <v-alert v-if="error" type="error" class="mb-4" data-test="error">{{ errorText }}</v-alert>

      <v-btn type="submit" color="primary" size="large" block :loading="isPending">
        Сохранить пароль
      </v-btn>
      <v-btn v-if="me?.mustChangePassword" variant="text" block class="mt-2" @click="logout()">
        Выйти
      </v-btn>
      <v-btn v-else variant="text" block class="mt-2" @click="router.back()">Отмена</v-btn>
    </v-form>
  </AuthCard>
</template>
