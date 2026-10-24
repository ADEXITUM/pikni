<script setup lang="ts">
import { mdiArrowLeft } from '@mdi/js'
import { api, ApiError, type StockBucket } from '@pikni/api'
import { useInfiniteQuery } from '@tanstack/vue-query'
import { computed } from 'vue'
import { useRoute } from 'vue-router'

import { formatDateTime } from '@/format'
import { documentKindLabels, zoneLabels } from '@/labels'
import { formatPrice, variantLabel } from '@/products'
import { signed } from '@/stock'

const route = useRoute()
const variantId = computed(() => String(route.query.variantId ?? ''))
const zoneId = computed(() => String(route.query.zoneId ?? ''))

const { data, isPending, error, hasNextPage, fetchNextPage, isFetchingNextPage } = useInfiniteQuery(
  {
    queryKey: ['stock-moves', variantId, zoneId],
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/stock/moves', {
        params: { query: { variantId: variantId.value, zoneId: zoneId.value, before: pageParam } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (page) => page.nextBefore,
  },
)
const moves = computed(() => data.value?.pages.flatMap((p) => p.items) ?? [])
const header = computed(() => data.value?.pages[0])
const hasCost = computed(() => moves.value.some((m) => m.costAmount !== undefined))
const bucketLabels: Record<StockBucket, string> = {
  on_hand: '',
  in_transit: 'в пути',
  deficit: 'недостача к разбору',
}
</script>

<template>
  <v-container class="pk-moves">
    <v-btn :prepend-icon="mdiArrowLeft" :to="{ name: 'stock' }" variant="text" class="mb-2 ml-n3">
      Остатки
    </v-btn>
    <h1 class="text-headline-small mb-1">Карточка движения</h1>
    <template v-if="header">
      <p class="text-body-medium mb-1">
        <span class="pk-code">{{ header.article }}</span> · {{ header.name }},
        {{ variantLabel(header) }}
      </p>
      <p class="text-body-medium text-medium-emphasis mb-4">
        {{ header.locationName }}, {{ zoneLabels[header.zoneKind].toLowerCase() }}
      </p>
    </template>

    <v-alert v-if="error" type="error">{{ error.message }}</v-alert>
    <v-progress-linear v-if="isPending" indeterminate />
    <p v-else-if="!moves.length" class="text-body-medium text-medium-emphasis">Движений нет.</p>
    <v-table v-else density="comfortable" data-test="moves">
      <thead>
        <tr>
          <th>Когда</th>
          <th>Документ</th>
          <th class="text-end">Изменение</th>
          <th class="text-end">Остаток после</th>
          <th v-if="hasCost" class="text-end">Сумма</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="m in moves" :key="m.id" data-test="move">
          <td>{{ formatDateTime(m.postedAt) }}</td>
          <td>
            <router-link :to="{ name: 'document', params: { id: m.documentId } }">
              {{ documentKindLabels[m.documentKind] }} {{ m.documentNumber }}
            </router-link>
          </td>
          <td class="text-end" :class="m.qty > 0 ? 'text-success' : 'text-error'">
            {{ signed(m.qty) }}
            <div v-if="bucketLabels[m.bucket]" class="text-body-small text-medium-emphasis">
              {{ bucketLabels[m.bucket] }}
            </div>
          </td>
          <td class="text-end">{{ m.onHandAfter }}</td>
          <td v-if="hasCost" class="text-end">
            {{ m.costAmount !== undefined ? formatPrice(m.costAmount) : '' }}
          </td>
        </tr>
      </tbody>
    </v-table>
    <v-btn
      v-if="hasNextPage"
      :loading="isFetchingNextPage"
      variant="text"
      class="mt-2"
      @click="fetchNextPage()"
    >
      Показать ещё
    </v-btn>
  </v-container>
</template>

<style scoped>
.pk-moves {
  max-width: 960px;
}
</style>
