<script setup lang="ts">
import logoDark from '@pikni/ui/assets/logo-dark.svg'
import logoLight from '@pikni/ui/assets/logo-light.svg'
import { computed } from 'vue'
import { useTheme } from 'vuetify'

const theme = useTheme()
const logo = computed(() => (theme.current.value.dark ? logoDark : logoLight))
</script>

<template>
  <v-app>
    <v-app-bar>
      <v-app-bar-title>
        <img :src="logo" alt="Пикни" height="32" class="d-block" />
      </v-app-bar-title>
    </v-app-bar>

    <v-main>
      <router-view />
    </v-main>
  </v-app>
</template>
