<script setup lang="ts">
import { api, ApiError, type Container } from '@pikni/api'
import { useMutation, useQuery } from '@tanstack/vue-query'
import { computed, reactive, watch } from 'vue'

import { containerLabels } from '@/products'
import { volumesQuery } from '@/queries'
import { formatVolume } from '@/refs'

const props = defineProps<{ productId: string }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [created: number] }>()

const { data: volumes } = useQuery(volumesQuery)
const activeVolumes = computed(() => (volumes.value ?? []).filter((v) => v.isActive))

const form = reactive({ volumes: [] as number[], containers: [] as Container[], giftPack: false })
const total = computed(() => form.volumes.length * Math.max(form.containers.length, 1))

const { mutate, isPending, error, reset } = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/products/{id}/variants/generate', {
      params: { path: { id: props.productId } },
      body: { volumes: form.volumes, containers: form.containers, giftPack: form.giftPack },
    })
    if (error) throw new ApiError(error)
    return data.items.length
  },
  onSuccess: (n) => {
    open.value = false
    emit('saved', n)
  },
})

watch(open, (isOpen) => {
  if (!isOpen) return
  reset()
  Object.assign(form, { volumes: [], containers: [], giftPack: false })
})
</script>

<template>
  <v-dialog v-model="open" max-width="560">
    <v-card title="Варианты по списку">
      <v-card-text>
        <div class="text-subtitle-2 mb-2">Объёмы</div>
        <v-chip-group v-model="form.volumes" multiple column filter data-test="generate-volumes">
          <v-chip v-for="v in activeVolumes" :key="v.ml" :value="v.ml" :text="formatVolume(v.ml)" />
        </v-chip-group>
        <div class="text-subtitle-2 mt-3 mb-2">Тара</div>
        <v-chip-group
          v-model="form.containers"
          multiple
          column
          filter
          data-test="generate-containers"
        >
          <v-chip
            v-for="(title, value) in containerLabels"
            :key="value"
            :value="value"
            :text="title"
          />
        </v-chip-group>
        <v-switch v-model="form.giftPack" label="Подарочная упаковка" hide-details />
        <p class="text-body-2 text-medium-emphasis">
          <template v-if="total">
            Будет создано до {{ total }}: каждый объём в каждой таре. Уже заведённые сочетания
            пропускаются.
          </template>
          <template v-else>Выберите хотя бы один объём. Без тары — варианты без тары.</template>
        </p>
        <v-alert v-if="error" type="error" class="mt-3">
          {{ error instanceof ApiError ? error.message : 'Нет связи с сервером' }}
        </v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="open = false">Отмена</v-btn>
        <v-btn
          color="primary"
          :disabled="!form.volumes.length"
          :loading="isPending"
          data-test="generate-save"
          @click="mutate()"
        >
          Создать
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
