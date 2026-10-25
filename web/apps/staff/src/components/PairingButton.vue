<script setup lang="ts">
import { mdiCellphoneLink, mdiCellphoneLinkOff } from '@mdi/js'
import { qrSvg } from '@pikni/scanner'
import { ref, watch } from 'vue'

import { closePairing, openPairing, pcPairing } from '@/pairing'

const open = ref(false)
const qr = ref('')
const error = ref<string>()
const busy = ref(false)

watch(
  () => pcPairing.value?.code,
  async (code) => (qr.value = code ? await qrSvg(code) : ''),
  { immediate: true },
)
// Смартфон подключился — окно с QR-кодом больше не нужно.
watch(
  () => pcPairing.value?.status,
  (s) => s === 'active' && (open.value = false),
)

async function start() {
  open.value = true
  error.value = undefined
  if (pcPairing.value) return
  busy.value = true
  try {
    await openPairing()
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Нет связи с сервером'
  } finally {
    busy.value = false
  }
}

async function stop() {
  open.value = false
  await closePairing()
}
</script>

<template>
  <v-btn
    :icon="pcPairing?.status === 'active' ? mdiCellphoneLink : mdiCellphoneLinkOff"
    :color="pcPairing?.status === 'active' ? 'success' : undefined"
    :title="
      pcPairing?.status === 'active' ? 'Смартфон подключён как сканер' : 'Смартфон как сканер'
    "
    variant="text"
    data-test="pairing"
    @click="start"
  />
  <v-dialog v-model="open" max-width="420">
    <v-card title="Смартфон как сканер">
      <v-card-text>
        <template v-if="pcPairing?.status === 'active'">
          <p>Смартфон подключён: коды с него попадают в открытый на компьютере экран.</p>
        </template>
        <template v-else>
          <p class="mb-4">
            Откройте «Пикни» на смартфоне под своей учётной записью, перейдите в «Сканер» и наведите
            камеру на этот код.
          </p>
          <v-progress-circular v-if="busy" indeterminate class="d-block mx-auto" />
          <!-- eslint-disable-next-line vue/no-v-html -- SVG собран нашим кодом -->
          <div v-if="qr" class="pk-qr" data-test="pairing-qr" v-html="qr" />
        </template>
        <v-alert v-if="error" type="error" class="mt-4">{{ error }}</v-alert>
      </v-card-text>
      <v-card-actions>
        <v-btn v-if="pcPairing" variant="text" data-test="unpair" @click="stop">Отключить</v-btn>
        <v-spacer />
        <v-btn @click="open = false">Закрыть</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.pk-qr {
  display: flex;
  justify-content: center;
}
.pk-qr :deep(svg) {
  width: 220px;
  height: 220px;
  background: #fff;
  padding: 8px;
  border-radius: 8px;
}
</style>
