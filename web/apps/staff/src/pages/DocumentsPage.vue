<script setup lang="ts">
import { mdiPlus, mdiStorefrontOutline, mdiTransferRight } from '@mdi/js'
import {
  api,
  ApiError,
  type DocumentKind,
  type DocumentStatus,
  type FloorDirection,
} from '@pikni/api'
import { useInfiniteQuery, useMutation } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'

import { formatDateTime } from '@/format'
import {
  documentKindLabels,
  documentStatusColors,
  documentStatusLabels,
  zoneLabels,
} from '@/labels'
import { useLocationChoice } from '@/stock'

const router = useRouter()

const kind = ref<DocumentKind | null>(null)
const status = ref<DocumentStatus | null>(null)
const kindItems = Object.entries(documentKindLabels).map(([value, title]) => ({ value, title }))
const statusItems = Object.entries(documentStatusLabels).map(([value, title]) => ({ value, title }))

const { data, isPending, hasNextPage, fetchNextPage, isFetchingNextPage, error } = useInfiniteQuery(
  {
    queryKey: ['documents', kind, status],
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET('/documents', {
        params: {
          query: {
            kind: kind.value ?? undefined,
            status: status.value ?? undefined,
            after: pageParam,
          },
        },
      })
      if (error) throw new ApiError(error)
      return data
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.nextAfter,
  },
)
const items = computed(() => data.value?.pages.flatMap((p) => p.items) ?? [])

// Ввод остатков — по зоне точки; передача — между складом магазина и залом.
const opening = useLocationChoice('stocktake.manage')
const openingZone = ref<string>()
const openingOpen = ref(false)
const zoneItems = computed(() =>
  (opening.selected.value?.zones ?? []).map((z) => ({ value: z.id, title: zoneLabels[z.kind] })),
)
function startOpening() {
  openingZone.value = zoneItems.value[0]?.value
  openingOpen.value = true
}

const transfer = useLocationChoice('floor.edit', (l) => l.kind === 'store')
const direction = ref<FloorDirection>('to_floor')
const transferOpen = ref(false)

const {
  mutate: create,
  isPending: creating,
  error: createError,
  reset,
} = useMutation({
  mutationFn: async (what: 'opening' | 'transfer') => {
    const { data, error } =
      what === 'opening'
        ? await api.POST('/documents/opening', { body: { zoneId: openingZone.value! } })
        : await api.POST('/documents/floor-transfer', {
            body: { locationId: transfer.selectedId.value!, direction: direction.value },
          })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (doc) => router.push({ name: 'document', params: { id: doc.id } }),
})

const zones = (d: (typeof items.value)[number]) =>
  d.destZoneKind
    ? `${zoneLabels[d.zoneKind]} → ${zoneLabels[d.destZoneKind]}`
    : zoneLabels[d.zoneKind]
</script>

<template>
  <v-container class="pk-documents">
    <h1 class="text-headline-small mb-1">Документы</h1>
    <p class="text-body-medium text-medium-emphasis mb-4">
      Остатки меняются только проведением документа. Черновик можно сканировать с нескольких
      устройств, проведённый документ не меняется.
    </p>

    <div class="d-flex flex-wrap ga-2 mb-4">
      <v-btn
        v-if="opening.options.value.length"
        :prepend-icon="mdiPlus"
        color="primary"
        variant="flat"
        data-test="new-opening"
        @click="startOpening"
      >
        Ввод остатков
      </v-btn>
      <v-btn
        v-if="transfer.options.value.length"
        :prepend-icon="mdiTransferRight"
        variant="tonal"
        data-test="new-transfer"
        @click="transferOpen = true"
      >
        Передача в зал
      </v-btn>
    </div>

    <div class="d-flex flex-wrap ga-2 mb-3">
      <v-select
        v-model="kind"
        :items="kindItems"
        label="Вид"
        clearable
        density="compact"
        hide-details
        class="pk-filter"
      />
      <v-select
        v-model="status"
        :items="statusItems"
        label="Статус"
        clearable
        density="compact"
        hide-details
        class="pk-filter"
      />
    </div>

    <v-alert v-if="error" type="error" class="mb-3">{{ error.message }}</v-alert>
    <v-progress-linear v-if="isPending" indeterminate />
    <p v-else-if="!items.length" class="text-body-medium text-medium-emphasis">
      Документов пока нет.
    </p>

    <v-list v-else lines="three" class="pk-list" data-test="documents">
      <v-list-item
        v-for="d in items"
        :key="d.id"
        :to="{ name: 'document', params: { id: d.id } }"
        data-test="document"
      >
        <v-list-item-title class="text-title-medium">
          {{ documentKindLabels[d.kind] }} <span class="pk-code">{{ d.number }}</span>
        </v-list-item-title>
        <v-list-item-subtitle>
          <v-icon :icon="mdiStorefrontOutline" size="16" /> {{ d.locationName }} · {{ zones(d) }}
        </v-list-item-subtitle>
        <v-list-item-subtitle>
          {{ formatDateTime(d.createdAt) }} · {{ d.authorName }} · строк {{ d.lineCount }},
          {{ d.totalQty }} шт.
        </v-list-item-subtitle>
        <template #append>
          <v-chip :color="documentStatusColors[d.status]" size="small" label>
            {{ documentStatusLabels[d.status] }}
          </v-chip>
        </template>
      </v-list-item>
    </v-list>
    <v-btn
      v-if="hasNextPage"
      :loading="isFetchingNextPage"
      variant="text"
      class="mt-2"
      @click="fetchNextPage()"
    >
      Показать ещё
    </v-btn>

    <v-dialog v-model="openingOpen" max-width="480" @after-leave="reset()">
      <v-card title="Ввод начальных остатков">
        <v-card-text>
          <p class="text-body-medium mb-4">
            Один документ на зону: сосчитайте товар сканированием, бутылки с марками — по маркам.
            Провести документ может руководитель или бухгалтер.
          </p>
          <v-select
            v-model="opening.selectedId.value"
            :items="opening.items.value"
            label="Точка"
            class="mb-2"
            @update:model-value="openingZone = zoneItems[0]?.value"
          />
          <v-select v-model="openingZone" :items="zoneItems" label="Зона" />
          <v-alert v-if="createError" type="error" class="mt-2">{{ createError.message }}</v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="openingOpen = false">Отмена</v-btn>
          <v-btn
            color="primary"
            variant="flat"
            :disabled="!openingZone"
            :loading="creating"
            data-test="create"
            @click="create('opening')"
          >
            Создать
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="transferOpen" max-width="480" @after-leave="reset()">
      <v-card title="Передача товара">
        <v-card-text>
          <v-select
            v-model="transfer.selectedId.value"
            :items="transfer.items.value"
            label="Магазин"
            class="mb-2"
          />
          <v-btn-toggle v-model="direction" mandatory color="primary" variant="outlined" divided>
            <v-btn value="to_floor">Со склада в зал</v-btn>
            <v-btn value="from_floor">Из зала на склад</v-btn>
          </v-btn-toggle>
          <v-alert v-if="createError" type="error" class="mt-4">{{ createError.message }}</v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="transferOpen = false">Отмена</v-btn>
          <v-btn
            color="primary"
            variant="flat"
            :disabled="!transfer.selectedId.value"
            :loading="creating"
            data-test="create"
            @click="create('transfer')"
          >
            Создать
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>

<style scoped>
.pk-documents {
  max-width: 960px;
}
.pk-filter {
  max-width: 240px;
}
</style>
