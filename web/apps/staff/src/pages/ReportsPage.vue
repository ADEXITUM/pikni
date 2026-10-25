<script setup lang="ts">
import { mdiFileExcelOutline, mdiFilePdfBox, mdiFolderClockOutline, mdiPlay } from '@mdi/js'
import {
  api,
  ApiError,
  type ReportColumn,
  type ReportDef,
  type ReportParams,
  type ReportTable,
} from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { categoryItems } from '@/categories'
import { formatPrice } from '@/products'
import { brandsQuery, categoriesQuery } from '@/queries'
import { useLocationChoice } from '@/stock'
import LocationSelect from '@/components/LocationSelect.vue'

// Каталог отчётов (ОТ-1, ОТ-2): выбрать отчёт, задать параметры, посмотреть на экране или
// получить файл. Тяжёлый отчёт за длинный период формируется в фоне (ОТ-3).
const route = useRoute()
const router = useRouter()
const places = useLocationChoice('reports.view')

const { data: catalog } = useQuery({
  queryKey: ['reports'],
  queryFn: async () => {
    const { data, error } = await api.GET('/reports')
    if (error) throw new ApiError(error)
    return data.items
  },
})
const code = computed(() =>
  typeof route.query.report === 'string' ? route.query.report : undefined,
)
const def = computed<ReportDef | undefined>(() => catalog.value?.find((d) => d.code === code.value))
const has = (p: ReportDef['params'][number]) => def.value?.params.includes(p) ?? false

const today = new Date().toLocaleDateString('sv-SE')
const monthStart = today.slice(0, 8) + '01'
const form = reactive({
  from: monthStart,
  to: today,
  date: today,
  locationIds: [] as string[],
  categoryIds: [] as string[],
  brandIds: [] as string[],
  group: 'location' as NonNullable<ReportParams['group']>,
  days: 90,
})
const { data: categories } = useQuery({
  ...categoriesQuery,
  enabled: computed(() => has('categories')),
})
const { data: brands } = useQuery({ ...brandsQuery, enabled: computed(() => has('brands')) })
const groupItems = [
  { value: 'location', title: 'по точкам' },
  { value: 'category', title: 'по категориям' },
  { value: 'brand', title: 'по брендам' },
  { value: 'cashier', title: 'по кассирам' },
  { value: 'day', title: 'по дням' },
]

function params(): ReportParams {
  const d = def.value!
  const p: ReportParams = {}
  if (d.params.includes('period')) Object.assign(p, { from: form.from, to: form.to })
  if (d.params.includes('date')) p.date = form.date
  if (d.params.includes('locations') && form.locationIds.length) p.locationIds = form.locationIds
  if (d.params.includes('categories') && form.categoryIds.length) p.categoryIds = form.categoryIds
  if (d.params.includes('brands') && form.brandIds.length) p.brandIds = form.brandIds
  if (d.params.includes('group')) p.group = form.group
  if (d.params.includes('days')) p.days = form.days
  return p
}

const table = ref<ReportTable>()
// Отчёт бывает на 20 тысяч строк: на экране — страницами по 100, итоги — по всем строкам.
const perPage = 100
const page = ref(1)
watch(table, () => (page.value = 1))
const pages = computed(() => Math.ceil((table.value?.rows.length ?? 0) / perPage))
const pageRows = computed(
  () => table.value?.rows.slice((page.value - 1) * perPage, page.value * perPage) ?? [],
)
const error = ref<string>()
const info = ref<string>()
const running = ref(false)
watch(code, () => {
  table.value = undefined
  error.value = info.value = undefined
})

async function run() {
  error.value = info.value = undefined
  running.value = true
  try {
    const { data, error: problem } = await api.POST('/reports/{code}', {
      params: { path: { code: code.value! } },
      body: params(),
    })
    if (problem) throw new ApiError(problem)
    table.value = data
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Нет связи с сервером'
  } finally {
    running.value = false
  }
}

async function exportAs(format: 'xlsx' | 'pdf') {
  error.value = info.value = undefined
  try {
    const {
      data,
      error: problem,
      response,
    } = await api.POST('/reports/{code}/export', {
      params: { path: { code: code.value! } },
      body: { params: params(), format },
      parseAs: 'blob',
    })
    if (problem) throw new ApiError(problem)
    if (response.status === 202) {
      info.value =
        'Отчёт формируется в фоне. Когда он будет готов, придёт уведомление, а файл появится в «Моих отчётах».'
      return
    }
    const url = URL.createObjectURL(data as unknown as Blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${def.value!.title}.${format}`
    a.click()
    setTimeout(() => URL.revokeObjectURL(url), 60_000)
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Нет связи с сервером'
  }
}

function cell(c: ReportColumn, v: unknown): string {
  if (v === null || v === undefined) return ''
  if (typeof v === 'number') {
    if (c.kind === 'money') return formatPrice(v)
    if (c.kind === 'liters') return v.toLocaleString('ru', { maximumFractionDigits: 3 })
    if (c.kind === 'percent') return `${v.toLocaleString('ru', { maximumFractionDigits: 1 })} %`
    return v.toLocaleString('ru')
  }
  if (c.kind === 'date' && typeof v === 'string' && v.length === 10)
    return `${v.slice(8, 10)}.${v.slice(5, 7)}.${v.slice(0, 4)}`
  return String(v)
}
const totals = computed(() => {
  const t = table.value
  if (!t || !t.columns.some((c) => c.sum)) return undefined
  return t.columns.map((c, i) =>
    c.sum
      ? t.rows.reduce((n, r) => n + (typeof r[i] === 'number' ? (r[i] as number) : 0), 0)
      : undefined,
  )
})
const numeric = (c: ReportColumn) => c.kind !== 'text' && c.kind !== 'date'
</script>

<template>
  <v-container class="pk-reports">
    <div class="d-flex flex-wrap align-center ga-2 mb-4">
      <h1 class="text-headline-small">Отчёты</h1>
      <v-spacer />
      <v-btn :prepend-icon="mdiFolderClockOutline" variant="text" :to="{ name: 'report-files' }"
        >Мои отчёты</v-btn
      >
    </div>

    <div class="pk-layout">
      <v-list density="compact" nav class="pk-catalog" data-test="catalog">
        <v-list-item
          v-for="d in catalog"
          :key="d.code"
          :active="d.code === code"
          :title="d.title"
          @click="router.replace({ query: { report: d.code } })"
        />
      </v-list>

      <div>
        <p v-if="!def" class="text-body-medium text-medium-emphasis">Выберите отчёт слева.</p>
        <template v-else>
          <h2 class="text-title-large mb-1">{{ def.title }}</h2>
          <p class="text-body-medium text-medium-emphasis mb-4">{{ def.description }}</p>

          <div class="pk-params mb-3">
            <template v-if="has('period')">
              <v-text-field
                v-model="form.from"
                type="date"
                label="С"
                density="compact"
                hide-details
              />
              <v-text-field
                v-model="form.to"
                type="date"
                label="По"
                density="compact"
                hide-details
              />
            </template>
            <v-text-field
              v-if="has('date')"
              v-model="form.date"
              type="date"
              label="На дату"
              density="compact"
              hide-details
            />
            <LocationSelect
              v-if="has('locations')"
              v-model="form.locationIds"
              :locations="places.options.value"
              label="Точки — все доступные"
              multiple
              chips
              closable-chips
              density="compact"
              hide-details
            />
            <v-autocomplete
              v-if="has('categories')"
              v-model="form.categoryIds"
              :items="categories ? categoryItems(categories.categories) : []"
              label="Категории"
              multiple
              chips
              closable-chips
              density="compact"
              hide-details
            />
            <v-autocomplete
              v-if="has('brands')"
              v-model="form.brandIds"
              :items="(brands ?? []).map((b) => ({ value: b.id, title: b.name }))"
              label="Бренды"
              multiple
              chips
              closable-chips
              density="compact"
              hide-details
            />
            <v-select
              v-if="has('group')"
              v-model="form.group"
              :items="groupItems"
              label="Группировка"
              density="compact"
              hide-details
            />
            <v-text-field
              v-if="has('days')"
              v-model.number="form.days"
              type="number"
              label="Без продаж, дней"
              density="compact"
              hide-details
            />
          </div>
          <div class="d-flex flex-wrap ga-2 mb-4">
            <v-btn
              :prepend-icon="mdiPlay"
              color="primary"
              variant="flat"
              :loading="running"
              data-test="run-report"
              @click="run"
              >Построить</v-btn
            >
            <v-btn
              :prepend-icon="mdiFileExcelOutline"
              variant="tonal"
              data-test="export-xlsx"
              @click="exportAs('xlsx')"
              >Excel</v-btn
            >
            <v-btn :prepend-icon="mdiFilePdfBox" variant="tonal" @click="exportAs('pdf')"
              >PDF</v-btn
            >
          </div>
          <v-alert v-if="error" type="error" class="mb-3">{{ error }}</v-alert>
          <v-alert v-if="info" type="info" variant="tonal" class="mb-3" data-test="queued">{{
            info
          }}</v-alert>

          <template v-if="table">
            <p v-if="!table.rows.length" class="text-body-medium text-medium-emphasis">
              За выбранные параметры данных нет.
            </p>
            <template v-else>
              <v-alert v-if="table.truncated" type="warning" variant="tonal" class="mb-3"
                >Показаны первые {{ table.rows.length }} строк — сузьте фильтр.</v-alert
              >
              <div class="pk-scroll">
                <v-table density="compact" fixed-header height="60vh" data-test="report-table">
                  <thead>
                    <tr>
                      <th
                        v-for="c in table.columns"
                        :key="c.title"
                        :class="{ 'text-end': numeric(c) }"
                      >
                        {{ c.title }}
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="(r, i) in pageRows" :key="(page - 1) * perPage + i">
                      <td
                        v-for="(c, j) in table.columns"
                        :key="j"
                        :class="{ 'text-end': numeric(c), 'text-no-wrap': numeric(c) }"
                      >
                        {{ cell(c, r[j]) }}
                      </td>
                    </tr>
                  </tbody>
                  <tfoot v-if="totals">
                    <tr class="font-weight-bold">
                      <td
                        v-for="(c, j) in table.columns"
                        :key="j"
                        :class="{ 'text-end': numeric(c) }"
                      >
                        {{ j === 0 ? 'Итого' : totals[j] !== undefined ? cell(c, totals[j]) : '' }}
                      </td>
                    </tr>
                  </tfoot>
                </v-table>
              </div>
              <div class="d-flex flex-wrap align-center ga-3 mt-2">
                <span class="text-body-small text-medium-emphasis"
                  >Строк: {{ table.rows.length }}</span
                >
                <v-spacer />
                <v-pagination
                  v-if="pages > 1"
                  v-model="page"
                  :length="pages"
                  :total-visible="7"
                  density="compact"
                  data-test="report-pages"
                />
              </div>
            </template>
          </template>
        </template>
      </div>
    </div>
  </v-container>
</template>

<style scoped>
.pk-reports {
  max-width: 1400px;
}
.pk-layout {
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 16px;
  align-items: start;
}
@media (max-width: 900px) {
  .pk-layout {
    grid-template-columns: 1fr;
  }
}
.pk-params {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 8px;
}
.pk-scroll {
  overflow-x: auto;
}
</style>
