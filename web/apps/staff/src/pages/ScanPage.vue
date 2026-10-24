<script setup lang="ts">
import { mdiBarcodeScan, mdiMagnify } from '@mdi/js'
import { api, ApiError, type CodeResolution } from '@pikni/api'
import type { RecognizedCode, ScanSource } from '@pikni/scanner'
import { computed, reactive, ref } from 'vue'

import { formatDateTime } from '@/format'
import { formatPrice, variantLabel } from '@/products'
import { codeKindLabels, useScanner } from '@/scanning'

interface Entry {
  id: string
  raw: string
  code: RecognizedCode
  source: ScanSource
  at: string
  status: 'loading' | 'found' | 'missing' | 'local' | 'error'
  result?: CodeResolution
  error?: string
}

const recent = ref<Entry[]>([])
const selectedId = ref<string>()
const current = computed(
  () => recent.value.find((e) => e.id === selectedId.value) ?? recent.value[0],
)

// На сервер уходят только коды, по которым ищется товар. Короб, имущество, служебный QR и
// марка нового образца объясняются сразу на устройстве.
const searchable = (c: RecognizedCode) =>
  ['product', 'crpt', 'excise_mark'].includes(c.kind) && !c.newMark

const { submit } = useScanner(async ({ event, code }) => {
  const entry = reactive<Entry>({
    id: event.id,
    raw: event.raw,
    code,
    source: event.source,
    at: event.scannedAt,
    status: searchable(code) ? 'loading' : 'local',
  })
  recent.value = [entry, ...recent.value].slice(0, 10)
  selectedId.value = undefined
  if (!searchable(code)) return
  try {
    const { data, error } = await api.GET('/codes/resolve', {
      params: { query: { code: event.raw } },
    })
    if (error) throw new ApiError(error)
    entry.result = data
    entry.status = data.product ? 'found' : 'missing'
  } catch (e) {
    entry.status = 'error'
    entry.error = e instanceof ApiError ? e.message : 'Нет связи с сервером'
  }
})

const manual = ref('')
function submitManual() {
  if (!manual.value.trim()) return
  submit(manual.value, 'manual')
  manual.value = ''
}

const short = (raw: string) => (raw.length > 32 ? `${raw.slice(0, 32)}…` : raw)
const sourceLabels: Record<ScanSource, string> = {
  camera: 'камера',
  wedge: 'сканер',
  manual: 'вручную',
  paired: 'смартфон',
}
const title = (e: Entry) =>
  e.result?.product?.name ?? (e.status === 'loading' ? 'Ищем…' : codeKindLabels[e.code.kind])
</script>

<template>
  <v-container class="pk-scan">
    <h1 class="text-h5 mb-2">Сканер</h1>
    <p class="text-body-2 text-medium-emphasis mb-4">
      Отсканируйте штрихкод, марку ЕГАИС или код «Честного знака» — ручной сканер работает без
      нажатия на поле. Повреждённый код можно ввести вручную.
    </p>

    <v-form class="mb-4" @submit.prevent="submitManual">
      <v-text-field
        v-model="manual"
        :prepend-inner-icon="mdiBarcodeScan"
        label="Код вручную"
        :append-inner-icon="mdiMagnify"
        hide-details
        data-test="manual"
        @click:append-inner="submitManual"
      />
    </v-form>

    <v-card v-if="current" class="mb-4" data-test="result">
      <v-card-item>
        <v-card-subtitle>
          <template v-if="current.status === 'found'">
            {{ codeKindLabels[current.code.kind] }} ·
          </template>
          {{ sourceLabels[current.source] }}
        </v-card-subtitle>
        <v-card-title class="text-wrap">{{ title(current) }}</v-card-title>
      </v-card-item>
      <v-card-text>
        <div class="pk-code text-medium-emphasis mb-3">{{ short(current.raw) }}</div>
        <p v-if="current.code.alcocode" class="mb-2">
          Алкокод из марки: <span class="pk-code">{{ current.code.alcocode }}</span>
        </p>
        <p v-if="current.code.kind === 'crpt'" class="mb-2">
          GTIN <span class="pk-code">{{ current.code.code }}</span
          >, серийный номер
          <span class="pk-code">{{ current.code.serial }}</span>
        </p>

        <v-progress-linear v-if="current.status === 'loading'" indeterminate />

        <template v-else-if="current.status === 'found' && current.result?.product">
          <dl class="pk-facts" data-test="found">
            <dt>Артикул</dt>
            <dd class="pk-code">{{ current.result.product.article }}</dd>
            <dt>Категория</dt>
            <dd>{{ current.result.product.categoryName }}</dd>
            <template v-if="current.result.variant">
              <dt>Вариант</dt>
              <dd>{{ variantLabel(current.result.variant) }}</dd>
              <dt>Цена</dt>
              <dd>
                {{
                  current.result.variant.retailPrice !== undefined
                    ? formatPrice(current.result.variant.retailPrice)
                    : 'не назначена'
                }}
                <span v-if="current.result.variant.minPrice" class="text-medium-emphasis">
                  · МРЦ {{ formatPrice(current.result.variant.minPrice) }}
                </span>
              </dd>
            </template>
          </dl>
          <v-btn
            :to="{ name: 'product', params: { id: current.result.product.id } }"
            variant="tonal"
            class="mt-3"
          >
            Открыть карточку
          </v-btn>
        </template>

        <v-alert
          v-else-if="current.status === 'missing'"
          type="warning"
          variant="tonal"
          data-test="missing"
        >
          Такого кода нет в каталоге. Привязать штрихкод к товару можно в карточке позиции, в блоке
          «Варианты».
        </v-alert>
        <v-alert v-else-if="current.status === 'error'" type="error">{{ current.error }}</v-alert>

        <v-alert
          v-else-if="current.status === 'local'"
          type="info"
          variant="tonal"
          data-test="local"
        >
          <template v-if="current.code.newMark">
            В марке нового образца нет алкокода. Товар по ней найдётся после приёмки, когда марка
            попадёт в систему из документов ЕГАИС.
          </template>
          <template v-else-if="current.code.kind === 'excise_mark'">
            Учебная марка имитатора ЕГАИС. Товар по ней найдётся после приёмки.
          </template>
          <template v-else-if="current.code.kind === 'package'">
            Короб или палета, SSCC <span class="pk-code">{{ current.code.code }}</span
            >. Его содержимое покажет документ приёмки.
          </template>
          <template v-else-if="current.code.kind === 'asset'">
            Инвентарный номер {{ current.code.code }}. Учёт имущества в системе пока не подключён.
          </template>
          <template v-else-if="current.code.kind === 'service_qr'">
            Служебный код Пикни. Откройте его на экране, где он нужен, — например, при сопряжении
            смартфона.
          </template>
          <template v-else>
            Код не распознан. Проверьте, что сканируете штрихкод товара, марку или код «Честного
            знака»; у штрихкода EAN могла не сойтись контрольная цифра.
          </template>
        </v-alert>
      </v-card-text>
    </v-card>

    <v-card v-if="recent.length > 1" title="Последние сканы">
      <v-list density="compact" data-test="recent">
        <v-list-item
          v-for="e in recent"
          :key="e.id"
          :active="e.id === current?.id"
          :title="title(e)"
          :subtitle="`${sourceLabels[e.source]} · ${formatDateTime(e.at)}`"
          @click="selectedId = e.id"
        />
      </v-list>
    </v-card>
  </v-container>
</template>

<style scoped>
.pk-scan {
  max-width: 720px;
}

.pk-facts {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 4px 16px;
}

.pk-facts dt {
  color: rgba(var(--v-theme-on-surface), var(--v-medium-emphasis-opacity));
}
</style>
