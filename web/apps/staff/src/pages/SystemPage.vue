<script setup lang="ts">
import { mdiRefresh } from '@mdi/js'
import { api, ApiError, type SystemStatus } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed } from 'vue'

import { formatAgo, formatDateTime } from '@/format'

// Состояние системы (п. 4.1.5): то, что администратор проверяет первым при жалобе из магазина.
const {
  data: s,
  error,
  refetch,
  isFetching,
} = useQuery({
  queryKey: ['system-status'],
  refetchInterval: 30_000,
  queryFn: async () => {
    const { data, error } = await api.GET('/system/status')
    if (error) throw new ApiError(error)
    return data
  },
})
type Utm = SystemStatus['locations'][number]['utm']
const utm: Record<Utm, { text: string; color: string }> = {
  ok: { text: 'на связи', color: 'success' },
  delayed: { text: 'задержка', color: 'warning' },
  down: { text: 'недоступен', color: 'error' },
  none: { text: 'нет ФСРАР ИД', color: 'secondary' },
}
const backupOld = computed(
  () =>
    !s.value?.lastBackup || Date.now() - new Date(s.value.lastBackup.at).getTime() > 26 * 3600_000,
)
const size = (b?: number) =>
  b === undefined ? '' : `${(b / 1024 / 1024).toFixed(1).replace('.', ',')} МБ`
</script>

<template>
  <v-container class="pk-system">
    <div class="d-flex align-center ga-2 mb-4">
      <h1 class="text-headline-small">Состояние системы</h1>
      <v-spacer />
      <v-btn :prepend-icon="mdiRefresh" variant="text" :loading="isFetching" @click="refetch()"
        >Обновить</v-btn
      >
    </div>
    <v-alert v-if="error" type="error">{{ error.message }}</v-alert>
    <template v-if="s">
      <div class="pk-grid mb-6" data-test="status">
        <v-card variant="flat" class="pa-4">
          <div class="text-medium-emphasis">Версия</div>
          <div class="text-title-large">{{ s.version }}</div>
          <div class="text-body-small">проверено {{ formatDateTime(s.checkedAt) }}</div>
        </v-card>
        <v-card variant="flat" class="pa-4">
          <div class="text-medium-emphasis">Фоновые задачи</div>
          <div class="text-title-large">{{ s.jobs.available + s.jobs.running }} в работе</div>
          <div class="text-body-small" :class="{ 'text-error': s.jobs.failedDay > 0 }">
            повторяются {{ s.jobs.retryable }}, отброшено за сутки {{ s.jobs.failedDay }}
          </div>
        </v-card>
        <v-card variant="flat" class="pa-4">
          <div class="text-medium-emphasis">Резервная копия</div>
          <div class="text-title-large" :class="{ 'text-error': backupOld }">
            {{ s.lastBackup ? formatAgo(s.lastBackup.at) : 'не делалась' }}
          </div>
          <div v-if="s.lastBackup" class="text-body-small">
            {{ s.lastBackup.ok ? 'успешно' : 'с ошибкой' }} · {{ size(s.lastBackup.sizeBytes) }}
          </div>
        </v-card>
        <v-card variant="flat" class="pa-4">
          <div class="text-medium-emphasis">Целостность остатков</div>
          <div
            class="text-title-large"
            :class="s.integrityProblems ? 'text-error' : 'text-success'"
          >
            {{ s.integrityProblems ? `${s.integrityProblems} расхождений` : 'сходится' }}
          </div>
          <div class="text-body-small">остатки = сумма движений</div>
        </v-card>
        <v-card variant="flat" class="pa-4">
          <div class="text-medium-emphasis">ЕГАИС и ГИС МТ (имитатор)</div>
          <div class="text-title-large" :class="s.govsim === 'ok' ? 'text-success' : 'text-error'">
            {{
              s.govsim === 'ok' ? 'на связи' : s.govsim === 'off' ? 'не подключён' : 'недоступен'
            }}
          </div>
        </v-card>
        <v-card variant="flat" class="pa-4">
          <div class="text-medium-emphasis">Ждут внимания</div>
          <div class="text-title-large">{{ s.unconfirmedTtn }} ТТН · {{ s.openShifts }} смен</div>
          <div class="text-body-small">неподтверждённые ТТН и открытые смены</div>
        </v-card>
      </div>

      <h2 class="text-title-large mb-2">УТМ точек</h2>
      <v-table density="comfortable" data-test="utm">
        <thead>
          <tr>
            <th>Точка</th>
            <th>УТМ</th>
            <th class="text-end">В очереди</th>
            <th>Последний обмен</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="l in s.locations" :key="l.code">
            <td>{{ l.code }} · {{ l.name }}</td>
            <td>
              <v-chip :color="utm[l.utm].color" size="small" label>{{ utm[l.utm].text }}</v-chip>
            </td>
            <td class="text-end">
              {{ l.queued
              }}<span v-if="l.oldestQueuedAt" class="text-body-small text-medium-emphasis">
                · с {{ formatAgo(l.oldestQueuedAt) }}</span
              >
            </td>
            <td>{{ l.lastExchangeAt ? formatAgo(l.lastExchangeAt) : '—' }}</td>
          </tr>
        </tbody>
      </v-table>
    </template>
  </v-container>
</template>

<style scoped>
.pk-system {
  max-width: 1100px;
}
.pk-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 12px;
}
</style>
