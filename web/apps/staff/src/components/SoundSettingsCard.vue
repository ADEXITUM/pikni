<script setup lang="ts">
import type { SoundEvent } from '@pikni/ui'
import { computed } from 'vue'

import { playFeedback, soundSettings } from '@/feedback'

const soundOn = computed({
  get: () => !soundSettings.muted,
  set: (on) => (soundSettings.muted = !on),
})

// Четыре сигнала сканера: продавцу хватит минуты, чтобы их запомнить.
const samples: { event: SoundEvent; label: string }[] = [
  { event: 'accepted', label: 'Принято' },
  { event: 'notInPlan', label: 'Проверьте' },
  { event: 'unknown', label: 'Не найден' },
  { event: 'stop', label: 'Стоп' },
]
</script>

<template>
  <v-card title="Звук и вибрация" subtitle="Сигналы сканера на этом устройстве">
    <v-card-text>
      <v-switch v-model="soundOn" label="Звук" hide-details data-test="sound" />
      <v-slider
        v-model="soundSettings.volume"
        :disabled="!soundOn"
        :min="0.1"
        :max="1"
        :step="0.1"
        label="Громкость"
        hide-details
        class="mb-2"
      />
      <v-switch
        v-model="soundSettings.haptics"
        label="Вибрация"
        hint="На iPhone вибрации нет — остаются звук и цвет"
        persistent-hint
      />
      <div class="text-body-2 text-medium-emphasis mt-4 mb-2">Послушать сигналы</div>
      <div class="d-flex flex-wrap ga-2">
        <v-btn
          v-for="s in samples"
          :key="s.event"
          variant="tonal"
          size="small"
          @click="playFeedback(s.event)"
        >
          {{ s.label }}
        </v-btn>
      </div>
    </v-card-text>
  </v-card>
</template>
