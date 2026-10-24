<script setup lang="ts">
import {
  mdiAlertCircleOutline,
  mdiBarcodeScan,
  mdiCameraOutline,
  mdiCheckCircleOutline,
  mdiHelpCircleOutline,
  mdiInformationOutline,
  mdiMagnify,
  mdiUndoVariant,
} from '@mdi/js'
import { api, ApiError, type CodeResolution } from '@pikni/api'
import type { RecognizedCode, ScanSource } from '@pikni/scanner'
import type { SoundEvent } from '@pikni/ui'
import { computed, reactive, ref } from 'vue'

import ScanCamera from '@/components/ScanCamera.vue'
import { playFeedback } from '@/feedback'
import { formatDateTime } from '@/format'
import { load, save } from '@/preferences'
import { useMe } from '@/session'
import { formatAbv, formatPrice, variantLabel } from '@/products'
import { codeKindLabels, useScanner } from '@/scanning'

type Outcome = 'found' | 'missing' | 'local' | 'unrecognized' | 'error'

// Исход скана — сигнал, цвет, значок и слово (СК-3). Цвет всегда продублирован словом и
// значком, звук — карточкой: информация не передаётся одним каналом.
const outcomes: Record<Outcome, { sound: SoundEvent; color: string; icon: string; word: string }> =
  {
    found: { sound: 'accepted', color: 'success', icon: mdiCheckCircleOutline, word: 'Найден' },
    local: { sound: 'notInPlan', color: 'warning', icon: mdiInformationOutline, word: 'Считан' },
    missing: {
      sound: 'unknown',
      color: 'error',
      icon: mdiHelpCircleOutline,
      word: 'Нет в каталоге',
    },
    unrecognized: {
      sound: 'unknown',
      color: 'error',
      icon: mdiHelpCircleOutline,
      word: 'Не распознан',
    },
    error: { sound: 'unknown', color: 'error', icon: mdiAlertCircleOutline, word: 'Ошибка' },
  }

interface Entry {
  id: string
  raw: string
  code: RecognizedCode
  source: ScanSource
  at: string
  status: 'loading' | Outcome
  result?: CodeResolution
  error?: string
}

// Править каталог — привязать штрихкод, сменить цену — может руководитель или администратор.
const { data: me } = useMe()
const canEdit = computed(() => me.value?.permissions.includes('refs.edit') ?? false)

const recent = ref<Entry[]>([])
const selectedId = ref<string>()
const current = computed(
  () => recent.value.find((e) => e.id === selectedId.value) ?? recent.value[0],
)

// На сервер уходят только коды, по которым ищется товар. Короб, имущество, служебный QR и
// марка нового образца объясняются сразу на устройстве.
const searchable = (c: RecognizedCode) =>
  ['product', 'article', 'crpt', 'excise_mark'].includes(c.kind) && !c.newMark

function settle(entry: Entry, status: Outcome) {
  entry.status = status
  playFeedback(outcomes[status].sound)
}

const { submit, forget } = useScanner(async ({ event, code }) => {
  const entry = reactive<Entry>({
    id: event.id,
    raw: event.raw,
    code,
    source: event.source,
    at: event.scannedAt,
    status: 'loading',
  })
  recent.value = [entry, ...recent.value].slice(0, 10)
  selectedId.value = undefined
  if (!searchable(code)) return settle(entry, code.kind === 'unknown' ? 'unrecognized' : 'local')
  try {
    const { data, error } = await api.GET('/codes/resolve', {
      params: { query: { code: event.raw } },
    })
    if (error) throw new ApiError(error)
    entry.result = data
    settle(entry, data.product ? 'found' : 'missing')
  } catch (e) {
    entry.error = e instanceof ApiError ? e.message : 'Нет связи с сервером'
    settle(entry, 'error')
  }
})

// Ошибочный скан убирается из списка, а тот же код можно сразу считать снова.
function undo(entry: Entry) {
  recent.value = recent.value.filter((e) => e.id !== entry.id)
  if (selectedId.value === entry.id) selectedId.value = undefined
  forget(entry.raw)
  playFeedback('undone')
}

// Уголки рамки камеры вспыхивают цветом исхода последнего скана.
const flash = computed(() => {
  const e = recent.value[0]
  return e && e.status !== 'loading' ? { color: outcomes[e.status].color, key: e.id } : undefined
})
const outcome = (e: Entry) => (e.status === 'loading' ? undefined : outcomes[e.status])

// Камера включена по умолчанию на телефоне и планшете; на ПК обычно стоит ручной сканер.
// Выбор запоминается на устройстве.
const stored = load('pikni.scan.camera')
const cameraOn = ref(
  stored === null ? (window.matchMedia?.('(pointer: coarse)').matches ?? false) : stored === '1',
)
function setCamera(on: boolean) {
  cameraOn.value = on
  save('pikni.scan.camera', on ? '1' : '0')
}

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
    <p class="text-body-2 text-medium-emphasis mb-4" :class="{ 'd-none d-sm-block': cameraOn }">
      Наведите камеру на штрихкод, марку ЕГАИС или код «Честного знака». Ручной сканер работает без
      нажатия на поле. Повреждённый код можно ввести вручную.
    </p>

    <ScanCamera
      v-if="cameraOn"
      class="mb-4"
      :flash="flash"
      @scan="(c) => submit(c.raw, 'camera', c.format)"
      @close="setCamera(false)"
    />
    <v-btn
      v-else
      :prepend-icon="mdiCameraOutline"
      variant="tonal"
      class="mb-4"
      data-test="camera-on"
      @click="setCamera(true)"
    >
      Включить камеру
    </v-btn>

    <!-- Поле ввода стоит над результатом: карточка растёт вниз и не сдвигает его. -->
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

    <v-card
      v-if="current"
      class="pk-result mb-4"
      :style="
        outcome(current) && { '--pk-outcome': `rgb(var(--v-theme-${outcome(current)!.color}))` }
      "
      data-test="result"
    >
      <!-- Сверху то, что известно сразу после скана: сам код и его вид. Ответ сервера
           появляется ниже, в своей строке, и верх карточки не сдвигается. -->
      <v-card-item>
        <template #prepend>
          <div class="pk-outcome-icon">
            <v-progress-circular
              v-if="current.status === 'loading'"
              indeterminate
              size="28"
              width="3"
            />
            <v-icon
              v-else
              :icon="outcome(current)!.icon"
              :color="outcome(current)!.color"
              size="32"
            />
          </div>
        </template>
        <div class="pk-code text-body-1 text-break" data-test="raw">{{ short(current.raw) }}</div>
        <div class="text-body-2 text-medium-emphasis" data-test="kind">
          {{ codeKindLabels[current.code.kind] }} · {{ sourceLabels[current.source] }}
        </div>
      </v-card-item>
      <v-card-text>
        <div
          class="font-weight-bold mb-1"
          :class="outcome(current) ? `text-${outcome(current)!.color}` : 'text-medium-emphasis'"
          data-test="outcome"
        >
          {{ outcome(current)?.word ?? 'Ищем…' }}
        </div>
        <div
          v-if="current.result?.product"
          class="text-title-large text-wrap mb-3"
          data-test="name"
        >
          {{ current.result.product.name }}
        </div>
        <p v-if="current.code.alcocode" class="mb-2">
          Алкокод из марки: <span class="pk-code">{{ current.code.alcocode }}</span>
        </p>
        <p v-if="current.code.kind === 'crpt'" class="mb-2">
          GTIN <span class="pk-code">{{ current.code.code }}</span
          >, серийный номер
          <span class="pk-code">{{ current.code.serial }}</span>
        </p>

        <dl
          v-if="current.status === 'found' && current.result?.product"
          class="pk-facts"
          data-test="found"
        >
          <template v-if="current.result.variant">
            <dt>Артикул</dt>
            <dd class="pk-code">{{ current.result.variant.article }}</dd>
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
          <dt>Категория</dt>
          <dd>{{ current.result.product.categoryName }}</dd>
          <template v-if="current.result.product.abv !== undefined">
            <dt>Крепость</dt>
            <dd>{{ formatAbv(current.result.product.abv) }}</dd>
          </template>
          <dt>Производитель</dt>
          <dd>
            {{ current.result.product.producerName }}, {{ current.result.product.countryName }}
          </dd>
        </dl>

        <v-alert
          v-else-if="current.status === 'missing'"
          type="error"
          variant="tonal"
          data-test="missing"
        >
          <template v-if="current.code.kind === 'article'">
            Артикула {{ current.code.code }} в каталоге нет.
          </template>
          <template v-else-if="canEdit">
            Такого кода нет в каталоге. Привязать штрихкод к товару можно в карточке позиции, в
            блоке «Варианты».
          </template>
          <template v-else>
            Такого кода нет в каталоге. Сообщите руководителю: штрихкод к товару привязывают в
            каталоге.
          </template>
        </v-alert>
        <v-alert v-else-if="current.status === 'error'" type="error">{{ current.error }}</v-alert>
        <v-alert
          v-else-if="current.status === 'unrecognized'"
          type="error"
          variant="tonal"
          data-test="unrecognized"
        >
          Код не распознан. Проверьте, что сканируете штрихкод товара, марку или код «Честного
          знака»; у штрихкода EAN могла не сойтись контрольная цифра. Повреждённый код можно ввести
          вручную.
        </v-alert>

        <v-alert
          v-else-if="current.status === 'local'"
          type="warning"
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
          <template v-else>
            Служебный код Пикни. Откройте его на экране, где он нужен, — например, при сопряжении
            смартфона.
          </template>
        </v-alert>
      </v-card-text>
      <v-card-actions v-if="current.status !== 'loading'">
        <v-btn
          v-if="canEdit && current.status === 'found' && current.result?.product"
          :to="{ name: 'product', params: { id: current.result.product.id } }"
          variant="tonal"
          data-test="edit-product"
        >
          Изменить в каталоге
        </v-btn>
        <v-spacer />
        <v-btn :prepend-icon="mdiUndoVariant" data-test="undo" @click="undo(current)">
          Отменить скан
        </v-btn>
      </v-card-actions>
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
        >
          <template #prepend>
            <v-icon v-if="outcome(e)" :icon="outcome(e)!.icon" :color="outcome(e)!.color" />
          </template>
          <template #append>
            <v-btn
              :icon="mdiUndoVariant"
              variant="text"
              size="small"
              aria-label="Отменить скан"
              @click.stop="undo(e)"
            />
          </template>
        </v-list-item>
      </v-list>
    </v-card>
  </v-container>
</template>

<style scoped>
.pk-scan {
  max-width: 720px;
}

/* Полоса слева — цвет исхода скана; слово и значок дублируют его. */
.pk-result {
  border-inline-start: 4px solid var(--pk-outcome, transparent);
}

/* Место под значок исхода занято с самого начала: пока идёт поиск, в нём крутится индикатор. */
.pk-outcome-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
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
