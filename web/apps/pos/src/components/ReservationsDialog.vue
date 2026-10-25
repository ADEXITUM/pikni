<script setup lang="ts">
import type { PosReservation } from '@pikni/api/pos'
import { ref, watch } from 'vue'

import { rubles } from '../cart'
import { reservations } from '../till'

// Выдача брони (НЛ-4): отложенный товар пробивается обычным чеком со ссылкой на бронь.
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ pick: [reservation: PosReservation] }>()
const items = ref<PosReservation[]>([])
const error = ref('')
const busy = ref(false)

watch(open, async (o) => {
  if (!o) return
  busy.value = true
  error.value = ''
  try {
    items.value = await reservations()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
})
</script>

<template>
  <v-dialog v-model="open" max-width="560">
    <v-card title="Брони к выдаче" :loading="busy">
      <v-card-text>
        <p class="text-body-medium mb-2">
          Сверьте имя и последние цифры телефона покупателя. Возраст проверяется как при обычной
          продаже.
        </p>
        <v-list>
          <v-list-item
            v-for="r in items"
            :key="r.id"
            :title="`${r.number} · ${r.customerName}, тел. …${r.phoneTail}`"
            :subtitle="`${r.item.name}, ${r.item.label} × ${r.qty} · ${rubles(Math.round(r.item.price * 100) * r.qty)}`"
            data-test="pos-reservation"
            @click="(emit('pick', r), (open = false))"
          />
          <v-list-item v-if="!busy && !items.length" title="Подтверждённых броней нет" />
        </v-list>
        <v-alert v-if="error" type="error" density="compact">{{ error }}</v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="open = false">Закрыть</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
