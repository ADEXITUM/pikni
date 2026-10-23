<script setup lang="ts">
import { mdiContentCopy } from '@mdi/js'
import type { OpeningHours } from '@pikni/api'

import { weekdays, type Weekday } from '@/hours'

defineProps<{ disabled?: boolean }>()
const hours = defineModel<OpeningHours>({ required: true })

function toggle(day: Weekday, working: boolean | null) {
  hours.value = { ...hours.value, [day]: working ? { open: '10:00', close: '22:00' } : undefined }
}

function setTime(day: Weekday, edge: 'open' | 'close', value: string) {
  const current = hours.value[day]
  if (current) hours.value = { ...hours.value, [day]: { ...current, [edge]: value } }
}

function copyMonday() {
  const mon = hours.value.mon
  hours.value = Object.fromEntries(
    weekdays.map((d) => [d.key, mon ? { ...mon } : undefined]),
  ) as OpeningHours
}
</script>

<template>
  <div>
    <div v-for="day in weekdays" :key="day.key" class="pk-day" :data-test="`day-${day.key}`">
      <v-switch
        :model-value="hours[day.key] !== undefined"
        :label="day.title"
        :disabled="disabled"
        class="pk-day__switch"
        @update:model-value="toggle(day.key, $event)"
      />
      <template v-if="hours[day.key]">
        <v-text-field
          :model-value="hours[day.key]!.open"
          type="time"
          label="с"
          density="compact"
          :disabled="disabled"
          @update:model-value="setTime(day.key, 'open', $event)"
        />
        <v-text-field
          :model-value="hours[day.key]!.close"
          type="time"
          label="до"
          density="compact"
          :disabled="disabled"
          @update:model-value="setTime(day.key, 'close', $event)"
        />
      </template>
      <span v-else class="text-medium-emphasis pk-day__off">выходной</span>
    </div>
    <v-btn
      v-if="!disabled"
      variant="text"
      size="small"
      :prepend-icon="mdiContentCopy"
      class="mt-2"
      @click="copyMonday()"
    >
      Как в понедельник — на все дни
    </v-btn>
  </div>
</template>

<style scoped>
.pk-day {
  display: grid;
  grid-template-columns: 200px 130px 130px;
  gap: 12px;
  align-items: center;
}

.pk-day__off {
  grid-column: span 2;
}

@media (max-width: 599px) {
  .pk-day {
    grid-template-columns: 1fr 1fr;
  }

  .pk-day__switch {
    grid-column: span 2;
  }
}
</style>
