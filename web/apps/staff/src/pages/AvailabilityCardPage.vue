<script setup lang="ts">
import {
  mdiArrowLeft,
  mdiBookmarkOutline,
  mdiMapMarkerRadiusOutline,
  mdiTruckDeliveryOutline,
} from '@mdi/js'
import { api, ApiError, type AvailabilityCell } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useDisplay } from 'vuetify'

import LocationsMap, { type MapPoint } from '@/components/LocationsMap.vue'
import SimilarProducts from '@/components/SimilarProducts.vue'
import { useLive } from '@/live'
import { distanceKm, formatDistance, inScope, type Scope } from '@/nearby'
import { formatAbv, formatPrice } from '@/products'
import { subjectsQuery } from '@/queries'
import { useMe } from '@/session'
import { ALL_LOCATIONS, useCurrentLocation } from '@/stores/currentLocation'

const route = useRoute()
const router = useRouter()
const productId = computed(() => String(route.params.productId))
const current = useCurrentLocation()
const { data: me } = useMe()
const { xs } = useDisplay()
const hereId = computed(() =>
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
const { data: subjects } = useQuery(subjectsQuery)
const regionName = computed(() => new Map((subjects.value ?? []).map((s) => [s.code, s.name])))

const variantId = ref<string>()
watch(
  () => card.value?.variants,
  (vs) => {
    if (vs && !vs.some((v) => v.id === variantId.value)) variantId.value = vs[0]?.id
  },
  { immediate: true },
)

const cells = computed(() => {
  const m = new Map<string, AvailabilityCell>()
  for (const c of card.value?.cells ?? []) m.set(`${c.variantId}:${c.locationId}`, c)
  return m
})
const here = computed(() => card.value?.locations.find((l) => l.id === hereId.value))
const hereCell = computed(() =>
  here.value && variantId.value
    ? cells.value.get(`${variantId.value}:${here.value.id}`)
    : undefined,
)
const onHand = (c?: AvailabilityCell) => (c ? c.floor + c.storage : 0)

// Строки — точки, где выбранный вариант есть или едет; своя точка — отдельно, сверху.
const rows = computed(() =>
  (card.value?.locations ?? []).flatMap((l) => {
    const c = variantId.value ? cells.value.get(`${variantId.value}:${l.id}`) : undefined
    if (!c || l.id === hereId.value) return []
    return [
      {
        ...l,
        region: regionName.value.get(l.subjectCode) ?? l.subjectCode,
        floor: c.floor,
        storage: c.storage,
        reserved: c.reserved,
        inTransit: c.inTransit,
        onHand: onHand(c),
        distance: distanceKm(here.value, l),
      },
    ]
  }),
)
type Row = (typeof rows.value)[number]

function summary(list: Row[]) {
  const stocked = list.filter((r) => r.onHand > 0)
  return { points: stocked.length, qty: stocked.reduce((n, r) => n + r.onHand, 0) }
}
const counts = computed(() => ({
  city: summary(rows.value.filter((r) => inScope('city', here.value, r))),
  region: summary(rows.value.filter((r) => inScope('region', here.value, r))),
  network: summary(rows.value),
}))
const nearest = computed(
  () =>
    rows.value
      .filter((r) => r.onHand > 0 && r.distance !== undefined)
      .sort((a, b) => a.distance! - b.distance!)[0],
)

// Сначала — свой город; если там нет, сразу регион или вся сеть. Шире — по кнопке.
const chosenScope = ref<Scope>()
const scope = computed<Scope>(() => {
  if (chosenScope.value) return chosenScope.value
  if (!here.value) return 'network'
  return counts.value.city.points ? 'city' : counts.value.region.points ? 'region' : 'network'
})
const scopes = computed(() => [
  { value: 'city' as const, title: here.value?.city ?? 'Город', ...counts.value.city },
  {
    value: 'region' as const,
    title: here.value ? (regionName.value.get(here.value.subjectCode) ?? 'Регион') : 'Регион',
    ...counts.value.region,
  },
  { value: 'network' as const, title: 'Вся сеть', ...counts.value.network },
])

// Плитка региона не нужна, если в регионе товар есть только в своём городе.
const tiles = computed(() =>
  !here.value
    ? scopes.value.slice(2)
    : scopes.value.filter(
        (s) =>
          s.value !== 'region' ||
          s.points !== counts.value.city.points ||
          s.qty !== counts.value.city.qty,
      ),
)

const region = ref<string | null>(null)
const city = ref<string | null>(null)
const kind = ref<'store' | 'warehouse' | null>(null)
const regionItems = computed(() =>
  [...new Set(rows.value.map((r) => r.subjectCode))]
    .map((code) => ({ value: code, title: regionName.value.get(code) ?? code }))
    .sort((a, b) => a.title.localeCompare(b.title, 'ru')),
)
const cityItems = computed(() =>
  [
    ...new Set(
      rows.value.filter((r) => !region.value || r.subjectCode === region.value).map((r) => r.city),
    ),
  ].sort((a, b) => a.localeCompare(b, 'ru')),
)
const shown = computed(() =>
  rows.value.filter(
    (r) =>
      inScope(scope.value, here.value, r) &&
      (scope.value !== 'network' || !region.value || r.subjectCode === region.value) &&
      (scope.value !== 'network' || !city.value || r.city === city.value) &&
      (!kind.value || r.kind === kind.value),
  ),
)
const maxQty = computed(() => Math.max(1, ...shown.value.map((r) => r.onHand)))

const headers = computed(() => [
  { title: 'Точка', key: 'code' },
  ...(scope.value === 'city' ? [] : [{ title: 'Город', key: 'city' }]),
  ...(here.value?.lat !== undefined
    ? [{ title: 'Расстояние', key: 'distance', align: 'end' as const }]
    : []),
  { title: 'Есть', key: 'onHand', align: 'end' as const, width: 200 },
  { title: 'Резерв', key: 'reserved', align: 'end' as const },
  { title: 'Едет', key: 'inTransit', align: 'end' as const },
  { title: '', key: 'actions', sortable: false, align: 'end' as const },
])
const sortBy = ref<{ key: string; order: 'asc' | 'desc' }[]>([])
watch(
  here,
  (h) => {
    if (!sortBy.value.length)
      sortBy.value = [
        h?.lat !== undefined ? { key: 'distance', order: 'asc' } : { key: 'onHand', order: 'desc' },
      ]
  },
  { immediate: true },
)

const points = computed<MapPoint[]>(() => [
  ...(here.value?.lat !== undefined && here.value.lon !== undefined
    ? [
        {
          id: here.value.id,
          lat: here.value.lat,
          lon: here.value.lon,
          title: `${here.value.code} · ${here.value.name}`,
          subtitle: onHand(hereCell.value)
            ? `у вас ${onHand(hereCell.value)} шт.`
            : 'ваша точка — нет в наличии',
          highlight: true,
        },
      ]
    : []),
  ...shown.value.flatMap((r) =>
    r.lat === undefined || r.lon === undefined || r.onHand === 0
      ? []
      : [
          {
            id: r.id,
            lat: r.lat,
            lon: r.lon,
            title: `${r.code} · ${r.name}`,
            subtitle: `в наличии ${r.onHand} шт.`,
          },
        ],
  ),
])

const variant = computed(() => card.value?.variants.find((v) => v.id === variantId.value))
const variantQty = (id: string) =>
  (card.value?.cells ?? []).filter((c) => c.variantId === id).reduce((n, c) => n + onHand(c), 0)

// Заказать перемещение можно из чужой точки в свой магазин (НЛ-3, СО-4).
const canRequest = computed(
  () => me.value?.permissions.includes('transfer.request') && !!hereId.value,
)
function requestTransfer(from: string) {
  if (!variant.value) return
  void router.push({
    name: 'transfer-new',
    query: { article: variant.value.article, from, to: hereId.value ?? undefined },
  })
}
const plural = (n: number, one: string, few: string, many: string) => {
  const d = n % 10
  const h = n % 100
  return d === 1 && h !== 11 ? one : d >= 2 && d <= 4 && (h < 12 || h > 14) ? few : many
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

      <v-chip-group
        v-if="card.variants.length > 1"
        v-model="variantId"
        mandatory
        selected-class="text-primary"
        class="mb-2"
        data-test="variants"
      >
        <v-chip v-for="v in card.variants" :key="v.id" :value="v.id" filter variant="outlined">
          {{ v.label }}<template v-if="v.price"> · {{ formatPrice(v.price) }}</template>
          <span class="text-medium-emphasis ms-2">{{ variantQty(v.id) }} шт.</span>
        </v-chip>
      </v-chip-group>
      <div v-else-if="variant" class="text-body-medium mb-3">
        {{ variant.label }} · <span class="pk-code">{{ variant.article }}</span>
        <template v-if="variant.price"> · {{ formatPrice(variant.price) }}</template>
      </div>

      <div class="pk-tiles mb-4">
        <v-card v-if="here" variant="tonal" color="primary" data-test="here-tile">
          <v-card-text>
            <div class="text-label-large">У вас · {{ here.code }}</div>
            <div class="text-display-small my-1">{{ onHand(hereCell) }}</div>
            <div class="text-body-small">
              <template v-if="here.kind === 'store'"
                >в зале {{ hereCell?.floor ?? 0 }} · на складе
                {{ hereCell?.storage ?? 0 }}</template
              >
              <template v-else>на складе {{ hereCell?.storage ?? 0 }}</template>
              <template v-if="hereCell?.reserved"> · резерв {{ hereCell.reserved }}</template>
              <template v-if="hereCell?.inTransit"> · едет {{ hereCell.inTransit }}</template>
            </div>
          </v-card-text>
        </v-card>
        <v-card v-if="nearest" variant="outlined">
          <v-card-text>
            <div class="text-label-large">Ближайшая точка</div>
            <div class="text-title-large my-1">
              {{ nearest.code }} · {{ formatDistance(nearest.distance) }}
            </div>
            <div class="text-body-small text-medium-emphasis">
              {{ nearest.name }}, {{ nearest.onHand }} шт.
            </div>
          </v-card-text>
        </v-card>
        <v-card v-for="s in tiles" :key="s.value" variant="outlined">
          <v-card-text>
            <div class="text-label-large text-truncate">{{ s.title }}</div>
            <div class="text-title-large my-1">{{ s.qty }} шт.</div>
            <div class="text-body-small text-medium-emphasis">
              в {{ s.points }} {{ plural(s.points, 'точке', 'точках', 'точках') }}
            </div>
          </v-card-text>
        </v-card>
      </div>

      <v-card class="mb-4">
        <v-card-title class="d-flex flex-wrap align-center ga-3">
          <v-icon :icon="mdiMapMarkerRadiusOutline" size="small" />
          Где есть
          <v-spacer />
          <v-btn-toggle
            v-if="here"
            :model-value="scope"
            mandatory
            density="compact"
            variant="outlined"
            divided
            data-test="scope"
            @update:model-value="(v: Scope) => (chosenScope = v)"
          >
            <v-btn v-for="s in scopes" :key="s.value" :value="s.value" size="small">
              {{ s.value === 'network' ? 'Вся сеть' : s.value === 'city' ? 'Мой город' : 'Регион' }}
              <v-badge inline :content="s.points" color="surface-variant" />
            </v-btn>
          </v-btn-toggle>
        </v-card-title>
        <v-card-text>
          <div class="pk-filters mb-2">
            <template v-if="scope === 'network'">
              <v-autocomplete
                v-model="region"
                :items="regionItems"
                label="Регион"
                density="compact"
                hide-details
                clearable
                @update:model-value="city = null"
              />
              <v-autocomplete
                v-model="city"
                :items="cityItems"
                label="Город"
                density="compact"
                hide-details
                clearable
              />
            </template>
            <v-select
              v-model="kind"
              :items="[
                { value: 'store', title: 'Магазины' },
                { value: 'warehouse', title: 'Склады' },
              ]"
              label="Тип точки"
              density="compact"
              hide-details
              clearable
            />
          </div>

          <v-data-table
            v-model:sort-by="sortBy"
            :headers="headers"
            :items="shown"
            :items-per-page="xs ? 10 : 20"
            :items-per-page-options="[10, 20, 50, 100]"
            :mobile="xs"
            density="comfortable"
            no-data-text="В этой зоне нет — расширьте поиск"
            data-test="availability-table"
          >
            <template #[`item.code`]="{ item }">
              <div class="py-2" data-test="availability-row">
                <div class="font-weight-medium">
                  {{ item.code }}
                  <v-chip v-if="item.kind === 'warehouse'" size="x-small" class="ms-1"
                    >склад</v-chip
                  >
                </div>
                <div class="text-body-small text-medium-emphasis">
                  {{ item.name }}<template v-if="item.address"> · {{ item.address }}</template>
                </div>
              </div>
            </template>
            <template #[`item.city`]="{ item }">
              <div>{{ item.city }}</div>
              <div v-if="item.region !== item.city" class="text-body-small text-medium-emphasis">
                {{ item.region }}
              </div>
            </template>
            <template #[`item.distance`]="{ item }">{{ formatDistance(item.distance) }}</template>
            <template #[`item.onHand`]="{ item }">
              <div class="pk-qty">
                <div class="pk-bar">
                  <span :style="{ width: `${(item.onHand / maxQty) * 100}%` }" />
                </div>
                <span class="text-body-large font-weight-medium">{{ item.onHand }}</span>
              </div>
              <div class="text-body-small text-medium-emphasis">
                <template v-if="item.kind === 'store'"
                  >зал {{ item.floor }} · склад {{ item.storage }}</template
                >
                <template v-else>на складе</template>
              </div>
            </template>
            <template #[`item.reserved`]="{ item }">
              <span :class="{ 'text-disabled': !item.reserved }">{{ item.reserved }}</span>
            </template>
            <template #[`item.inTransit`]="{ item }">
              <span :class="{ 'text-disabled': !item.inTransit }">{{ item.inTransit }}</span>
            </template>
            <template #[`item.actions`]="{ item }">
              <div class="d-flex justify-end ga-1">
                <v-btn
                  v-if="canRequest && item.onHand > 0"
                  :icon="mdiTruckDeliveryOutline"
                  size="small"
                  variant="text"
                  title="Привезти в мою точку"
                  data-test="request-transfer"
                  @click="requestTransfer(item.id)"
                />
                <v-btn
                  v-if="
                    router.hasRoute('reservation-new') && item.kind === 'store' && item.onHand > 0
                  "
                  :icon="mdiBookmarkOutline"
                  size="small"
                  variant="text"
                  title="Отложить для покупателя"
                  :to="{
                    name: 'reservation-new',
                    query: { article: variant?.article, location: item.id },
                  }"
                />
              </div>
            </template>
          </v-data-table>
        </v-card-text>
        <v-card-text class="pt-0">
          <LocationsMap :points="points" />
        </v-card-text>
      </v-card>

      <v-card>
        <v-card-title>Похожие товары</v-card-title>
        <v-card-text>
          <SimilarProducts v-if="variantId" :variant-id="variantId" :here="hereId" />
        </v-card-text>
      </v-card>
    </template>
  </v-container>
</template>

<style scoped>
.pk-tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 12px;
}
.pk-filters {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 260px));
  gap: 8px;
}
.pk-qty {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}
.pk-bar {
  flex: 1;
  max-width: 120px;
  height: 6px;
  border-radius: 3px;
  background: rgba(var(--v-theme-on-surface), 0.08);
  overflow: hidden;
}
.pk-bar span {
  display: block;
  height: 100%;
  border-radius: 3px;
  background: rgb(var(--v-theme-success));
}
</style>
