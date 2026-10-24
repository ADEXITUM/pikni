<script setup lang="ts">
import { ref } from 'vue'

import { logout, openShift, till } from '../till'

const error = ref('')
const busy = ref(false)

async function open() {
  busy.value = true
  error.value = ''
  try {
    await openShift()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <v-main class="d-flex align-center justify-center">
    <v-card width="420" class="ma-4" :title="till.session?.cashierName">
      <v-card-subtitle
        >{{ till.session?.deviceName }} · {{ till.session?.locationName }}</v-card-subtitle
      >
      <v-card-text>
        <p class="mt-2">Смена закрыта. Откройте смену, чтобы пробивать чеки.</p>
        <v-alert v-if="error" type="error" density="compact" class="mt-4">{{ error }}</v-alert>
      </v-card-text>
      <v-card-actions>
        <v-btn variant="text" @click="logout()">Выйти</v-btn>
        <v-spacer />
        <v-btn
          color="primary"
          variant="flat"
          :loading="busy"
          data-test="open-shift"
          @click="open()"
        >
          Открыть смену
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-main>
</template>
