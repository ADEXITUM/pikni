<script setup lang="ts">
import { api, ApiError } from '@pikni/api'
import { useMutation, useQuery } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'

import { categoryItems } from '@/categories'
import { zoneLabels } from '@/labels'
import { brandsQuery, categoriesQuery } from '@/queries'
import { useLocationChoice } from '@/stock'

// Ревизия полная или выборочная (СО-6): пустой выбор зон, категорий и брендов — вся точка.
const open = defineModel<boolean>({ required: true })
const router = useRouter()
const place = useLocationChoice('stocktake.manage')
const zoneIds = ref<string[]>([])
const categoryIds = ref<string[]>([])
const brandIds = ref<string[]>([])
const comment = ref('')
// По умолчанию касса работает: проданное во время подсчёта сличение учитывает (решение 0060).
const stopSales = ref(false)

const { data: categories } = useQuery({ ...categoriesQuery, enabled: open })
const { data: brands } = useQuery({ ...brandsQuery, enabled: open })
const zoneItems = computed(() =>
  (place.selected.value?.zones ?? []).map((z) => ({ value: z.id, title: zoneLabels[z.kind] })),
)
const brandItems = computed(() => (brands.value ?? []).map((b) => ({ value: b.id, title: b.name })))
const scopeText = computed(() =>
  zoneIds.value.length || categoryIds.value.length || brandIds.value.length
    ? 'Выборочная ревизия: останавливается и считается только выбранное.'
    : 'Полная ревизия: все зоны и все товары точки.',
)

const {
  mutate: create,
  isPending,
  error,
  reset,
} = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/documents/stocktake', {
      body: {
        locationId: place.selectedId.value!,
        zoneIds: zoneIds.value.length ? zoneIds.value : undefined,
        categoryIds: categoryIds.value.length ? categoryIds.value : undefined,
        brandIds: brandIds.value.length ? brandIds.value : undefined,
        comment: comment.value.trim() || undefined,
        stopSales: stopSales.value || undefined,
      },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (doc) => router.push({ name: 'document', params: { id: doc.id } }),
})
</script>

<template>
  <v-dialog v-model="open" max-width="520" @after-leave="reset()">
    <v-card title="Ревизия">
      <v-card-text>
        <v-select
          v-model="place.selectedId.value"
          :items="place.items.value"
          label="Точка"
          class="mb-2"
          @update:model-value="zoneIds = []"
        />
        <v-select
          v-model="zoneIds"
          :items="zoneItems"
          label="Зоны"
          hint="Пусто — все зоны"
          persistent-hint
          multiple
          chips
          closable-chips
          class="mb-2"
          data-test="stocktake-zones"
        />
        <v-autocomplete
          v-model="categoryIds"
          :items="categories ? categoryItems(categories.categories) : []"
          label="Категории"
          hint="Пусто — все товары"
          persistent-hint
          multiple
          chips
          closable-chips
          class="mb-2"
        />
        <v-autocomplete
          v-model="brandIds"
          :items="brandItems"
          label="Бренды"
          multiple
          chips
          closable-chips
          class="mb-2"
        />
        <v-text-field v-model="comment" label="Основание, комментарий" class="mb-2" />
        <v-checkbox
          v-model="stopSales"
          label="Остановить и продажи на время подсчёта"
          hint="Для годовой инвентаризации. Обычно касса работает, а сличение само учитывает проданное"
          persistent-hint
          density="compact"
          class="mb-2"
        />
        <p class="text-body-medium text-medium-emphasis">{{ scopeText }}</p>
        <v-alert v-if="error" type="error" class="mt-2">{{ error.message }}</v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="open = false">Отмена</v-btn>
        <v-btn
          color="primary"
          variant="flat"
          :disabled="!place.selectedId.value"
          :loading="isPending"
          data-test="create-stocktake"
          @click="create()"
          >Создать</v-btn
        >
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
