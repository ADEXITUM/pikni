<script setup lang="ts">
import { mdiBarcodeScan, mdiClose, mdiMagnify, mdiPrinterOutline } from '@mdi/js'
import { api, ApiError, type LabelFormat } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'

import { formatDateTime } from '@/format'
import { playFeedback } from '@/feedback'
import { openPdf } from '@/print'
import { formatPrice, variantLabel } from '@/products'
import { useScanner } from '@/scanning'
import { useLocationChoice } from '@/stock'

// Ценники (ЭТ-2, ЭТ-3): после смены цен — список товаров зала с устаревшими ценниками; выборочно —
// сканом или поиском. Печать — PDF на A4 с сеткой или на термоэтикетках 58 × 40 мм.
const place = useLocationChoice('floor.view', (l) => l.kind === 'store')
const tab = ref<'stale' | 'pick'>('stale')
const format = ref<LabelFormat>('a4')
const error = ref<string>()

const { data: stale, refetch } = useQuery({
  queryKey: computed(() => ['price-tags', 'stale', place.selectedId.value]),
  enabled: computed(() => !!place.selectedId.value),
  queryFn: async () => {
    const { data, error } = await api.GET('/price-tags/stale', {
      params: { query: { locationId: place.selectedId.value! } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
})
const chosen = ref<string[]>([])
watch(stale, (items) => (chosen.value = (items ?? []).map((i) => i.variantId)))
const kopecks = (n: number) => formatPrice(n / 100)

// Выборочно: товар добавляют сканом штрихкода или поиском по каталогу.
interface Picked {
  variantId: string
  name: string
  label: string
}
const picked = ref<Picked[]>([])
async function add(code: string) {
  error.value = undefined
  const { data, error: problem } = await api.GET('/codes/resolve', { params: { query: { code } } })
  if (problem || !data.product || !data.variant) {
    error.value = problem ? new ApiError(problem).message : `Товар по коду ${code} не найден`
    playFeedback('unknown')
    return
  }
  const v = data.variant
  if (!picked.value.some((p) => p.variantId === v.id)) {
    picked.value = [
      ...picked.value,
      { variantId: v.id, name: data.product.name, label: `${v.article} · ${variantLabel(v)}` },
    ]
  }
  playFeedback('accepted')
}
const { submit } = useScanner(({ event }) => {
  tab.value = 'pick'
  void add(event.raw)
})
const manual = ref('')
function submitManual() {
  if (!manual.value.trim()) return
  submit(manual.value, 'manual')
  manual.value = ''
}

const printing = ref(false)
async function print(variantIds: string[]) {
  error.value = undefined
  printing.value = true
  try {
    await openPdf(
      api.POST('/price-tags', {
        body: { locationId: place.selectedId.value!, variantIds, format: format.value },
        parseAs: 'blob',
      }),
    )
    void refetch()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Нет связи с сервером'
  } finally {
    printing.value = false
  }
}
</script>

<template>
  <v-container class="pk-tags">
    <h1 class="text-headline-small mb-1">Ценники</h1>
    <p class="text-body-medium text-medium-emphasis mb-4">
      На ценнике — наименование, объём, крепость, цена за штуку и за литр, штрихкод. Ценники из
      поступления печатаются прямо из документа.
    </p>

    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <v-select
        v-model="place.selectedId.value"
        :items="place.items.value"
        label="Магазин"
        hide-details
        density="compact"
        class="pk-place"
      />
      <v-btn-toggle v-model="format" mandatory density="compact" variant="outlined" divided>
        <v-btn value="a4" data-test="format-a4">A4, 21 на листе</v-btn>
        <v-btn value="thermal" data-test="format-thermal">Термо 58 × 40</v-btn>
      </v-btn-toggle>
    </div>
    <v-alert v-if="error" type="error" class="mb-3" closable @click:close="error = undefined">{{
      error
    }}</v-alert>

    <v-tabs v-model="tab" class="mb-3">
      <v-tab value="stale">Изменились цены · {{ stale?.length ?? 0 }}</v-tab>
      <v-tab value="pick">Выборочно · {{ picked.length }}</v-tab>
    </v-tabs>

    <template v-if="tab === 'stale'">
      <p v-if="stale && !stale.length" class="text-body-medium text-success">
        Все ценники в зале актуальны.
      </p>
      <template v-else-if="stale">
        <v-table density="comfortable" class="mb-4" data-test="stale">
          <thead>
            <tr>
              <th />
              <th>Товар</th>
              <th class="text-end">Было</th>
              <th class="text-end">Стало</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="it in stale" :key="it.variantId">
              <td class="pk-check">
                <v-checkbox-btn v-model="chosen" :value="it.variantId" density="compact" />
              </td>
              <td>
                <div>{{ it.name }}</div>
                <div class="text-body-small text-medium-emphasis">
                  <span class="pk-code">{{ it.article }}</span> · {{ it.label }}
                </div>
              </td>
              <td class="text-end text-medium-emphasis">
                <template v-if="it.printedKopecks !== undefined">
                  {{ kopecks(it.printedKopecks) }}
                  <div class="text-body-small">{{ formatDateTime(it.printedAt!) }}</div>
                </template>
                <template v-else>не печатался</template>
              </td>
              <td class="text-end font-weight-bold">{{ kopecks(it.priceKopecks) }}</td>
            </tr>
          </tbody>
        </v-table>
        <v-btn
          :prepend-icon="mdiPrinterOutline"
          color="primary"
          variant="flat"
          :disabled="!chosen.length"
          :loading="printing"
          data-test="print-stale"
          @click="print(chosen)"
          >Напечатать {{ chosen.length }}</v-btn
        >
      </template>
    </template>

    <template v-else>
      <v-form class="mb-3" @submit.prevent="submitManual">
        <v-text-field
          v-model="manual"
          :prepend-inner-icon="mdiBarcodeScan"
          :append-inner-icon="mdiMagnify"
          label="Штрихкод, артикул или марка"
          hint="Ручной сканер работает без нажатия на поле"
          persistent-hint
          data-test="manual"
          @click:append-inner="submitManual"
        />
      </v-form>
      <v-list v-if="picked.length" density="compact" class="mb-4" data-test="picked">
        <v-list-item v-for="p in picked" :key="p.variantId" :title="p.name" :subtitle="p.label">
          <template #append>
            <v-btn
              :icon="mdiClose"
              variant="text"
              size="small"
              :aria-label="`Убрать ${p.name}`"
              @click="picked = picked.filter((x) => x.variantId !== p.variantId)"
            />
          </template>
        </v-list-item>
      </v-list>
      <v-btn
        :prepend-icon="mdiPrinterOutline"
        color="primary"
        variant="flat"
        :disabled="!picked.length"
        :loading="printing"
        data-test="print-picked"
        @click="print(picked.map((p) => p.variantId))"
        >Напечатать {{ picked.length }}</v-btn
      >
    </template>
  </v-container>
</template>

<style scoped>
.pk-tags {
  max-width: 960px;
}
.pk-place {
  max-width: 360px;
}
.pk-check {
  width: 48px;
}
</style>
