<script setup lang="ts">
import { mdiBarcode, mdiPencilOutline, mdiPlus, mdiTableLarge } from '@mdi/js'
import { api, ApiError, type Variant } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import { confirm } from '@/confirm'
import BarcodeDialog from '@/components/BarcodeDialog.vue'
import PricesDialog from '@/components/PricesDialog.vue'
import VariantDialog from '@/components/VariantDialog.vue'
import VariantGenerateDialog from '@/components/VariantGenerateDialog.vue'
import { formatPrice, variantLabel } from '@/products'

const props = defineProps<{
  productId: string
  isEgais: boolean
  canEdit: boolean
  showCost: boolean
}>()

const queryClient = useQueryClient()
const key = computed(() => ['variants', props.productId])
const {
  data: variants,
  isPending,
  isError,
  error,
} = useQuery({
  queryKey: key,
  queryFn: async () => {
    const { data, error } = await api.GET('/products/{id}/variants', {
      params: { path: { id: props.productId } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
})
const refresh = () => queryClient.invalidateQueries({ queryKey: key.value })

const variantOpen = ref(false)
const generateOpen = ref(false)
const barcodeOpen = ref(false)
const pricesOpen = ref(false)
const target = ref<Variant>()
const generated = ref<number>()

function edit(v?: Variant) {
  target.value = v
  variantOpen.value = true
}
function addBarcode(v: Variant) {
  target.value = v
  barcodeOpen.value = true
}
function openPrices(v: Variant) {
  target.value = v
  pricesOpen.value = true
}
// Цена ниже МРЦ бывает, если МРЦ подняли после назначения цены: касса такую продажу не пропустит.
const belowMin = (v: Variant) =>
  v.retailPrice !== undefined && v.minPrice !== undefined && v.retailPrice < v.minPrice
function onGenerated(n: number) {
  generated.value = n
  refresh()
}

const { mutate: removeBarcode, error: removeError } = useMutation({
  mutationFn: async (code: string) => {
    const { error } = await api.DELETE('/barcodes/{code}', { params: { path: { code } } })
    if (error) throw new ApiError(error)
  },
  onSettled: refresh,
})

async function askRemoveBarcode(v: Variant, code: string) {
  const ok = await confirm({
    title: 'Удалить штрихкод?',
    text: `Штрихкод ${code} отвяжется от варианта ${v.article} · ${variantLabel(v)}: сканер перестанет его узнавать.`,
    danger: true,
  })
  if (ok) removeBarcode(code)
}
</script>

<template>
  <v-card class="mb-4" data-test="variants">
    <v-card-item title="Варианты" subtitle="Что стоит на полке и пробивается на кассе" />

    <v-card-text>
      <div v-if="canEdit" class="d-flex flex-wrap ga-2 mb-3">
        <v-btn
          size="small"
          variant="tonal"
          :prepend-icon="mdiPlus"
          data-test="variant-add"
          @click="edit()"
        >
          Вариант
        </v-btn>
        <v-btn
          size="small"
          variant="tonal"
          :prepend-icon="mdiTableLarge"
          data-test="variants-generate"
          @click="generateOpen = true"
        >
          По списку объёмов
        </v-btn>
      </div>
      <v-alert v-if="isError" type="error" class="mb-3">{{ error?.message }}</v-alert>
      <v-alert v-if="removeError" type="error" class="mb-3">{{ removeError.message }}</v-alert>
      <v-alert
        v-if="generated !== undefined"
        type="success"
        variant="tonal"
        closable
        class="mb-3"
        @click:close="generated = undefined"
      >
        {{ generated ? `Создано вариантов: ${generated}` : 'Все эти сочетания уже были заведены' }}
      </v-alert>

      <v-progress-linear v-if="isPending" indeterminate />
      <p v-else-if="!variants?.length" class="text-body-medium text-medium-emphasis">
        Вариантов пока нет. Без варианта позицию нельзя принять на склад и продать.
      </p>
      <v-table v-else density="comfortable">
        <thead>
          <tr>
            <th>Артикул</th>
            <th>Вариант</th>
            <th v-if="isEgais">Алкокод</th>
            <th>GTIN</th>
            <th>Штрихкоды</th>
            <th class="text-end">Цена</th>
            <th v-if="showCost" class="text-end">Закупка</th>
            <th v-if="canEdit"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="v in variants"
            :key="v.id"
            :class="{ 'text-disabled': !v.isActive }"
            :data-test="`variant-${v.id}`"
          >
            <td class="text-no-wrap">{{ v.article }}</td>
            <td class="pk-variant">
              {{ variantLabel(v) }}
              <v-chip v-if="!v.isActive" size="x-small" class="ml-1">в архиве</v-chip>
            </td>
            <td v-if="isEgais" class="text-no-wrap">{{ v.alcocode ?? '—' }}</td>
            <td class="text-no-wrap">{{ v.gtin ?? '—' }}</td>
            <td>
              <div class="d-flex flex-wrap ga-1 py-1">
                <v-chip
                  v-for="b in v.barcodes"
                  :key="b.code"
                  size="small"
                  :closable="canEdit"
                  :title="b.kind === 'internal' ? 'Внутренний штрихкод' : undefined"
                  :data-test="`barcode-${b.code}`"
                  @click:close="askRemoveBarcode(v, b.code)"
                >
                  {{ b.code }}
                </v-chip>
                <span v-if="!v.barcodes.length" class="text-medium-emphasis">—</span>
              </div>
            </td>
            <td class="text-end text-no-wrap">
              <v-btn
                variant="text"
                size="small"
                :color="belowMin(v) ? 'error' : undefined"
                title="История цен"
                data-test="variant-price"
                @click="openPrices(v)"
              >
                {{
                  v.retailPrice !== undefined
                    ? formatPrice(v.retailPrice)
                    : canEdit
                      ? 'назначить'
                      : '—'
                }}
              </v-btn>
              <div v-if="belowMin(v)" class="text-body-small text-error" data-test="below-min">
                ниже МРЦ {{ formatPrice(v.minPrice!) }}
              </div>
            </td>
            <td v-if="showCost" class="text-end text-no-wrap">
              {{ v.purchasePrice !== undefined ? formatPrice(v.purchasePrice) : '—' }}
            </td>
            <td v-if="canEdit" class="text-end text-no-wrap">
              <v-btn
                :icon="mdiBarcode"
                variant="text"
                size="small"
                title="Привязать штрихкод"
                data-test="barcode-add"
                @click="addBarcode(v)"
              />
              <v-btn
                :icon="mdiPencilOutline"
                variant="text"
                size="small"
                title="Изменить вариант"
                @click="edit(v)"
              />
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card-text>

    <VariantDialog
      v-model="variantOpen"
      :product-id="productId"
      :is-egais="isEgais"
      :variant="target"
      @saved="refresh"
    />
    <VariantGenerateDialog v-model="generateOpen" :product-id="productId" @saved="onGenerated" />
    <BarcodeDialog v-model="barcodeOpen" :variant="target" @saved="refresh" />
    <PricesDialog
      v-model="pricesOpen"
      :variant="target"
      :can-edit="canEdit"
      :show-purchase="showCost"
      @saved="refresh"
    />
  </v-card>
</template>

<style scoped>
.pk-variant {
  min-width: 150px;
}
</style>
