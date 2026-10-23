<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useDisplay } from 'vuetify'

import LocationPicker from '@/components/LocationPicker.vue'
import PikniLogo from '@/components/PikniLogo.vue'
import UserMenu from '@/components/UserMenu.vue'
import { navItems } from '@/navigation'
import { canOpen, useMe } from '@/session'
import { useCurrentLocation } from '@/stores/currentLocation'

const router = useRouter()
const { xs, mdAndUp } = useDisplay()
const { data: me } = useMe()
const current = useCurrentLocation()

watch(me, (m) => m && current.restore(m), { immediate: true })

const items = computed(() => {
  const user = me.value
  if (!user) return []
  return navItems.filter((item) => canOpen(user, router.resolve({ name: item.route })))
})
</script>

<template>
  <v-app-bar>
    <template #prepend>
      <PikniLogo :height="28" class="ml-2 mr-3" />
    </template>
    <LocationPicker />
    <v-spacer />
    <template #append>
      <UserMenu />
    </template>
  </v-app-bar>

  <!-- ПК — меню полностью, планшет — только значки, смартфон — нижняя панель. -->
  <v-navigation-drawer v-if="!xs" permanent :rail="!mdAndUp">
    <v-list nav>
      <v-list-item
        v-for="item in items"
        :key="item.route"
        :to="{ name: item.route }"
        :prepend-icon="item.icon"
        :title="item.title"
      />
    </v-list>
  </v-navigation-drawer>

  <v-bottom-navigation v-if="xs && items.length > 1">
    <v-btn v-for="item in items.slice(0, 4)" :key="item.route" :to="{ name: item.route }">
      <v-icon :icon="item.icon" />
      <span>{{ item.title }}</span>
    </v-btn>
  </v-bottom-navigation>
</template>
