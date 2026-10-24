<script setup lang="ts">
import { api, ApiError } from '@pikni/api'
import { useMutation, useQuery } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'
import type { SubmitEventPromise } from 'vuetify'

import { variantLabel } from '@/products'

const props = defineProps<{ locationId: string }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

// Позиция ищется на сервере тем же поиском, что и в списке товаров; запрос — после паузы.
const search = ref('')
const query = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
// После выбора Vuetify подставляет в строку поиска название пункта — по нему искать не нужно,
// иначе выбранная позиция пропадёт из списка.
watch(search, (value) => {
  clearTimeout(timer)
  if (productItems.value.some((i) => i.title === value)) return
  timer = setTimeout(() => (query.value = (value ?? '').trim()), 300)
})

const { data: found, isFetching: searching } = useQuery({
  queryKey: ['assortment-search', query],
  queryFn: async () => {
    const { data, error } = await api.GET('/products', {
      params: { query: { q: query.value, limit: 20 } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
  enabled: computed(() => open.value && query.value.length >= 2),
})
const productItems = computed(() =>
  (found.value ?? []).map((p) => ({ value: p.id, title: p.name })),
)

const productId = ref<string | null>(null)
const { data: variants } = useQuery({
  queryKey: ['variants', productId],
  queryFn: async () => {
    const { data, error } = await api.GET('/products/{id}/variants', {
      params: { path: { id: productId.value! } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
  enabled: computed(() => Boolean(productId.value)),
})
const variantItems = computed(() =>
  (variants.value ?? [])
    .filter((v) => v.isActive)
    .map((v) => ({ value: v.id, title: `${v.article} · ${variantLabel(v)}` })),
)

const variantId = ref<string | null>(null)
const minQty = ref('1')
watch(variantItems, (items) => {
  if (items.length === 1) variantId.value = items[0]!.value
})

const qtyRule = (v: string) =>
  (/^\d+$/.test(v.trim()) && Number(v) >= 1 && Number(v) <= 9999) || 'Целое число от 1 до 9999'

const { mutate, isPending, error, reset } = useMutation({
  mutationFn: async () => {
    const { error } = await api.PUT('/locations/{id}/assortment/{variantId}', {
      params: { path: { id: props.locationId, variantId: variantId.value! } },
      body: { minQty: Number(minQty.value) },
    })
    if (error) throw new ApiError(error)
  },
  onSuccess: () => {
    open.value = false
    emit('saved')
  },
})

watch(open, (isOpen) => {
  if (!isOpen) return
  reset()
  search.value = ''
  productId.value = null
  variantId.value = null
  minQty.value = '1'
})

async function submit(event: SubmitEventPromise) {
  if ((await event).valid && variantId.value) mutate()
}
</script>

<template>
  <v-dialog v-model="open" max-width="560">
    <v-card title="Вариант в матрицу">
      <v-form @submit.prevent="submit">
        <v-card-text>
          <v-autocomplete
            v-model="productId"
            v-model:search="search"
            :items="productItems"
            :loading="searching"
            label="Позиция"
            placeholder="Название, артикул или штрихкод"
            no-filter
            :no-data-text="query.length < 2 ? 'Начните вводить название' : 'Ничего не найдено'"
            class="mb-3"
            data-test="assortment-product"
          />
          <v-select
            v-model="variantId"
            :items="variantItems"
            label="Вариант"
            :disabled="!productId"
            no-data-text="У позиции нет действующих вариантов"
            class="mb-3"
            data-test="assortment-variant"
          />
          <v-text-field
            v-model="minQty"
            label="Минимальный остаток, шт."
            inputmode="numeric"
            :rules="[qtyRule]"
            hint="Меньше — товар попадёт в «Товары к пополнению»"
            persistent-hint
            data-test="assortment-qty"
          />
          <v-alert v-if="error" type="error" class="mt-3">
            {{ error instanceof ApiError ? error.message : 'Нет связи с сервером' }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="open = false">Отмена</v-btn>
          <v-btn
            type="submit"
            color="primary"
            :disabled="!variantId"
            :loading="isPending"
            data-test="assortment-save"
          >
            Добавить
          </v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>
