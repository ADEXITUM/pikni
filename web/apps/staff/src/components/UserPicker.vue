<script setup lang="ts">
import { api, ApiError } from '@pikni/api'
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'

// Выбор сотрудника с поиском на сервере: в федеральной сети их тысячи, весь список не грузится.
const model = defineModel<string | null>({ default: null })
defineProps<{ label?: string }>()

const search = ref('')
const q = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
watch(search, (v) => {
  clearTimeout(timer)
  timer = setTimeout(() => (q.value = (v ?? '').trim()), 300)
})

const { data, isFetching } = useQuery({
  queryKey: computed(() => ['users', 'picker', q.value]),
  placeholderData: keepPreviousData,
  queryFn: async () => {
    const { data, error } = await api.GET('/users', {
      params: { query: { q: q.value || undefined, limit: 20 } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
})
// Выбранный остаётся в списке, даже если его нет среди найденных по текущему поиску.
const chosen = ref<{ value: string; title: string }>()
const items = computed(() => {
  const found = (data.value ?? []).map((u) => ({
    value: u.id,
    title: `${u.fullName} (${u.login})`,
  }))
  if (chosen.value && !found.some((i) => i.value === chosen.value!.value))
    found.unshift(chosen.value)
  return found
})
watch(model, (id) => {
  chosen.value = id ? items.value.find((i) => i.value === id) : undefined
})
</script>

<template>
  <v-autocomplete
    v-model="model"
    v-model:search="search"
    :items="items"
    :loading="isFetching"
    :label="label ?? 'Сотрудник'"
    no-filter
    clearable
    no-data-text="Никого не нашли"
  />
</template>
