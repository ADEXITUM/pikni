<script setup lang="ts">
import { api, ApiError, type User } from '@pikni/api'
import { useMutation } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'

const user = defineModel<User | null>({ required: true })
const pin = ref('')
const valid = computed(() => /^[0-9]{4,6}$/.test(pin.value))

const save = useMutation({
  mutationFn: async () => {
    const { error } = await api.PUT('/users/{id}/pin', {
      params: { path: { id: user.value!.id } },
      body: { pin: pin.value },
    })
    if (error) throw new ApiError(error)
  },
  onSuccess: () => (user.value = null),
})

watch(user, () => {
  pin.value = ''
  save.reset()
})
</script>

<template>
  <v-dialog :model-value="user !== null" max-width="400" @update:model-value="user = null">
    <v-card v-if="user" title="PIN для кассы">
      <v-card-text>
        <p class="mb-4">
          {{ user.fullName }} будет входить на кассе по логину <b>{{ user.login }}</b> и этому PIN.
          Прежний PIN перестанет действовать.
        </p>
        <v-text-field
          v-model="pin"
          label="PIN"
          inputmode="numeric"
          maxlength="6"
          hint="От 4 до 6 цифр"
          persistent-hint
          autofocus
          data-test="pin"
          @keyup.enter="valid && save.mutate()"
        />
        <v-alert v-if="save.error.value" type="error" density="compact" class="mt-4">
          {{ save.error.value.message }}
        </v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="user = null">Отмена</v-btn>
        <v-btn
          color="primary"
          :disabled="!valid"
          :loading="save.isPending.value"
          @click="save.mutate()"
        >
          Сохранить
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
