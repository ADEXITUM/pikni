<script setup lang="ts">
import { api, ApiError, type Container, type Variant } from '@pikni/api'
import { useMutation, useQuery } from '@tanstack/vue-query'
import { computed, reactive, watch } from 'vue'
import type { SubmitEventPromise } from 'vuetify'

import { containerLabels, variantLabel } from '@/products'
import { volumesQuery } from '@/queries'
import { formatVolume } from '@/refs'

// Без variant — новый вариант позиции productId.
const props = defineProps<{ productId: string; isEgais: boolean; variant?: Variant }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const { data: volumes } = useQuery(volumesQuery)
const volumeItems = computed(() =>
  (volumes.value ?? [])
    .filter((v) => v.isActive)
    .map((v) => ({ value: v.ml, title: formatVolume(v.ml) })),
)
const containerItems = Object.entries(containerLabels).map(([value, title]) => ({ value, title }))

const form = reactive({
  volumeMl: null as number | null,
  container: null as Container | null,
  giftPack: false,
  alcocode: '',
  gtin: '',
  isActive: true,
})

const volumeRule = (v: number | null) =>
  !props.isEgais || v !== null || 'Для продукции в ЕГАИС укажите объём'
const alcocodeRule = (v: string) =>
  v.trim() === '' || /^\d{19}$/.test(v.trim()) || 'Алкокод — 19 цифр'

const { mutate, isPending, error, reset } = useMutation({
  mutationFn: async () => {
    const codes = {
      alcocode: form.alcocode.trim() || undefined,
      gtin: form.gtin.trim() || undefined,
    }
    const { error } = props.variant
      ? await api.PUT('/variants/{id}', {
          params: { path: { id: props.variant.id } },
          body: { ...codes, isActive: form.isActive },
        })
      : await api.POST('/products/{id}/variants', {
          params: { path: { id: props.productId } },
          body: {
            ...codes,
            volumeMl: form.volumeMl ?? undefined,
            container: form.container ?? undefined,
            giftPack: form.giftPack,
          },
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
  const v = props.variant
  Object.assign(form, {
    volumeMl: v?.volumeMl ?? null,
    container: v?.container ?? null,
    giftPack: v?.giftPack ?? false,
    alcocode: v?.alcocode ?? '',
    gtin: v?.gtin ?? '',
    isActive: v?.isActive ?? true,
  })
})

async function submit(event: SubmitEventPromise) {
  if ((await event).valid) mutate()
}
</script>

<template>
  <v-dialog v-model="open" max-width="520">
    <v-card
      :title="variant ? 'Изменить вариант' : 'Новый вариант'"
      :subtitle="variant ? variantLabel(variant) : undefined"
    >
      <v-form @submit.prevent="submit">
        <v-card-text>
          <template v-if="!variant">
            <v-row density="compact">
              <v-col cols="6">
                <v-select
                  v-model="form.volumeMl"
                  :items="volumeItems"
                  label="Объём"
                  :rules="[volumeRule]"
                  clearable
                  data-test="variant-volume"
                />
              </v-col>
              <v-col cols="6">
                <v-select
                  v-model="form.container"
                  :items="containerItems"
                  label="Тара"
                  clearable
                  data-test="variant-container"
                />
              </v-col>
            </v-row>
            <v-switch v-model="form.giftPack" label="Подарочная упаковка" hide-details />
            <p class="text-caption text-medium-emphasis mb-3">
              Объём, тара и упаковка задают вариант и после создания не меняются.
            </p>
          </template>
          <v-text-field
            v-if="isEgais"
            v-model="form.alcocode"
            label="Алкокод ЕГАИС"
            inputmode="numeric"
            :rules="[alcocodeRule]"
            class="mb-3"
            data-test="variant-alcocode"
          />
          <v-text-field
            v-model="form.gtin"
            label="GTIN"
            inputmode="numeric"
            hint="Для кодов «Честного знака»; подойдёт и EAN-13 с этикетки"
            persistent-hint
            data-test="variant-gtin"
          />
          <v-switch
            v-if="variant"
            v-model="form.isActive"
            label="Используется (выключено — в архиве)"
          />
          <v-alert v-if="error" type="error" class="mt-3">
            {{ error instanceof ApiError ? error.message : 'Нет связи с сервером' }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="open = false">Отмена</v-btn>
          <v-btn type="submit" color="primary" :loading="isPending" data-test="variant-save">
            Сохранить
          </v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>
