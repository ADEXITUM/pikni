<script setup lang="ts">
import { mdiLogout } from '@mdi/js'
import { useRoute } from 'vue-router'

import PikniLogo from '@/components/PikniLogo.vue'
import { useLogout, useMe } from '@/session'

const route = useRoute()
const { data: me } = useMe()
const logout = useLogout()
</script>

<template>
  <v-app>
    <v-app-bar v-if="!route.meta.bare">
      <v-app-bar-title>
        <PikniLogo :height="32" />
      </v-app-bar-title>
      <template #append>
        <span v-if="me" class="text-body-2 mr-2 d-none d-sm-inline">{{ me.fullName }}</span>
        <v-btn :icon="mdiLogout" title="Выйти" @click="logout()" />
      </template>
    </v-app-bar>

    <v-main>
      <router-view />
    </v-main>
  </v-app>
</template>
