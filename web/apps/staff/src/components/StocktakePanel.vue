<script setup lang="ts">
import {
  mdiArrowLeft,
  mdiBarcodeScan,
  mdiCameraOutline,
  mdiCheckCircleOutline,
  mdiFileExcelOutline,
  mdiFilePdfBox,
  mdiHelpCircleOutline,
  mdiInformationOutline,
  mdiLockOpenVariantOutline,
  mdiLockOutline,
  mdiMagnify,
  mdiRefresh,
  mdiUndoVariant,
} from '@mdi/js'
import {
  api,
  ApiError,
  type Document,
  type DocumentScan,
  type DocumentScanStatus,
  type StocktakeLine,
  type StocktakeMark,
  type StocktakeZone,
} from '@pikni/api'
import type { SoundEvent } from '@pikni/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import ExchangeLog from '@/components/ExchangeLog.vue'
import ScanCamera from '@pikni/ui/ScanCamera.vue'
import { confirm } from '@/confirm'
import { playFeedback } from '@/feedback'
import { formatDateTime } from '@/format'
import { scanStatusLabels, zoneLabels } from '@/labels'
import { useLive } from '@/live'
import { enqueue, isOffline, pendingFor } from '@/offline'
import { load, save } from '@/preferences'
import { formatPrice } from '@/products'
import { useScanner } from '@/scanning'

// Ревизия (СО-6, СО-7): зоны считают параллельно с разных устройств, прогресс «учёт / факт»
// обновляется у всей комиссии; закрытые зоны сводятся в сличительную ведомость.
const props = defineProps<{ doc: Document }>()
const queryClient = useQueryClient()
const docKey = computed(() => ['document', props.doc.id])
const key = computed(() => ['stocktake', props.doc.id])
useLive(
  () => [`doc:${props.doc.id}`],
  () => [key.value],
)

const { data: state, error: loadError } = useQuery({
  queryKey: key,
  queryFn: async () => {
    const { data, error } = await api.GET('/documents/{id}/stocktake', {
      params: { path: { id: props.doc.id } },
    })
    if (error) throw new ApiError(error)
    return data
  },
})

const status = computed(() => props.doc.status)
const counting = computed(() => status.value === 'counting')
const sheetVisible = computed(() =>
  ['reconciling', 'pending_approval', 'posted', 'awaiting_egais', 'egais_rejected'].includes(
    status.value,
  ),
)
const refresh = () => {
  void queryClient.invalidateQueries({ queryKey: docKey.value })
  void queryClient.invalidateQueries({ queryKey: key.value })
}

const actionError = ref<string>()
function action<T>(fn: (arg: T) => Promise<unknown>) {
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      actionError.value = undefined
      refresh()
    },
    onError: (e) => (actionError.value = e.message),
  })
}
const version = () => ({ version: props.doc.version })
const path = () => ({ params: { path: { id: props.doc.id } } })
const ok = <R extends { error?: unknown }>(r: R) => {
  if (r.error) throw new ApiError(r.error as never)
  return r
}

const { mutate: start, isPending: starting } = action(async () =>
  ok(await api.POST('/documents/{id}/start', { ...path(), body: version() })),
)
const { mutate: reconcile, isPending: reconciling } = action(async () =>
  ok(await api.POST('/documents/{id}/reconcile', { ...path(), body: version() })),
)
const { mutate: submitSheet, isPending: submitting } = action(async () =>
  ok(await api.POST('/documents/{id}/post', { ...path(), body: version() })),
)
const { mutate: cancel, isPending: cancelling } = action(async () =>
  ok(await api.POST('/documents/{id}/cancel', { ...path(), body: version() })),
)
const { mutate: zoneAction } = action(
  async ({ zone, act }: { zone: string; act: 'close' | 'reopen' | 'recount' }) =>
    ok(
      await api.POST('/documents/{id}/zones/{zoneId}/{action}', {
        params: { path: { id: props.doc.id, zoneId: zone, action: act } },
      }),
    ),
)

async function askStart() {
  const yes = await confirm({
    title: `Начать подсчёт ${props.doc.number}?`,
    text: 'Учётный остаток зафиксируется. Приход, перемещения и списания товаров ревизии остановятся до её завершения; продажи идут, если ревизия не строгая, — проданное за время подсчёта сличение учтёт само.',
    confirmText: 'Начать',
  })
  if (yes) start(undefined)
}
async function askCancel() {
  const yes = await confirm({
    title: 'Отменить ревизию?',
    text: 'Подсчёт не сохранится в учёте, блокировка товаров снимется.',
    confirmText: 'Отменить ревизию',
    danger: true,
  })
  if (yes) cancel(undefined)
}
async function askRecount(z: StocktakeZone) {
  const yes = await confirm({
    title: `Пересчитать «${zoneLabels[z.zoneKind]}»?`,
    text: 'Все сканы и подсчёт зоны отменятся — её считают заново.',
    confirmText: 'Пересчитать',
    danger: true,
  })
  if (yes) zoneAction({ zone: z.zoneId, act: 'recount' })
}
async function askSubmit() {
  const yes = await confirm({
    title: `Отправить ведомость ${props.doc.number} на утверждение?`,
    text: `Недостача ${shortage.value} шт., излишек ${surplus.value} шт. После утверждения другим сотрудником расхождения попадут в учёт, а в ЕГАИС уйдут акты списания и постановки на баланс.`,
    confirmText: 'Отправить',
  })
  if (yes) submitSheet(undefined)
}

// Зона, которую считает это устройство.
const zoneId = ref<string>()
const zone = computed(() => state.value?.zones.find((z) => z.zoneId === zoneId.value))
const allClosed = computed(
  () => !!state.value?.zones.length && state.value.zones.every((z) => z.status === 'closed'),
)

const outcome: Record<DocumentScanStatus, { color: string; icon: string; sound: SoundEvent }> = {
  accepted: { color: 'success', icon: mdiCheckCircleOutline, sound: 'accepted' },
  selected: { color: 'info', icon: mdiInformationOutline, sound: 'accepted' },
  duplicate: { color: 'warning', icon: mdiInformationOutline, sound: 'notInPlan' },
  unknown: { color: 'error', icon: mdiHelpCircleOutline, sound: 'unknown' },
  foreign: { color: 'warning', icon: mdiInformationOutline, sound: 'notInPlan' },
  rejected: { color: 'error', icon: mdiHelpCircleOutline, sound: 'unknown' },
}
const last = ref<DocumentScan>()
const scanError = ref<string>()
// Марку нового образца относят к товару, штрихкод которого отсканирован перед ней.
const selected = ref<{ variantId: string; name: string }>()
const queued = pendingFor(() => props.doc.id)

const { submit, forget } = useScanner(async ({ event }) => {
  if (!counting.value || !zone.value || zone.value.status !== 'counting') return
  scanError.value = undefined
  const body = {
    id: event.id,
    raw: event.raw,
    source: event.source === 'paired' ? ('wedge' as const) : event.source,
    scannedAt: event.scannedAt,
    zoneId: zone.value.zoneId,
    variantId: selected.value?.variantId,
  }
  try {
    const { data, error } = await api.POST('/documents/{id}/scans', { ...path(), body })
    if (error) throw new ApiError(error)
    last.value = data.scan
    if (data.scan.status === 'selected' && data.scan.variantId) {
      selected.value = { variantId: data.scan.variantId, name: data.scan.name ?? '' }
    }
    playFeedback(outcome[data.scan.status].sound)
    void queryClient.invalidateQueries({ queryKey: key.value })
    void queryClient.invalidateQueries({ queryKey: docKey.value })
  } catch (e) {
    if (isOffline(e)) {
      enqueue(props.doc.id, body)
      playFeedback('notInPlan')
      return
    }
    scanError.value = e instanceof ApiError ? e.message : 'Нет связи с сервером'
    playFeedback('unknown')
  }
})

const { mutate: undo } = action(async (scan: DocumentScan) => {
  ok(
    await api.DELETE('/documents/{id}/scans/{scanId}', {
      params: { path: { id: props.doc.id, scanId: scan.id } },
    }),
  )
  forget(scan.raw)
  if (last.value?.id === scan.id) last.value = undefined
  playFeedback('undone')
})

const { mutate: setCount } = action(async ({ line, qty }: { line: StocktakeLine; qty: number }) =>
  ok(
    await api.PUT('/documents/{id}/zones/{zoneId}/counts/{variantId}', {
      params: { path: { id: props.doc.id, zoneId: line.zoneId, variantId: line.variantId } },
      body: { qty },
    }),
  ),
)
function onCount(line: StocktakeLine, input: HTMLInputElement) {
  const qty = Number(input.value)
  if (!Number.isInteger(qty) || qty < 0 || qty > 99999) {
    input.value = String(line.factQty)
    return
  }
  if (qty !== line.factQty) setCount({ line, qty })
}

const zoneLines = computed(() =>
  (state.value?.lines ?? []).filter((l) => l.zoneId === zoneId.value),
)
const diffLines = computed(() => (state.value?.lines ?? []).filter((l) => l.factQty !== l.bookQty))
const shortage = computed(() =>
  diffLines.value.reduce((n, l) => n + Math.max(l.bookQty - l.factQty, 0), 0),
)
const surplus = computed(() =>
  diffLines.value.reduce((n, l) => n + Math.max(l.factQty - l.bookQty, 0), 0),
)
const showCost = computed(() => state.value?.lines.some((l) => l.unitCost !== undefined) ?? false)
const costOf = (l: StocktakeLine) => (l.unitCost ?? 0) * (l.factQty - l.bookQty)

const markKindLabels: Record<StocktakeMark['kind'], string> = {
  not_found: 'Не найдена — спишется',
  found: 'Найдена',
  moved: 'Найдена в другой зоне — переместится',
  foreign: 'Чужая марка',
}
const markProblems = computed(() => (state.value?.marks ?? []).filter((m) => m.kind !== 'found'))
const foreignHint = (m: StocktakeMark) =>
  m.kind !== 'foreign'
    ? ''
    : m.known
      ? 'числится в другой точке или продана — на учёт не встанет, нужна проверка'
      : 'встанет на баланс актом постановки'

const progress = (z: StocktakeZone) => {
  const book = z.bookMarks + z.bookQty
  const fact = z.foundMarks + z.countedQty
  return { book, fact, pct: book ? Math.min(100, Math.round((fact / book) * 100)) : 100 }
}

const stored = load('pikni.scan.camera')
const cameraOn = ref(
  stored === null ? (window.matchMedia?.('(pointer: coarse)').matches ?? false) : stored === '1',
)
function setCamera(on: boolean) {
  cameraOn.value = on
  save('pikni.scan.camera', on ? '1' : '0')
}
const flash = computed(() =>
  last.value ? { color: outcome[last.value.status].color, key: last.value.id } : undefined,
)
const manual = ref('')
function submitManual() {
  if (!manual.value.trim()) return
  submit(manual.value, 'manual')
  manual.value = ''
}
const short = (raw: string) => (raw.length > 32 ? `${raw.slice(0, 32)}…` : raw)
const formUrl = (form: 'inv3' | 'inv19', format: 'pdf' | 'xlsx') =>
  `/api/v1/documents/${props.doc.id}/forms/${form}?format=${format}`
</script>

<template>
  <div class="pk-stocktake">
    <v-alert v-if="loadError" type="error" class="mb-4">{{ loadError.message }}</v-alert>
    <p v-if="doc.comment" class="text-body-medium mb-3">{{ doc.comment }}</p>

    <v-alert v-if="status === 'draft'" type="info" variant="tonal" class="mb-4">
      Ревизия создана. Когда комиссия будет на местах, нажмите «Начать подсчёт»: с этого момента
      учётный остаток зафиксирован, приход и перемещения товаров ревизии остановлены. Касса работает
      — проданное во время подсчёта сличение учтёт.
    </v-alert>

    <!-- Подсчёт: выбор зоны, затем сканирование в неё. -->
    <template v-if="counting && !zone">
      <h2 class="text-title-large mb-2">Зоны</h2>
      <p class="text-body-medium text-medium-emphasis mb-3">
        Выберите зону, которую считаете. Каждую зону можно считать со своего устройства.
      </p>
    </template>
    <div v-if="state && (counting ? !zone : status === 'draft')" class="pk-zones mb-6">
      <v-card
        v-for="z in state.zones"
        :key="z.zoneId"
        :disabled="!counting"
        data-test="zone"
        @click="counting && (zoneId = z.zoneId)"
      >
        <v-card-title class="d-flex align-center">
          {{ zoneLabels[z.zoneKind] }}
          <v-spacer />
          <v-icon
            :icon="z.status === 'closed' ? mdiLockOutline : mdiLockOpenVariantOutline"
            :color="z.status === 'closed' ? 'success' : undefined"
            size="small"
          />
        </v-card-title>
        <v-card-text>
          <v-progress-linear
            :model-value="progress(z).pct"
            :color="z.status === 'closed' ? 'success' : 'primary'"
            height="8"
            rounded
            class="mb-2"
          />
          <div>Марки: {{ z.foundMarks }} из {{ z.bookMarks }}</div>
          <div>Без марок: {{ z.countedQty }} из {{ z.bookQty }} шт.</div>
          <div v-if="z.status === 'closed'" class="text-body-small text-medium-emphasis mt-1">
            Закрыта {{ z.closedByName }} {{ z.closedAt ? formatDateTime(z.closedAt) : '' }}
          </div>
        </v-card-text>
      </v-card>
    </div>

    <template v-if="counting && zone">
      <div class="d-flex flex-wrap align-center ga-2 mb-3">
        <v-btn :prepend-icon="mdiArrowLeft" variant="text" class="ml-n3" @click="zoneId = undefined"
          >Все зоны</v-btn
        >
        <h2 class="text-title-large">{{ zoneLabels[zone.zoneKind] }}</h2>
        <v-chip :color="zone.status === 'closed' ? 'success' : 'primary'" label size="small">
          {{ zone.status === 'closed' ? 'Закрыта' : 'Идёт подсчёт' }}
        </v-chip>
        <v-spacer />
        <v-btn
          v-if="zone.status === 'counting'"
          :prepend-icon="mdiLockOutline"
          variant="tonal"
          data-test="close-zone"
          @click="zoneAction({ zone: zone.zoneId, act: 'close' })"
          >Закрыть зону</v-btn
        >
        <template v-else-if="doc.canPost">
          <v-btn
            :prepend-icon="mdiLockOpenVariantOutline"
            variant="text"
            @click="zoneAction({ zone: zone.zoneId, act: 'reopen' })"
            >Открыть снова</v-btn
          >
          <v-btn :prepend-icon="mdiRefresh" variant="text" @click="askRecount(zone)"
            >Пересчитать</v-btn
          >
        </template>
      </div>
      <p class="text-body-medium mb-3">
        Марки: <b>{{ zone.foundMarks }}</b> из {{ zone.bookMarks }} · без марок:
        <b>{{ zone.countedQty }}</b> из {{ zone.bookQty }} шт.
      </p>

      <template v-if="zone.status === 'counting'">
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
          @click="setCamera(true)"
          >Включить камеру</v-btn
        >
        <v-form class="mb-3" @submit.prevent="submitManual">
          <v-text-field
            v-model="manual"
            :prepend-inner-icon="mdiBarcodeScan"
            :append-inner-icon="mdiMagnify"
            label="Код вручную"
            hide-details
            data-test="manual"
            @click:append-inner="submitManual"
          />
        </v-form>
        <v-chip
          v-if="selected"
          closable
          class="mb-3"
          data-test="selected"
          @click:close="selected = undefined"
          >Марки относятся к: {{ selected.name }}</v-chip
        >
        <v-alert
          v-if="queued"
          type="warning"
          variant="tonal"
          class="mb-3"
          data-test="offline-queue"
        >
          Нет связи с сервером. Сканов на устройстве: {{ queued }} — уйдут сами, когда связь
          появится. Продолжайте считать.
        </v-alert>
        <v-alert
          v-if="scanError"
          type="error"
          class="mb-3"
          closable
          @click:close="scanError = undefined"
          >{{ scanError }}</v-alert
        >
        <v-alert
          v-else-if="last"
          :color="outcome[last.status].color"
          :icon="outcome[last.status].icon"
          variant="tonal"
          class="mb-4"
          data-test="last-scan"
        >
          <div class="font-weight-bold">
            {{ scanStatusLabels[last.status]
            }}<template v-if="last.name">: {{ last.name }}</template>
          </div>
          <div v-if="last.reason">{{ last.reason }}</div>
          <div class="pk-code text-medium-emphasis text-break">{{ short(last.raw) }}</div>
        </v-alert>
      </template>

      <v-table v-if="zoneLines.length" density="comfortable" class="mb-4" data-test="zone-lines">
        <thead>
          <tr>
            <th>Товар</th>
            <th class="text-end">Учёт</th>
            <th class="text-end">Факт</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="l in zoneLines" :key="l.variantId">
            <td>
              <div>{{ l.name }}</div>
              <div class="text-body-small text-medium-emphasis">
                <span class="pk-code">{{ l.article }}</span> · {{ l.label }}
              </div>
            </td>
            <td class="text-end">{{ l.bookQty }}</td>
            <td
              class="text-end"
              :class="{ 'text-warning font-weight-bold': l.factQty !== l.bookQty }"
            >
              <input
                v-if="zone.status === 'counting' && l.accounting !== 'marked'"
                :value="l.factQty"
                type="number"
                min="0"
                class="pk-qty-input"
                :aria-label="`Посчитано: ${l.name}`"
                data-test="count"
                @change="(e) => onCount(l, e.target as HTMLInputElement)"
              />
              <span v-else>{{ l.factQty }}</span>
            </td>
          </tr>
        </tbody>
      </v-table>

      <template v-if="doc.scans.length">
        <h3 class="text-title-medium mb-2">Последние сканы</h3>
        <v-list density="compact" class="mb-4">
          <v-list-item
            v-for="s in doc.scans.slice(0, 10)"
            :key="s.id"
            :class="{ 'pk-undone': s.undoneAt }"
          >
            <template #prepend>
              <v-icon :icon="outcome[s.status].icon" :color="outcome[s.status].color" />
            </template>
            <v-list-item-title
              >{{ s.name ?? short(s.raw) }}
              <span class="text-medium-emphasis">· {{ scanStatusLabels[s.status] }}</span>
            </v-list-item-title>
            <v-list-item-subtitle>{{ s.reason ?? short(s.raw) }}</v-list-item-subtitle>
            <template v-if="!s.undoneAt && zone.status === 'counting'" #append>
              <v-btn
                :icon="mdiUndoVariant"
                variant="text"
                size="small"
                :aria-label="`Отменить скан ${s.name ?? s.raw}`"
                @click="undo(s)"
              />
            </template>
          </v-list-item>
        </v-list>
      </template>
    </template>

    <!-- Сличительная ведомость. -->
    <template v-if="sheetVisible && state">
      <div class="d-flex flex-wrap align-center ga-2 mb-2">
        <h2 class="text-title-large">Сличительная ведомость</h2>
        <v-spacer />
        <v-btn
          :prepend-icon="mdiFilePdfBox"
          :href="formUrl('inv3', 'pdf')"
          variant="text"
          size="small"
          >ИНВ-3</v-btn
        >
        <v-btn
          :prepend-icon="mdiFilePdfBox"
          :href="formUrl('inv19', 'pdf')"
          variant="text"
          size="small"
          >ИНВ-19</v-btn
        >
        <v-btn
          :prepend-icon="mdiFileExcelOutline"
          :href="formUrl('inv19', 'xlsx')"
          variant="text"
          size="small"
          >Excel</v-btn
        >
      </div>
      <p class="text-body-medium mb-3" data-test="sheet-summary">
        Недостача <b>{{ shortage }}</b> шт., излишек <b>{{ surplus }}</b> шт.<template
          v-if="!diffLines.length && !markProblems.length"
        >
          — расхождений нет.</template
        >
      </p>
      <v-table v-if="diffLines.length" density="comfortable" class="mb-4" data-test="sheet">
        <thead>
          <tr>
            <th>Товар</th>
            <th class="d-none d-sm-table-cell">Зона</th>
            <th class="text-end">Учёт</th>
            <th class="text-end d-none d-md-table-cell">Продано за подсчёт</th>
            <th class="text-end">Факт</th>
            <th class="text-end">Разница</th>
            <th v-if="showCost" class="text-end">Сумма</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="l in diffLines" :key="`${l.zoneId}-${l.variantId}`">
            <td>
              <div>{{ l.name }}</div>
              <div class="text-body-small text-medium-emphasis">
                <span class="pk-code">{{ l.article }}</span> · {{ l.label }}
              </div>
            </td>
            <td class="d-none d-sm-table-cell">{{ zoneLabels[l.zoneKind] }}</td>
            <td class="text-end">{{ l.bookQty }}</td>
            <td class="text-end d-none d-md-table-cell text-medium-emphasis">
              {{ l.soldQty || '' }}
            </td>
            <td class="text-end">{{ l.factQty }}</td>
            <td
              class="text-end font-weight-bold"
              :class="l.factQty < l.bookQty ? 'text-error' : 'text-success'"
            >
              {{ l.factQty > l.bookQty ? '+' : '' }}{{ l.factQty - l.bookQty }}
            </td>
            <td v-if="showCost" class="text-end">{{ formatPrice(costOf(l)) }}</td>
          </tr>
        </tbody>
      </v-table>
      <template v-if="markProblems.length">
        <h3 class="text-title-medium mb-2">Марки</h3>
        <v-list density="compact" class="mb-4" data-test="sheet-marks">
          <v-list-item v-for="m in markProblems" :key="m.code">
            <v-list-item-title>
              {{ m.name ?? 'Товар не определён' }}
              <span class="text-medium-emphasis">· {{ zoneLabels[m.zoneKind] }}</span>
            </v-list-item-title>
            <v-list-item-subtitle>
              {{ markKindLabels[m.kind]
              }}<template v-if="foreignHint(m)">: {{ foreignHint(m) }}</template> ·
              <span class="pk-code">{{ short(m.code) }}</span>
            </v-list-item-subtitle>
          </v-list-item>
        </v-list>
      </template>
    </template>

    <template v-if="doc.exchange?.length">
      <h2 class="text-title-large mb-2">Обмен с ЕГАИС</h2>
      <ExchangeLog :messages="doc.exchange" />
    </template>

    <v-alert v-if="actionError" type="error" class="mb-3">{{ actionError }}</v-alert>
    <div
      v-if="doc.canPost && ['draft', 'counting', 'reconciling'].includes(status)"
      class="pk-actions"
    >
      <v-btn variant="text" :loading="cancelling" data-test="cancel" @click="askCancel"
        >Отменить ревизию</v-btn
      >
      <v-spacer />
      <v-btn
        v-if="status === 'draft'"
        color="primary"
        variant="flat"
        :loading="starting"
        data-test="start"
        @click="askStart"
        >Начать подсчёт</v-btn
      >
      <v-btn
        v-else-if="counting"
        color="primary"
        variant="flat"
        :disabled="!allClosed"
        :title="allClosed ? '' : 'Сначала закройте все зоны'"
        :loading="reconciling"
        data-test="reconcile"
        @click="reconcile(undefined)"
        >К сличению</v-btn
      >
      <v-btn
        v-else
        color="primary"
        variant="flat"
        :loading="submitting"
        data-test="submit"
        @click="askSubmit"
        >Отправить на утверждение</v-btn
      >
    </div>
  </div>
</template>

<style scoped>
.pk-zones {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px;
}
.pk-qty-input {
  width: 6em;
  text-align: right;
  padding: 4px 8px;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 4px;
  color: inherit;
}
.pk-undone {
  opacity: 0.55;
  text-decoration: line-through;
}
.pk-actions {
  position: sticky;
  bottom: 12px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px;
  border-radius: 8px;
  background: rgb(var(--v-theme-surface));
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
}
</style>
