<script setup lang="ts">
import {
  mdiAccountCircleOutline,
  mdiBookmarkOutline,
  mdiCameraOffOutline,
  mdiCameraOutline,
  mdiCash,
  mdiCreditCardOutline,
  mdiDeleteOutline,
  mdiKeyboardReturn,
  mdiLanDisconnect,
  mdiStoreClockOutline,
} from '@mdi/js'
import type { PosReceipt, PosReservation } from '@pikni/api/pos'
import { createDeduper, listenWedge } from '@pikni/scanner'
import { createSounds } from '@pikni/ui'
import ScanCamera from '@pikni/ui/ScanCamera.vue'
import { computed, onMounted, onUnmounted, ref, useTemplateRef } from 'vue'

import {
  addLine,
  canSellOffline,
  isDuplicateCode,
  linePrice,
  needsAge,
  removeLine,
  rubles,
  setQty,
  toReceipt,
  totalKopecks,
  type Line,
  type Payment,
} from '../cart'
import { catalogIndex, findOffline } from '../catalog'
import { local } from '../storage'
import { check, closeShift, enqueue, logout, sendReceipt, till, TillError } from '../till'
import ReceiptDialog from './ReceiptDialog.vue'
import ReservationsDialog from './ReservationsDialog.vue'
import ReturnDialog from './ReturnDialog.vue'

const sounds = createSounds({ volume: 0.8, muted: false, haptics: false })

const lines = ref<Line[]>([])
const selected = ref(-1)
// Идентификатор чека живёт, пока чек не принят: повтор после обрыва связи не проведёт продажу дважды.
const receiptId = ref(crypto.randomUUID())
const code = ref('')
const codeField = useTemplateRef<HTMLInputElement>('codeField')
const busy = ref(false)
const refusal = ref('')
const total = computed(() => totalKopecks(lines.value))
const index = computed(() => catalogIndex(till.catalog))

const ageOpen = ref(false)
let afterAge: (() => void) | null = null
const qtyOpen = ref(false)
const qtyValue = ref('1')
const printed = ref<{ receipt: PosReceipt | null; offline: boolean; total: number } | null>(null)
const returnOpen = ref(false)
// Выдача брони: чек ссылается на бронь, сервер снимает резерв в той же транзакции.
const reservationsOpen = ref(false)
const reservation = ref<PosReservation | null>(null)
function loadReservation(r: PosReservation) {
  if (lines.value.length && !confirm('Заменить текущий чек выдачей брони?')) return
  resetReceipt()
  reservation.value = r
  lines.value = r.marks.length
    ? r.marks.map((m) => ({ item: r.item, qty: 1, markCode: m }))
    : [{ item: r.item, qty: r.qty }]
  selected.value = 0
}

// Камера — для планшета и телефона; включённая остаётся включённой и после перезагрузки.
const cameraOn = ref(local.get('pikni.pos.camera') === '1')
const flash = ref<{ color: string; key: string }>()
const fresh = createDeduper(2000)

function setCamera(on: boolean) {
  cameraOn.value = on
  local.set('pikni.pos.camera', on ? '1' : null)
}

function onCamera(raw: string) {
  if (fresh(raw)) void scan(raw)
}

function refuse(message: string) {
  refusal.value = message
  flash.value = { color: 'error', key: crypto.randomUUID() }
  sounds.play('stop')
}

function add(line: Line) {
  if (isDuplicateCode(lines.value, line.markCode, line.crptCode)) {
    refuse('Этот код уже в чеке — каждую бутылку и пачку сканируют один раз')
    return
  }
  lines.value = addLine(lines.value, line)
  selected.value = lines.value.findIndex(
    (l) =>
      l.item.variantId === line.item.variantId &&
      l.markCode === line.markCode &&
      l.crptCode === line.crptCode,
  )
  refusal.value = ''
  flash.value = { color: 'success', key: crypto.randomUUID() }
  sounds.play('accepted')
}

async function scan(raw: string) {
  raw = raw.trim()
  code.value = ''
  if (!raw || busy.value) return
  if (till.online) {
    busy.value = true
    try {
      const c = await check(raw)
      if (c.refusals.length > 0 || !c.item) {
        refuse(c.refusals[0]?.message ?? 'Товар не найден')
        return
      }
      add({
        item: c.item,
        qty: 1,
        markCode: c.markCode,
        crptCode: c.crptCode,
        price: c.price,
        maxPrice: c.maxPrice,
      })
      return
    } catch (e) {
      if (!(e instanceof TillError) || till.online) {
        refuse((e as Error).message)
        return
      }
    } finally {
      busy.value = false
    }
  }
  const found = findOffline(index.value, raw)
  if ('refusal' in found) refuse(found.refusal)
  else add({ item: found.item, qty: 1 })
}

function resetReceipt() {
  reservation.value = null
  lines.value = []
  selected.value = -1
  receiptId.value = crypto.randomUUID()
}

function pay(payment: Payment) {
  if (lines.value.length === 0 || busy.value) return
  if (needsAge(lines.value)) {
    afterAge = () => void submit(payment, true)
    ageOpen.value = true
    return
  }
  void submit(payment, false)
}

async function submit(payment: Payment, ageConfirmed: boolean) {
  busy.value = true
  refusal.value = ''
  const sum = total.value
  const receipt = toReceipt(lines.value, {
    id: receiptId.value,
    payment,
    ageConfirmed,
    now: new Date(),
    reservationId: reservation.value?.id,
  })
  try {
    const res = await sendReceipt(receipt)
    if ('receipt' in res) {
      printed.value = { receipt: res.receipt, offline: false, total: sum }
      resetReceipt()
    } else if ('refusal' in res) {
      refuse(res.refusal.message)
    } else if (canSellOffline(lines.value) && !reservation.value) {
      enqueue(receipt)
      printed.value = { receipt: null, offline: true, total: sum }
      resetReceipt()
    } else {
      refuse('Нет связи — чек с маркированным товаром пробить нельзя. Уберите его из чека')
    }
  } catch (e) {
    refuse((e as Error).message)
  } finally {
    busy.value = false
  }
}

function confirmAge(ok: boolean) {
  ageOpen.value = false
  if (ok) afterAge?.()
  else refuse('Без документа, подтверждающего 18 лет, алкоголь и табак не продаются')
  afterAge = null
}

function openQty() {
  const l = lines.value[selected.value]
  if (!l) return
  if (l.markCode || l.crptCode) {
    refuse('Товар с маркой пробивается поштучно — отсканируйте следующую марку')
    return
  }
  qtyValue.value = String(l.qty)
  qtyOpen.value = true
}

function applyQty() {
  lines.value = setQty(lines.value, selected.value, Number(qtyValue.value))
  qtyOpen.value = false
}

function remove() {
  if (selected.value < 0) return
  lines.value = removeLine(lines.value, selected.value)
  selected.value = Math.min(selected.value, lines.value.length - 1)
}

function cancel() {
  if (lines.value.length && confirm('Отменить чек?')) resetReceipt()
}

async function close() {
  if (lines.value.length) {
    refuse('Сначала пробейте или отмените текущий чек')
    return
  }
  if (
    till.queued > 0 &&
    !confirm(`${till.queued} чек(ов) ещё не отправлены. Всё равно закрыть смену?`)
  )
    return
  if (!confirm('Закрыть смену?')) return
  try {
    await closeShift()
  } catch (e) {
    refuse((e as Error).message)
  }
}

// Горячие клавиши (КС-7): F2 — код вручную, F4 — количество, F8 — удалить строку,
// F9 — оплата картой, F10 — наличными, Esc — отменить чек.
function onKey(e: KeyboardEvent) {
  if (ageOpen.value || qtyOpen.value || printed.value || returnOpen.value) return
  const actions: Record<string, () => void> = {
    F2: () => codeField.value?.focus(),
    F4: openQty,
    F8: remove,
    F9: () => pay('card'),
    F10: () => pay('cash'),
    Escape: cancel,
  }
  const action = actions[e.key]
  if (action) {
    e.preventDefault()
    action()
  }
}

let stopWedge = () => {}
onMounted(() => {
  window.addEventListener('keydown', onKey)
  stopWedge = listenWedge({ onScan: (raw) => void scan(raw) })
})
onUnmounted(() => {
  window.removeEventListener('keydown', onKey)
  stopWedge()
})

const alcohol = computed(() => {
  const w = till.time?.strong
  if (!w) return null
  if (w.ban) return { text: `Алкоголь не продаётся: ${w.ban}`, color: 'error' }
  return w.allowed
    ? { text: `Алкоголь до ${w.to}`, color: 'success' }
    : { text: `Алкоголь с ${w.from} до ${w.to}`, color: 'warning' }
})
</script>

<template>
  <v-app-bar density="compact" flat border>
    <v-app-bar-title class="text-body-large">
      {{ till.session?.deviceName }} · {{ till.session?.locationName }}
      <span class="text-medium-emphasis">· смена № {{ till.shift?.number }}</span>
    </v-app-bar-title>
    <v-chip
      v-if="alcohol"
      :color="alcohol.color"
      :prepend-icon="mdiStoreClockOutline"
      size="small"
      class="mr-2"
    >
      {{ alcohol.text }}
    </v-chip>
    <v-chip
      v-if="!till.online"
      color="error"
      :prepend-icon="mdiLanDisconnect"
      size="small"
      class="mr-2"
    >
      Нет связи
    </v-chip>
    <v-chip v-if="till.queued" color="warning" size="small" class="mr-2" data-test="queued">
      Не отправлено: {{ till.queued }}
    </v-chip>
    <v-chip v-if="till.rejected" color="error" size="small" class="mr-2"
      >Отклонено: {{ till.rejected }}</v-chip
    >
    <v-menu>
      <template #activator="{ props }">
        <v-btn v-bind="props" :prepend-icon="mdiAccountCircleOutline" variant="text">
          {{ till.session?.cashierName }}
        </v-btn>
      </template>
      <v-list density="compact">
        <v-list-item title="Возврат" @click="returnOpen = true" />
        <v-list-item title="Закрыть смену" data-test="close-shift" @click="close()" />
        <v-list-item title="Выйти" @click="logout()" />
      </v-list>
    </v-menu>
  </v-app-bar>

  <v-main>
    <div class="pk-sale">
      <section class="pk-lines">
        <v-text-field
          ref="codeField"
          v-model="code"
          placeholder="Сканируйте товар или введите штрихкод (F2)"
          density="comfortable"
          hide-details
          :loading="busy"
          class="mb-2"
          data-test="code"
          @keyup.enter="scan(code)"
        >
          <template #append-inner>
            <v-btn
              :icon="cameraOn ? mdiCameraOffOutline : mdiCameraOutline"
              :title="cameraOn ? 'Выключить камеру' : 'Сканировать камерой'"
              variant="text"
              size="small"
              data-test="camera-toggle"
              @click="setCamera(!cameraOn)"
            />
          </template>
        </v-text-field>
        <ScanCamera
          v-if="cameraOn"
          class="mb-2"
          :flash="flash"
          @scan="(c) => onCamera(c.raw)"
          @close="setCamera(false)"
        />
        <v-chip
          v-if="reservation"
          color="primary"
          class="mb-2"
          closable
          data-test="reservation-chip"
          @click:close="resetReceipt()"
        >
          Выдача брони {{ reservation.number }} · {{ reservation.customerName }}
        </v-chip>
        <v-alert
          v-if="refusal"
          type="error"
          variant="flat"
          class="mb-2"
          closable
          data-test="refusal"
          @click:close="refusal = ''"
        >
          {{ refusal }}
        </v-alert>
        <v-table density="comfortable" class="pk-table" hover>
          <thead>
            <tr>
              <th>Товар</th>
              <th class="text-end">Цена</th>
              <th class="text-end">Кол-во</th>
              <th class="text-end">Сумма</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(l, i) in lines"
              :key="`${l.item.variantId}-${l.markCode ?? l.crptCode ?? ''}`"
              :class="{ 'pk-selected': i === selected }"
              data-test="line"
              @click="selected = i"
            >
              <td>
                <div>{{ l.item.name }}</div>
                <div class="text-body-small text-medium-emphasis">
                  {{ l.item.label }}
                  <span v-if="l.markCode"> · марка …{{ l.markCode.slice(-6) }}</span>
                  <span v-if="l.crptCode"> · код ЧЗ</span>
                  <span v-if="l.maxPrice">
                    · МРЦ на пачке {{ rubles(Math.round(l.maxPrice * 100)) }}</span
                  >
                </div>
              </td>
              <td class="text-end text-no-wrap">{{ rubles(Math.round(linePrice(l) * 100)) }}</td>
              <td class="text-end">{{ l.qty }}</td>
              <td class="text-end text-no-wrap">
                {{ rubles(Math.round(linePrice(l) * 100) * l.qty) }}
              </td>
            </tr>
            <tr v-if="lines.length === 0">
              <td colspan="4" class="text-center text-medium-emphasis py-8">
                Чек пуст. Отсканируйте первый товар.
              </td>
            </tr>
          </tbody>
        </v-table>
      </section>

      <aside class="pk-side">
        <div class="text-body-medium text-medium-emphasis">Итого</div>
        <div class="pk-total" data-test="total">{{ rubles(total) }}</div>
        <v-btn
          block
          size="x-large"
          color="primary"
          :prepend-icon="mdiCash"
          :disabled="!lines.length"
          :loading="busy"
          data-test="pay-cash"
          @click="pay('cash')"
        >
          Наличные · F10
        </v-btn>
        <v-btn
          block
          size="x-large"
          variant="tonal"
          :prepend-icon="mdiCreditCardOutline"
          :disabled="!lines.length || busy"
          data-test="pay-card"
          @click="pay('card')"
        >
          Карта · F9
        </v-btn>
        <div class="d-flex ga-2">
          <v-btn class="flex-grow-1" variant="text" :disabled="selected < 0" @click="openQty()"
            >Кол-во · F4</v-btn
          >
          <v-btn
            class="flex-grow-1"
            variant="text"
            :prepend-icon="mdiDeleteOutline"
            :disabled="selected < 0"
            @click="remove()"
          >
            F8
          </v-btn>
        </div>
        <v-btn variant="text" :disabled="!lines.length" @click="cancel()">Отменить чек · Esc</v-btn>
        <v-spacer />
        <v-btn
          variant="outlined"
          :prepend-icon="mdiBookmarkOutline"
          data-test="pos-reservations"
          @click="reservationsOpen = true"
          >Брони</v-btn
        >
        <v-btn variant="outlined" :prepend-icon="mdiKeyboardReturn" @click="returnOpen = true"
          >Возврат</v-btn
        >
        <div v-if="till.shift" class="text-body-small text-medium-emphasis mt-2">
          Чеков: {{ till.shift.receipts }} · наличные
          {{ rubles(Math.round(till.shift.cash * 100)) }} · карта
          {{ rubles(Math.round(till.shift.card * 100)) }}
        </div>
      </aside>
    </div>
  </v-main>

  <v-dialog v-model="ageOpen" max-width="440" persistent>
    <v-card title="Проверьте возраст покупателя">
      <v-card-text>
        В чеке алкоголь или табак. Попросите паспорт или другой документ с фотографией: покупателю
        должно быть 18 лет.
      </v-card-text>
      <v-card-actions>
        <v-btn color="error" variant="text" @click="confirmAge(false)"
          >Нет документа или меньше 18</v-btn
        >
        <v-spacer />
        <v-btn color="primary" variant="flat" data-test="age-ok" @click="confirmAge(true)"
          >Есть 18 лет</v-btn
        >
      </v-card-actions>
    </v-card>
  </v-dialog>

  <v-dialog v-model="qtyOpen" max-width="320">
    <v-card title="Количество">
      <v-card-text>
        <v-text-field
          v-model="qtyValue"
          type="number"
          min="1"
          autofocus
          @keyup.enter="applyQty()"
        />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn color="primary" @click="applyQty()">Готово</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <ReceiptDialog v-model="printed" />
  <ReservationsDialog v-model="reservationsOpen" @pick="loadReservation" />
  <ReturnDialog
    v-model="returnOpen"
    @refused="refuse"
    @returned="(r) => (printed = { receipt: r, offline: false, total: r.total * 100 })"
  />
</template>

<style scoped>
.pk-sale {
  display: grid;
  grid-template-columns: 1fr 320px;
  gap: 16px;
  padding: 16px;
  height: 100%;
}
.pk-side {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.pk-total {
  font-size: 2.6rem;
  font-weight: 700;
  line-height: 1.1;
  margin-bottom: 8px;
}
.pk-selected {
  background: rgba(var(--v-theme-primary), 0.12);
}
@media (max-width: 760px) {
  .pk-sale {
    grid-template-columns: 1fr;
  }
}
</style>
