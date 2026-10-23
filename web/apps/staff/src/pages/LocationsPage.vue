<script setup lang="ts">
import type { LocationKind } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'

import { locationsQuery } from '@/queries'

const { data, isPending, isError, error, refetch } = useQuery(locationsQuery)

const headers = [
  { title: 'Код', key: 'code' },
  { title: 'Название', key: 'name' },
  { title: 'Тип', key: 'kind' },
  { title: 'Город', key: 'city' },
  { title: 'Часовой пояс', key: 'timezone' },
]

const kindLabels: Record<LocationKind, string> = {
  warehouse: 'Склад',
  store: 'Магазин',
}
</script>

<template>
  <v-container>
    <h1 class="text-h5 mb-4">Точки сети</h1>

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
      >
        <template #[`item.code`]="{ value }">
          <span class="pk-code">{{ value }}</span>
        </template>
        <template #[`item.kind`]="{ value }">
          <v-chip size="small" :color="value === 'store' ? 'primary' : 'secondary'">
            {{ kindLabels[value as LocationKind] }}
          </v-chip>
        </template>
      </v-data-table>
    </v-card>
  </v-container>
</template>
