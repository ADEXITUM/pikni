<script setup lang="ts">
import logoDark from '@pikni/ui/assets/logo-dark.svg'
import logoLight from '@pikni/ui/assets/logo-light.svg'
import { computed } from 'vue'
import { useTheme } from 'vuetify'

defineProps<{ height: number }>()

const theme = useTheme()
const logo = computed(() => (theme.current.value.dark ? logoDark : logoLight))
</script>

<template>
  <img :src="logo" alt="Пикни" :height="height" class="d-block" />
</template>
