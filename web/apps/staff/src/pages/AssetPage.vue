<script setup lang="ts">
import {
  mdiArrowLeft,
  mdiCameraPlusOutline,
  mdiDeleteOutline,
  mdiFilePdfBox,
  mdiPencilOutline,
  mdiPrinterOutline,
  mdiTruckOutline,
  mdiWrenchOutline,
} from '@mdi/js'
import { api, ApiError, type AssetCard } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'

import AssetFormDialog from '@/components/AssetFormDialog.vue'
import LocationSelect from '@/components/LocationSelect.vue'
import { assetStatusColors, assetStatusLabels, describeEvent } from '@/assets'
import { confirm } from '@/confirm'
import { formatDate, formatDateTime } from '@/format'
import { shrinkPhoto } from '@/photo'
import { openPdf } from '@/print'
import { formatPrice } from '@/products'
import { useLocationChoice } from '@/stock'

// Карточка объекта имущества (ИМ-1) с историей перемещений, смены МОЛ и состояния (ИМ-3) и
// списанием «двумя ключами» (ИМ-5).
const route = useRoute()
const id = computed(() => String(route.params.id))
const key = computed(() => ['asset', id.value])
const queryClient = useQueryClient()

const { data: card, error: loadError } = useQuery({
  queryKey: key,
  queryFn: async () => {
    const { data, error } = await api.GET('/assets/{id}', { params: { path: { id: id.value } } })
    if (error) throw new ApiError(error)
    return data
  },
})
const asset = computed(() => card.value?.asset)
const photoVersion = ref(0)

const actionError = ref<string>()
function act<T>(fn: (arg: T) => Promise<{ data?: AssetCard; error?: unknown }>) {
  return useMutation({
    mutationFn: async (arg: T) => {
      const { data, error } = await fn(arg)
      if (error) throw new ApiError(error as never)
      return data!
    },
    onSuccess: (data) => {
      actionError.value = undefined
      queryClient.setQueryData(key.value, data)
      void queryClient.invalidateQueries({ queryKey: ['assets'] })
    },
    onError: (e) => (actionError.value = e.message),
  })
}
const path = () => ({ params: { path: { id: id.value } } })

const editOpen = ref(false)

// Перемещение и смена МОЛ — одним действием, каждое изменение отдельной строкой истории.
const moveOpen = ref(false)
const target = useLocationChoice('assets.edit')
const moveTo = ref<string>()
const moveResponsible = ref<string | null>(null)
const moveComment = ref('')
function startMove() {
  moveTo.value = asset.value!.locationId
  moveResponsible.value = asset.value!.responsibleId ?? null
  moveComment.value = ''
  moveOpen.value = true
}
const { data: people } = useQuery({
  queryKey: computed(() => ['asset-responsibles', moveTo.value]),
  enabled: computed(() => moveOpen.value && !!moveTo.value),
  queryFn: async () => {
    const { data, error } = await api.GET('/assets/responsibles', {
      params: { query: { locationId: moveTo.value! } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
})
const { mutate: move, isPending: moving } = act(() =>
  api.POST('/assets/{id}/move', {
    ...path(),
    body: {
      version: asset.value!.version,
      locationId: moveTo.value!,
      responsibleId: moveResponsible.value ?? undefined,
      comment: moveComment.value.trim() || undefined,
    },
  }),
)

const { mutate: setStatus, isPending: changing } = act((status: 'in_use' | 'repair') =>
  api.POST('/assets/{id}/status', { ...path(), body: { version: asset.value!.version, status } }),
)

const writeoffOpen = ref(false)
const reason = ref('')
const { mutate: requestWriteoff, isPending: requesting } = act(() =>
  api.POST('/assets/{id}/writeoff', {
    ...path(),
    body: { version: asset.value!.version, reason: reason.value.trim() },
  }),
)
const decisionComment = ref('')
const { mutate: decide, isPending: deciding } = act((decision: 'approve' | 'reject') =>
  api.POST('/assets/{id}/writeoff/{decision}', {
    params: { path: { id: id.value, decision } },
    body: { comment: decisionComment.value.trim() || undefined },
  }),
)
async function askApprove() {
  const ok = await confirm({
    title: `Списать ${asset.value!.number}?`,
    text: `${asset.value!.name} будет снят с учёта; акт о списании можно будет распечатать.`,
    confirmText: 'Утвердить списание',
    danger: true,
  })
  if (ok) decide('approve')
}

const photoInput = ref<HTMLInputElement>()
async function uploadPhoto(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  actionError.value = undefined
  try {
    const { error } = await api.PUT('/assets/{id}/photo', {
      ...path(),
      body: { data: await shrinkPhoto(file) },
    })
    if (error) throw new ApiError(error)
    photoVersion.value++
    void queryClient.invalidateQueries({ queryKey: key.value })
  } catch (err) {
    actionError.value = (err as Error).message
  } finally {
    if (photoInput.value) photoInput.value.value = ''
  }
}

async function printLabel() {
  actionError.value = undefined
  try {
    await openPdf(
      api.POST('/assets/labels', {
        body: { assetIds: [id.value], format: 'thermal' },
        parseAs: 'blob',
      }),
    )
  } catch (e) {
    actionError.value = (e as Error).message
  }
}
</script>

<template>
  <v-container class="pk-asset">
    <v-btn :prepend-icon="mdiArrowLeft" :to="{ name: 'assets' }" variant="text" class="mb-2 ml-n3"
      >Имущество</v-btn
    >
    <v-alert v-if="loadError" type="error">{{ loadError.message }}</v-alert>

    <template v-if="card && asset">
      <div class="d-flex flex-wrap align-center ga-2 mb-1">
        <h1 class="text-headline-small">
          <span class="pk-code">{{ asset.number }}</span> · {{ asset.name }}
        </h1>
        <v-chip :color="assetStatusColors[asset.status]" label data-test="asset-status">{{
          assetStatusLabels[asset.status]
        }}</v-chip>
      </div>
      <p class="text-body-medium text-medium-emphasis mb-4">{{ asset.categoryName }}</p>

      <v-alert
        v-if="card.writeoff?.status === 'pending'"
        type="info"
        variant="tonal"
        class="mb-4"
        data-test="writeoff-pending"
      >
        {{ card.writeoff.requestedByName }} оформил списание
        {{ formatDateTime(card.writeoff.requestedAt) }}: «{{ card.writeoff.reason }}». Объект
        остаётся в учёте, пока списание не утвердит другой сотрудник с правом утверждения.
        <template v-if="card.canApprove">
          <v-text-field
            v-model="decisionComment"
            label="Комментарий"
            density="compact"
            hide-details
            class="mt-3"
          />
          <div class="d-flex ga-2 mt-2">
            <v-btn variant="text" :loading="deciding" @click="decide('reject')">Отклонить</v-btn>
            <v-btn
              color="error"
              variant="flat"
              :loading="deciding"
              data-test="approve-writeoff"
              @click="askApprove"
              >Утвердить списание</v-btn
            >
          </div>
        </template>
      </v-alert>
      <v-alert
        v-else-if="card.writeoff?.status === 'approved'"
        type="warning"
        variant="tonal"
        class="mb-4"
        data-test="written-off"
      >
        Списан {{ formatDateTime(card.writeoff.decidedAt!) }}, утвердил
        {{ card.writeoff.decidedByName }}. Причина: «{{ card.writeoff.reason }}».
        <template #append>
          <v-btn
            :prepend-icon="mdiFilePdfBox"
            variant="text"
            :href="`/api/v1/assets/${asset.id}/writeoff-act`"
            target="_blank"
            >Акт</v-btn
          >
        </template>
      </v-alert>

      <div class="pk-card mb-6">
        <div class="pk-photo">
          <img
            v-if="asset.hasPhoto"
            :src="`/api/v1/assets/${asset.id}/photo?v=${photoVersion}`"
            :alt="asset.name"
          />
          <div v-else class="pk-nophoto text-medium-emphasis">Нет фото</div>
          <template v-if="card.canEdit">
            <input
              ref="photoInput"
              type="file"
              accept="image/*"
              capture="environment"
              hidden
              @change="uploadPhoto"
            />
            <v-btn
              :prepend-icon="mdiCameraPlusOutline"
              size="small"
              variant="text"
              @click="photoInput?.click()"
              >Сфотографировать</v-btn
            >
          </template>
        </div>
        <v-table density="compact" class="pk-props">
          <tbody>
            <tr>
              <th>Точка</th>
              <td>{{ asset.locationCode }} · {{ asset.locationName }}</td>
            </tr>
            <tr>
              <th>МОЛ</th>
              <td>{{ asset.responsibleName ?? 'не назначен' }}</td>
            </tr>
            <tr>
              <th>Серийный номер</th>
              <td>{{ asset.serial || '—' }}</td>
            </tr>
            <tr>
              <th>Принят к учёту</th>
              <td>{{ formatDate(asset.acceptedAt) }}</td>
            </tr>
            <tr>
              <th>Стоимость</th>
              <td>{{ formatPrice(asset.costKopecks / 100) }}</td>
            </tr>
          </tbody>
        </v-table>
      </div>

      <div class="d-flex flex-wrap ga-2 mb-6">
        <v-btn :prepend-icon="mdiPrinterOutline" variant="tonal" @click="printLabel"
          >Наклейка</v-btn
        >
        <template v-if="card.canEdit">
          <v-btn :prepend-icon="mdiPencilOutline" variant="tonal" @click="editOpen = true"
            >Изменить</v-btn
          >
          <v-btn :prepend-icon="mdiTruckOutline" variant="tonal" data-test="move" @click="startMove"
            >Переместить, сменить МОЛ</v-btn
          >
          <v-btn
            :prepend-icon="mdiWrenchOutline"
            variant="tonal"
            :loading="changing"
            data-test="toggle-repair"
            @click="setStatus(asset.status === 'repair' ? 'in_use' : 'repair')"
            >{{
              asset.status === 'repair' ? 'Вернуть в эксплуатацию' : 'Отправить в ремонт'
            }}</v-btn
          >
          <v-btn
            v-if="card.writeoff?.status !== 'pending'"
            :prepend-icon="mdiDeleteOutline"
            variant="text"
            color="error"
            data-test="request-writeoff"
            @click="writeoffOpen = true"
            >Списать</v-btn
          >
        </template>
      </div>
      <v-alert v-if="actionError" type="error" class="mb-4">{{ actionError }}</v-alert>

      <h2 class="text-title-large mb-2">История</h2>
      <v-timeline density="compact" side="end" align="start" data-test="history">
        <v-timeline-item v-for="e in card.events" :key="e.id" size="x-small" dot-color="primary">
          <div>{{ describeEvent(e) }}</div>
          <div class="text-body-small text-medium-emphasis">
            {{ formatDateTime(e.at) }}<template v-if="e.userName"> · {{ e.userName }}</template
            ><template v-if="e.comment"> · «{{ e.comment }}»</template>
          </div>
        </v-timeline-item>
      </v-timeline>

      <AssetFormDialog v-model="editOpen" :asset="asset" />

      <v-dialog v-model="moveOpen" max-width="480">
        <v-card title="Переместить, сменить МОЛ">
          <v-card-text>
            <LocationSelect
              v-model="moveTo"
              :locations="target.options.value"
              label="Точка"
              class="mb-2"
              @update:model-value="moveResponsible = null"
            />
            <v-select
              v-model="moveResponsible"
              :items="(people ?? []).map((p) => ({ value: p.id, title: p.fullName }))"
              label="Материально ответственное лицо"
              clearable
              class="mb-2"
            />
            <v-text-field v-model="moveComment" label="Основание" />
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn variant="text" @click="moveOpen = false">Отмена</v-btn>
            <v-btn
              color="primary"
              variant="flat"
              :loading="moving"
              data-test="save-move"
              @click="move(undefined, { onSuccess: () => (moveOpen = false) })"
              >Сохранить</v-btn
            >
          </v-card-actions>
        </v-card>
      </v-dialog>

      <v-dialog v-model="writeoffOpen" max-width="480">
        <v-card title="Списание объекта">
          <v-card-text>
            <p class="mb-3">
              Списание вступит в силу после утверждения другим сотрудником с правом утверждения.
            </p>
            <v-textarea
              v-model="reason"
              label="Причина"
              rows="3"
              auto-grow
              data-test="writeoff-reason"
            />
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn variant="text" @click="writeoffOpen = false">Отмена</v-btn>
            <v-btn
              color="error"
              variant="flat"
              :disabled="!reason.trim()"
              :loading="requesting"
              data-test="submit-writeoff"
              @click="requestWriteoff(undefined, { onSuccess: () => (writeoffOpen = false) })"
              >Оформить</v-btn
            >
          </v-card-actions>
        </v-card>
      </v-dialog>
    </template>
  </v-container>
</template>

<style scoped>
.pk-asset {
  max-width: 960px;
}
.pk-card {
  display: grid;
  grid-template-columns: 220px 1fr;
  gap: 16px;
  align-items: start;
}
@media (max-width: 600px) {
  .pk-card {
    grid-template-columns: 1fr;
  }
}
.pk-photo img,
.pk-nophoto {
  width: 100%;
  aspect-ratio: 4 / 3;
  object-fit: cover;
  border-radius: 8px;
  background: rgb(var(--v-theme-surface-light));
}
.pk-nophoto {
  display: flex;
  align-items: center;
  justify-content: center;
}
.pk-props th {
  width: 40%;
  font-weight: normal;
  color: rgba(var(--v-theme-on-surface), var(--v-medium-emphasis-opacity));
}
</style>
