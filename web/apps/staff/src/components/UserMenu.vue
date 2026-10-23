<script setup lang="ts">
import {
  mdiAccountCircleOutline,
  mdiLogout,
  mdiThemeLightDark,
  mdiWeatherNight,
  mdiWhiteBalanceSunny,
} from '@mdi/js'
import { computed } from 'vue'
import { useTheme } from 'vuetify'

import { roleLabels } from '@/labels'
import { saveTheme, type ThemePreference } from '@/preferences'
import { useLogout, useMe } from '@/session'

const { data: me } = useMe()
const logout = useLogout()
const theme = useTheme()

const initials = computed(() =>
  (me.value?.fullName ?? '')
    .split(/\s+/)
    .slice(0, 2)
    .map((word) => word.charAt(0))
    .join('')
    .toUpperCase(),
)

const themePreference = computed<ThemePreference>({
  get: () => (theme.isSystem.value ? 'system' : (theme.name.value as ThemePreference)),
  set: (value) => {
    theme.change(value)
    saveTheme(value)
  },
})
</script>

<template>
  <v-menu v-if="me" :close-on-content-click="false">
    <template #activator="{ props }">
      <v-btn v-bind="props" icon :title="me.fullName" data-test="user-menu">
        <v-avatar color="primary" size="34" class="text-body-2 font-weight-bold">{{
          initials
        }}</v-avatar>
      </v-btn>
    </template>

    <v-card min-width="280">
      <v-card-item :title="me.fullName" :subtitle="roleLabels[me.role]" />
      <v-divider />
      <v-card-text>
        <div class="text-caption text-medium-emphasis mb-2">Тема</div>
        <v-btn-toggle
          v-model="themePreference"
          mandatory
          divided
          variant="outlined"
          color="primary"
          class="w-100"
        >
          <v-btn value="light" :icon="mdiWhiteBalanceSunny" title="Светлая" class="flex-1-1" />
          <v-btn value="dark" :icon="mdiWeatherNight" title="Тёмная" class="flex-1-1" />
          <v-btn value="system" :icon="mdiThemeLightDark" title="Как в системе" class="flex-1-1" />
        </v-btn-toggle>
      </v-card-text>
      <v-divider />
      <v-list density="compact">
        <v-list-item
          :prepend-icon="mdiAccountCircleOutline"
          title="Профиль и сеансы"
          :to="{ name: 'profile' }"
        />
        <v-list-item :prepend-icon="mdiLogout" title="Выйти" @click="logout()" />
      </v-list>
    </v-card>
  </v-menu>
</template>
