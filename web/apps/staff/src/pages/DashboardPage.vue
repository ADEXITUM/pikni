<script setup lang="ts">
import {
  mdiAlertCircleOutline,
  mdiBottleWineOutline,
  mdiCashMultiple,
  mdiGlassFragile,
  mdiPackageVariantClosed,
  mdiScaleBalance,
  mdiSync,
  mdiTruckAlertOutline,
} from '@mdi/js'
import { api, ApiError } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import BarChart from '@/components/BarChart.vue'
import { formatPrice } from '@/products'
import { useLocationChoice } from '@/stock'

// Дашборд руководителя (ОТ-4): показатели и графики по сети или точке за период.
const places = useLocationChoice('reports.view')
const location = ref<string | null>(null)
const days = ref(30)
// Конец периода — «сегодня» на сервере: у дальневосточных точек день наступает раньше.
const range = computed(() => ({
  from: new Date(Date.now() - (days.value - 1) * 86400000).toLocaleDateString('sv-SE'),
}))

const { data, error, isPending } = useQuery({
  queryKey: computed(() => ['dashboard', range.value, location.value]),
  queryFn: async () => {
    const { data, error } = await api.GET('/dashboard', {
      params: { query: { ...range.value, locationId: location.value ?? undefined } },
    })
    if (error) throw new ApiError(error)
    return data
  },
})

const rub = (v: number) => formatPrice(v)
const short = (v: number) =>
  v >= 1_000_000
    ? `${(v / 1_000_000).toFixed(1).replace('.', ',')} млн ₽`
    : v >= 1000
      ? `${Math.round(v / 1000)} тыс. ₽`
      : formatPrice(v)
const dayLabel = (s: string) => `${s.slice(8, 10)}.${s.slice(5, 7)}`

const tiles = computed(() => {
  const d = data.value
  if (!d) return []
  return [
    {
      title: 'Стоимость запаса',
      value: short(d.stockCost),
      icon: mdiPackageVariantClosed,
      hint: 'по себестоимости',
    },
    {
      title: 'Выручка за период',
      value: short(d.revenue),
      icon: mdiCashMultiple,
      hint: `${days.value} дней`,
    },
    {
      title: 'Оборачиваемость',
      value: d.turnoverDays !== undefined ? `${Math.round(d.turnoverDays)} дн.` : '—',
      icon: mdiSync,
      hint: 'за сколько дней продаётся запас',
    },
    {
      title: 'Неликвид',
      value: `${d.deadShare.toFixed(1).replace('.', ',')} %`,
      icon: mdiBottleWineOutline,
      hint: 'без продаж 90 дней, доля запаса',
    },
    {
      title: 'Расхождения с ЕГАИС',
      value: String(d.egaisMismatches),
      icon: mdiScaleBalance,
      hint: 'позиции и марки',
      to: { name: 'reconciliation' },
      alert: d.egaisMismatches > 0,
    },
    {
      title: 'Неподтверждённые ТТН',
      value: String(d.unconfirmedTtn),
      icon: mdiTruckAlertOutline,
      hint: 'ждут приёмки',
      to: { name: 'documents' },
      alert: d.unconfirmedTtn > 0,
    },
    {
      title: 'Бой за период',
      value: short(d.breakage),
      icon: mdiGlassFragile,
      hint: 'по себестоимости',
    },
  ]
})
</script>

<template>
  <v-container class="pk-dash">
    <h1 class="text-headline-small mb-3">Дашборд</h1>
    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <v-btn-toggle v-model="days" mandatory density="compact" variant="outlined" divided>
        <v-btn :value="7">7 дней</v-btn>
        <v-btn :value="30">30 дней</v-btn>
        <v-btn :value="90">90 дней</v-btn>
      </v-btn-toggle>
      <v-select
        v-model="location"
        :items="places.items.value"
        label="Вся сеть"
        clearable
        density="compact"
        hide-details
        class="pk-place"
      />
    </div>
    <v-alert v-if="error" type="error" class="mb-3">{{ error.message }}</v-alert>
    <v-progress-linear v-if="isPending" indeterminate class="mb-3" />

    <template v-if="data">
      <div class="pk-tiles mb-6" data-test="tiles">
        <v-card v-for="t in tiles" :key="t.title" :to="t.to" variant="flat" class="pk-tile">
          <div class="d-flex align-center ga-2 text-body-medium text-medium-emphasis">
            <v-icon :icon="t.icon" size="small" />{{ t.title }}
          </div>
          <div class="text-headline-small mt-1 d-flex align-center ga-1">
            {{ t.value }}
            <v-icon v-if="t.alert" :icon="mdiAlertCircleOutline" color="warning" size="small" />
          </div>
          <div class="text-body-small text-medium-emphasis">{{ t.hint }}</div>
        </v-card>
      </div>

      <div class="pk-charts">
        <v-card variant="flat" class="pa-4 pk-wide">
          <h2 class="text-title-medium mb-2">Выручка по дням</h2>
          <BarChart :points="data.revenueByDay" :format="rub" :label="dayLabel" />
        </v-card>
        <v-card variant="flat" class="pa-4">
          <h2 class="text-title-medium mb-2">Запас по категориям</h2>
          <BarChart :points="data.stockByCategory.slice(0, 8)" :format="short" horizontal />
        </v-card>
        <v-card variant="flat" class="pa-4">
          <h2 class="text-title-medium mb-2">Запас по точкам</h2>
          <BarChart :points="data.stockByLocation" :format="short" horizontal />
        </v-card>
      </div>
    </template>
  </v-container>
</template>

<style scoped>
.pk-dash {
  max-width: 1200px;
}
.pk-place {
  max-width: 320px;
}
.pk-tiles {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 12px;
}
.pk-tile {
  padding: 16px;
}
.pk-charts {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.pk-wide {
  grid-column: 1 / -1;
}
@media (max-width: 900px) {
  .pk-charts {
    grid-template-columns: 1fr;
  }
}
</style>
