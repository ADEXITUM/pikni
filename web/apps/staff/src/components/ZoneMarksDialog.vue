<script setup lang="ts">
import { api, ApiError } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

// Повреждённую марку, которую сканер не читает, выбирают из марок на остатке зоны (СО-5).
const props = defineProps<{ zoneId: string; variantId?: string }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ pick: [code: string] }>()
const filter = ref('')

const { data, isPending } = useQuery({
  queryKey: computed(() => ['zone-marks', props.zoneId, props.variantId ?? null]),
  queryFn: async () => {
    const { data, error } = await api.GET('/zones/{zoneId}/marks', {
      params: { path: { zoneId: props.zoneId }, query: { variantId: props.variantId } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
  enabled: open,
})
const marks = computed(() =>
  (data.value ?? []).filter((m) =>
    m.code.toLowerCase().includes(filter.value.trim().toLowerCase()),
  ),
)
</script>

<template>
  <v-dialog v-model="open" max-width="520" scrollable>
    <v-card title="Выбрать марку">
      <v-card-text>
        <p class="text-body-medium mb-2">
          Найдите марку по последним знакам — они видны на повреждённой марке.
        </p>
        <v-text-field v-model="filter" label="Знаки марки" autofocus density="compact" />
        <v-progress-linear v-if="isPending" indeterminate />
        <v-list density="compact" max-height="360">
          <v-list-item
            v-for="m in marks"
            :key="m.code"
            :title="m.code"
            class="pk-code"
            data-test="zone-mark"
            @click="(emit('pick', m.code), (open = false))"
          />
          <v-list-item v-if="!isPending && marks.length === 0" title="Марок не найдено" />
        </v-list>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="open = false">Закрыть</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
