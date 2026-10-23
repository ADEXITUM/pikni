<script setup lang="ts">
import { mdiDeleteOutline } from '@mdi/js'
import { api, ApiError, type PriceKind, type Variant } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, reactive, ref, watch } from 'vue'
import type { SubmitEventPromise } from 'vuetify'

import { formatDateTime } from '@/format'
import { formatPrice, variantLabel } from '@/products'

const props = defineProps<{ variant?: Variant; canEdit: boolean; showPurchase: boolean }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const kind = ref<PriceKind>('retail')
const path = computed(() =>
  kind.value === 'retail'
    ? ('/variants/{id}/retail-prices' as const)
    : ('/variants/{id}/purchase-prices' as const),
)

const queryClient = useQueryClient()
const key = computed(() => ['prices', props.variant?.id, kind.value])
const { data: history } = useQuery({
  queryKey: key,
  queryFn: async () => {
    const { data, error } = await api.GET(path.value, {
      params: { path: { id: props.variant!.id } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
  enabled: computed(() => open.value && Boolean(props.variant)),
})

// Поле даты пустое — цена действует сразу.
const form = reactive({ price: '', validFrom: '' })
const now = Date.now()
const isPlanned = (validFrom: string) => new Date(validFrom).getTime() > now
const priceRule = (v: string) =>
  Number(v.replace(',', '.')) > 0 || 'Укажите цену больше нуля, например 449,90'

const done = async () => {
  await queryClient.invalidateQueries({ queryKey: ['prices', props.variant?.id] })
  emit('saved')
}

const { mutate, isPending, error, reset } = useMutation({
  mutationFn: async () => {
    const { error } = await api.POST(path.value, {
      params: { path: { id: props.variant!.id } },
      body: {
        price: Number(form.price.replace(',', '.')),
        validFrom: form.validFrom ? new Date(form.validFrom).toISOString() : undefined,
      },
    })
    if (error) throw new ApiError(error)
  },
  onSuccess: async () => {
    Object.assign(form, { price: '', validFrom: '' })
    await done()
  },
})

const { mutate: remove, error: removeError } = useMutation({
  mutationFn: async (id: string) => {
    const { error } = await api.DELETE('/prices/{id}', { params: { path: { id } } })
    if (error) throw new ApiError(error)
  },
  onSuccess: done,
})

const failure = computed(() => error.value ?? removeError.value)

watch(open, (isOpen) => {
  if (!isOpen) return
  reset()
  kind.value = 'retail'
  Object.assign(form, { price: '', validFrom: '' })
})

async function submit(event: SubmitEventPromise) {
  if ((await event).valid) mutate()
}
</script>

<template>
  <v-dialog v-model="open" max-width="560">
    <v-card title="Цены" :subtitle="variant ? variantLabel(variant) : undefined">
      <v-tabs v-if="showPurchase" v-model="kind" class="px-4">
        <v-tab value="retail">Розничная</v-tab>
        <v-tab value="purchase" data-test="tab-purchase">Закупочная</v-tab>
      </v-tabs>
      <v-card-text>
        <p
          v-if="kind === 'retail' && variant?.minPrice"
          class="text-body-2 mb-3"
          data-test="min-price"
        >
          МРЦ сейчас: {{ formatPrice(variant.minPrice) }}. Дешевле продавать нельзя.
        </p>

        <v-form v-if="canEdit" @submit.prevent="submit">
          <v-row density="compact">
            <v-col cols="12" sm="5">
              <v-text-field
                v-model="form.price"
                label="Новая цена"
                suffix="₽"
                inputmode="decimal"
                :rules="[priceRule]"
                data-test="price-value"
              />
            </v-col>
            <v-col cols="12" sm="7">
              <v-text-field
                v-model="form.validFrom"
                label="Действует с"
                type="datetime-local"
                hint="Пусто — сразу. Задним числом нельзя"
                persistent-hint
              />
            </v-col>
          </v-row>
          <div class="d-flex justify-end mt-2">
            <v-btn type="submit" color="primary" :loading="isPending" data-test="price-save">
              Назначить
            </v-btn>
          </div>
        </v-form>

        <v-alert v-if="failure" type="error" class="mt-3">
          {{ failure instanceof ApiError ? failure.message : 'Нет связи с сервером' }}
        </v-alert>

        <div class="text-subtitle-2 mt-4 mb-1">История</div>
        <v-list v-if="history?.length" density="compact" data-test="price-history">
          <v-list-item
            v-for="p in history"
            :key="p.id"
            :title="formatPrice(p.price)"
            :subtitle="`с ${formatDateTime(p.validFrom)}${p.setBy ? ' · ' + p.setBy : ''}`"
          >
            <template #append>
              <v-chip v-if="isPlanned(p.validFrom)" size="x-small" color="info">назначена</v-chip>
              <v-btn
                v-if="canEdit && isPlanned(p.validFrom)"
                :icon="mdiDeleteOutline"
                variant="text"
                size="small"
                title="Отменить назначенную цену"
                @click="remove(p.id)"
              />
            </template>
          </v-list-item>
        </v-list>
        <p v-else class="text-body-2 text-medium-emphasis">Цен ещё не было</p>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="open = false">Закрыть</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
