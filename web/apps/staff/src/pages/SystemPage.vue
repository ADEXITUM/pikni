<script setup lang="ts">
import { mdiMagnify, mdiRefresh } from '@mdi/js'
import { api, ApiError, type SystemStatus } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

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
// Точек может быть тысячи: таблица постраничная, проблемные — сверху, есть поиск.
const search = ref('')
const problemsOnly = ref(false)
const order: Record<Utm, number> = { down: 0, delayed: 1, ok: 2, none: 3 }
const points = computed(() =>
  (s.value?.locations ?? [])
    .filter((l) => !problemsOnly.value || l.utm === 'down' || l.utm === 'delayed')
    .map((l) => ({ ...l, title: `${l.code} · ${l.name}`, rank: order[l.utm] })),
)
const problems = computed(
  () => (s.value?.locations ?? []).filter((l) => l.utm === 'down' || l.utm === 'delayed').length,
)
const headers = [
  { title: 'Точка', key: 'title' },
  { title: 'УТМ', key: 'rank' },
  { title: 'В очереди', key: 'queued', align: 'end' as const },
  { title: 'Последний обмен', key: 'lastExchangeAt' },
]
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
          <div class="text-body-small">
            остатки = сумма движений ·
            {{
              s.integrityCheckedAt
                ? `проверено ${formatAgo(s.integrityCheckedAt)}`
                : 'проверка ещё не проходила'
            }}
          </div>
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

      <div class="d-flex flex-wrap align-center ga-3 mb-2">
        <h2 class="text-title-large">УТМ точек</h2>
        <v-chip v-if="problems" color="error" size="small" label
          >с проблемами: {{ problems }}</v-chip
        >
        <v-spacer />
        <v-switch
          v-model="problemsOnly"
          label="Только с проблемами"
          color="primary"
          density="compact"
          hide-details
        />
        <v-text-field
          v-model="search"
          :prepend-inner-icon="mdiMagnify"
          placeholder="Код или название точки"
          density="compact"
          hide-details
          clearable
          class="pk-search"
        />
      </div>
      <v-card>
        <v-data-table
          :headers="headers"
          :items="points"
          :search="search"
          :sort-by="[{ key: 'rank', order: 'asc' }]"
          :items-per-page="20"
          density="comfortable"
          no-data-text="Точек нет"
          data-test="utm"
        >
          <template #[`item.rank`]="{ item }">
            <v-chip :color="utm[item.utm].color" size="small" label>{{
              utm[item.utm].text
            }}</v-chip>
          </template>
          <template #[`item.queued`]="{ item }">
            {{ item.queued
            }}<span v-if="item.oldestQueuedAt" class="text-body-small text-medium-emphasis">
              · с {{ formatAgo(item.oldestQueuedAt) }}</span
            >
          </template>
          <template #[`item.lastExchangeAt`]="{ item }">
            {{ item.lastExchangeAt ? formatAgo(item.lastExchangeAt) : '—' }}
          </template>
        </v-data-table>
      </v-card>
    </template>
  </v-container>
</template>

<style scoped>
.pk-system {
  max-width: 1100px;
}
.pk-search {
  max-width: 280px;
}
.pk-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 12px;
}
</style>
