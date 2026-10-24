<script setup lang="ts">
import { mdiArrowDownBold, mdiArrowUpBold } from '@mdi/js'
import type { ExchangeMessage } from '@pikni/api'

import { formatDateTime } from '@/format'

defineProps<{ messages: ExchangeMessage[] }>()

// Журнал обмена с ЕГАИС по документу (ИН-3): что ушло, что пришло и чем ответила ЕГАИС.
const docTypes: Record<string, string> = {
  WayBill: 'ТТН от поставщика',
  WayBillAct: 'Акт по ТТН',
  TransferToShop: 'Перевод в торговый зал',
  TransferFromShop: 'Перевод из зала на склад',
  Ticket: 'Квитанция',
  QueryRests: 'Запрос остатков',
  ReplyRests: 'Остатки ЕГАИС',
}
const statuses: Record<ExchangeMessage['status'], { text: string; color: string }> = {
  queued: { text: 'в очереди', color: 'warning' },
  sent: { text: 'отправлен, ждёт квитанции', color: 'info' },
  accepted: { text: 'принят ЕГАИС', color: 'success' },
  rejected: { text: 'отклонён ЕГАИС', color: 'error' },
  error: { text: 'ошибка', color: 'error' },
  processed: { text: 'получен', color: 'success' },
}
</script>

<template>
  <v-list density="compact" class="mb-6" data-test="exchange">
    <v-list-item v-for="m in messages" :key="m.id">
      <template #prepend>
        <v-icon
          :icon="m.direction === 'out' ? mdiArrowUpBold : mdiArrowDownBold"
          :aria-label="m.direction === 'out' ? 'отправлено' : 'получено'"
        />
      </template>
      <v-list-item-title>
        {{ docTypes[m.docType] ?? m.docType }}
        <span :class="`text-${statuses[m.status].color}`">· {{ statuses[m.status].text }}</span>
      </v-list-item-title>
      <v-list-item-subtitle>
        {{ formatDateTime(m.createdAt) }}
        <template v-if="m.reason"> · {{ m.reason }}</template>
        <template v-if="m.status === 'queued' && m.lastError">
          · {{ m.lastError }}, попыток {{ m.attempts
          }}<template v-if="m.nextAttemptAt"
            >, следующая {{ formatDateTime(m.nextAttemptAt) }}</template
          >
        </template>
      </v-list-item-subtitle>
    </v-list-item>
  </v-list>
</template>
