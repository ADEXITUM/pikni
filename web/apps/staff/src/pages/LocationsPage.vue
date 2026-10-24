<script setup lang="ts">
import { mdiPlus } from '@mdi/js'
import type { Location, LocationKind } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed } from 'vue'
import { useRouter } from 'vue-router'

import { formatHours } from '@/hours'
import { locationKindLabels } from '@/labels'
import { locationsQuery } from '@/queries'
import { useMe } from '@/session'

const router = useRouter()
const { data: me } = useMe()
const { data, isPending, isError, error, refetch } = useQuery(locationsQuery)

const canEdit = computed(() => me.value?.permissions.includes('refs.edit') ?? false)

const headers = [
  { title: 'Код', key: 'code' },
  { title: 'Название', key: 'name' },
  { title: 'Тип', key: 'kind' },
  { title: 'Город', key: 'city' },
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

    <v-alert v-if="isError" type="error" title="Не удалось загрузить точки" class="mb-4">
      {{ error?.message }}
      <template #append>
        <v-btn variant="text" @click="refetch()">Повторить</v-btn>
      </template>
    </v-alert>

    <v-card>
      <v-data-table
        :headers="headers"
        :items="data ?? []"
        :loading="isPending"
        :items-per-page="-1"
        item-value="id"
        mobile-breakpoint="sm"
        hide-default-footer
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
