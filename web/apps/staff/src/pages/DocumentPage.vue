<script setup lang="ts">
import {
  mdiArrowLeft,
  mdiBarcodeScan,
  mdiBottleWineOutline,
  mdiCameraOutline,
  mdiCheckCircleOutline,
  mdiClose,
  mdiHelpCircleOutline,
  mdiInformationOutline,
  mdiMagnify,
  mdiUndoVariant,
} from '@mdi/js'
import {
  api,
  ApiError,
  type Document,
  type DocumentLine,
  type DocumentScan,
  type DocumentScanResult,
  type DocumentScanStatus,
} from '@pikni/api'
import type { SoundEvent } from '@pikni/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'

import ExchangeLog from '@/components/ExchangeLog.vue'
import ReceiptPanel from '@/components/ReceiptPanel.vue'
import ScanCamera from '@pikni/ui/ScanCamera.vue'
import { confirm } from '@/confirm'
import { playFeedback } from '@/feedback'
import { formatDateTime } from '@/format'
import {
  documentKindLabels,
  documentStatusColors,
  documentStatusLabels,
  scanStatusLabels,
  zoneLabels,
} from '@/labels'
import { useLive } from '@/live'
import { load, save } from '@/preferences'
import { accountingLabels, formatPrice, variantLabel } from '@/products'
import { useScanner } from '@/scanning'

const route = useRoute()
const id = computed(() => String(route.params.id))
const queryClient = useQueryClient()
const key = computed(() => ['document', id.value])
// Сканы коллег, квитанции ЕГАИС и проведение видны без перезагрузки (УВ-1).
useLive(
  () => [`doc:${id.value}`],
  () => [key.value],
)

const { data: doc, error: loadError } = useQuery({
  queryKey: key,
  queryFn: async () => {
    const { data, error } = await api.GET('/documents/{id}', { params: { path: { id: id.value } } })
    if (error) throw new ApiError(error)
    return data
  },
})

// Исход скана: цвет и значок дублируют слово, звук — карточку (СК-3).
const outcome: Record<DocumentScanStatus, { color: string; icon: string; sound: SoundEvent }> = {
  accepted: { color: 'success', icon: mdiCheckCircleOutline, sound: 'accepted' },
  selected: { color: 'info', icon: mdiInformationOutline, sound: 'accepted' },
  duplicate: { color: 'warning', icon: mdiInformationOutline, sound: 'notInPlan' },
  unknown: { color: 'error', icon: mdiHelpCircleOutline, sound: 'unknown' },
  foreign: { color: 'error', icon: mdiHelpCircleOutline, sound: 'unknown' },
  rejected: { color: 'error', icon: mdiHelpCircleOutline, sound: 'unknown' },
}

// Ответ на скан меняет одну строку и ленту — документ не перезапрашивается целиком.
function apply(res: DocumentScanResult) {
  queryClient.setQueryData<Document>(key.value, (d) => {
    if (!d) return d
    const scans = [res.scan, ...d.scans.filter((s) => s.id !== res.scan.id)].slice(0, 50)
    let lines = d.lines
    if (res.line) {
      const line = res.line
      const known = lines.some((l) => l.variantId === line.variantId)
      lines = known
        ? lines.map((l) => (l.variantId === line.variantId ? line : l))
        : [...lines, line]
      lines = lines.filter((l) => l.qty > 0)
    }
    const totalQty = lines.reduce((n, l) => n + l.qty, 0)
    return { ...d, scans, lines, lineCount: lines.length, totalQty }
  })
}

// Марка нового образца не содержит кода продукции: при вводе остатков сначала сканируется
// штрихкод бутылки, и следующие марки относятся к этому товару.
const selected = ref<{ variantId: string; name: string }>()
const last = ref<DocumentScan>()
const scanError = ref<string>()

const { submit, forget } = useScanner(async ({ event }) => {
  scanError.value = undefined
  try {
    const { data, error } = await api.POST('/documents/{id}/scans', {
      params: { path: { id: id.value } },
      body: {
        id: event.id,
        raw: event.raw,
        source: event.source === 'paired' ? 'wedge' : event.source,
        scannedAt: event.scannedAt,
        variantId: selected.value?.variantId,
      },
    })
    if (error) throw new ApiError(error)
    last.value = data.scan
    if (data.scan.status === 'selected' && data.scan.variantId) {
      selected.value = { variantId: data.scan.variantId, name: data.scan.name ?? '' }
    }
    apply(data)
    playFeedback(outcome[data.scan.status].sound)
    // В поступлении скан короба меняет факт сразу нескольких строк и состояние коробов.
    if (doc.value?.kind === 'receipt') void queryClient.invalidateQueries({ queryKey: key.value })
  } catch (e) {
    scanError.value = e instanceof ApiError ? e.message : 'Нет связи с сервером'
    playFeedback('unknown')
  }
})

const { mutate: undo } = useMutation({
  mutationFn: async (scan: DocumentScan) => {
    const { data, error } = await api.DELETE('/documents/{id}/scans/{scanId}', {
      params: { path: { id: id.value, scanId: scan.id } },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (data, scan) => {
    apply(data)
    if (doc.value?.kind === 'receipt') void queryClient.invalidateQueries({ queryKey: key.value })
    forget(scan.raw)
    if (last.value?.id === scan.id) last.value = undefined
    playFeedback('undone')
  },
  onError: (e) => (scanError.value = e.message),
})

// Количество меняют вручную только у товара без марок: строку с марками считают сканы.
const { mutate: setQty } = useMutation({
  mutationFn: async ({ line, qty }: { line: DocumentLine; qty: number }) => {
    const { data, error } = await api.PUT('/documents/{id}/lines/{variantId}', {
      params: { path: { id: id.value, variantId: line.variantId } },
      body: { qty },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (data) => queryClient.setQueryData(key.value, data),
  onError: (e) => {
    scanError.value = e.message
    void queryClient.invalidateQueries({ queryKey: key.value })
  },
})
async function onQty(line: DocumentLine, value: string, input: HTMLInputElement) {
  const qty = Number(value)
  if (!Number.isInteger(qty) || qty < 0 || qty > 99999 || qty === line.qty) {
    input.value = String(line.qty)
    return
  }
  if (qty === 0) {
    const ok = await confirm({
      title: 'Убрать строку?',
      text: `${line.article} · ${line.name}, ${variantLabel(line)} — ${line.qty} шт. уйдут из документа.`,
      confirmText: 'Убрать',
      danger: true,
    })
    if (!ok) {
      input.value = String(line.qty)
      return
    }
  }
  setQty({ line, qty })
}

const kindText = computed(() => (doc.value ? documentKindLabels[doc.value.kind] : ''))
const zonesText = computed(() => {
  const d = doc.value
  if (!d) return ''
  if (d.kind === 'transfer') return `в ${d.destLocationName}`
  return d.destZoneKind
    ? `${zoneLabels[d.zoneKind]} → ${zoneLabels[d.destZoneKind]}`
    : zoneLabels[d.zoneKind]
})
// В поступлении количество — факт приёмки, он считается по сканам и коробам.
const totalQty = computed(() =>
  doc.value?.kind === 'receipt'
    ? doc.value.lines.reduce((n, l) => n + (l.qtyFact ?? l.qty), 0)
    : (doc.value?.totalQty ?? 0),
)
const showCost = computed(() => doc.value?.lines.some((l) => l.unitCost !== undefined) ?? false)

const actionError = ref<string>()
const { mutate: post, isPending: posting } = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/documents/{id}/post', {
      params: { path: { id: id.value } },
      body: { version: doc.value!.version },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (data) => {
    queryClient.setQueryData(key.value, data)
    void queryClient.invalidateQueries({ queryKey: ['documents'] })
    void queryClient.invalidateQueries({ queryKey: ['stock'] })
    playFeedback('posted')
  },
  onError: (e) => (actionError.value = e.message),
})
async function askPost() {
  const d = doc.value!
  actionError.value = undefined
  if (d.kind === 'transfer') {
    const ok = await confirm({
      title: `Отгрузить ${d.number}?`,
      text: `Собранное (${d.totalQty} шт.) спишется со склада и встанет «в пути» в ${d.destLocationName}; на алкоголь в ЕГАИС уйдёт ТТН. Получатель примет товар документом поступления.`,
      confirmText: 'Отгрузить',
    })
    if (ok) post()
    return
  }
  if (d.kind === 'receipt') {
    const r = d.receipt
    const accepted = d.lines.reduce((n, l) => n + (l.qtyAccepted ?? 0), 0)
    const ok = await confirm({
      title: `Завершить приёмку ${d.ttnNumber ?? d.number}?`,
      text: r?.differences
        ? `Будет принято ${accepted} шт. Недостача ${r.shortage}, излишек ${r.surplus}, чужих марок ${r.foreign} — в ЕГАИС уйдёт акт расхождения.`
        : `Будет принято ${accepted} шт. — в ЕГАИС уйдёт акт подтверждения.`,
      confirmText: 'Завершить приёмку',
    })
    if (ok) post()
    return
  }
  const effect =
    d.kind === 'opening'
      ? 'Товар встанет на остаток по закупочным ценам, марки — на учёт в зоне.'
      : `Товар переместится: ${zonesText.value.toLowerCase()}. Перевод пива уйдёт в ЕГАИС.`
  const ok = await confirm({
    title: `Провести ${d.number}?`,
    text: `Строк: ${d.lineCount}, всего ${d.totalQty} шт. ${effect} Проведённый документ не меняется.`,
    confirmText: 'Провести',
  })
  if (ok) post()
}

// Заявку на перемещение согласует или отклоняет точка-отправитель (СО-4).
const rejectOpen = ref(false)
const rejectReason = ref('')
const { mutate: decide, isPending: deciding } = useMutation({
  mutationFn: async (approve: boolean) => {
    const path = { params: { path: { id: id.value } } }
    const { data, error } = approve
      ? await api.POST('/documents/{id}/approve', {
          ...path,
          body: { version: doc.value!.version },
        })
      : await api.POST('/documents/{id}/reject', {
          ...path,
          body: { version: doc.value!.version, reason: rejectReason.value.trim() },
        })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (data) => {
    rejectOpen.value = false
    queryClient.setQueryData(key.value, data)
    void queryClient.invalidateQueries({ queryKey: ['documents'] })
  },
  onError: (e) => (actionError.value = e.message),
})

const { mutate: cancel, isPending: cancelling } = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/documents/{id}/cancel', {
      params: { path: { id: id.value } },
      body: { version: doc.value!.version },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (data) => {
    queryClient.setQueryData(key.value, data)
    void queryClient.invalidateQueries({ queryKey: ['documents'] })
  },
  onError: (e) => (actionError.value = e.message),
})
async function askCancel() {
  actionError.value = undefined
  const ok = await confirm({
    title: 'Отменить черновик?',
    text: 'Сканы останутся в истории, но документ нельзя будет провести.',
    confirmText: 'Отменить черновик',
    danger: true,
  })
  if (ok) cancel()
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
</script>

<template>
  <v-container class="pk-document">
    <v-btn
      :prepend-icon="mdiArrowLeft"
      :to="{ name: 'documents' }"
      variant="text"
      class="mb-2 ml-n3"
    >
      Документы
    </v-btn>
    <v-alert v-if="loadError" type="error">{{ loadError.message }}</v-alert>

    <template v-if="doc">
      <div class="d-flex flex-wrap align-center ga-2 mb-1">
        <h1 class="text-headline-small">
          {{ kindText }} <span class="pk-code">{{ doc.number }}</span>
        </h1>
        <v-chip :color="documentStatusColors[doc.status]" label data-test="status">
          {{ documentStatusLabels[doc.status] }}
        </v-chip>
      </div>
      <p class="text-body-medium text-medium-emphasis mb-4">
        {{ doc.locationName }} · {{ zonesText }} · {{ formatDateTime(doc.createdAt) }} ·
        {{ doc.authorName }}
        <template v-if="doc.postedAt">
          · проведён {{ formatDateTime(doc.postedAt) }}, {{ doc.postedByName }}
        </template>
      </p>
      <v-alert
        v-if="doc.status === 'awaiting_egais'"
        type="warning"
        variant="tonal"
        class="mb-4"
        data-test="awaiting-egais"
      >
        Учёт уже изменён, документ ждёт квитанции ЕГАИС. Если УТМ точки недоступен, сообщение уйдёт
        само, когда связь появится.
      </v-alert>
      <template v-if="doc.kind === 'transfer'">
        <p class="text-body-medium mb-3" data-test="transfer-route">
          Из <b>{{ doc.locationName }}</b> в <b>{{ doc.destLocationName }}</b
          ><template v-if="doc.comment">. {{ doc.comment }}</template>
        </p>
        <v-alert
          v-if="doc.status === 'requested'"
          type="info"
          variant="tonal"
          class="mb-4"
          data-test="transfer-requested"
        >
          Заявка ждёт согласования точкой-отправителем. После согласования товар соберут
          сканированием и отгрузят.
          <template v-if="doc.canApprove" #append>
            <div class="d-flex ga-2">
              <v-btn variant="text" :loading="deciding" @click="rejectOpen = true">Отклонить</v-btn>
              <v-btn
                color="primary"
                variant="flat"
                :loading="deciding"
                data-test="approve"
                @click="decide(true)"
                >Согласовать</v-btn
              >
            </div>
          </template>
        </v-alert>
        <v-alert v-if="doc.status === 'in_transit'" type="warning" variant="tonal" class="mb-4">
          Товар в пути. Получатель примет его документом
          <router-link
            v-if="doc.receiptId"
            :to="{ name: 'document', params: { id: doc.receiptId } }"
            data-test="transfer-receipt"
            >{{ doc.receiptNumber }}</router-link
          >.
        </v-alert>
      </template>
      <v-alert
        v-if="doc.transferId"
        type="info"
        variant="tonal"
        class="mb-4"
        data-test="from-transfer"
      >
        Поступление по перемещению
        <router-link :to="{ name: 'document', params: { id: doc.transferId } }">{{
          doc.transferNumber
        }}</router-link
        >: отсканируйте пришедшие марки и товар. Непринятое останется «в пути» до акта списания.
      </v-alert>
      <p v-if="doc.kind === 'receipt'" class="text-body-medium mb-3" data-test="ttn">
        {{ doc.ttnNumber }} · {{ doc.supplierName ?? 'поставщик не найден в справочнике' }}
      </p>

      <template v-if="doc.canEdit">
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
        >
          Включить камеру
        </v-btn>

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
          v-if="doc.kind === 'opening' && selected"
          :prepend-icon="mdiBottleWineOutline"
          closable
          class="mb-3"
          data-test="selected"
          @click:close="selected = undefined"
        >
          Марки относятся к: {{ selected.name }}
        </v-chip>

        <v-alert
          v-if="scanError"
          type="error"
          class="mb-3"
          closable
          @click:close="scanError = undefined"
        >
          {{ scanError }}
        </v-alert>
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

      <ReceiptPanel
        v-if="doc.receipt"
        :doc="doc"
        @changed="(d) => queryClient.setQueryData(key, d)"
      />

      <h2 class="text-title-large mb-2">Строки · {{ totalQty }} шт.</h2>
      <p v-if="!doc.lines.length" class="text-body-medium text-medium-emphasis mb-4">
        Отсканируйте товар: штрихкод, марку ЕГАИС или код «Честного знака».
      </p>
      <v-table v-else density="comfortable" class="mb-6" data-test="lines">
        <thead>
          <tr>
            <th class="d-none d-sm-table-cell">Артикул</th>
            <th>Товар</th>
            <template v-if="doc.kind === 'receipt'">
              <th class="text-end text-no-wrap">План</th>
              <th class="text-end text-no-wrap">Факт</th>
              <th class="text-end text-no-wrap">Примем</th>
            </template>
            <template v-else-if="doc.kind === 'transfer'">
              <th class="text-end text-no-wrap">Заявлено</th>
              <th class="text-end text-no-wrap">Собрано</th>
            </template>
            <th v-else class="text-end text-no-wrap">Кол-во</th>
            <th v-if="showCost" class="text-end">Себестоимость</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="l in doc.lines" :key="l.variantId" data-test="line">
            <td class="pk-code text-no-wrap d-none d-sm-table-cell">{{ l.article }}</td>
            <td>
              <div>{{ l.name }}</div>
              <div class="pk-code text-body-small d-sm-none">{{ l.article }}</div>
              <div class="text-body-small text-medium-emphasis">
                {{ variantLabel(l) }} · {{ accountingLabels[l.egaisAccounting ?? 'none'] }}
              </div>
            </td>
            <template v-if="doc.kind === 'receipt'">
              <td class="text-end">{{ l.qtyPlan ?? 'нет в ТТН' }}</td>
              <td
                class="text-end"
                :class="{ 'text-warning font-weight-bold': l.qtyFact !== (l.qtyPlan ?? 0) }"
                data-test="fact"
              >
                {{ l.qtyFact ?? l.qty }}
              </td>
              <td class="text-end">{{ l.qtyAccepted ?? 0 }}</td>
            </template>
            <template v-else-if="doc.kind === 'transfer'">
              <td class="text-end">{{ l.qtyPlan ?? '—' }}</td>
              <td
                class="text-end"
                :class="{ 'text-warning font-weight-bold': l.qty !== (l.qtyPlan ?? 0) }"
              >
                {{ l.qty }}
              </td>
            </template>
            <td v-else class="text-end pk-qty">
              <input
                v-if="doc.canEdit && l.egaisAccounting !== 'marked'"
                :value="l.qty"
                type="number"
                min="0"
                class="pk-qty-input"
                :aria-label="`Количество: ${l.name}`"
                data-test="qty"
                @change="
                  (e) =>
                    onQty(l, (e.target as HTMLInputElement).value, e.target as HTMLInputElement)
                "
              />
              <span v-else>{{ l.qty }}</span>
            </td>
            <td v-if="showCost" class="text-end">
              {{ l.unitCost !== undefined ? formatPrice(l.unitCost) : '' }}
            </td>
          </tr>
        </tbody>
      </v-table>

      <h2 class="text-title-large mb-2">Сканы</h2>
      <p v-if="!doc.scans.length" class="text-body-medium text-medium-emphasis mb-4">
        Сканов пока нет.
      </p>
      <v-list v-else density="compact" class="mb-6" data-test="scans">
        <v-list-item
          v-for="s in doc.scans"
          :key="s.id"
          :class="{ 'pk-undone': s.undoneAt }"
          data-test="scan"
        >
          <template #prepend>
            <v-icon
              :icon="outcome[s.status].icon"
              :color="s.undoneAt ? undefined : outcome[s.status].color"
            />
          </template>
          <v-list-item-title>
            {{ s.name ?? short(s.raw) }}
            <span class="text-medium-emphasis">· {{ scanStatusLabels[s.status] }}</span>
          </v-list-item-title>
          <v-list-item-subtitle>
            {{ s.reason ?? short(s.raw) }}
            <template v-if="s.undoneAt"> · отменён</template>
          </v-list-item-subtitle>
          <template v-if="doc.canEdit && !s.undoneAt" #append>
            <v-btn
              :icon="mdiUndoVariant"
              variant="text"
              size="small"
              :aria-label="`Отменить скан ${s.name ?? s.raw}`"
              data-test="undo"
              @click="undo(s)"
            />
          </template>
        </v-list-item>
      </v-list>

      <template v-if="doc.exchange?.length">
        <h2 class="text-title-large mb-2">Обмен с ЕГАИС</h2>
        <ExchangeLog :messages="doc.exchange" />
      </template>

      <v-alert v-if="actionError" type="error" class="mb-3">{{ actionError }}</v-alert>
      <div v-if="doc.canEdit || doc.canPost" class="pk-actions">
        <v-btn
          v-if="doc.canEdit"
          :prepend-icon="mdiClose"
          variant="text"
          :loading="cancelling"
          data-test="cancel"
          @click="askCancel"
        >
          Отменить черновик
        </v-btn>
        <v-spacer />
        <v-btn
          v-if="doc.canPost"
          color="primary"
          variant="flat"
          :disabled="!doc.lines.length"
          :loading="posting"
          data-test="post"
          @click="askPost"
        >
          {{
            doc.kind === 'receipt'
              ? 'Завершить приёмку'
              : doc.kind === 'transfer'
                ? 'Отгрузить'
                : 'Провести'
          }}
        </v-btn>
      </div>
      <p v-else-if="doc.status === 'draft'" class="text-body-medium text-medium-emphasis">
        Черновик заполняют сотрудники точки<template v-if="doc.kind === 'opening'"
          >, проводит руководитель или бухгалтер</template
        >.
      </p>
    </template>

    <v-dialog v-model="rejectOpen" max-width="440">
      <v-card title="Отклонить заявку">
        <v-card-text>
          <v-text-field
            v-model="rejectReason"
            label="Причина"
            hint="Получатель увидит её в заявке"
            persistent-hint
            autofocus
          />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="rejectOpen = false">Отмена</v-btn>
          <v-btn
            color="error"
            variant="flat"
            :disabled="!rejectReason.trim()"
            :loading="deciding"
            @click="decide(false)"
            >Отклонить</v-btn
          >
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>

<style scoped>
.pk-document {
  max-width: 960px;
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
