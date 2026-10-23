<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import RefTable from '@/components/RefTable.vue'
import { brandsQuery, countriesQuery, producersQuery, productKindsQuery } from '@/queries'
import { buildRefs } from '@/refs'
import { useMe } from '@/session'

const { data: me } = useMe()
const canEdit = computed(() => me.value?.permissions.includes('refs.edit') ?? false)

// Те же ключи, что у вкладок, поэтому после правки обновятся и подписи в других вкладках.
const { data: countries } = useQuery(countriesQuery)
const { data: producers } = useQuery(producersQuery)
const { data: brands } = useQuery(brandsQuery)
const { data: kinds } = useQuery(productKindsQuery)

const refs = buildRefs({
  countries: () => countries.value ?? [],
  producers: () => producers.value ?? [],
  brands: () => brands.value ?? [],
  kinds: () => kinds.value ?? [],
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
