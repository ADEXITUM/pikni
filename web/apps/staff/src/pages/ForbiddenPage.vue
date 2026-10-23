<script setup lang="ts">
import mascotError from '@pikni/ui/assets/mascot/error.svg'
</script>

<template>
  <v-container class="text-center py-12">
    <img :src="mascotError" alt="" width="136" class="mb-4" />
    <h1 class="text-h6 mb-2">Нет доступа к этому разделу</h1>
    <p class="text-body-2 text-medium-emphasis mb-6">
      Если он нужен для работы, обратитесь к администратору.
    </p>
    <v-btn color="primary" to="/">На главную</v-btn>
  </v-container>
</template>
