<script setup lang="ts">
import { computed } from 'vue'

import { confirmState, settleConfirm } from '@/confirm'

const open = computed({
  get: () => confirmState.open,
  set: (v) => {
    if (!v) settleConfirm(false)
  },
})
const request = computed(() => confirmState.request)
</script>

<template>
  <v-dialog v-model="open" max-width="560">
    <v-card v-if="request" :title="request.title" data-test="confirm-dialog">
      <v-card-text>
        <p v-if="request.text" class="mb-3">{{ request.text }}</p>
        <v-table v-if="request.changes?.length" density="compact" data-test="confirm-changes">
          <thead>
            <tr>
              <th>Поле</th>
              <th>Было</th>
              <th>Станет</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="c in request.changes" :key="c.label">
              <td>{{ c.label }}</td>
              <td class="text-medium-emphasis">{{ c.old }}</td>
              <td class="font-weight-medium">{{ c.new }}</td>
            </tr>
          </tbody>
        </v-table>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" data-test="confirm-cancel" @click="settleConfirm(false)"
          >Отмена</v-btn
        >
        <v-btn
          :color="request.danger ? 'error' : 'primary'"
          variant="flat"
          data-test="confirm-ok"
          @click="settleConfirm(true)"
        >
          {{ request.confirmText ?? (request.danger ? 'Удалить' : 'Подтвердить') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
