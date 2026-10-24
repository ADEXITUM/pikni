<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from 'vue'

import { login, setKey } from '../till'
import PinPad from './PinPad.vue'

const name = ref('')
const pin = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  if (!name.value.trim() || pin.value.length < 4 || busy.value) return
  busy.value = true
  error.value = ''
  try {
    await login(name.value.trim(), pin.value)
  } catch (e) {
    error.value = (e as Error).message
    pin.value = ''
  } finally {
    busy.value = false
  }
  // После ошибки цифры с клавиатуры — снова PIN, а не продолжение логина.
  await nextTick()
  if (document.activeElement instanceof HTMLInputElement) document.activeElement.blur()
}

// PIN можно набирать и с клавиатуры кассы.
function onKey(e: KeyboardEvent) {
  if (e.target instanceof HTMLInputElement) return
  if (/^\d$/.test(e.key) && pin.value.length < 6) pin.value += e.key
  else if (e.key === 'Backspace') pin.value = pin.value.slice(0, -1)
  else if (e.key === 'Enter') void submit()
}
onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))

function forget() {
  if (confirm('Отключить кассу от этого устройства? Понадобится новый ключ.')) setKey(null)
}
</script>

<template>
  <v-main class="d-flex align-center justify-center">
    <v-card width="360" class="ma-4" title="Вход кассира" :loading="busy">
      <v-card-text>
        <v-text-field
          v-model="name"
          label="Логин"
          autofocus
          autocomplete="username"
          data-test="login"
          @keyup.enter="($event.target as HTMLInputElement).blur()"
        />
        <PinPad v-model="pin" @submit="submit()" />
        <v-alert v-if="error" type="error" density="compact" class="mt-4" data-test="login-error">
          {{ error }}
        </v-alert>
      </v-card-text>
      <v-card-actions>
        <v-btn size="small" variant="text" @click="forget()">Отключить кассу</v-btn>
      </v-card-actions>
    </v-card>
  </v-main>
</template>
