<script setup lang="ts">
import { mdiFilterRemoveOutline, mdiMagnify, mdiPlus } from '@mdi/js'
import { api, ApiError, type Product } from '@pikni/api'
import { useInfiniteQuery, useQuery } from '@tanstack/vue-query'
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import { categoryItems } from '@/categories'
import { accountingLabels, formatAbv, scanLabels } from '@/products'
import { brandsQuery, categoriesQuery } from '@/queries'
import { useMe } from '@/session'

const router = useRouter()
const { data: me } = useMe()
const canEdit = computed(() => me.value?.permissions.includes('refs.edit') ?? false)

const filters = reactive({
  search: '',
  categoryId: null as string | null,
  brandId: null as string | null,
})

// Поиск уходит на сервер через 300 мс после последней буквы, а не на каждое нажатие.
// Сканер в режиме клавиатуры заканчивает код клавишей Enter — тогда ищем сразу.
const search = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
function applySearch() {
  clearTimeout(timer)
  search.value = (filters.search ?? '').trim()
}
watch(
  () => filters.search,
  () => {
    clearTimeout(timer)
    timer = setTimeout(applySearch, 300)
  },
)

const query = computed(() => ({
  q: search.value || undefined,
  categoryId: filters.categoryId ?? undefined,
  brandId: filters.brandId ?? undefined,
}))

const { data, isPending, isError, error, hasNextPage, fetchNextPage, isFetchingNextPage } =
  useInfiniteQuery({
    queryKey: ['products', query],
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/products', {
        params: { query: { ...query.value, after: pageParam } },
      })
      if (error) throw new ApiError(error)
      return data
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.nextAfter,
  })
const products = computed(() => data.value?.pages.flatMap((p) => p.items) ?? [])
const scan = computed(() => data.value?.pages[0]?.scan)

const { data: tree } = useQuery(categoriesQuery)
const { data: brands } = useQuery(brandsQuery)
const categoryOptions = computed(() => categoryItems(tree.value?.categories ?? []))
const brandOptions = computed(() =>
  (brands.value ?? []).map((b) => ({ value: b.id, title: b.name })),
)

const hasFilters = computed(() => Boolean(filters.search || filters.categoryId || filters.brandId))
function clearFilters() {
  Object.assign(filters, { search: '', categoryId: null, brandId: null })
}

// Порядок задаёт сервер (по названию) и подгрузка страниц, поэтому без сортировки.
const headers = [
  { title: 'Артикул', key: 'article', sortable: false },
  { title: 'Наименование', key: 'name', sortable: false },
  { title: 'Категория', key: 'categoryName', sortable: false },
  { title: 'Бренд', key: 'brandName', sortable: false },
  { title: 'Крепость', key: 'abv', sortable: false },
  { title: 'ЕГАИС', key: 'egaisAccounting', sortable: false },
]

function open(_: Event, { item }: { item: Product }) {
  void router.push({ name: 'product', params: { id: item.id } })
}
</script>

<template>
  <v-container>
    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <h1 class="text-h5 flex-grow-1">Товары</h1>
      <v-btn
        v-if="canEdit"
        color="primary"
        :prepend-icon="mdiPlus"
        :to="{ name: 'product-new' }"
        data-test="product-add"
      >
        Позиция
      </v-btn>
    </div>

    <v-row density="compact" class="mb-2">
      <v-col cols="12" md="4">
        <v-text-field
          v-model="filters.search"
          :prepend-inner-icon="mdiMagnify"
          label="Название, артикул, штрихкод или скан марки"
          clearable
          data-test="search"
          @keydown.enter="applySearch()"
        />
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <v-autocomplete
          v-model="filters.categoryId"
          :items="categoryOptions"
          label="Категория"
          clearable
        />
      </v-col>
      <v-col cols="10" sm="5" md="3">
        <v-autocomplete v-model="filters.brandId" :items="brandOptions" label="Бренд" clearable />
      </v-col>
      <v-col cols="2" sm="1" class="d-flex align-center">
        <v-btn
          v-if="hasFilters"
          :icon="mdiFilterRemoveOutline"
          variant="text"
          title="Сбросить фильтры"
          @click="clearFilters()"
        />
      </v-col>
    </v-row>

    <v-alert
      v-if="scan?.kind === 'egais_mark_new'"
      type="info"
      variant="tonal"
      class="mb-4"
      data-test="scan"
    >
      Марка нового образца {{ scan.code }} не содержит алкокода. Товар по ней найдётся после
      приёмки, когда марка попадёт в систему из документов ЕГАИС.
    </v-alert>
    <p v-else-if="scan" class="text-body-2 text-medium-emphasis mb-3" data-test="scan">
      Поиск по коду: {{ scanLabels[scan.kind] }} {{ scan.code }}
    </p>

    <v-alert v-if="isError" type="error" title="Не удалось загрузить товары" class="mb-4">
      {{ error?.message }}
    </v-alert>

    <v-card>
      <v-data-table
        :headers="headers"
        :items="products"
        :loading="isPending"
        :items-per-page="-1"
        item-value="id"
        hover
        mobile-breakpoint="sm"
        hide-default-footer
        no-data-text="Позиций нет"
        @click:row="open"
      >
        <template #[`item.article`]="{ value }">
          <span class="text-no-wrap">{{ value }}</span>
        </template>
        <template #[`item.name`]="{ item }">
          {{ item.name }}
          <v-chip v-if="!item.isActive" size="x-small" class="ml-1">в архиве</v-chip>
        </template>
        <template #[`item.abv`]="{ value }">
          <span class="text-no-wrap">{{ value === undefined ? '' : formatAbv(value) }}</span>
        </template>
        <template #[`item.egaisAccounting`]="{ item }">
          <span class="text-no-wrap text-body-2">
            {{ item.egaisAccounting ? accountingLabels[item.egaisAccounting] : '—' }}
          </span>
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
