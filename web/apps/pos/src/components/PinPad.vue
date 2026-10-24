<script setup lang="ts">
import { mdiBackspaceOutline } from '@mdi/js'

const pin = defineModel<string>({ required: true })
const emit = defineEmits<{ submit: [] }>()

const keys = ['1', '2', '3', '4', '5', '6', '7', '8', '9']

function press(d: string) {
  if (pin.value.length < 6) pin.value += d
}
</script>

<template>
  <div class="pk-pad">
    <div class="pk-dots mb-4" data-test="pin-dots">
      <span v-for="i in 6" :key="i" :class="{ filled: i <= pin.length }" />
    </div>
    <div class="pk-keys">
      <v-btn v-for="d in keys" :key="d" size="x-large" variant="tonal" @click="press(d)">{{
        d
      }}</v-btn>
      <v-btn
        size="x-large"
        variant="text"
        :icon="mdiBackspaceOutline"
        title="Стереть"
        @click="pin = pin.slice(0, -1)"
      />
      <v-btn size="x-large" variant="tonal" @click="press('0')">0</v-btn>
      <v-btn size="x-large" color="primary" :disabled="pin.length < 4" @click="emit('submit')"
        >OK</v-btn
      >
    </div>
  </div>
</template>

<style scoped>
.pk-dots {
  display: flex;
  justify-content: center;
  gap: 12px;
}
.pk-dots span {
  width: 14px;
  height: 14px;
  border-radius: 50%;
  border: 2px solid rgb(var(--v-theme-on-surface));
  opacity: 0.5;
}
.pk-dots span.filled {
  background: rgb(var(--v-theme-primary));
  border-color: rgb(var(--v-theme-primary));
  opacity: 1;
}
.pk-keys {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}
</style>
