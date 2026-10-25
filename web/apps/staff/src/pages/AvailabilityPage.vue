<script setup lang="ts">
import { mdiFilterVariant, mdiMagnify } from '@mdi/js'
import { api, ApiError, type AvailabilityItem, type CategoryAttribute } from '@pikni/api'
import { useInfiniteQuery, useQuery } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import {
  type AvailabilityFilters,
  filterableAttributes,
  filtersFromQuery,
  queryFromFilters,
} from '@/availability'
import { ancestors, categoryItems } from '@/categories'
import { formatAbv, formatPrice } from '@/products'
import {
  brandsQuery,
  categoriesQuery,
  countriesQuery,
  locationsQuery,
  productKindsQuery,
} from '@/queries'
import { useScanner } from '@/scanning'
import { ALL_LOCATIONS, useCurrentLocation } from '@/stores/currentLocation'

const route = useRoute()
const router = useRouter()
const current = useCurrentLocation()
const here = computed(() =>
  current.selected && current.selected !== ALL_LOCATIONS ? current.selected : undefined,
)

const filters = computed(() => filtersFromQuery(route.query))
function update(patch: Partial<AvailabilityFilters>) {
  void router.replace({ query: queryFromFilters({ ...filters.value, ...patch }) })
}

// Строка поиска применяется после паузы в наборе, а не на каждую букву.
const search = ref(filters.value.q)
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, (q) => {
  clearTimeout(timer)
  timer = setTimeout(() => update({ q }), 350)
})
watch(
  () => filters.value.q,
  (q) => {
    if (q !== search.value) search.value = q
  },
)

// Скан ручного сканера сразу ищет товар — бутылку в зале можно не набирать.
useScanner(({ event }) => {
  search.value = event.raw
  update({ q: event.raw })
})

const { data: categories } = useQuery(categoriesQuery)
const { data: locations } = useQuery(locationsQuery)
const { data: kinds } = useQuery(productKindsQuery)
const { data: brands } = useQuery(brandsQuery)
const { data: countries } = useQuery(countriesQuery)

const cities = computed(() =>
  [...new Set((locations.value ?? []).filter((l) => l.isActive).map((l) => l.city))].sort(),
)
const points = computed(() =>
  (locations.value ?? [])
    .filter((l) => l.isActive && (!filters.value.city || l.city === filters.value.city))
    .map((l) => ({ value: l.id, title: `${l.code} · ${l.name}` })),
)
const attrs = computed<CategoryAttribute[]>(() => {
  const all = categories.value
  const id = filters.value.categoryId
  const c = all?.categories.find((x) => x.id === id)
  if (!all || !c) return []
  return filterableAttributes(
    all.attributes,
    [...ancestors(c, all.categories), c].map((x) => x.id),
  )
})
function setAttr(code: string, value: unknown) {
  update({ attrs: { ...filters.value.attrs, [code]: value as string } })
}
function setRange(code: string, bound: 'min' | 'max', raw: string) {
  const prev = filters.value.attrs[code]
  const r = typeof prev === 'object' && prev !== null ? { ...prev } : {}
  const n = Number(raw.replace(',', '.'))
  if (raw === '' || !Number.isFinite(n)) delete r[bound]
  else r[bound] = n
  update({ attrs: { ...filters.value.attrs, [code]: r } })
}
const attrValue = (code: string) => {
  const v = filters.value.attrs[code]
  return typeof v === 'object' ? null : (v ?? null)
}
const attrText = (code: string) => {
  const v = filters.value.attrs[code]
  return typeof v === 'string' ? v : null
}
const rangeValue = (code: string, bound: 'min' | 'max') => {
  const v = filters.value.attrs[code]
  return typeof v === 'object' && v !== null ? (v[bound] ?? '') : ''
}

const sortItems = [
  { value: 'name', title: 'По наименованию' },
  { value: 'price', title: 'Сначала дешевле' },
  { value: 'stock', title: 'Больше в наличии' },
  { value: 'new', title: 'Сначала новые' },
]
const showFilters = ref(false)
const active = computed(() => Object.keys(queryFromFilters(filters.value)).length)

const pageSize = 30
const { data, isPending, error, hasNextPage, fetchNextPage, isFetchingNextPage } = useInfiniteQuery(
  {
    queryKey: computed(() => ['availability', filters.value, here.value ?? null]),
    queryFn: async ({ pageParam }) => {
      const f = filters.value
      const { data, error } = await api.GET('/availability', {
        params: {
          query: {
            q: f.q || undefined,
            categoryId: f.categoryId,
            kindCode: f.kindCode,
            brandId: f.brandId,
            countryCode: f.countryCode,
            volumeMl: f.volumeMl,
            abvFrom: f.abvFrom,
            abvTo: f.abvTo,
            priceFrom: f.priceFrom,
            priceTo: f.priceTo,
            city: f.city,
            locationId: f.locationId,
            locationKind: f.locationKind,
            inStock: f.inStock || undefined,
            attrs: Object.keys(f.attrs).length ? JSON.stringify(f.attrs) : undefined,
            here: here.value,
            sort: f.sort,
            offset: pageParam,
            limit: pageSize,
          },
        },
      })
      if (error) throw new ApiError(error)
      return data
    },
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0)
      return loaded < last.total ? loaded : undefined
    },
  },
)
const items = computed(() => data.value?.pages.flatMap((p) => p.items) ?? [])
const total = computed(() => data.value?.pages[0]?.total ?? 0)

const scoped = computed(
  () => !!(filters.value.city || filters.value.locationId || filters.value.locationKind),
)
const price = (i: AvailabilityItem) =>
  !i.minPrice
    ? 'цена не назначена'
    : i.maxPrice && i.maxPrice !== i.minPrice
      ? `${formatPrice(i.minPrice)} – ${formatPrice(i.maxPrice)}`
      : formatPrice(i.minPrice)
</script>

<template>
  <v-container>
    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <h1 class="text-headline-small flex-grow-1">Наличие по сети</h1>
      <v-select
        :model-value="filters.sort"
        :items="sortItems"
        aria-label="Сортировка"
        density="compact"
        hide-details
        class="pk-sort"
        @update:model-value="(v) => update({ sort: v })"
      />
    </div>

    <div class="d-flex ga-2 mb-3">
      <v-text-field
        v-model="search"
        :prepend-inner-icon="mdiMagnify"
        placeholder="Название, артикул, штрихкод или скан марки"
        density="comfortable"
        hide-details
        clearable
        autofocus
        data-test="availability-search"
      />
      <v-btn
        :prepend-icon="mdiFilterVariant"
        variant="tonal"
        height="48"
        data-test="filters"
        @click="showFilters = !showFilters"
      >
        Фильтры<v-badge
          v-if="active > (filters.q ? 1 : 0)"
          inline
          :content="active - (filters.q ? 1 : 0)"
        />
      </v-btn>
    </div>

    <v-expand-transition>
      <v-card v-show="showFilters" class="mb-4" variant="outlined">
        <v-card-text>
          <div class="pk-filters">
            <v-autocomplete
              :model-value="filters.categoryId"
              :items="categories ? categoryItems(categories.categories) : []"
              label="Категория"
              clearable
              @update:model-value="(v) => update({ categoryId: v ?? undefined, attrs: {} })"
            />
            <v-autocomplete
              :model-value="filters.kindCode"
              :items="(kinds ?? []).map((k) => ({ value: k.code, title: `${k.code} · ${k.name}` }))"
              label="Вид продукции"
              clearable
              @update:model-value="(v) => update({ kindCode: v ?? undefined })"
            />
            <v-autocomplete
              :model-value="filters.brandId"
              :items="(brands ?? []).map((b) => ({ value: b.id, title: b.name }))"
              label="Бренд"
              clearable
              @update:model-value="(v) => update({ brandId: v ?? undefined })"
            />
            <v-autocomplete
              :model-value="filters.countryCode"
              :items="(countries ?? []).map((c) => ({ value: c.code, title: c.name }))"
              label="Страна"
              clearable
              @update:model-value="(v) => update({ countryCode: v ?? undefined })"
            />
            <v-text-field
              :model-value="filters.volumeMl ?? ''"
              label="Объём, мл"
              type="number"
              @change="
                (e: Event) =>
                  update({ volumeMl: Number((e.target as HTMLInputElement).value) || undefined })
              "
            />
            <div class="d-flex ga-2">
              <v-text-field
                :model-value="filters.abvFrom ?? ''"
                label="Крепость от, %"
                type="number"
                @change="
                  (e: Event) =>
                    update({ abvFrom: Number((e.target as HTMLInputElement).value) || undefined })
                "
              />
              <v-text-field
                :model-value="filters.abvTo ?? ''"
                label="до"
                type="number"
                @change="
                  (e: Event) =>
                    update({ abvTo: Number((e.target as HTMLInputElement).value) || undefined })
                "
              />
            </div>
            <div class="d-flex ga-2">
              <v-text-field
                :model-value="filters.priceFrom ?? ''"
                label="Цена от, ₽"
                type="number"
                @change="
                  (e: Event) =>
                    update({ priceFrom: Number((e.target as HTMLInputElement).value) || undefined })
                "
              />
              <v-text-field
                :model-value="filters.priceTo ?? ''"
                label="до"
                type="number"
                @change="
                  (e: Event) =>
                    update({ priceTo: Number((e.target as HTMLInputElement).value) || undefined })
                "
              />
            </div>
            <v-select
              :model-value="filters.city"
              :items="cities"
              label="Город"
              clearable
              @update:model-value="(v) => update({ city: v ?? undefined, locationId: undefined })"
            />
            <v-autocomplete
              :model-value="filters.locationId"
              :items="points"
              label="Точка"
              clearable
              @update:model-value="(v) => update({ locationId: v ?? undefined })"
            />
            <v-select
              :model-value="filters.locationKind"
              :items="[
                { value: 'store', title: 'Магазины' },
                { value: 'warehouse', title: 'Склады' },
              ]"
              label="Тип точки"
              clearable
              @update:model-value="(v) => update({ locationKind: v ?? undefined })"
            />
            <template v-for="a in attrs" :key="a.id">
              <v-select
                v-if="a.type === 'enum'"
                :model-value="attrText(a.code)"
                :items="a.options.filter((o) => !o.archived).map((o) => o.value)"
                :label="a.name"
                clearable
                @update:model-value="(v) => setAttr(a.code, v ?? '')"
              />
              <v-select
                v-else-if="a.type === 'bool'"
                :model-value="attrValue(a.code)"
                :items="[
                  { value: true, title: 'да' },
                  { value: false, title: 'нет' },
                ]"
                :label="a.name"
                clearable
                @update:model-value="(v) => setAttr(a.code, v ?? '')"
              />
              <div v-else-if="a.type === 'number'" class="d-flex ga-2">
                <v-text-field
                  :model-value="rangeValue(a.code, 'min')"
                  :label="`${a.name}${a.unit ? ', ' + a.unit : ''} от`"
                  type="number"
                  @change="
                    (e: Event) => setRange(a.code, 'min', (e.target as HTMLInputElement).value)
                  "
                />
                <v-text-field
                  :model-value="rangeValue(a.code, 'max')"
                  label="до"
                  type="number"
                  @change="
                    (e: Event) => setRange(a.code, 'max', (e.target as HTMLInputElement).value)
                  "
                />
              </div>
              <v-text-field
                v-else
                :model-value="attrText(a.code) ?? ''"
                :label="a.name"
                @change="(e: Event) => setAttr(a.code, (e.target as HTMLInputElement).value)"
              />
            </template>
          </div>
          <div class="d-flex align-center flex-wrap ga-4">
            <v-switch
              :model-value="filters.inStock"
              label="Только в наличии"
              color="primary"
              hide-details
              data-test="in-stock"
              @update:model-value="(v) => update({ inStock: !!v })"
            />
            <v-spacer />
            <v-btn variant="text" @click="router.replace({ query: {} })">Сбросить всё</v-btn>
          </div>
        </v-card-text>
      </v-card>
    </v-expand-transition>

    <v-alert v-if="error" type="error" class="mb-4">{{ error.message }}</v-alert>
    <div class="text-body-medium text-medium-emphasis mb-2">
      Найдено: {{ total }}<template v-if="scoped"> · остаток считается в выбранных точках</template>
    </div>

    <v-card>
      <v-progress-linear v-if="isPending" indeterminate />
      <v-list lines="two" role="group" aria-label="Товары">
        <v-list-item
          v-for="i in items"
          :key="i.productId"
          :to="{ name: 'availability-card', params: { productId: i.productId } }"
          data-test="availability-item"
        >
          <v-list-item-title>{{ i.name }}</v-list-item-title>
          <v-list-item-subtitle>
            {{ i.categoryName }}<template v-if="i.abv"> · {{ formatAbv(i.abv) }}</template> ·
            {{ i.countryName }} · {{ price(i) }}
          </v-list-item-subtitle>
          <template #append>
            <div class="text-end">
              <div v-if="here" class="text-body-large" :class="i.hereQty > 0 ? 'text-success' : ''">
                здесь {{ i.hereQty }}
              </div>
              <div class="text-body-small text-medium-emphasis">
                <template v-if="scoped">в точках {{ i.scopeQty }} · </template>в сети
                {{ i.networkQty }}
              </div>
            </div>
          </template>
        </v-list-item>
        <v-list-item v-if="!isPending && items.length === 0">
          <v-list-item-title class="text-medium-emphasis">Ничего не найдено</v-list-item-title>
        </v-list-item>
      </v-list>
      <v-card-actions v-if="hasNextPage">
        <v-btn block :loading="isFetchingNextPage" @click="fetchNextPage()">Показать ещё</v-btn>
      </v-card-actions>
    </v-card>
  </v-container>
</template>

<style scoped>
.pk-sort {
  max-width: 220px;
}
.pk-filters {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 0 16px;
}
</style>
