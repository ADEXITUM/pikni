<script setup lang="ts">
import PikniLogo from '@/components/PikniLogo.vue'

defineProps<{ title: string; mascot: string }>()
</script>

<template>
  <div class="pk-auth-screen">
    <div class="pk-auth">
      <img :src="mascot" alt="" class="pk-auth__mascot" data-test="mascot" />
      <v-card class="pa-6">
        <PikniLogo :height="28" class="mb-6" />
        <h1 class="text-h6 mb-4">{{ title }}</h1>
        <slot />
      </v-card>
    </div>
  </div>
</template>

<style scoped>
.pk-auth-screen {
  min-height: 100dvh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
}

.pk-auth {
  width: 100%;
  max-width: 400px;
}

.pk-auth__mascot {
  display: block;
  width: 136px;
  margin: 0 auto -12px;
  position: relative;
  z-index: 1;
}
</style>
