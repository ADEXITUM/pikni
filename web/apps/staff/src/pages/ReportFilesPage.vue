<script setup lang="ts">
import { mdiArrowLeft, mdiDownload } from '@mdi/js'
import { api, ApiError, type ReportFile } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'

import { formatDateTime } from '@/format'
import { useLive } from '@/live'
import { useMe } from '@/session'

// «Мои отчёты» (ОТ-3): отчёты, сформированные в фоне, хранятся семь дней.
const { data: me } = useMe()
const { data: files, error } = useQuery({
  queryKey: ['report-files'],
  queryFn: async () => {
    const { data, error } = await api.GET('/report-files')
    if (error) throw new ApiError(error)
    return data.items
  },
})
// Готовность приходит уведомлением — список обновляется сам.
useLive(
  () => (me.value ? [`user:${me.value.id}`] : []),
  () => [['report-files']],
)
const statusLabels: Record<ReportFile['status'], string> = {
  queued: 'В очереди',
  running: 'Формируется',
  done: 'Готов',
  failed: 'Ошибка',
}
const statusColors: Record<ReportFile['status'], string> = {
  queued: 'secondary',
  running: 'info',
  done: 'success',
  failed: 'error',
}
</script>

<template>
  <v-container class="pk-files">
    <v-btn :prepend-icon="mdiArrowLeft" :to="{ name: 'reports' }" variant="text" class="mb-2 ml-n3"
      >Отчёты</v-btn
    >
    <h1 class="text-headline-small mb-1">Мои отчёты</h1>
    <p class="text-body-medium text-medium-emphasis mb-4">
      Отчёты, которые формировались в фоне. Файл хранится семь дней.
    </p>
    <v-alert v-if="error" type="error">{{ error.message }}</v-alert>
    <p v-if="files && !files.length" class="text-body-medium text-medium-emphasis">
      Фоновых отчётов пока нет.
    </p>
    <v-list v-else lines="two" data-test="files">
      <v-list-item v-for="f in files" :key="f.id" :title="f.title">
        <v-list-item-subtitle>
          {{ f.format.toUpperCase() }} · заказан {{ formatDateTime(f.createdAt) }} · хранится до
          {{ formatDateTime(f.expiresAt) }}<template v-if="f.error"> · {{ f.error }}</template>
        </v-list-item-subtitle>
        <template #append>
          <v-chip :color="statusColors[f.status]" size="small" label class="mr-2">{{
            statusLabels[f.status]
          }}</v-chip>
          <v-btn
            v-if="f.status === 'done'"
            :icon="mdiDownload"
            variant="text"
            :href="`/api/v1/report-files/${f.id}`"
            :download="`${f.title}.${f.format}`"
            :aria-label="`Скачать ${f.title}`"
          />
        </template>
      </v-list-item>
    </v-list>
  </v-container>
</template>

<style scoped>
.pk-files {
  max-width: 900px;
}
</style>
