<script setup lang="ts">
import { mdiChevronDown, mdiMagnify, mdiMapMarkerOutline } from '@mdi/js'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useDisplay } from 'vuetify'

import { locationsQuery } from '@/queries'
import { useMe } from '@/session'
import { ALL_LOCATIONS, locationOptions, useCurrentLocation } from '@/stores/currentLocation'

const { data: me } = useMe()
const { data: locations } = useQuery(locationsQuery)
const current = useCurrentLocation()
const { xs } = useDisplay()

const options = computed(() => {
  if (!me.value) return []
  const byId = new Map((locations.value ?? []).map((l) => [l.id, l]))
  return locationOptions(me.value).map((id) => {
    const l = byId.get(id)
    const title = id === ALL_LOCATIONS ? 'Все точки' : l ? `${l.code} · ${l.name}` : '…'
    return { id, title, short: l?.code ?? title, city: l?.city ?? '' }
  })
})
// В федеральной сети точек тысячи: в меню поиск по коду, названию и городу, список прокручивается
// виртуально — рисуются только видимые строки.
const open = ref(false)
const search = ref('')
const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return options.value
  return options.value.filter((o) => `${o.title} ${o.city}`.toLowerCase().includes(q))
})
function choose(id: string) {
  current.select(id)
  open.value = false
  search.value = ''
}

const selected = computed(() => options.value.find((o) => o.id === current.selected))
const label = computed(() => {
  if (!selected.value) return 'Точка не назначена'
  return xs.value ? selected.value.short : selected.value.title
})
</script>

<template>
  <v-menu v-if="options.length > 1" v-model="open" :close-on-content-click="false">
    <template #activator="{ props }">
      <v-btn
        v-bind="props"
        variant="text"
        class="text-none pk-location"
        :prepend-icon="mdiMapMarkerOutline"
        :append-icon="mdiChevronDown"
        data-test="location-picker"
      >
        <span class="pk-location__label">{{ label }}</span>
      </v-btn>
    </template>
    <v-card min-width="320" max-width="420">
      <v-text-field
        v-if="options.length > 8"
        v-model="search"
        :prepend-inner-icon="mdiMagnify"
        placeholder="Код, название или город"
        density="compact"
        hide-details
        autofocus
        clearable
        class="pa-2"
        data-test="location-search"
      />
      <v-virtual-scroll
        :items="filtered"
        :height="Math.min(filtered.length, 8) * 48 || 48"
        item-height="48"
      >
        <template #default="{ item: o }">
          <v-list-item
            :title="o.title"
            :subtitle="o.city"
            :active="o.id === current.selected"
            density="compact"
            @click="choose(o.id)"
          />
        </template>
      </v-virtual-scroll>
      <p v-if="!filtered.length" class="text-body-medium text-medium-emphasis pa-3">
        Ничего не нашли
      </p>
    </v-card>
  </v-menu>
  <div v-else class="d-flex align-center text-body-medium px-2" data-test="location-picker">
    <v-icon :icon="mdiMapMarkerOutline" size="small" class="mr-1" />
    <span class="pk-location__label">{{ label }}</span>
  </div>
</template>

<style scoped>
.pk-location__label {
  display: inline-block;
  max-width: 40vw;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
