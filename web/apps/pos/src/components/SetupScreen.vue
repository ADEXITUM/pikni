<script setup lang="ts">
import { computed, ref } from 'vue'

import { validKey } from '../device'
import { setKey } from '../till'

const key = ref('')
const valid = computed(() => validKey(key.value))
</script>

<template>
  <v-main class="d-flex align-center justify-center">
    <v-card max-width="520" class="ma-4" title="Настройка кассы">
      <v-card-text>
        <p class="mb-4">
          Касса ещё не подключена. Управляющий сетью регистрирует её в разделе «Кассы» основного
          приложения и показывает ключ и QR-код. Отсканируйте QR-код камерой устройства или вставьте
          ключ сюда.
        </p>
        <v-text-field
          v-model="key"
          label="Ключ кассы"
          placeholder="pos_…"
          autofocus
          data-test="device-key"
          :error-messages="key && !valid ? 'Ключ начинается с pos_ и выдаётся при регистрации' : ''"
          @keyup.enter="valid && setKey(key.trim())"
        />
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn color="primary" :disabled="!valid" @click="setKey(key.trim())">Подключить</v-btn>
      </v-card-actions>
    </v-card>
  </v-main>
</template>
