<script setup lang="ts">
import { mdiArrowLeft, mdiBookmarkOutline, mdiTruckDeliveryOutline } from '@mdi/js'
import { api, ApiError, type AvailabilityCell, type AvailabilityLocation } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import LocationsMap, { type MapPoint } from '@/components/LocationsMap.vue'
import SimilarProducts from '@/components/SimilarProducts.vue'
import { useLive } from '@/live'
import { formatAbv, formatPrice } from '@/products'
import { useMe } from '@/session'
import { ALL_LOCATIONS, useCurrentLocation } from '@/stores/currentLocation'

const route = useRoute()
const router = useRouter()
const productId = computed(() => String(route.params.productId))
const current = useCurrentLocation()
const { data: me } = useMe()
const here = computed(() =>
  current.selected && current.selected !== ALL_LOCATIONS ? current.selected : null,
)

const key = computed(() => ['availability-card', productId.value])
const { data: card, error } = useQuery({
  queryKey: key,
  queryFn: async () => {
    const { data, error } = await api.GET('/availability/{productId}', {
      params: { path: { productId: productId.value } },
    })
    if (error) throw new ApiError(error)
    return data
  },
})
// Остатки меняются у всех, у кого карточка открыта (УВ-1).
useLive(
  () => [`product:${productId.value}`],
  () => [key.value, ['similar']],
)

const cells = computed(() => {
  const m = new Map<string, AvailabilityCell>()
  for (const c of card.value?.cells ?? []) m.set(`${c.variantId}:${c.locationId}`, c)
  return m
})
const cell = (variantId: string, locationId: string) =>
  cells.value.get(`${variantId}:${locationId}`)

// Столбцы — точки, сгруппированные по городам; своя точка первой в своём городе.
const cities = computed(() => {
  const groups = new Map<string, AvailabilityLocation[]>()
  for (const l of card.value?.locations ?? [])
    groups.set(l.city, [...(groups.get(l.city) ?? []), l])
  for (const list of groups.values())
    list.sort((a, b) => Number(b.id === here.value) - Number(a.id === here.value))
  return [...groups.entries()].map(([city, locations]) => ({ city, locations }))
})
const columns = computed(() => cities.value.flatMap((c) => c.locations))

const points = computed<MapPoint[]>(() =>
  (card.value?.locations ?? []).flatMap((l) => {
    const qty = (card.value?.cells ?? [])
      .filter((c) => c.locationId === l.id)
      .reduce((n, c) => n + c.floor + c.storage, 0)
    if (l.lat === undefined || l.lon === undefined || (qty === 0 && l.id !== here.value)) return []
    return [
      {
        id: l.id,
        lat: l.lat,
        lon: l.lon,
        title: `${l.code} · ${l.name}`,
        subtitle: qty > 0 ? `в наличии ${qty} шт.` : 'ваша точка — нет в наличии',
        highlight: l.id === here.value,
      },
    ]
  }),
)

const variantId = ref<string>()
watch(
  () => card.value?.variants,
  (vs) => {
    if (vs && !vs.some((v) => v.id === variantId.value)) variantId.value = vs[0]?.id
  },
  { immediate: true },
)

// Заказать перемещение можно из чужой точки в свой магазин (НЛ-3, СО-4).
const canRequest = computed(
  () => me.value?.permissions.includes('transfer.request') && !!here.value,
)
function requestTransfer(article: string, from: string) {
  void router.push({
    name: 'transfer-new',
    query: { article, from, to: here.value ?? undefined },
  })
}
</script>

<template>
  <v-container>
    <v-btn :prepend-icon="mdiArrowLeft" variant="text" class="mb-2" @click="router.back()"
      >Назад</v-btn
    >
    <v-alert v-if="error" type="error">{{ error.message }}</v-alert>
    <template v-if="card">
      <h1 class="text-headline-small">{{ card.name }}</h1>
      <div class="text-body-medium text-medium-emphasis mb-3">
        {{ card.categoryName }}<template v-if="card.abv"> · {{ formatAbv(card.abv) }}</template> ·
        {{ card.countryName }}<template v-if="card.brandName"> · {{ card.brandName }}</template>
        <template v-if="card.kindCode"> · вид {{ card.kindCode }}</template>
      </div>
      <div v-if="card.attributes.length" class="d-flex flex-wrap ga-2 mb-4">
        <v-chip v-for="a in card.attributes" :key="a.code" size="small" variant="tonal">
          {{ a.name }}: {{ a.value }}
        </v-chip>
      </div>

      <v-card class="mb-4">
        <div class="pk-table-wrap">
          <table class="pk-avail" data-test="availability-table">
            <thead>
              <tr>
                <th rowspan="2" class="text-start">Вариант</th>
                <th v-for="c in cities" :key="c.city" :colspan="c.locations.length">
                  {{ c.city }}
                </th>
              </tr>
              <tr>
                <th
                  v-for="l in columns"
                  :key="l.id"
                  :class="{ 'pk-here': l.id === here }"
                  :title="l.name"
                >
                  {{ l.code }}<span v-if="l.id === here"> · вы</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="v in card.variants" :key="v.id">
                <td class="text-start">
                  <div>{{ v.label }}</div>
                  <div class="text-body-small text-medium-emphasis">
                    <span class="pk-code">{{ v.article }}</span>
                    <template v-if="v.price"> · {{ formatPrice(v.price) }}</template>
                  </div>
                </td>
                <td
                  v-for="l in columns"
                  :key="l.id"
                  :class="{ 'pk-here': l.id === here }"
                  data-test="availability-cell"
                >
                  <v-menu v-if="cell(v.id, l.id)">
                    <template #activator="{ props }">
                      <button v-bind="props" class="pk-cell">
                        <div class="text-body-large">
                          {{
                            l.kind === 'store' ? cell(v.id, l.id)!.floor : cell(v.id, l.id)!.storage
                          }}
                        </div>
                        <div class="text-body-small text-medium-emphasis">
                          <template v-if="l.kind === 'store'"
                            >склад {{ cell(v.id, l.id)!.storage }} ·
                          </template>
                          резерв {{ cell(v.id, l.id)!.reserved }} · в пути
                          {{ cell(v.id, l.id)!.inTransit }}
                        </div>
                      </button>
                    </template>
                    <v-list density="compact">
                      <v-list-item
                        :prepend-icon="mdiTruckDeliveryOutline"
                        title="Привезти в мою точку"
                        :disabled="!canRequest || l.id === here"
                        data-test="request-transfer"
                        @click="requestTransfer(v.article, l.id)"
                      />
                      <v-list-item
                        v-if="router.hasRoute('reservation-new')"
                        :prepend-icon="mdiBookmarkOutline"
                        title="Отложить для покупателя"
                        :to="{
                          name: 'reservation-new',
                          query: { article: v.article, location: l.id },
                        }"
                      />
                    </v-list>
                  </v-menu>
                  <span v-else class="text-disabled">—</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <v-card-text class="text-body-small text-medium-emphasis pt-0">
          В ячейке магазина крупно — в торговом зале; у склада — на складе. Нажмите на ячейку, чтобы
          заказать перемещение в свою точку.
        </v-card-text>
      </v-card>

      <v-card class="mb-4">
        <v-card-title class="d-flex flex-wrap align-center ga-2">
          Похожие товары
          <v-chip-group v-if="card.variants.length > 1" v-model="variantId" mandatory>
            <v-chip v-for="v in card.variants" :key="v.id" :value="v.id" size="small" filter>{{
              v.label
            }}</v-chip>
          </v-chip-group>
        </v-card-title>
        <v-card-text>
          <SimilarProducts v-if="variantId" :variant-id="variantId" :here="here" />
        </v-card-text>
      </v-card>

      <v-card>
        <v-card-title>Где есть</v-card-title>
        <v-card-text>
          <LocationsMap :points="points" />
        </v-card-text>
      </v-card>
    </template>
  </v-container>
</template>

<style scoped>
.pk-table-wrap {
  overflow-x: auto;
}
.pk-avail {
  border-collapse: collapse;
  width: 100%;
}
.pk-avail th,
.pk-avail td {
  padding: 8px 12px;
  border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  text-align: center;
  white-space: nowrap;
}
.pk-avail th {
  font-weight: 600;
  font-size: 0.85rem;
}
.pk-here {
  background: rgba(var(--v-theme-primary), 0.08);
}
.pk-cell {
  background: none;
  border: 0;
  cursor: pointer;
  color: inherit;
  font: inherit;
}
</style>
