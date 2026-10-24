<script setup lang="ts">
import { mdiChevronDown, mdiMapMarkerOutline } from '@mdi/js'
import { useQuery } from '@tanstack/vue-query'
import { computed } from 'vue'
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
    return { id, title, short: l?.code ?? title }
  })
})

const selected = computed(() => options.value.find((o) => o.id === current.selected))
const label = computed(() => {
  if (!selected.value) return 'Точка не назначена'
  return xs.value ? selected.value.short : selected.value.title
})
</script>

<template>
  <v-menu v-if="options.length > 1">
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
    <v-list density="compact">
      <v-list-item
        v-for="o in options"
        :key="o.id"
        :title="o.title"
        :active="o.id === current.selected"
        @click="current.select(o.id)"
      />
    </v-list>
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
