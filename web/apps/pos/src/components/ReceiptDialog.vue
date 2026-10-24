<script setup lang="ts">
import type { PosReceipt } from '@pikni/api/pos'
import { qrSvg } from '@pikni/scanner'
import { ref, watch } from 'vue'

import { rubles } from '../cart'
import { till } from '../till'

const printed = defineModel<{ receipt: PosReceipt | null; offline: boolean; total: number } | null>(
  {
    required: true,
  },
)
const qr = ref('')

watch(printed, async (p) => {
  qr.value = p?.receipt?.egaisQr ? await qrSvg(p.receipt.egaisQr) : ''
})

const time = (iso: string) => new Date(iso).toLocaleString('ru')
const print = () => window.print()
</script>

<template>
  <v-dialog :model-value="printed !== null" max-width="400" @update:model-value="printed = null">
    <v-card v-if="printed">
      <v-card-text class="pk-receipt" data-test="receipt">
        <div class="text-center font-weight-bold">{{ till.session?.locationName }}</div>
        <div class="text-center text-body-small mb-2">
          Учебный чек · не является фискальным документом
        </div>
        <template v-if="printed.receipt">
          <div class="text-body-small">
            {{ printed.receipt.kind === 'return' ? 'Возврат' : 'Продажа' }} №
            {{ printed.receipt.number }} · смена № {{ printed.receipt.shiftNumber }} ·
            {{ time(printed.receipt.createdAt) }}
          </div>
          <div class="text-body-small mb-2">Кассир: {{ till.session?.cashierName }}</div>
          <div
            v-for="(l, i) in printed.receipt.lines"
            :key="i"
            class="d-flex justify-space-between text-body-medium"
          >
            <span>{{ l.name }}, {{ l.label }} × {{ l.qty }}</span>
            <span class="text-no-wrap ml-2">{{ rubles(Math.round(l.price * 100) * l.qty) }}</span>
          </div>
        </template>
        <v-alert v-else type="warning" density="compact" class="my-2">
          Нет связи: чек сохранён на кассе и отправится на сервер автоматически.
        </v-alert>
        <v-divider class="my-2" />
        <div class="d-flex justify-space-between font-weight-bold text-title-medium">
          <span>Итого</span>
          <span>{{ rubles(printed.total) }}</span>
        </div>
        <div v-if="printed.receipt" class="text-body-small">
          Оплата: {{ printed.receipt.payment === 'card' ? 'картой' : 'наличными' }}
        </div>
        <template v-if="qr">
          <!-- eslint-disable-next-line vue/no-v-html -- SVG собран нашим кодом из ссылки ЕГАИС -->
          <div class="pk-qr mt-3" data-test="egais-qr" v-html="qr" />
          <div class="text-center text-body-small">Проверка чека в ЕГАИС</div>
        </template>
      </v-card-text>
      <v-card-actions>
        <v-btn variant="text" @click="print()">Печать</v-btn>
        <v-spacer />
        <v-btn
          color="primary"
          variant="flat"
          autofocus
          data-test="receipt-done"
          @click="printed = null"
        >
          Следующий покупатель
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.pk-receipt {
  font-family: 'JetBrains Mono', monospace;
}
.pk-qr {
  display: flex;
  justify-content: center;
}
.pk-qr :deep(svg) {
  width: 140px;
  height: 140px;
  background: #fff;
}
</style>
