<script setup lang="ts">
import type { PosShift } from '@pikni/api/pos'

import { rubles } from '../cart'

const report = defineModel<PosShift | null>({ required: true })

const kop = (rub: number) => Math.round(rub * 100)
</script>

<template>
  <v-dialog :model-value="report !== null" max-width="420" persistent>
    <v-card v-if="report" :title="`Смена № ${report.number} закрыта`" data-test="shift-report">
      <v-card-text>
        <v-table density="compact">
          <tbody>
            <tr>
              <td>Открыта</td>
              <td class="text-end">{{ new Date(report.openedAt).toLocaleString('ru') }}</td>
            </tr>
            <tr v-if="report.closedAt">
              <td>Закрыта</td>
              <td class="text-end">{{ new Date(report.closedAt).toLocaleString('ru') }}</td>
            </tr>
            <tr>
              <td>Чеков продажи</td>
              <td class="text-end">{{ report.receipts }}</td>
            </tr>
            <tr>
              <td>Чеков возврата</td>
              <td class="text-end">{{ report.returns }}</td>
            </tr>
            <tr>
              <td>Наличные</td>
              <td class="text-end">{{ rubles(kop(report.cash)) }}</td>
            </tr>
            <tr>
              <td>Карта</td>
              <td class="text-end">{{ rubles(kop(report.card)) }}</td>
            </tr>
            <tr class="font-weight-bold">
              <td>Выручка</td>
              <td class="text-end">{{ rubles(kop(report.cash) + kop(report.card)) }}</td>
            </tr>
          </tbody>
        </v-table>
        <v-alert v-if="report.egaisAct" type="info" density="compact" class="mt-4">
          Проданное пиво и другая продукция без марок отправлены в ЕГАИС актом списания из торгового
          зала.
        </v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn color="primary" variant="flat" @click="report = null">Готово</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
