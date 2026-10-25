<script setup lang="ts">
import { api, ApiError, type Asset } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, reactive, watch } from 'vue'
import { useRouter } from 'vue-router'

import { assetCategoriesQuery } from '@/assets'
import { useLocationChoice } from '@/stock'

// Принять объект к учёту или исправить карточку (ИМ-1). Точку и МОЛ существующего объекта меняют
// перемещением — чтобы осталась история.
const open = defineModel<boolean>({ required: true })
const props = defineProps<{ asset?: Asset }>()
const router = useRouter()
const queryClient = useQueryClient()
const place = useLocationChoice('assets.edit')
const { data: categories } = useQuery({ ...assetCategoriesQuery, enabled: open })

const form = reactive({
  name: '',
  categoryCode: '',
  serial: '',
  acceptedAt: new Date().toISOString().slice(0, 10),
  cost: '',
  responsibleId: null as string | null,
})
watch(open, (o) => {
  if (!o) return
  const a = props.asset
  Object.assign(form, {
    name: a?.name ?? '',
    categoryCode: a?.categoryCode ?? '',
    serial: a?.serial ?? '',
    acceptedAt: a?.acceptedAt ?? new Date().toISOString().slice(0, 10),
    cost: a ? String(a.costKopecks / 100) : '',
    responsibleId: null,
  })
})

const { data: people } = useQuery({
  queryKey: computed(() => ['asset-responsibles', place.selectedId.value]),
  enabled: computed(() => open.value && !props.asset && !!place.selectedId.value),
  queryFn: async () => {
    const { data, error } = await api.GET('/assets/responsibles', {
      params: { query: { locationId: place.selectedId.value! } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
})

const costKopecks = computed(() =>
  Math.round(Number(form.cost.replace(',', '.').replace(/\s/g, '')) * 100),
)
const valid = computed(
  () =>
    form.name.trim() !== '' &&
    form.categoryCode !== '' &&
    !!form.acceptedAt &&
    Number.isFinite(costKopecks.value) &&
    costKopecks.value >= 0,
)

const {
  mutate: save,
  isPending,
  error,
  reset,
} = useMutation({
  mutationFn: async () => {
    const fields = {
      name: form.name.trim(),
      categoryCode: form.categoryCode,
      serial: form.serial.trim() || undefined,
      acceptedAt: form.acceptedAt,
      costKopecks: costKopecks.value,
    }
    const { data, error } = props.asset
      ? await api.PUT('/assets/{id}', {
          params: { path: { id: props.asset.id } },
          body: { ...fields, version: props.asset.version },
        })
      : await api.POST('/assets', {
          body: {
            ...fields,
            locationId: place.selectedId.value!,
            responsibleId: form.responsibleId ?? undefined,
          },
        })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (card) => {
    open.value = false
    void queryClient.invalidateQueries({ queryKey: ['assets'] })
    queryClient.setQueryData(['asset', card.asset.id], card)
    if (!props.asset) void router.push({ name: 'asset', params: { id: card.asset.id } })
  },
})
</script>

<template>
  <v-dialog v-model="open" max-width="520" @after-leave="reset()">
    <v-card :title="asset ? `Карточка ${asset.number}` : 'Принять к учёту'">
      <v-card-text>
        <template v-if="!asset">
          <v-select
            v-model="place.selectedId.value"
            :items="place.items.value"
            label="Точка"
            class="mb-2"
            @update:model-value="form.responsibleId = null"
          />
          <v-select
            v-model="form.responsibleId"
            :items="(people ?? []).map((p) => ({ value: p.id, title: p.fullName }))"
            label="Материально ответственное лицо"
            clearable
            class="mb-2"
          />
        </template>
        <v-text-field
          v-model="form.name"
          label="Наименование"
          class="mb-2"
          data-test="asset-name"
        />
        <v-select
          v-model="form.categoryCode"
          :items="(categories ?? []).map((c) => ({ value: c.code, title: c.name }))"
          label="Категория"
          class="mb-2"
          data-test="asset-category"
        />
        <v-text-field v-model="form.serial" label="Серийный номер" class="mb-2" />
        <div class="d-flex ga-2">
          <v-text-field
            v-model="form.acceptedAt"
            type="date"
            label="Принят к учёту"
            data-test="asset-date"
          />
          <v-text-field
            v-model="form.cost"
            label="Стоимость, ₽"
            inputmode="decimal"
            data-test="asset-cost"
          />
        </div>
        <v-alert v-if="error" type="error" class="mt-2">{{ error.message }}</v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="open = false">Отмена</v-btn>
        <v-btn
          color="primary"
          variant="flat"
          :disabled="!valid || (!asset && !place.selectedId.value)"
          :loading="isPending"
          data-test="save-asset"
          @click="save()"
          >{{ asset ? 'Сохранить' : 'Принять' }}</v-btn
        >
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
