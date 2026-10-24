<script setup lang="ts">
import { mdiPackageVariant, mdiPackageVariantClosed, mdiPackageVariantClosedCheck } from '@mdi/js'
import { api, ApiError, type Document, type ReceiptPackage, type ReceivingMode } from '@pikni/api'
import { useMutation } from '@tanstack/vue-query'
import { computed } from 'vue'

import { receivingModeLabels } from '@/labels'

const props = defineProps<{ doc: Document }>()
const emit = defineEmits<{ changed: [doc: Document] }>()

const info = computed(() => props.doc.receipt!)
const marked = computed(() => props.doc.lines.some((l) => l.egaisAccounting === 'marked'))
const byBox = computed(() => info.value.mode === 'boxes' || info.value.mode === 'sample')

// Режимы приёмки (СО-2). Продукцию с марками сервер не даст принять только по документу или по коробам.
const modes: { value: ReceivingMode; hint: string }[] = [
  { value: 'document', hint: 'факт равен ТТН, сканировать не нужно' },
  { value: 'boxes', hint: 'сканируется этикетка каждого короба' },
  { value: 'sample', hint: 'короба по этикетке, каждый пятый — вскрыть и поштучно' },
  { value: 'piece', hint: 'сканируется каждая бутылка и марка' },
]

const {
  mutate: setMode,
  isPending,
  error,
} = useMutation({
  mutationFn: async (mode: ReceivingMode) => {
    const { data, error } = await api.PUT('/documents/{id}/mode', {
      params: { path: { id: props.doc.id } },
      body: { mode },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (doc) => emit('changed', doc),
})

// При выборочной приёмке расхождение во вскрытом коробе или чужая марка — повод сосчитать всё поштучно.
const sampleProblem = computed(
  () =>
    info.value.mode === 'sample' &&
    (info.value.foreign > 0 ||
      info.value.packages.some((p) => p.toOpen && p.scanned && p.counted < p.qty)),
)

const boxState = (p: ReceiptPackage) => {
  if (p.toOpen && info.value.mode === 'sample') {
    return p.scanned
      ? {
          icon: mdiPackageVariant,
          text: `вскрыт: ${p.counted} из ${p.qty}`,
          color: p.counted < p.qty ? 'warning' : 'success',
        }
      : { icon: mdiPackageVariantClosed, text: 'вскрыть и сосчитать поштучно', color: 'info' }
  }
  return p.scanned
    ? { icon: mdiPackageVariantClosedCheck, text: `засчитан, ${p.qty} шт.`, color: 'success' }
    : { icon: mdiPackageVariantClosed, text: `не отсканирован, ${p.qty} шт.`, color: undefined }
}
const shortCode = (code: string) => `…${code.slice(-6)}`
</script>

<template>
  <div class="pk-receipt">
    <div v-if="doc.canEdit" class="mb-3" data-test="modes">
      <div class="text-title-small mb-1">Режим приёмки</div>
      <v-btn-toggle
        :model-value="info.mode"
        mandatory
        divided
        variant="outlined"
        color="primary"
        density="comfortable"
        class="pk-modes"
        :disabled="isPending"
        @update:model-value="(m: ReceivingMode) => m !== info.mode && setMode(m)"
      >
        <v-btn
          v-for="m in modes"
          :key="m.value"
          :value="m.value"
          :disabled="marked && (m.value === 'document' || m.value === 'boxes')"
          :data-test="`mode-${m.value}`"
        >
          {{ receivingModeLabels[m.value] }}
        </v-btn>
      </v-btn-toggle>
      <div class="text-body-small text-medium-emphasis mt-1">
        {{ modes.find((m) => m.value === info.mode)?.hint }}
        <template v-if="marked">· продукцию с марками принимают выборочно или поштучно</template>
      </div>
      <v-alert v-if="error" type="error" class="mt-2">{{ error.message }}</v-alert>
    </div>
    <p v-else class="text-body-medium mb-3">
      Режим приёмки: {{ receivingModeLabels[info.mode].toLowerCase() }}
    </p>

    <v-alert
      v-if="info.unmapped.length"
      type="error"
      variant="tonal"
      class="mb-3"
      data-test="unmapped"
    >
      В ТТН есть продукция, которой нет в каталоге. Укажите её алкокод у варианта, иначе приёмку не
      завершить:
      <ul class="mt-1">
        <li v-for="u in info.unmapped" :key="u.identity">
          {{ u.name }} — алкокод <span class="pk-code">{{ u.alcocode }}</span
          >, {{ u.qty }} шт.
        </li>
      </ul>
    </v-alert>

    <v-alert
      v-if="sampleProblem"
      type="warning"
      variant="tonal"
      class="mb-3"
      data-test="sample-problem"
    >
      Во вскрытом коробе расхождение. Сосчитайте поставку поштучно — так найдётся каждая недостающая
      или чужая марка.
      <template v-if="doc.canEdit" #append>
        <v-btn variant="flat" color="warning" data-test="to-piece" @click="setMode('piece')">
          Поштучно
        </v-btn>
      </template>
    </v-alert>

    <v-alert
      :type="info.differences ? 'warning' : 'success'"
      variant="tonal"
      class="mb-4"
      data-test="summary"
    >
      <template v-if="info.differences">
        Расхождения: недостача {{ info.shortage }}, излишек {{ info.surplus }}, чужих марок
        {{ info.foreign }}. По итогам в ЕГАИС уйдёт акт расхождения — принято будет только то, что
        есть и в ТТН, и в коробах.
      </template>
      <template v-else> Поставка сходится с ТТН — в ЕГАИС уйдёт акт подтверждения. </template>
    </v-alert>

    <template v-if="byBox && info.packages.length">
      <div class="text-title-small mb-1">
        Короба · {{ info.packages.filter((p) => p.scanned).length }} из {{ info.packages.length }}
      </div>
      <div class="d-flex flex-wrap ga-2 mb-4" data-test="packages">
        <v-chip
          v-for="p in info.packages"
          :key="p.code"
          :prepend-icon="boxState(p).icon"
          :color="boxState(p).color"
          variant="tonal"
          label
          :title="p.code"
        >
          <span class="pk-code mr-1">{{ shortCode(p.code) }}</span> {{ boxState(p).text }}
        </v-chip>
      </div>
    </template>
  </div>
</template>

<style scoped>
.pk-modes {
  flex-wrap: wrap;
  height: auto;
}
</style>
