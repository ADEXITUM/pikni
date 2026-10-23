<script setup lang="ts">
import { api, ApiError, type Variant } from '@pikni/api'
import { useMutation } from '@tanstack/vue-query'
import { ref, watch } from 'vue'

import { variantLabel } from '@/products'

const props = defineProps<{ variant?: Variant }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const code = ref('')

// Пустой код — сервер создаст внутренний EAN-13 (для подарочных наборов и своей упаковки).
const { mutate, isPending, error, reset } = useMutation({
  mutationFn: async (withCode: boolean) => {
    const { error } = await api.POST('/variants/{id}/barcodes', {
      params: { path: { id: props.variant!.id } },
      body: withCode ? { code: code.value.trim() } : {},
    })
    if (error) throw new ApiError(error)
  },
  onSuccess: () => {
    open.value = false
    emit('saved')
  },
})

watch(open, (isOpen) => {
  if (!isOpen) return
  reset()
  code.value = ''
})
</script>

<template>
  <v-dialog v-model="open" max-width="480">
    <v-card title="Штрихкод" :subtitle="variant ? variantLabel(variant) : undefined">
      <v-form @submit.prevent="code.trim() && mutate(true)">
        <v-card-text>
          <v-text-field
            v-model="code"
            label="Штрихкод"
            hint="Отсканируйте сканером или введите цифры с этикетки"
            persistent-hint
            autofocus
            data-test="barcode-code"
          />
          <v-alert v-if="error" type="error" class="mt-3">
            {{ error instanceof ApiError ? error.message : 'Нет связи с сервером' }}
          </v-alert>
        </v-card-text>
        <v-card-actions class="flex-wrap">
          <v-btn variant="text" data-test="barcode-internal" @click="mutate(false)">
            Создать внутренний
          </v-btn>
          <v-spacer />
          <v-btn variant="text" @click="open = false">Отмена</v-btn>
          <v-btn
            type="submit"
            color="primary"
            :disabled="!code.trim()"
            :loading="isPending"
            data-test="barcode-save"
          >
            Привязать
          </v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>
