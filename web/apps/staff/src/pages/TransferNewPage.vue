<script setup lang="ts">
import { mdiArrowLeft, mdiDeleteOutline } from '@mdi/js'
import { api, ApiError } from '@pikni/api'
import { useMutation, useQuery } from '@tanstack/vue-query'
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { variantLabel } from '@/products'
import { locationsQuery } from '@/queries'
import { useScanner } from '@/scanning'
import { useLocationChoice } from '@/stock'

// Заявка на перемещение в свою точку (СО-4, НЛ-3): что, сколько, откуда и почему.
const route = useRoute()
const router = useRouter()
const to = useLocationChoice('transfer.request')
if (typeof route.query.to === 'string') to.selectedId.value = route.query.to
const { data: locations } = useQuery(locationsQuery)
const from = ref<string | null>(typeof route.query.from === 'string' ? route.query.from : null)
const fromItems = computed(() =>
  (locations.value ?? [])
    .filter((l) => l.isActive && l.id !== to.selectedId.value)
    .map((l) => ({ value: l.id, title: `${l.code} · ${l.name}` })),
)

interface Line {
  variantId: string
  article: string
  name: string
  label: string
  qty: number
}
const lines = ref<Line[]>([])
const comment = ref('')
const code = ref('')
const lookupError = ref('')

async function add(raw: string) {
  lookupError.value = ''
  const value = raw.trim()
  if (!value) return
  const { data, error } = await api.GET('/codes/resolve', { params: { query: { code: value } } })
  if (error || !data?.variant || !data.product) {
    lookupError.value = 'Товар не найден — отсканируйте штрихкод или введите артикул варианта'
    return
  }
  const v = data.variant
  const existing = lines.value.find((l) => l.variantId === v.id)
  if (existing) existing.qty++
  else
    lines.value.push({
      variantId: v.id,
      article: v.article,
      name: data.product.name,
      label: variantLabel(v),
      qty: 1,
    })
  code.value = ''
}
useScanner(({ event }) => void add(event.raw))
onMounted(() => {
  if (typeof route.query.article === 'string') void add(route.query.article)
})

const valid = computed(
  () =>
    !!from.value &&
    !!to.selectedId.value &&
    lines.value.length > 0 &&
    lines.value.every((l) => Number.isInteger(l.qty) && l.qty > 0),
)
const {
  mutate: submit,
  isPending,
  error,
} = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/documents/transfer', {
      body: {
        fromLocationId: from.value!,
        toLocationId: to.selectedId.value!,
        comment: comment.value.trim() || undefined,
        lines: lines.value.map((l) => ({ variantId: l.variantId, qty: l.qty })),
      },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (doc) => router.replace({ name: 'document', params: { id: doc.id } }),
})
</script>

<template>
  <v-container class="pk-narrow">
    <v-btn :prepend-icon="mdiArrowLeft" variant="text" class="mb-2" @click="router.back()"
      >Назад</v-btn
    >
    <h1 class="text-headline-small mb-4">Заявка на перемещение</h1>
    <v-card class="mb-4">
      <v-card-text>
        <v-autocomplete
          v-model="from"
          :items="fromItems"
          label="Откуда"
          hint="Точка, где товар есть"
          persistent-hint
          class="mb-3"
          data-test="from"
        />
        <v-select v-model="to.selectedId.value" :items="to.items.value" label="Куда — ваша точка" />
        <v-text-field
          v-model="comment"
          label="Причина"
          placeholder="Например, «покупатель ждёт в магазине»"
          maxlength="1000"
        />
      </v-card-text>
    </v-card>

    <v-card class="mb-4">
      <v-card-title>Товары</v-card-title>
      <v-card-text>
        <v-text-field
          v-model="code"
          label="Штрихкод или артикул"
          hint="Можно сканировать ручным сканером"
          persistent-hint
          :error-messages="lookupError"
          @keyup.enter="add(code)"
        />
        <v-table v-if="lines.length" density="comfortable" class="mt-2">
          <tbody>
            <tr v-for="(l, i) in lines" :key="l.variantId" data-test="transfer-line">
              <td>
                <div>{{ l.name }}</div>
                <div class="text-body-small text-medium-emphasis">
                  <span class="pk-code">{{ l.article }}</span> · {{ l.label }}
                </div>
              </td>
              <td class="pk-qty">
                <v-text-field
                  v-model.number="l.qty"
                  type="number"
                  min="1"
                  density="compact"
                  hide-details
                />
              </td>
              <td class="text-end">
                <v-btn
                  :icon="mdiDeleteOutline"
                  variant="text"
                  size="small"
                  title="Убрать"
                  @click="lines.splice(i, 1)"
                />
              </td>
            </tr>
          </tbody>
        </v-table>
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
        data-test="submit-transfer"
        @click="submit()"
        >Отправить заявку</v-btn
      >
    </div>
  </v-container>
</template>

<style scoped>
.pk-narrow {
  max-width: 760px;
}
.pk-qty {
  width: 110px;
}
</style>
