<script setup lang="ts">
import { mdiFilterRemoveOutline, mdiMagnify, mdiPlus } from '@mdi/js'
import type { Location, LocationKind } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed, reactive } from 'vue'
import { useRouter } from 'vue-router'

import { formatHours } from '@/hours'
import { locationKindLabels } from '@/labels'
import { locationsQuery, subjectsQuery } from '@/queries'
import { useMe } from '@/session'

const router = useRouter()
const { data: me } = useMe()
const { data, isPending, isError, error, refetch } = useQuery(locationsQuery)

const canEdit = computed(() => me.value?.permissions.includes('refs.edit') ?? false)
const { data: subjects } = useQuery(subjectsQuery)

// Точек федеральной сети — тысячи: поиск, фильтры по региону, городу, типу и статусу,
// сортировка по столбцам и страница по 25. Справочник точек и так загружен целиком.
const filters = reactive({
  search: '',
  subject: null as string | null,
  city: null as string | null,
  kind: null as LocationKind | null,
  active: true as boolean | null,
})
const subjectName = computed(() => new Map((subjects.value ?? []).map((s) => [s.code, s.name])))
const subjectItems = computed(() =>
  [...new Set((data.value ?? []).map((l) => l.subjectCode))]
    .map((code) => ({ value: code, title: subjectName.value.get(code) ?? code }))
    .sort((a, b) => a.title.localeCompare(b.title, 'ru')),
)
const cityItems = computed(() =>
  [
    ...new Set(
      (data.value ?? [])
        .filter((l) => !filters.subject || l.subjectCode === filters.subject)
        .map((l) => l.city),
    ),
  ].sort((a, b) => a.localeCompare(b, 'ru')),
)
const kindItems = Object.entries(locationKindLabels).map(([value, title]) => ({ value, title }))
const rows = computed(() => {
  const q = filters.search.trim().toLowerCase()
  return (data.value ?? [])
    .filter(
      (l) =>
        (!filters.subject || l.subjectCode === filters.subject) &&
        (!filters.city || l.city === filters.city) &&
        (!filters.kind || l.kind === filters.kind) &&
        (filters.active === null || l.isActive === filters.active) &&
        (!q || `${l.code} ${l.name} ${l.address ?? ''}`.toLowerCase().includes(q)),
    )
    .map((l) => ({ ...l, region: subjectName.value.get(l.subjectCode) ?? l.subjectCode }))
})
const hasFilters = computed(() =>
  Boolean(
    filters.search || filters.subject || filters.city || filters.kind || filters.active !== true,
  ),
)
function clearFilters() {
  Object.assign(filters, { search: '', subject: null, city: null, kind: null, active: true })
}

const headers = [
  { title: 'Код', key: 'code' },
  { title: 'Название', key: 'name' },
  { title: 'Тип', key: 'kind' },
  { title: 'Город', key: 'city' },
  { title: 'Регион', key: 'region' },
  { title: 'Режим работы', key: 'openingHours', sortable: false },
]

function open(_: Event, { item }: { item: Location }) {
  void router.push({ name: 'location', params: { id: item.id } })
}
</script>

<template>
  <v-container>
    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <h1 class="text-headline-small flex-grow-1">Точки сети</h1>
      <span v-if="data" class="text-body-medium text-medium-emphasis"
        >{{ rows.length }} из {{ data.length }}</span
      >
      <v-btn
        v-if="canEdit"
        color="primary"
        :prepend-icon="mdiPlus"
        :to="{ name: 'location-new' }"
        data-test="add-location"
      >
        Добавить точку
      </v-btn>
    </div>

    <v-row density="compact" class="mb-2">
      <v-col cols="12" md="3">
        <v-text-field
          v-model="filters.search"
          :prepend-inner-icon="mdiMagnify"
          label="Код, название, адрес"
          clearable
        />
      </v-col>
      <v-col cols="6" md="2">
        <v-autocomplete
          v-model="filters.subject"
          :items="subjectItems"
          label="Регион"
          clearable
          @update:model-value="filters.city = null"
        />
      </v-col>
      <v-col cols="6" md="2">
        <v-autocomplete v-model="filters.city" :items="cityItems" label="Город" clearable />
      </v-col>
      <v-col cols="5" md="2">
        <v-select v-model="filters.kind" :items="kindItems" label="Тип" clearable />
      </v-col>
      <v-col cols="5" md="2">
        <v-select
          v-model="filters.active"
          :items="[
            { value: true, title: 'действует' },
            { value: false, title: 'закрыта' },
          ]"
          label="Статус"
          clearable
        />
      </v-col>
      <v-col cols="2" md="1" class="d-flex align-center">
        <v-btn
          v-if="hasFilters"
          :icon="mdiFilterRemoveOutline"
          variant="text"
          title="Сбросить фильтры"
          @click="clearFilters()"
        />
      </v-col>
    </v-row>

    <v-alert v-if="isError" type="error" title="Не удалось загрузить точки" class="mb-4">
      {{ error?.message }}
      <template #append>
        <v-btn variant="text" @click="refetch()">Повторить</v-btn>
      </template>
    </v-alert>

    <v-card>
      <v-data-table
        :headers="headers"
        :items="rows"
        :loading="isPending"
        :items-per-page="25"
        :items-per-page-options="[25, 50, 100]"
        item-value="id"
        mobile-breakpoint="sm"
        no-data-text="Точек не нашлось"
        hover
        class="pk-clickable"
        @click:row="open"
      >
        <template #[`item.code`]="{ value }">
          <span class="pk-code">{{ value }}</span>
        </template>
        <template #[`item.name`]="{ item }">
          {{ item.name }}
          <v-chip v-if="!item.isActive" size="x-small" color="error" class="ml-2"
            >не действует</v-chip
          >
        </template>
        <template #[`item.kind`]="{ value }">
          <v-chip size="small" :color="value === 'store' ? 'primary' : 'secondary'">
            {{ locationKindLabels[value as LocationKind] }}
          </v-chip>
        </template>
        <template #[`item.openingHours`]="{ item }">
          <span class="text-body-medium">{{ formatHours(item.openingHours) }}</span>
        </template>
      </v-data-table>
    </v-card>
  </v-container>
</template>

<style scoped>
.pk-clickable :deep(tbody tr) {
  cursor: pointer;
}
</style>
