<script setup lang="ts">
import { mdiCheck, mdiContentCopy } from '@mdi/js'
import type { PosDeviceKey } from '@pikni/api'
import { qrSvg } from '@pikni/scanner'
import { computed, ref, watch } from 'vue'
import QrCode from '@/components/QrCode.vue'

const issued = defineModel<PosDeviceKey | null>({ required: true })
const copied = ref(false)
const qr = ref('')

// Ссылка открывает кассу уже настроенной. Ключ — после #, поэтому он не попадает в журналы
// веб-сервера.
const link = computed(() =>
  issued.value ? `${location.origin}/pos/#key=${encodeURIComponent(issued.value.key)}` : '',
)

watch(issued, async () => {
  copied.value = false
  qr.value = link.value ? await qrSvg(link.value) : ''
})

async function copy() {
  if (!issued.value) return
  await navigator.clipboard.writeText(issued.value.key)
  copied.value = true
}
</script>

<template>
  <v-dialog :model-value="issued !== null" max-width="480" persistent>
    <v-card v-if="issued" :title="`Ключ кассы «${issued.device.name}»`">
      <v-card-text>
        <p class="mb-4">
          Откройте кассовую программу и отсканируйте QR-код камерой или вставьте ключ вручную.
        </p>
        <!-- eslint-disable-next-line vue/no-v-html -- SVG собран нашим кодом из ключа -->
        <div class="d-flex justify-center mb-4">
          <QrCode :svg="qr" :size="220" data-test="device-qr" />
        </div>
        <div class="d-flex align-center ga-2 mb-4">
          <code class="pk-key flex-grow-1" data-test="device-key">{{ issued.key }}</code>
          <v-btn
            :icon="copied ? mdiCheck : mdiContentCopy"
            :title="copied ? 'Скопировано' : 'Скопировать'"
            variant="tonal"
            @click="copy()"
          />
        </div>
        <v-alert type="warning" density="compact">
          Ключ показывается один раз. Если он потеряется, отключите кассу и зарегистрируйте заново.
        </v-alert>
      </v-card-text>
      <v-card-actions>
        <v-btn :href="link" target="_blank" variant="text">Открыть кассу здесь</v-btn>
        <v-spacer />
        <v-btn color="primary" @click="issued = null">Готово</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.pk-key {
  font-size: 0.85rem;
  word-break: break-all;
  padding: 8px 12px;
  border-radius: 8px;
  background: rgb(var(--v-theme-surface-light));
}
</style>
