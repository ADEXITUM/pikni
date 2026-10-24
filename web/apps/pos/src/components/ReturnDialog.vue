<script setup lang="ts">
import type { PosReceipt } from '@pikni/api/pos'
import { computed, ref, watch } from 'vue'

import { rubles } from '../cart'
import { findReceipts, sendReceipt, till } from '../till'

const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ returned: [receipt: PosReceipt]; refused: [message: string] }>()

const query = ref('')
const found = ref<PosReceipt[]>([])
const chosen = ref<PosReceipt | null>(null)
const picked = ref<number[]>([])
const error = ref('')
const busy = ref(false)

watch(open, () => {
  query.value = ''
  found.value = []
  chosen.value = null
  picked.value = []
  error.value = ''
})

// Чек ищут по номеру из чека покупателя или сканом марки с возвращаемой бутылки.
async function search() {
  const q = query.value.trim()
  if (!q) return
  busy.value = true
  error.value = ''
  try {
    found.value = await findReceipts(/^\d{1,6}$/.test(q) ? { number: Number(q) } : { mark: q })
    if (found.value.length === 0) error.value = 'Чек не найден'
    if (found.value.length === 1) choose(found.value[0]!)
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}

function choose(r: PosReceipt) {
  chosen.value = r
  picked.value = r.lines.map((_, i) => i)
}

const sum = computed(() =>
  (chosen.value?.lines ?? [])
    .filter((_, i) => picked.value.includes(i))
    .reduce((s, l) => s + Math.round(l.price * 100) * l.qty, 0),
)

async function submit() {
  const r = chosen.value
  if (!r || picked.value.length === 0) return
  busy.value = true
  try {
    const res = await sendReceipt({
      id: crypto.randomUUID(),
      kind: 'return',
      returnOf: r.id,
      payment: r.payment,
      ageConfirmed: false,
      createdOnDeviceAt: new Date().toISOString(),
      lines: r.lines
        .filter((_, i) => picked.value.includes(i))
        .map((l) => ({
          variantId: l.variantId,
          qty: l.qty,
          price: l.price,
          markCode: l.markCode,
          crptCode: l.crptCode,
        })),
    })
    if ('receipt' in res) {
      open.value = false
      emit('returned', res.receipt)
    } else if ('refusal' in res) {
      error.value = res.refusal.message
    } else {
      error.value = 'Нет связи — возврат можно оформить только на связи с сервером'
    }
  } catch (e) {
    emit('refused', (e as Error).message)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <v-dialog v-model="open" max-width="560">
    <v-card title="Возврат" :loading="busy">
      <v-card-text>
        <v-text-field
          v-model="query"
          label="Номер чека или скан марки"
          autofocus
          :disabled="!till.online"
          :hint="till.online ? '' : 'Нет связи с сервером'"
          persistent-hint
          data-test="return-query"
          @keyup.enter="search()"
        />
        <v-list v-if="!chosen && found.length > 1" density="compact">
          <v-list-item
            v-for="r in found"
            :key="r.id"
            :title="`Чек № ${r.number}, смена № ${r.shiftNumber}`"
            :subtitle="`${new Date(r.createdAt).toLocaleString('ru')} · ${rubles(Math.round(r.total * 100))}`"
            @click="choose(r)"
          />
        </v-list>
        <template v-if="chosen">
          <div class="text-body-medium mb-2">
            Чек № {{ chosen.number }} от {{ new Date(chosen.createdAt).toLocaleString('ru') }} —
            отметьте, что возвращают:
          </div>
          <v-checkbox
            v-for="(l, i) in chosen.lines"
            :key="i"
            v-model="picked"
            :value="i"
            density="compact"
            hide-details
            :label="`${l.name}, ${l.label} × ${l.qty} — ${rubles(Math.round(l.price * 100) * l.qty)}`"
          />
          <div class="mt-2 font-weight-bold">
            Вернуть покупателю {{ chosen.payment === 'card' ? 'на карту' : 'наличными' }}:
            {{ rubles(sum) }}
          </div>
        </template>
        <v-alert v-if="error" type="error" density="compact" class="mt-4">{{ error }}</v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="open = false">Отмена</v-btn>
        <v-btn
          color="primary"
          variant="flat"
          :disabled="!chosen || !picked.length"
          :loading="busy"
          @click="submit()"
        >
          Оформить возврат
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
