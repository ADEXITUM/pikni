<script setup lang="ts">
import { mdiMagnify } from '@mdi/js'
import { api, ApiError, type StockBalance, type ZoneKind } from '@pikni/api'
import { useInfiniteQuery } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'

import { zoneLabels } from '@/labels'
import { useLive } from '@/live'
import { formatPrice, variantLabel } from '@/products'
import { useLocationChoice } from '@/stock'
import LocationSelect from '@/components/LocationSelect.vue'

const location = useLocationChoice('availability.view')
useLive(
  () => (location.selectedId.value ? [`location:${location.selectedId.value}`] : []),
  [['stock']],
)
const zone = ref<string | null>(null)
const zones = computed(() => location.selected.value?.zones ?? [])
watch(location.selectedId, () => (zone.value = null))

// Поиск — на сервере, через 300 мс после последней буквы или сразу по Enter (так заканчивает
// код ручной сканер).
const filter = ref('')
const search = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
function applySearch() {
  clearTimeout(timer)
  search.value = filter.value.trim()
}
watch(filter, () => {
  clearTimeout(timer)
  timer = setTimeout(applySearch, 300)
})

const { data, isPending, error, hasNextPage, fetchNextPage, isFetchingNextPage } = useInfiniteQuery(
  {
    queryKey: ['stock', location.selectedId, zone, search],
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/stock/balances', {
        params: {
          query: {
            locationId: location.selectedId.value!,
            zoneId: zone.value ?? undefined,
            q: search.value || undefined,
            after: pageParam,
          },
        },
      })
      if (error) throw new ApiError(error)
      return data
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.nextAfter,
    enabled: computed(() => !!location.selectedId.value),
  },
)

// Сервер отдаёт строку на каждую зону — на экране один вариант в одной строке, зоны колонками.
interface Row {
  variant: StockBalance
  zones: Partial<Record<ZoneKind, StockBalance>>
  deficit: number
}
const rows = computed(() => {
  const byVariant = new Map<string, Row>()
  for (const b of data.value?.pages.flatMap((p) => p.items) ?? []) {
    let row = byVariant.get(b.variantId)
    if (!row) byVariant.set(b.variantId, (row = { variant: b, zones: {}, deficit: 0 }))
    row.zones[b.zoneKind] = b
    row.deficit += b.deficit
  }
  return [...byVariant.values()]
})
const columns = computed(() =>
  zones.value.filter((z) => !zone.value || z.id === zone.value).map((z) => z.kind),
)
const hasDeficit = computed(() => rows.value.some((r) => r.deficit > 0))
const hasCost = computed(() => rows.value.some((r) => r.variant.avgCost !== undefined))
</script>

<template>
  <v-container class="pk-stock">
    <h1 class="text-headline-small mb-1">Остатки</h1>
    <p class="text-body-medium text-medium-emphasis mb-4">
      Остатки по зонам меняются только проведёнными документами. Нажмите на количество, чтобы
      открыть карточку движения.
    </p>

    <div class="d-flex flex-wrap align-center ga-2 mb-3">
      <LocationSelect
        v-model="location.selectedId.value"
        :locations="location.options.value"
        label="Точка"
        density="compact"
        hide-details
        class="pk-filter"
      />
      <v-chip-group v-model="zone" selected-class="text-primary" data-test="zones">
        <v-chip :value="null" filter>Все зоны</v-chip>
        <v-chip v-for="z in zones" :key="z.id" :value="z.id" filter>{{
          zoneLabels[z.kind]
        }}</v-chip>
      </v-chip-group>
    </div>
    <v-text-field
      v-model="filter"
      :prepend-inner-icon="mdiMagnify"
      placeholder="Название, артикул, штрихкод или марка"
      density="compact"
      hide-details
      clearable
      class="mb-3"
      @keydown.enter="applySearch"
    />

    <v-alert v-if="error" type="error">{{ error.message }}</v-alert>
    <v-progress-linear v-if="isPending && location.selectedId.value" indeterminate />
    <p v-else-if="!rows.length" class="text-body-medium text-medium-emphasis">
      {{ search ? 'Ничего не найдено.' : 'В этой точке пока нет остатков.' }}
    </p>
    <v-table v-else density="comfortable" data-test="balances">
      <thead>
        <tr>
          <th>Артикул</th>
          <th>Товар</th>
          <th v-for="k in columns" :key="k" class="text-end">{{ zoneLabels[k] }}</th>
          <th v-if="hasDeficit" class="text-end">Недостача к разбору</th>
          <th v-if="hasCost" class="text-end">Средняя себестоимость</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in rows" :key="r.variant.variantId" data-test="balance">
          <td class="pk-code">{{ r.variant.article }}</td>
          <td>
            <div>{{ r.variant.name }}</div>
            <div class="text-body-small text-medium-emphasis">{{ variantLabel(r.variant) }}</div>
          </td>
          <td v-for="k in columns" :key="k" class="text-end">
            <router-link
              v-if="r.zones[k]"
              :to="{
                name: 'stock-moves',
                query: { variantId: r.variant.variantId, zoneId: r.zones[k]!.zoneId },
              }"
            >
              {{ r.zones[k]!.onHand }}
            </router-link>
            <span v-else class="text-medium-emphasis">0</span>
            <div v-if="r.zones[k]?.reserved" class="text-body-small text-medium-emphasis">
              в резерве {{ r.zones[k]!.reserved }}
            </div>
          </td>
          <td v-if="hasDeficit" class="text-end" :class="{ 'text-error': r.deficit }">
            {{ r.deficit || '' }}
          </td>
          <td v-if="hasCost" class="text-end">
            {{ r.variant.avgCost !== undefined ? formatPrice(r.variant.avgCost) : '' }}
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
.pk-stock {
  max-width: 1100px;
}
.pk-filter {
  max-width: 320px;
}
</style>
