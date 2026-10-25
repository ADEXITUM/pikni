<script setup lang="ts">
import {
  mdiClipboardCheckOutline,
  mdiFileExcelOutline,
  mdiMagnify,
  mdiPlus,
  mdiPrinterOutline,
} from '@mdi/js'
import { api, ApiError, type AssetStatus, type LabelFormat } from '@pikni/api'
import { keepPreviousData, useMutation, useQuery } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import AssetFormDialog from '@/components/AssetFormDialog.vue'
import LocationSelect from '@/components/LocationSelect.vue'
import { assetStatusColors, assetStatusLabels, assetCategoriesQuery } from '@/assets'
import { formatDateTime } from '@/format'
import { openPdf } from '@/print'
import { formatPrice } from '@/products'
import { useMe } from '@/session'
import { useLocationChoice } from '@/stock'

// Ведомость имущества по точкам и МОЛ (ИМ-6), печать наклеек (ИМ-2), инвентаризации (ИМ-4).
const router = useRouter()
const { data: me } = useMe()
const canEdit = computed(() => me.value?.permissions.includes('assets.edit') ?? false)
const places = useLocationChoice('assets.view')
const editable = useLocationChoice('assets.edit')
const tab = ref<'register' | 'audits'>('register')

const location = ref<string | null>(null)
const category = ref<string | null>(null)
const status = ref<AssetStatus | null>(null)
const q = ref('')
const term = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
watch(q, (v) => {
  clearTimeout(timer)
  timer = setTimeout(() => (term.value = (v ?? '').trim()), 300)
})
const query = computed(() => ({
  locationId: location.value ?? undefined,
  category: category.value ?? undefined,
  status: status.value ?? undefined,
  q: term.value || undefined,
}))
// В федеральной сети объектов сотни тысяч: ведомость приходит страницами, итог считает сервер.
const perPage = 50
const page = ref(1)
watch(query, () => (page.value = 1))
const { data: categories } = useQuery(assetCategoriesQuery)
const {
  data: register,
  isPending,
  error,
} = useQuery({
  queryKey: computed(() => ['assets', query.value, page.value]),
  placeholderData: keepPreviousData,
  queryFn: async () => {
    const { data, error } = await api.GET('/assets', {
      params: { query: { ...query.value, offset: (page.value - 1) * perPage, limit: perPage } },
    })
    if (error) throw new ApiError(error)
    return data
  },
})
const assets = computed(() => register.value?.items)
const total = computed(() => register.value?.totalCostKopecks ?? 0)
const pages = computed(() => Math.ceil((register.value?.total ?? 0) / perPage))
const registerUrl = computed(() => {
  const params = new URLSearchParams(
    Object.entries(query.value).filter(([, v]) => v !== undefined) as [string, string][],
  )
  return `/api/v1/assets/register?${params}`
})

const selected = ref<string[]>([])
const format = ref<LabelFormat>('thermal')
const printError = ref<string>()
async function printLabels() {
  printError.value = undefined
  try {
    await openPdf(
      api.POST('/assets/labels', {
        body: { assetIds: selected.value, format: format.value },
        parseAs: 'blob',
      }),
    )
  } catch (e) {
    printError.value = e instanceof Error ? e.message : 'Нет связи с сервером'
  }
}

const createOpen = ref(false)

const { data: audits } = useQuery({
  queryKey: ['asset-audits'],
  enabled: computed(() => tab.value === 'audits'),
  queryFn: async () => {
    const { data, error } = await api.GET('/asset-audits')
    if (error) throw new ApiError(error)
    return data.items
  },
})
const auditPlace = ref<string>()
const {
  mutate: startAudit,
  isPending: starting,
  error: startError,
} = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/asset-audits', {
      body: { locationId: auditPlace.value ?? editable.selectedId.value! },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (st) => router.push({ name: 'asset-audit', params: { id: st.audit.id } }),
})
const auditStatus = { counting: 'Идёт', finished: 'Завершена', cancelled: 'Отменена' } as const
</script>

<template>
  <v-container class="pk-assets">
    <h1 class="text-headline-small mb-1">Имущество</h1>
    <p class="text-body-medium text-medium-emphasis mb-4">
      Оборудование и мебель точек: кто отвечает, где стоит, в каком состоянии. Наклейку с
      инвентарным номером читает любой сканер и камера телефона.
    </p>

    <v-tabs v-model="tab" class="mb-4">
      <v-tab value="register">Ведомость</v-tab>
      <v-tab value="audits">Инвентаризации</v-tab>
    </v-tabs>

    <template v-if="tab === 'register'">
      <div class="d-flex flex-wrap ga-2 mb-3">
        <v-btn
          v-if="canEdit"
          :prepend-icon="mdiPlus"
          color="primary"
          variant="flat"
          data-test="new-asset"
          @click="createOpen = true"
          >Принять к учёту</v-btn
        >
        <v-btn :prepend-icon="mdiFileExcelOutline" variant="tonal" :href="registerUrl"
          >Ведомость в Excel</v-btn
        >
      </div>
      <div class="d-flex flex-wrap ga-2 mb-3">
        <LocationSelect
          v-model="location"
          :locations="places.options.value"
          label="Точка"
          clearable
          density="compact"
          hide-details
          class="pk-filter"
        />
        <v-select
          v-model="category"
          :items="(categories ?? []).map((c) => ({ value: c.code, title: c.name }))"
          label="Категория"
          clearable
          density="compact"
          hide-details
          class="pk-filter"
        />
        <v-select
          v-model="status"
          :items="Object.entries(assetStatusLabels).map(([value, title]) => ({ value, title }))"
          label="Состояние"
          clearable
          density="compact"
          hide-details
          class="pk-filter"
        />
        <v-text-field
          v-model="q"
          :prepend-inner-icon="mdiMagnify"
          label="Номер, наименование, серийный"
          density="compact"
          hide-details
          class="pk-filter"
          data-test="asset-search"
        />
      </div>

      <v-alert v-if="error" type="error" class="mb-3">{{ error.message }}</v-alert>
      <v-progress-linear v-if="isPending" indeterminate />
      <p v-else-if="!assets?.length" class="text-body-medium text-medium-emphasis">
        Объектов не найдено.
      </p>
      <template v-else>
        <v-table density="comfortable" class="mb-3" data-test="assets">
          <thead>
            <tr>
              <th />
              <th>Объект</th>
              <th class="d-none d-md-table-cell">Точка</th>
              <th class="d-none d-sm-table-cell">МОЛ</th>
              <th class="text-end">Стоимость</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="a in assets" :key="a.id" data-test="asset">
              <td class="pk-check">
                <v-checkbox-btn v-model="selected" :value="a.id" density="compact" />
              </td>
              <td>
                <router-link :to="{ name: 'asset', params: { id: a.id } }" class="pk-link">
                  <span class="pk-code">{{ a.number }}</span> · {{ a.name }}
                </router-link>
                <div class="text-body-small text-medium-emphasis">
                  {{ a.categoryName }}
                  <v-chip
                    v-if="a.status !== 'in_use'"
                    :color="assetStatusColors[a.status]"
                    size="x-small"
                    label
                    class="ml-1"
                    >{{ assetStatusLabels[a.status] }}</v-chip
                  >
                </div>
              </td>
              <td class="d-none d-md-table-cell">{{ a.locationCode }} · {{ a.locationName }}</td>
              <td class="d-none d-sm-table-cell">{{ a.responsibleName ?? '—' }}</td>
              <td class="text-end">{{ formatPrice(a.costKopecks / 100) }}</td>
            </tr>
          </tbody>
          <tfoot>
            <tr>
              <td />
              <td class="font-weight-bold">Итого: {{ register?.total ?? 0 }}</td>
              <td class="d-none d-md-table-cell" />
              <td class="d-none d-sm-table-cell" />
              <td class="text-end font-weight-bold">{{ formatPrice(total / 100) }}</td>
            </tr>
          </tfoot>
        </v-table>
        <v-pagination
          v-if="pages > 1"
          v-model="page"
          :length="pages"
          :total-visible="7"
          density="comfortable"
          class="mb-3"
          data-test="asset-pages"
        />
        <div class="d-flex flex-wrap align-center ga-2">
          <v-btn-toggle v-model="format" mandatory density="compact" variant="outlined" divided>
            <v-btn value="thermal">Термо 58 × 40</v-btn>
            <v-btn value="a4">A4</v-btn>
          </v-btn-toggle>
          <v-btn
            :prepend-icon="mdiPrinterOutline"
            variant="tonal"
            :disabled="!selected.length"
            data-test="print-labels"
            @click="printLabels"
            >Наклейки · {{ selected.length }}</v-btn
          >
        </div>
        <v-alert v-if="printError" type="error" class="mt-3">{{ printError }}</v-alert>
      </template>
      <AssetFormDialog v-model="createOpen" />
    </template>

    <template v-else>
      <div v-if="canEdit" class="d-flex flex-wrap align-center ga-2 mb-4">
        <LocationSelect
          v-model="auditPlace"
          :locations="editable.options.value"
          label="Точка"
          density="compact"
          hide-details
          class="pk-filter"
        />
        <v-btn
          :prepend-icon="mdiClipboardCheckOutline"
          color="primary"
          variant="flat"
          :loading="starting"
          data-test="start-audit"
          @click="startAudit()"
          >Начать инвентаризацию</v-btn
        >
      </div>
      <v-alert v-if="startError" type="error" class="mb-3">{{ startError.message }}</v-alert>
      <p v-if="audits && !audits.length" class="text-body-medium text-medium-emphasis">
        Инвентаризаций имущества ещё не было.
      </p>
      <v-list v-else lines="two">
        <v-list-item
          v-for="a in audits"
          :key="a.id"
          :to="{ name: 'asset-audit', params: { id: a.id } }"
          :title="`${a.locationName} · ${auditStatus[a.status]}`"
          :subtitle="`${formatDateTime(a.startedAt)} · ${a.startedByName}`"
        />
      </v-list>
    </template>
  </v-container>
</template>

<style scoped>
.pk-assets {
  max-width: 1100px;
}
.pk-filter {
  max-width: 260px;
  min-width: 180px;
}
.pk-check {
  width: 48px;
}
.pk-link {
  color: inherit;
  text-decoration: none;
}
.pk-link:hover {
  text-decoration: underline;
}
</style>
