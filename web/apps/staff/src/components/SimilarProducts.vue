<script setup lang="ts">
import { api, ApiError, type SimilarItem } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed } from 'vue'

import { formatPrice } from '@/products'

// Похожие товары (НЛ-6): продавец предлагает замену, если нужного нет. У каждого — наличие
// здесь и по сети; нажатие открывает карточку наличия.
const props = defineProps<{ variantId: string; here?: string | null }>()

const { data, isPending } = useQuery({
  queryKey: computed(() => ['similar', props.variantId, props.here ?? null]),
  queryFn: async () => {
    const { data, error } = await api.GET('/availability/variants/{variantId}/similar', {
      params: { path: { variantId: props.variantId }, query: { here: props.here ?? undefined } },
    })
    if (error) throw new ApiError(error)
    return data
  },
})

const groups = computed(() =>
  data.value
    ? [
        { key: 'pack', title: 'Другой объём или тара', items: data.value.pack },
        { key: 'line', title: 'Другие разновидности', items: data.value.line },
        { key: 'analogs', title: 'Аналоги в наличии', items: data.value.analogs },
      ].filter((g) => g.items.length > 0)
    : [],
)

const stock = (i: SimilarItem) =>
  props.here ? `здесь ${i.hereQty} · в сети ${i.networkQty}` : `в сети ${i.networkQty}`
</script>

<template>
  <div data-test="similar">
    <v-progress-linear v-if="isPending" indeterminate class="mb-2" />
    <p v-else-if="groups.length === 0" class="text-body-medium text-medium-emphasis">
      Похожих товаров нет.
    </p>
    <section v-for="g in groups" :key="g.key" class="mb-4" :data-test="`similar-${g.key}`">
      <div class="text-title-small mb-2">{{ g.title }}</div>
      <div class="pk-strip">
        <v-card
          v-for="i in g.items"
          :key="i.variantId"
          :to="{ name: 'availability-card', params: { productId: i.productId } }"
          variant="outlined"
          class="pk-similar"
        >
          <v-card-text class="pa-3">
            <div class="text-body-medium font-weight-medium pk-name">{{ i.name }}</div>
            <div class="text-body-small text-medium-emphasis">{{ i.label }}</div>
            <div class="d-flex justify-space-between align-end mt-2">
              <span class="text-body-medium">{{ i.price ? formatPrice(i.price) : '—' }}</span>
              <span
                class="text-body-small"
                :class="i.hereQty > 0 || (!here && i.networkQty > 0) ? 'text-success' : ''"
                >{{ stock(i) }}</span
              >
            </div>
          </v-card-text>
        </v-card>
      </div>
    </section>
  </div>
</template>

<style scoped>
.pk-strip {
  display: flex;
  gap: 8px;
  overflow-x: auto;
  padding-bottom: 4px;
}
.pk-similar {
  flex: 0 0 200px;
}
.pk-name {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
