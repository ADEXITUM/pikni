<script setup lang="ts">
import { mdiArrowLeft, mdiContentCopy, mdiDeleteOutline, mdiMagnify, mdiPlus } from '@mdi/js'
import { api, ApiError, type AssortmentItem } from '@pikni/api'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'

import AssortmentAddDialog from '@/components/AssortmentAddDialog.vue'
import { variantLabel } from '@/products'
import { locationsQuery } from '@/queries'
import { useMe } from '@/session'

const route = useRoute()
const id = computed(() => String(route.params.id))
const queryClient = useQueryClient()
const { data: me } = useMe()
const canEdit = computed(() => me.value?.permissions.includes('refs.edit') ?? false)

const { data: locations } = useQuery(locationsQuery)
const location = computed(() => locations.value?.find((l) => l.id === id.value))

// В магазине тысячи вариантов, поэтому матрица приходит с сервера страницами, а поиск идёт
// на сервере: через 300 мс после последней буквы или сразу по Enter — им заканчивает код сканер.
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

const { data, isPending, hasNextPage, fetchNextPage, isFetchingNextPage } = useInfiniteQuery({
  queryKey: ['assortment', id, search],
  queryFn: async ({ pageParam }) => {
    const { data, error } = await api.GET('/locations/{id}/assortment', {
      params: { path: { id: id.value }, query: { q: search.value || undefined, after: pageParam } },
    })
    if (error) throw new ApiError(error)
    return data
  },
  initialPageParam: undefined as string | undefined,
  getNextPageParam: (page) => page.nextAfter,
})
const items = computed(() => data.value?.pages.flatMap((p) => p.items) ?? [])
const total = computed(() => data.value?.pages[0]?.total ?? 0)
const refresh = () => queryClient.invalidateQueries({ queryKey: ['assortment', id.value] })

const path = (a: AssortmentItem) => ({ id: id.value, variantId: a.variantId })

// Минимальный остаток меняется прямо в таблице: новое значение уходит, когда поле теряет фокус.
const { mutate: setQty, error: setError } = useMutation({
  mutationFn: async ({ item, qty }: { item: AssortmentItem; qty: number }) => {
    const { error } = await api.PUT('/locations/{id}/assortment/{variantId}', {
      params: { path: path(item) },
      body: { minQty: qty },
    })
    if (error) throw new ApiError(error)
  },
  onSettled: refresh,
})
function onQty(item: AssortmentItem, value: string) {
  const qty = Number(value)
  if (Number.isInteger(qty) && qty >= 1 && qty <= 9999 && qty !== item.minQty) setQty({ item, qty })
}

const { mutate: remove, error: removeError } = useMutation({
  mutationFn: async (item: AssortmentItem) => {
    const { error } = await api.DELETE('/locations/{id}/assortment/{variantId}', {
      params: { path: path(item) },
    })
    if (error) throw new ApiError(error)
  },
  onSettled: refresh,
})

const addOpen = ref(false)
const copyOpen = ref(false)
const copyFrom = ref<string | null>(null)
const copied = ref<number>()
const sourceItems = computed(() =>
  (locations.value ?? [])
    .filter((l) => l.id !== id.value)
    .map((l) => ({ value: l.id, title: `${l.code} · ${l.name}` })),
)
const { mutate: copy, isPending: copying } = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/locations/{id}/assortment/copy', {
      params: { path: { id: id.value } },
      body: { fromLocationId: copyFrom.value! },
    })
    if (error) throw new ApiError(error)
    return data.added
  },
  onSuccess: (added) => {
    copied.value = added
    copyOpen.value = false
    refresh()
  },
})

const failure = computed(() => setError.value ?? removeError.value)
</script>

<template>
  <v-container class="pk-assortment">
    <v-btn
      :prepend-icon="mdiArrowLeft"
      :to="{ name: 'location', params: { id } }"
      variant="text"
      class="mb-2 ml-n3"
    >
      {{ location ? `${location.code} · ${location.name}` : 'К точке' }}
    </v-btn>
    <h1 class="text-headline-small mb-1">Ассортиментная матрица</h1>
    <p class="text-body-medium text-medium-emphasis mb-4">
      Что точка должна держать в наличии и минимальный остаток: ниже него товар попадёт в отчёт
      «Товары к пополнению».
    </p>

    <div class="d-flex flex-wrap align-center ga-2 mb-3">
      <v-text-field
        v-model="filter"
        :prepend-inner-icon="mdiMagnify"
        placeholder="Название, артикул или штрихкод"
        density="compact"
        hide-details
        clearable
        class="pk-filter"
        data-test="assortment-search"
        @keydown.enter="applySearch"
      />
      <span class="text-body-medium text-medium-emphasis" data-test="assortment-total">
        {{ search ? 'Найдено' : 'Вариантов' }}: {{ total }}
      </span>
      <v-spacer />
      <template v-if="canEdit">
        <v-btn
          variant="tonal"
          :prepend-icon="mdiContentCopy"
          data-test="assortment-copy"
          @click="copyOpen = true"
        >
          Из другой точки
        </v-btn>
        <v-btn
          variant="tonal"
          :prepend-icon="mdiPlus"
          data-test="assortment-add"
          @click="addOpen = true"
        >
          Вариант
        </v-btn>
      </template>
    </div>

    <v-alert v-if="failure" type="error" class="mb-3">{{ failure.message }}</v-alert>
    <v-alert
      v-if="copied !== undefined"
      type="success"
      variant="tonal"
      closable
      class="mb-3"
      @click:close="copied = undefined"
    >
      {{ copied ? `Добавлено вариантов: ${copied}` : 'Все варианты этой точки уже были в матрице' }}
    </v-alert>

    <v-card>
      <v-progress-linear v-if="isPending" indeterminate />
      <p v-else-if="!items.length" class="text-body-medium text-medium-emphasis pa-4">
        {{
          search
            ? 'Ничего не найдено.'
            : 'Матрица пуста: точка ничего не обязана держать в наличии.'
        }}
      </p>
      <v-table v-else density="compact">
        <thead>
          <tr>
            <th class="d-none d-sm-table-cell">Артикул</th>
            <th>Позиция</th>
            <th>Вариант</th>
            <th class="text-end">Мин. остаток</th>
            <th v-if="canEdit"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="a in items"
            :key="a.variantId"
            :class="{ 'text-disabled': !a.isActive }"
            :data-test="`assortment-${a.variantId}`"
          >
            <td class="text-no-wrap d-none d-sm-table-cell">{{ a.article }}</td>
            <td>
              <router-link :to="{ name: 'product', params: { id: a.productId } }">
                {{ a.name }}
              </router-link>
              <v-chip v-if="!a.isActive" size="x-small" class="ml-1">в архиве</v-chip>
              <div class="d-sm-none text-body-medium text-medium-emphasis">{{ a.article }}</div>
            </td>
            <td class="text-no-wrap">{{ variantLabel(a) }}</td>
            <td class="text-end">
              <v-text-field
                v-if="canEdit"
                :model-value="String(a.minQty)"
                inputmode="numeric"
                density="compact"
                variant="plain"
                hide-details
                class="pk-qty"
                data-test="assortment-min"
                @change="onQty(a, ($event.target as HTMLInputElement).value)"
              />
              <span v-else>{{ a.minQty }}</span>
            </td>
            <td v-if="canEdit" class="text-end">
              <v-btn
                :icon="mdiDeleteOutline"
                variant="text"
                size="small"
                title="Убрать из матрицы"
                data-test="assortment-remove"
                @click="remove(a)"
              />
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <div v-if="hasNextPage" class="text-center mt-4">
      <v-btn variant="tonal" :loading="isFetchingNextPage" @click="fetchNextPage()">
        Показать ещё
      </v-btn>
    </div>

    <AssortmentAddDialog v-model="addOpen" :location-id="id" @saved="refresh" />

    <v-dialog v-model="copyOpen" max-width="480">
      <v-card title="Скопировать матрицу из другой точки">
        <v-card-text>
          <v-select
            v-model="copyFrom"
            :items="sourceItems"
            label="Точка"
            hint="Добавятся только варианты, которых здесь ещё нет"
            persistent-hint
            data-test="assortment-copy-from"
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="copyOpen = false">Отмена</v-btn>
          <v-btn
            color="primary"
            :disabled="!copyFrom"
            :loading="copying"
            data-test="assortment-copy-run"
            @click="copy()"
          >
            Скопировать
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>

<style scoped>
.pk-assortment {
  max-width: 1100px;
}

.pk-filter {
  max-width: 340px;
  min-width: 220px;
}

.pk-qty {
  max-width: 80px;
  margin-left: auto;
}

.pk-qty :deep(input) {
  text-align: right;
  padding-top: 0;
  padding-bottom: 0;
  min-height: 0;
}
</style>
