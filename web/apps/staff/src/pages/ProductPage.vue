<script setup lang="ts">
import { mdiArrowLeft } from '@mdi/js'
import { api, ApiError, type CategoryAttribute, type Product } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { SubmitEventPromise } from 'vuetify'

import { categoryItems, schemaOf } from '@/categories'
import VariantsCard from '@/components/VariantsCard.vue'
import {
  accountingLabels,
  attributeValues,
  fitAttributes,
  isEmpty,
  type AttributeValues,
} from '@/products'
import {
  brandsQuery,
  categoriesQuery,
  countriesQuery,
  originRegionsQuery,
  producersQuery,
  productKindsQuery,
  productLinesQuery,
} from '@/queries'
import { useMe } from '@/session'

const route = useRoute()
const router = useRouter()
const queryClient = useQueryClient()
const { data: me } = useMe()
const canEdit = computed(() => me.value?.permissions.includes('refs.edit') ?? false)
const showCost = computed(() => me.value?.permissions.includes('cost.view') ?? false)

const isNew = computed(() => route.name === 'product-new')
const id = computed(() => String(route.params.id ?? ''))

const {
  data: product,
  isError,
  error: loadError,
  refetch,
} = useQuery({
  queryKey: ['product', id],
  queryFn: async () => {
    const { data, error } = await api.GET('/products/{id}', { params: { path: { id: id.value } } })
    if (error) throw new ApiError(error)
    return data
  },
  enabled: computed(() => !isNew.value),
})

const { data: tree } = useQuery(categoriesQuery)
const { data: kinds } = useQuery(productKindsQuery)
const { data: producers } = useQuery(producersQuery)
const { data: brands } = useQuery(brandsQuery)
const { data: lines } = useQuery(productLinesQuery)
const { data: countries } = useQuery(countriesQuery)
const { data: regions } = useQuery(originRegionsQuery)

const form = reactive({
  name: '',
  categoryId: null as string | null,
  kindCode: null as string | null,
  abv: '' as string | number,
  crptMarked: false,
  producerId: null as string | null,
  brandId: null as string | null,
  productLineId: null as string | null,
  variety: '',
  countryCode: '643',
  originRegionId: null as string | null,
  attributes: {} as AttributeValues,
  isActive: true,
})
const dropped = ref<string[]>([])

watch(
  product,
  (p) => {
    if (!p) return
    Object.assign(form, {
      name: p.name,
      categoryId: p.categoryId,
      kindCode: p.kindCode ?? null,
      abv: p.abv ?? '',
      crptMarked: p.crptMarked,
      producerId: p.producerId,
      brandId: p.brandId ?? null,
      productLineId: p.productLineId ?? null,
      variety: p.variety ?? '',
      countryCode: p.countryCode,
      originRegionId: p.originRegionId ?? null,
      attributes: { ...p.attributes },
      isActive: p.isActive,
    })
    dropped.value = []
  },
  { immediate: true },
)

// В списках выбора — действующие записи и та, что уже стоит в карточке.
function usable<T extends { id: string; isActive: boolean }>(
  list: T[] | undefined,
  current: string | null,
) {
  return (list ?? []).filter((x) => x.isActive || x.id === current)
}

const categories = computed(() => tree.value?.categories ?? [])
const categoryOptions = computed(() => categoryItems(categories.value, form.categoryId))
const kindOptions = computed(() =>
  (kinds.value ?? []).map((k) => ({ value: k.code, title: `${k.code} — ${k.name}` })),
)
const producerOptions = computed(() =>
  usable(producers.value, form.producerId).map((p) => ({ value: p.id, title: p.name })),
)
const brandOptions = computed(() =>
  usable(brands.value, form.brandId)
    .filter((b) => b.producerId === form.producerId)
    .map((b) => ({ value: b.id, title: b.name })),
)
const lineOptions = computed(() =>
  usable(lines.value, form.productLineId)
    .filter((l) => l.brandId === form.brandId)
    .map((l) => ({ value: l.id, title: l.name })),
)
const countryOptions = computed(() =>
  (countries.value ?? []).map((c) => ({ value: c.code, title: c.name })),
)
const regionOptions = computed(() =>
  usable(regions.value, form.originRegionId)
    .filter((r) => r.countryCode === form.countryCode)
    .map((r) => ({ value: r.id, title: r.name })),
)

// Бренд — только этого производителя, линейка — этого бренда, регион — этой страны.
function onProducer() {
  if (brands.value?.find((b) => b.id === form.brandId)?.producerId !== form.producerId) {
    form.brandId = null
    form.productLineId = null
  }
}
function onBrand() {
  if (lines.value?.find((l) => l.id === form.productLineId)?.brandId !== form.brandId) {
    form.productLineId = null
  }
}
function onCountry() {
  if (regions.value?.find((r) => r.id === form.originRegionId)?.countryCode !== form.countryCode) {
    form.originRegionId = null
  }
}

const schemaFor = (categoryId: string | null) => {
  const category = categories.value.find((c) => c.id === categoryId)
  if (!category) return []
  return schemaOf(category, categories.value, tree.value?.attributes ?? []).map((r) => r.attribute)
}
const schema = computed(() => schemaFor(form.categoryId))
// Архивную характеристику показываем, только если у позиции уже есть её значение.
const visibleSchema = computed(() =>
  schema.value.filter((a) => a.isActive || !isEmpty(form.attributes[a.code])),
)

function changeCategory(next: string | null) {
  const fitted = fitAttributes(form.attributes, schema.value, schemaFor(next))
  form.categoryId = next
  form.attributes = fitted.values
  dropped.value = fitted.dropped
}

const optionItems = (a: CategoryAttribute) =>
  a.options
    .filter((o) => !o.archived || o.value === form.attributes[a.code])
    .map((o) => ({ value: o.value, title: o.archived ? `${o.value} (в архиве)` : o.value }))
const yesNo = [
  { value: true, title: 'да' },
  { value: false, title: 'нет' },
]

const accounting = computed(() => {
  const kind = kinds.value?.find((k) => k.code === form.kindCode)
  return accountingLabels[kind?.egaisAccounting ?? 'none']
})

const required = (v: unknown) => (v !== null && String(v).trim() !== '') || 'Обязательное поле'
const isNumber = (v: unknown) =>
  v === null ||
  v === undefined ||
  String(v).trim() === '' ||
  !Number.isNaN(Number(String(v).replace(',', '.'))) ||
  'Нужно число'
const abvRule = (v: unknown) =>
  !form.kindCode || String(v ?? '').trim() !== '' || 'Для алкогольной продукции укажите крепость'

const {
  mutate: save,
  isPending: saving,
  error: saveError,
  reset: resetSave,
} = useMutation({
  mutationFn: async (): Promise<Product> => {
    const abv = String(form.abv ?? '').trim()
    const fields = {
      name: form.name,
      categoryId: form.categoryId!,
      kindCode: form.kindCode ?? undefined,
      abv: abv === '' ? undefined : Number(abv.replace(',', '.')),
      crptMarked: form.crptMarked,
      producerId: form.producerId!,
      brandId: form.brandId ?? undefined,
      productLineId: form.productLineId ?? undefined,
      variety: form.variety.trim() || undefined,
      countryCode: form.countryCode,
      originRegionId: form.originRegionId ?? undefined,
      attributes: attributeValues(form.attributes, schema.value),
    }
    const { data, error } = isNew.value
      ? await api.POST('/products', { body: fields })
      : await api.PUT('/products/{id}', {
          params: { path: { id: id.value } },
          body: { ...fields, isActive: form.isActive, version: product.value!.version },
        })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: async (saved) => {
    queryClient.setQueryData(['product', saved.id], saved)
    dropped.value = []
    await queryClient.invalidateQueries({ queryKey: ['products'] })
    if (isNew.value) await router.replace({ name: 'product', params: { id: saved.id } })
  },
})

const conflict = computed(
  () => saveError.value instanceof ApiError && saveError.value.problem.status === 409,
)
async function reload() {
  resetSave()
  await refetch()
}

const title = computed(() =>
  isNew.value ? 'Новая позиция' : product.value ? product.value.name : '',
)

async function submit(event: SubmitEventPromise) {
  if ((await event).valid) save()
}
</script>

<template>
  <v-container class="pk-product">
    <div class="d-flex align-center ga-2 mb-2">
      <v-btn
        :icon="mdiArrowLeft"
        variant="text"
        :to="{ name: 'products' }"
        title="К списку товаров"
      />
      <h1 class="text-h5">{{ title }}</h1>
    </div>
    <div class="d-flex flex-wrap ga-2 mb-4">
      <v-chip size="small" data-test="accounting">{{ accounting }}</v-chip>
      <v-chip v-if="product && !product.isActive" color="error" size="small">в архиве</v-chip>
    </div>

    <v-alert v-if="isError" type="error" title="Не удалось загрузить позицию" class="mb-4">
      {{ loadError?.message }}
    </v-alert>

    <v-form v-if="isNew || product" :disabled="!canEdit" @submit.prevent="submit">
      <v-card title="Основное" class="mb-4">
        <v-card-text>
          <v-row density="compact">
            <v-col cols="12">
              <v-text-field
                v-model="form.name"
                label="Наименование"
                :rules="[required]"
                data-test="name"
              />
            </v-col>
            <v-col cols="12" md="6">
              <v-autocomplete
                :model-value="form.categoryId"
                :items="categoryOptions"
                label="Категория"
                :rules="[required]"
                data-test="category"
                @update:model-value="changeCategory"
              />
            </v-col>
            <v-col cols="12" md="6">
              <v-autocomplete
                v-model="form.kindCode"
                :items="kindOptions"
                label="Вид продукции ЕГАИС"
                hint="Пусто — товар не учитывается в ЕГАИС: снеки, безалкогольное"
                persistent-hint
                clearable
                data-test="kind"
              />
            </v-col>
            <v-col cols="12" sm="4" md="3">
              <v-text-field
                v-model="form.abv"
                label="Крепость"
                suffix="%"
                inputmode="decimal"
                :rules="[isNumber, abvRule]"
                data-test="abv"
              />
            </v-col>
            <v-col cols="12" sm="8" md="5">
              <v-switch v-model="form.crptMarked" label="Маркировка «Честным знаком»" />
            </v-col>
            <v-col v-if="!isNew" cols="12" md="4">
              <v-switch v-model="form.isActive" label="Используется" />
            </v-col>
          </v-row>
        </v-card-text>
      </v-card>

      <v-card title="Происхождение" class="mb-4">
        <v-card-text>
          <v-row density="compact">
            <v-col cols="12" md="6">
              <v-autocomplete
                v-model="form.producerId"
                :items="producerOptions"
                label="Производитель"
                :rules="[required]"
                data-test="producer"
                @update:model-value="onProducer"
              />
            </v-col>
            <v-col cols="12" md="6">
              <v-autocomplete
                v-model="form.brandId"
                :items="brandOptions"
                label="Бренд"
                :no-data-text="
                  form.producerId ? 'У производителя нет брендов' : 'Сначала выберите производителя'
                "
                clearable
                data-test="brand"
                @update:model-value="onBrand"
              />
            </v-col>
            <v-col cols="12" md="6">
              <v-select
                v-model="form.productLineId"
                :items="lineOptions"
                label="Линейка"
                :disabled="!form.brandId || !canEdit"
                hint="Позиции бренда, которые различаются разновидностью"
                persistent-hint
                clearable
              />
            </v-col>
            <v-col cols="12" md="6">
              <v-text-field
                v-model="form.variety"
                label="Разновидность"
                placeholder="на пшенице, 5 лет, нефильтрованное"
              />
            </v-col>
            <v-col cols="12" md="6">
              <v-autocomplete
                v-model="form.countryCode"
                :items="countryOptions"
                label="Страна"
                :rules="[required]"
                @update:model-value="onCountry"
              />
            </v-col>
            <v-col cols="12" md="6">
              <v-select
                v-model="form.originRegionId"
                :items="regionOptions"
                label="Регион происхождения"
                no-data-text="Для этой страны регионов нет"
                clearable
              />
            </v-col>
          </v-row>
        </v-card-text>
      </v-card>

      <v-card title="Характеристики" class="mb-4" data-test="attributes">
        <v-card-text>
          <v-alert
            v-if="dropped.length"
            type="warning"
            variant="tonal"
            class="mb-4"
            data-test="dropped"
          >
            У новой категории нет характеристик: {{ dropped.join(', ') }}. Их значения будут убраны
            при сохранении.
          </v-alert>
          <p v-if="!form.categoryId" class="text-body-2 text-medium-emphasis">
            Набор характеристик зависит от категории
          </p>
          <p v-else-if="!visibleSchema.length" class="text-body-2 text-medium-emphasis">
            У категории нет характеристик
          </p>
          <v-row v-else density="compact">
            <v-col v-for="a in visibleSchema" :key="a.code" cols="12" md="6">
              <v-select
                v-if="a.type === 'enum'"
                v-model="form.attributes[a.code]"
                :label="a.name"
                :items="optionItems(a)"
                :hint="a.isActive ? undefined : 'Характеристика в архиве'"
                persistent-hint
                clearable
                :data-test="`attr-${a.code}`"
              />
              <v-select
                v-else-if="a.type === 'bool'"
                v-model="form.attributes[a.code]"
                :label="a.name"
                :items="yesNo"
                clearable
                :data-test="`attr-${a.code}`"
              />
              <v-text-field
                v-else
                v-model="form.attributes[a.code]"
                :label="a.name"
                :suffix="a.unit"
                :inputmode="a.type === 'number' ? 'decimal' : undefined"
                :rules="a.type === 'number' ? [isNumber] : []"
                :data-test="`attr-${a.code}`"
              />
            </v-col>
          </v-row>
        </v-card-text>
      </v-card>

      <v-alert v-if="saveError" type="error" class="mb-4" data-test="save-error">
        {{ saveError instanceof ApiError ? saveError.message : 'Нет связи с сервером' }}
        <template v-if="conflict" #append>
          <v-btn variant="text" @click="reload()">Загрузить свежую версию</v-btn>
        </template>
      </v-alert>

      <div v-if="canEdit" class="d-flex justify-end">
        <v-btn type="submit" color="primary" size="large" :loading="saving" data-test="save">
          {{ isNew ? 'Добавить позицию' : 'Сохранить' }}
        </v-btn>
      </div>
    </v-form>

    <VariantsCard
      v-if="product"
      :product-id="product.id"
      :is-egais="Boolean(product.kindCode)"
      :can-edit="canEdit"
      :show-cost="showCost"
      class="mt-4"
    />
    <p v-else-if="isNew" class="text-body-2 text-medium-emphasis mt-4">
      Варианты — объёмы и тару — можно добавить после сохранения позиции.
    </p>
  </v-container>
</template>

<style scoped>
.pk-product {
  max-width: 960px;
}
</style>
