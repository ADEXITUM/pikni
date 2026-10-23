<script setup lang="ts">
import { mdiContentCopy, mdiDeleteOutline, mdiMagnify, mdiPlus } from '@mdi/js'
import { api, ApiError, type AssortmentItem } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import AssortmentAddDialog from '@/components/AssortmentAddDialog.vue'
import { variantLabel } from '@/products'
import { locationsQuery } from '@/queries'

const props = defineProps<{ locationId: string; canEdit: boolean }>()

const queryClient = useQueryClient()
const key = computed(() => ['assortment', props.locationId])
const { data: items, isPending } = useQuery({
  queryKey: key,
  queryFn: async () => {
    const { data, error } = await api.GET('/locations/{id}/assortment', {
      params: { path: { id: props.locationId } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
})
const refresh = () => queryClient.invalidateQueries({ queryKey: key.value })

const filter = ref('')
const shown = computed(() => {
  const f = filter.value.trim().toLowerCase()
  return (items.value ?? []).filter(
    (a) => !f || a.name.toLowerCase().includes(f) || a.article.toLowerCase().includes(f),
  )
})

const path = (a: AssortmentItem) => ({ id: props.locationId, variantId: a.variantId })

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
const { data: locations } = useQuery(locationsQuery)
const sourceItems = computed(() =>
  (locations.value ?? [])
    .filter((l) => l.id !== props.locationId)
    .map((l) => ({ value: l.id, title: `${l.code} · ${l.name}` })),
)
const { mutate: copy, isPending: copying } = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/locations/{id}/assortment/copy', {
      params: { path: { id: props.locationId } },
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
  <v-card class="mb-4" data-test="assortment">
    <v-card-item
      title="Ассортиментная матрица"
      subtitle="Что должно быть в наличии и минимальный остаток"
    />
    <v-card-text>
      <div class="d-flex flex-wrap align-center ga-2 mb-3">
        <v-text-field
          v-if="(items?.length ?? 0) > 8"
          v-model="filter"
          :prepend-inner-icon="mdiMagnify"
          placeholder="Название или артикул"
          density="compact"
          hide-details
          class="pk-filter"
        />
        <v-spacer />
        <template v-if="canEdit">
          <v-btn
            size="small"
            variant="tonal"
            :prepend-icon="mdiContentCopy"
            data-test="assortment-copy"
            @click="copyOpen = true"
          >
            Из другой точки
          </v-btn>
          <v-btn
            size="small"
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
        {{
          copied ? `Добавлено вариантов: ${copied}` : 'Все варианты этой точки уже были в матрице'
        }}
      </v-alert>

      <v-progress-linear v-if="isPending" indeterminate />
      <p v-else-if="!items?.length" class="text-body-2 text-medium-emphasis">
        Матрица пуста: точка ничего не обязана держать в наличии.
      </p>
      <v-table v-else density="compact">
        <thead>
          <tr>
            <th>Артикул</th>
            <th>Позиция</th>
            <th>Вариант</th>
            <th class="text-end">Мин. остаток</th>
            <th v-if="canEdit"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="a in shown"
            :key="a.variantId"
            :class="{ 'text-disabled': !a.isActive }"
            :data-test="`assortment-${a.variantId}`"
          >
            <td class="text-no-wrap">{{ a.article }}</td>
            <td>
              <router-link :to="{ name: 'product', params: { id: a.productId } }">
                {{ a.name }}
              </router-link>
              <v-chip v-if="!a.isActive" size="x-small" class="ml-1">в архиве</v-chip>
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
    </v-card-text>

    <AssortmentAddDialog v-model="addOpen" :location-id="locationId" @saved="refresh" />

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
  </v-card>
</template>

<style scoped>
.pk-filter {
  max-width: 280px;
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
