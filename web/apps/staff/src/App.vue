<script setup lang="ts">
import { useRoute } from 'vue-router'

import AppShell from '@/components/AppShell.vue'
import ConfirmHost from '@/components/ConfirmHost.vue'

const route = useRoute()
</script>

<template>
  <v-app>
    <AppShell v-if="!route.meta.bare" />
    <v-main>
      <router-view />
    </v-main>
    <ConfirmHost />
  </v-app>
</template>
