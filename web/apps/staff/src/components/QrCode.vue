<script setup lang="ts">
// QR-код на белой плашке с полем вокруг: камеры читают код только на светлом фоне с отступом,
// поэтому плашка одинаково белая и в тёмной теме. Пока код готовится — заглушка того же размера.
withDefaults(defineProps<{ svg?: string; size?: number }>(), { svg: '', size: 200 })
</script>

<template>
  <div class="pk-qr-tile" :style="{ width: `${size}px`, height: `${size}px` }">
    <!-- eslint-disable-next-line vue/no-v-html -- SVG собирает наш генератор QR -->
    <div v-if="svg" class="pk-qr-code" v-html="svg" />
    <v-progress-circular v-else indeterminate size="32" color="grey" />
  </div>
</template>

<style scoped>
.pk-qr-tile {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 10px;
  background: #fff;
  border-radius: 16px;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.12);
}
.pk-qr-code,
.pk-qr-code :deep(svg) {
  display: block;
  width: 100%;
  height: 100%;
}
</style>
