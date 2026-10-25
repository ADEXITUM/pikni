<script setup lang="ts">
import { mdiArrowLeft } from '@mdi/js'
import { api, ApiError } from '@pikni/api'
import { useMutation } from '@tanstack/vue-query'
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { variantLabel } from '@/products'
import { useScanner } from '@/scanning'
import { useLocationChoice } from '@/stock'

// Бронь для покупателя (НЛ-4): товар, количество, имя и телефон с согласием на обработку
// персональных данных. Срок — до конца следующего дня.
const route = useRoute()
const router = useRouter()
const place = useLocationChoice('reservations.manage', (l) => l.kind === 'store')
if (typeof route.query.location === 'string') place.selectedId.value = route.query.location

const item = ref<{ variantId: string; article: string; name: string; label: string }>()
const code = ref('')
const lookupError = ref('')
const qty = ref(1)
const customerName = ref('')
const phone = ref('')
const consent = ref(false)
const comment = ref('')

async function find(raw: string) {
  lookupError.value = ''
  const { data, error } = await api.GET('/codes/resolve', {
    params: { query: { code: raw.trim() } },
  })
  if (error || !data?.variant || !data.product) {
    lookupError.value = 'Товар не найден — отсканируйте штрихкод или введите артикул'
    return
  }
  item.value = {
    variantId: data.variant.id,
    article: data.variant.article,
    name: data.product.name,
    label: variantLabel(data.variant),
  }
  code.value = ''
}
useScanner(({ event }) => void find(event.raw))
onMounted(() => {
  if (typeof route.query.article === 'string') void find(route.query.article)
})

const valid = computed(
  () =>
    !!item.value &&
    !!place.selectedId.value &&
    qty.value >= 1 &&
    customerName.value.trim() !== '' &&
    /^\+?[0-9 ()-]{6,20}$/.test(phone.value.trim()) &&
    consent.value,
)
const {
  mutate: submit,
  isPending,
  error,
} = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/reservations', {
      body: {
        locationId: place.selectedId.value!,
        variantId: item.value!.variantId,
        qty: qty.value,
        customerName: customerName.value.trim(),
        customerPhone: phone.value.trim(),
        consent: consent.value,
        comment: comment.value.trim() || undefined,
      },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: () => router.replace({ name: 'reservations' }),
})
</script>

<template>
  <v-container class="pk-narrow">
    <v-btn :prepend-icon="mdiArrowLeft" variant="text" class="mb-2" @click="router.back()"
      >Назад</v-btn
    >
    <h1 class="text-headline-small mb-4">Отложить для покупателя</h1>
    <v-card class="mb-4">
      <v-card-text>
        <v-select v-model="place.selectedId.value" :items="place.items.value" label="Магазин" />
        <div v-if="item" class="mb-4" data-test="reservation-item">
          <div class="text-title-medium">{{ item.name }}</div>
          <div class="text-body-small text-medium-emphasis">
            <span class="pk-code">{{ item.article }}</span> · {{ item.label }}
          </div>
        </div>
        <v-text-field
          v-model="code"
          :label="item ? 'Другой товар — штрихкод или артикул' : 'Штрихкод или артикул'"
          :error-messages="lookupError"
          @keyup.enter="find(code)"
        />
        <v-text-field v-model.number="qty" label="Количество" type="number" min="1" />
        <v-text-field v-model="customerName" label="Имя покупателя" data-test="customer-name" />
        <v-text-field
          v-model="phone"
          label="Телефон"
          type="tel"
          placeholder="+7 914 000-00-00"
          data-test="customer-phone"
        />
        <v-text-field v-model="comment" label="Комментарий" maxlength="500" />
        <v-checkbox
          v-model="consent"
          label="Покупатель согласен на обработку персональных данных — данные обезличатся через 30 дней после закрытия брони"
          data-test="consent"
        />
      </v-card-text>
    </v-card>
    <v-alert v-if="error" type="error" class="mb-3">{{ error.message }}</v-alert>
    <div class="d-flex">
      <v-spacer />
      <v-btn
        color="primary"
        variant="flat"
        :disabled="!valid"
        :loading="isPending"
        data-test="submit-reservation"
        @click="submit()"
        >Оформить бронь</v-btn
      >
    </div>
  </v-container>
</template>

<style scoped>
.pk-narrow {
  max-width: 640px;
}
</style>
