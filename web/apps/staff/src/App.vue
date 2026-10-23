<script setup lang="ts">
import { useRoute } from 'vue-router'

import AppShell from '@/components/AppShell.vue'

const route = useRoute()
</script>

<template>
  <v-app>
    <AppShell v-if="!route.meta.bare" />
    <v-main>
      <router-view />
    </v-main>
  </v-app>
</template>
