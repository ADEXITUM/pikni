<script setup lang="ts">
import { mdiFilterRemoveOutline, mdiMicrosoftExcel } from '@mdi/js'
import { api, ApiError, type AuditAction } from '@pikni/api'
import { useInfiniteQuery, useQuery } from '@tanstack/vue-query'
import { computed, reactive } from 'vue'
import UserPicker from '@/components/UserPicker.vue'

import {
  auditActionColors,
  auditActionLabels,
  auditActor,
  auditDetails,
  auditObject,
} from '@/audit'
import { formatDateTime } from '@/format'
import { locationsQuery } from '@/queries'

const filters = reactive({
  from: '',
  to: '',
  userId: null as string | null,
  action: null as AuditAction | null,
})

// Даты из полей — локальные сутки: «по 23.10» значит до начала 24.10.
const query = computed(() => {
  const nextDay = (date: string) => {
    const d = new Date(`${date}T00:00`)
    d.setDate(d.getDate() + 1)
    return d.toISOString()
  }
  return {
    from: filters.from ? new Date(`${filters.from}T00:00`).toISOString() : undefined,
    to: filters.to ? nextDay(filters.to) : undefined,
    userId: filters.userId ?? undefined,
    action: filters.action ?? undefined,
  }
})

const { data, isPending, isError, error, refetch, hasNextPage, fetchNextPage, isFetchingNextPage } =
  useInfiniteQuery({
    queryKey: ['audit', query],
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/audit', {
        params: { query: { ...query.value, before: pageParam } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (page) => page.nextBefore,
  })

const entries = computed(() => data.value?.pages.flatMap((p) => p.items) ?? [])

const { data: locations } = useQuery(locationsQuery)
const codes = computed(() => new Map((locations.value ?? []).map((l) => [l.id, l.code])))

const actionItems = Object.entries(auditActionLabels).map(([value, title]) => ({ value, title }))

const exportUrl = computed(() => {
  const params = new URLSearchParams({ tz: Intl.DateTimeFormat().resolvedOptions().timeZone })
  for (const [key, value] of Object.entries(query.value)) {
    if (value) params.set(key, value)
  }
  return `/api/v1/audit/export?${params}`
})

const hasFilters = computed(() => Object.values(filters).some(Boolean))
function clearFilters() {
  Object.assign(filters, { from: '', to: '', userId: null, action: null })
}

// Порядок задаёт сервер (от новых к старым) и подгрузка страниц, поэтому без сортировки.
const headers = [
  { title: 'Время', key: 'at', sortable: false },
  { title: 'Событие', key: 'action', sortable: false },
  { title: 'Кто', key: 'actor', sortable: false },
  { title: 'Объект', key: 'object', sortable: false },
  { title: '', key: 'data-table-expand', sortable: false },
]
</script>

<template>
  <v-container>
    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <h1 class="text-headline-small flex-grow-1">Журнал действий</h1>
      <v-btn :href="exportUrl" :prepend-icon="mdiMicrosoftExcel" variant="tonal" data-test="export">
        Выгрузить в Excel
      </v-btn>
    </div>

    <v-row density="compact" class="mb-2">
      <v-col cols="6" md="2">
        <v-text-field v-model="filters.from" type="date" label="С" />
      </v-col>
      <v-col cols="6" md="2">
        <v-text-field v-model="filters.to" type="date" label="По" />
      </v-col>
      <v-col cols="12" md="4">
        <UserPicker v-model="filters.userId" />
      </v-col>
      <v-col cols="12" md="3">
        <v-select v-model="filters.action" :items="actionItems" label="Событие" clearable />
      </v-col>
      <v-col cols="12" md="1" class="d-flex align-center">
        <v-btn
          v-if="hasFilters"
          :icon="mdiFilterRemoveOutline"
          variant="text"
          title="Сбросить фильтры"
          @click="clearFilters()"
        />
      </v-col>
    </v-row>

    <v-alert v-if="isError" type="error" title="Не удалось загрузить журнал" class="mb-4">
      {{ error?.message }}
      <template #append>
        <v-btn variant="text" @click="refetch()">Повторить</v-btn>
      </template>
    </v-alert>

    <v-card>
      <v-data-table
        :headers="headers"
        :items="entries"
        :loading="isPending"
        :items-per-page="-1"
        item-value="id"
        show-expand
        mobile-breakpoint="sm"
        hide-default-footer
        no-data-text="Записей нет"
      >
        <template #[`item.at`]="{ item }">
          <span class="text-no-wrap">{{ formatDateTime(item.at) }}</span>
        </template>
        <template #[`item.action`]="{ item }">
          <v-chip size="small" :color="auditActionColors[item.action]">
            {{ auditActionLabels[item.action] ?? item.action }}
          </v-chip>
        </template>
        <template #[`item.actor`]="{ item }">{{ auditActor(item) }}</template>
        <template #[`item.object`]="{ item }">
          <span class="pk-code">{{ auditObject(item) }}</span>
        </template>
        <template #expanded-row="{ columns, item }">
          <tr>
            <td :colspan="columns.length" class="py-3">
              <dl class="pk-details" data-test="details">
                <template v-for="d in auditDetails(item, codes)" :key="d.label">
                  <dt>{{ d.label }}</dt>
                  <dd>{{ d.value }}</dd>
                </template>
              </dl>
            </td>
          </tr>
        </template>
      </v-data-table>
    </v-card>

    <div v-if="hasNextPage" class="text-center mt-4">
      <v-btn variant="tonal" :loading="isFetchingNextPage" @click="fetchNextPage()">
        Показать ещё
      </v-btn>
    </div>
  </v-container>
</template>

<style scoped>
.pk-details {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 4px 16px;
}

.pk-details dt {
  color: rgba(var(--v-theme-on-surface), var(--v-medium-emphasis-opacity));
}
</style>
