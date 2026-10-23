<script setup lang="ts">
defineProps<{ title: string; text: string; confirmText: string; loading?: boolean }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ confirm: [] }>()
</script>

<template>
  <v-dialog v-model="open" max-width="440">
    <v-card :title="title">
      <v-card-text>{{ text }}</v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="open = false">Отмена</v-btn>
        <v-btn color="error" :loading="loading" data-test="confirm" @click="emit('confirm')">
          {{ confirmText }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
