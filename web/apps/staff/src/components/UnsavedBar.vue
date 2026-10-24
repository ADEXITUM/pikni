<script setup lang="ts">
defineProps<{ saving?: boolean }>()
const emit = defineEmits<{ discard: [] }>()
</script>

<!-- Панель прилипает к низу экрана, пока в форме есть несохранённые правки: кнопка
     «Сохранить» всегда под рукой, а не посередине длинной карточки. -->
<template>
  <v-sheet class="pk-unsaved" border rounded="lg" data-test="unsaved">
    <span class="text-body-medium">Есть несохранённые изменения</span>
    <v-spacer />
    <v-btn variant="text" data-test="discard" @click="emit('discard')">Отменить</v-btn>
    <v-btn type="submit" color="primary" variant="flat" :loading="saving" data-test="save">
      Сохранить
    </v-btn>
  </v-sheet>
</template>

<style scoped>
.pk-unsaved {
  position: sticky;
  bottom: 12px;
  z-index: 2;
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 16px;
  padding: 8px 8px 8px 16px;
}
</style>
