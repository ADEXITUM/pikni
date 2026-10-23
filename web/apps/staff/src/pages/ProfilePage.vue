<script setup lang="ts">
import { mdiKeyOutline } from '@mdi/js'
import { api, ApiError } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed } from 'vue'

import { describeDevice } from '@/device'
import { formatAgo } from '@/format'
import { roleLabels } from '@/labels'
import { useMe } from '@/session'

const queryClient = useQueryClient()
const { data: me } = useMe()

const sessionsKey = ['sessions']
const {
  data: sessions,
  isError,
  error,
  refetch,
} = useQuery({
  queryKey: sessionsKey,
  queryFn: async () => {
    const { data, error } = await api.GET('/auth/sessions')
    if (error) throw new ApiError(error)
    return data.items
  },
})

const rows = computed(() =>
  (sessions.value ?? []).map((s) => ({ ...s, device: describeDevice(s.userAgent) })),
)
const hasOthers = computed(() => rows.value.some((s) => !s.current))

const refresh = () => queryClient.invalidateQueries({ queryKey: sessionsKey })

const {
  mutate: endSession,
  isPending: ending,
  variables: endingId,
  error: endError,
} = useMutation({
  mutationFn: async (id: string) => {
    const { error } = await api.DELETE('/auth/sessions/{id}', { params: { path: { id } } })
    if (error) throw new ApiError(error)
  },
  onSettled: refresh,
})

const {
  mutate: endOthers,
  isPending: endingOthers,
  error: endOthersError,
} = useMutation({
  mutationFn: async () => {
    const { error } = await api.DELETE('/auth/sessions')
    if (error) throw new ApiError(error)
  },
  onSettled: refresh,
})

const actionError = computed(() => endError.value ?? endOthersError.value)
</script>

<template>
  <v-container class="pk-profile">
    <h1 class="text-h5 mb-4">Профиль</h1>

    <v-card v-if="me" class="mb-4">
      <v-card-item :title="me.fullName" :subtitle="`${me.login} · ${roleLabels[me.role]}`" />
      <v-card-actions>
        <v-btn :prepend-icon="mdiKeyOutline" :to="{ name: 'change-password' }"
          >Сменить пароль</v-btn
        >
      </v-card-actions>
    </v-card>

    <v-card>
      <v-card-item title="Сеансы" subtitle="Устройства, на которых выполнен вход" />

      <v-alert v-if="isError" type="error" class="mx-4 mb-4" title="Не удалось загрузить сеансы">
        {{ error?.message }}
        <template #append>
          <v-btn variant="text" @click="refetch()">Повторить</v-btn>
        </template>
      </v-alert>
      <v-alert v-if="actionError" type="error" class="mx-4 mb-4">{{ actionError.message }}</v-alert>

      <v-list lines="two">
        <v-list-item v-for="s in rows" :key="s.id" :data-test="`session-${s.id}`">
          <template #prepend>
            <v-badge :icon="s.device.browserIcon" color="surface" location="bottom end" bordered>
              <v-avatar color="surface-light">
                <v-icon :icon="s.device.osIcon" />
              </v-avatar>
            </v-badge>
          </template>

          <v-list-item-title>{{ s.device.title }}</v-list-item-title>
          <v-list-item-subtitle>
            {{ s.ip ?? 'адрес неизвестен' }} ·
            {{ s.current ? 'сейчас' : `активность ${formatAgo(s.lastSeenAt)}` }}
          </v-list-item-subtitle>

          <template #append>
            <v-chip v-if="s.current" color="success" size="small">Это устройство</v-chip>
            <v-btn
              v-else
              variant="text"
              color="error"
              size="small"
              :loading="ending && endingId === s.id"
              @click="endSession(s.id)"
            >
              Завершить
            </v-btn>
          </template>
        </v-list-item>
      </v-list>

      <v-card-actions v-if="hasOthers">
        <v-btn
          variant="text"
          color="error"
          :loading="endingOthers"
          data-test="end-others"
          @click="endOthers()"
        >
          Завершить все остальные сеансы
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-container>
</template>

<style scoped>
.pk-profile {
  max-width: 760px;
}
</style>
