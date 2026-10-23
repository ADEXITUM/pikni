<script setup lang="ts">
import { mdiCheck, mdiContentCopy } from '@mdi/js'
import { ref, watch } from 'vue'

const issued = defineModel<{ login: string; password: string } | null>({ required: true })
const copied = ref(false)

watch(issued, () => (copied.value = false))

async function copy() {
  if (!issued.value) return
  await navigator.clipboard.writeText(issued.value.password)
  copied.value = true
}
</script>

<template>
  <v-dialog :model-value="issued !== null" max-width="440" persistent>
    <v-card v-if="issued" title="Временный пароль">
      <v-card-text>
        <p class="mb-4">
          Передайте пароль сотруднику <b>{{ issued.login }}</b
          >. При первом входе он придумает свой.
        </p>
        <div class="d-flex align-center ga-2 mb-4">
          <code class="pk-temp-password flex-grow-1" data-test="temporary-password">
            {{ issued.password }}
          </code>
          <v-btn
            :icon="copied ? mdiCheck : mdiContentCopy"
            :title="copied ? 'Скопировано' : 'Скопировать'"
            variant="tonal"
            @click="copy()"
          />
        </div>
        <v-alert type="warning" density="compact">
          Пароль показывается один раз. Если он потеряется, сбросьте пароль ещё раз.
        </v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn color="primary" @click="issued = null">Готово</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.pk-temp-password {
  font-size: 1.4rem;
  letter-spacing: 0.08em;
  padding: 8px 12px;
  border-radius: 8px;
  background: rgb(var(--v-theme-surface-light));
}
</style>
