<script setup lang="ts">
import { api, ApiError } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import RefTable from '@/components/RefTable.vue'
import { buildRefs } from '@/refs'
import { useMe } from '@/session'

const { data: me } = useMe()
const canEdit = computed(() => me.value?.permissions.includes('refs.edit') ?? false)

// Те же ключи, что у вкладок «Страны» и «Производители»: после правки обновятся и подписи.
const { data: countries } = useQuery({
  queryKey: ['ref', 'countries'],
  queryFn: async () => {
    const { data, error } = await api.GET('/countries')
    if (error) throw new ApiError(error)
    return data.items
  },
  staleTime: Infinity,
})
const { data: producers } = useQuery({
  queryKey: ['ref', 'producers'],
  queryFn: async () => {
    const { data, error } = await api.GET('/producers')
    if (error) throw new ApiError(error)
    return data.items
  },
})

const refs = buildRefs({
  countries: () => countries.value ?? [],
  producers: () => producers.value ?? [],
})
const tab = ref(refs[0]!.key)
</script>

<template>
  <v-container>
    <h1 class="text-h5 mb-4">Справочники</h1>

    <v-tabs v-model="tab" show-arrows class="mb-4">
      <v-tab v-for="r in refs" :key="r.key" :value="r.key" :data-test="`tab-${r.key}`">{{
        r.title
      }}</v-tab>
    </v-tabs>

    <v-window v-model="tab">
      <v-window-item v-for="r in refs" :key="r.key" :value="r.key">
        <RefTable :config="r" :can-edit="canEdit" />
      </v-window-item>
    </v-window>
  </v-container>
</template>
