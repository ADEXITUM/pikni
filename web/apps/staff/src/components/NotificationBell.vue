<script setup lang="ts">
import { mdiBellOutline, mdiCheckAll } from '@mdi/js'
import { api, ApiError, type Notification } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'

import { formatAgo } from '@/format'
import { useLive } from '@/live'
import { useMe } from '@/session'

// Центр уведомлений (УВ-2): колокольчик с числом непрочитанных; новое приходит без перезагрузки
// по личной теме пользователя.
const router = useRouter()
const queryClient = useQueryClient()
const { data: me } = useMe()
const key = ['notifications']
const fresh = ref<Notification>()

const { data } = useQuery({
  queryKey: key,
  enabled: computed(() => !!me.value),
  queryFn: async () => {
    const { data, error } = await api.GET('/notifications')
    if (error) throw new ApiError(error)
    return data
  },
})
useLive(
  () => (me.value ? [`user:${me.value.id}`] : []),
  () => [key],
  async (e) => {
    if (e.type !== 'notification') return
    const list = await queryClient.fetchQuery({ queryKey: key, staleTime: 0 })
    fresh.value = (list as typeof data.value)?.items.find((n) => n.id === e.id)
  },
)

const { mutate: read } = useMutation({
  mutationFn: async (id?: string) => {
    const { error } = await api.POST('/notifications/read', { body: { id } })
    if (error) throw new ApiError(error)
  },
  onSuccess: () => queryClient.invalidateQueries({ queryKey: key }),
})

function open(n: Notification) {
  if (!n.readAt) read(n.id)
  fresh.value = undefined
  if (n.link) void router.push(n.link)
}
</script>

<template>
  <v-menu location="bottom end" :close-on-content-click="false" max-width="400">
    <template #activator="{ props }">
      <v-btn v-bind="props" icon variant="text" aria-label="Уведомления" data-test="bell">
        <v-badge :model-value="!!data?.unread" :content="data?.unread" color="error">
          <v-icon :icon="mdiBellOutline" />
        </v-badge>
      </v-btn>
    </template>
    <v-card min-width="320" data-test="notifications">
      <v-card-title class="d-flex align-center">
        Уведомления
        <v-spacer />
        <v-btn
          v-if="data?.unread"
          :icon="mdiCheckAll"
          variant="text"
          size="small"
          title="Прочитать все"
          @click="read(undefined)"
        />
      </v-card-title>
      <p v-if="!data?.items.length" class="px-4 pb-4 text-body-medium text-medium-emphasis">
        Уведомлений нет.
      </p>
      <v-list v-else density="compact" lines="three" class="pk-list">
        <v-list-item
          v-for="n in data.items"
          :key="n.id"
          :class="{ 'pk-unread': !n.readAt }"
          data-test="notification"
          @click="open(n)"
        >
          <v-list-item-title class="text-wrap">{{ n.title }}</v-list-item-title>
          <v-list-item-subtitle>
            <template v-if="n.body">{{ n.body }} · </template>{{ formatAgo(n.createdAt) }}
          </v-list-item-subtitle>
        </v-list-item>
      </v-list>
    </v-card>
  </v-menu>
  <v-snackbar
    :model-value="!!fresh"
    timeout="6000"
    location="top right"
    @update:model-value="fresh = undefined"
  >
    {{ fresh?.title }}
    <template #actions>
      <v-btn v-if="fresh?.link" variant="text" @click="open(fresh!)">Открыть</v-btn>
    </template>
  </v-snackbar>
</template>

<style scoped>
.pk-list {
  max-height: 60vh;
  overflow-y: auto;
}
.pk-unread {
  border-left: 3px solid rgb(var(--v-theme-primary));
}
</style>
