<script setup lang="ts">
import { mdiBookmarkPlusOutline } from '@mdi/js'
import { api, ApiError, type Reservation, type ReservationStatus } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import { formatDateTime } from '@/format'
import { useLive } from '@/live'
import { useScanner } from '@/scanning'
import { useLocationChoice } from '@/stock'
import LocationSelect from '@/components/LocationSelect.vue'

// Брони покупателей (НЛ-4): оформленные ждут подтверждения сканом отложенной бутылки,
// подтверждённые выдаются через кассу, просроченные снимаются сами.
const place = useLocationChoice('reservations.manage', (l) => l.kind === 'store')
const openOnly = ref(true)
const queryClient = useQueryClient()
const key = computed(() => ['reservations', place.selectedId.value ?? null, openOnly.value])
const { data, isPending, error } = useQuery({
  queryKey: key,
  queryFn: async () => {
    const { data, error } = await api.GET('/reservations', {
      params: { query: { locationId: place.selectedId.value, open: openOnly.value } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
  enabled: computed(() => !!place.selectedId.value),
})
useLive(
  () => (place.selectedId.value ? [`location:${place.selectedId.value}`] : []),
  [['reservations']],
)

const statusLabels: Record<ReservationStatus, string> = {
  new: 'Ждёт подтверждения',
  confirmed: 'Отложено',
  issued: 'Выдана',
  cancelled: 'Снята',
  expired: 'Истекла',
}
const statusColors: Record<ReservationStatus, string> = {
  new: 'info',
  confirmed: 'success',
  issued: 'secondary',
  cancelled: 'secondary',
  expired: 'warning',
}

// Подтверждение: марка каждой отложенной бутылки или штрихкод товара.
const confirming = ref<Reservation | null>(null)
const codes = ref<string[]>([])
const code = ref('')
const marked = computed(() => confirming.value?.accounting === 'marked')
const need = computed(() => (marked.value ? (confirming.value?.qty ?? 0) : 1))
function addCode(raw: string) {
  const v = raw.trim()
  if (!confirming.value || !v || codes.value.includes(v) || codes.value.length >= need.value) return
  codes.value.push(v)
  code.value = ''
}
useScanner(({ event }) => addCode(event.raw))

const actionError = ref('')
const refresh = () => queryClient.invalidateQueries({ queryKey: ['reservations'] })
const { mutate: confirmIt, isPending: confirmBusy } = useMutation({
  mutationFn: async () => {
    const { error } = await api.POST('/reservations/{id}/confirm', {
      params: { path: { id: confirming.value!.id } },
      body: { codes: codes.value },
    })
    if (error) throw new ApiError(error)
  },
  onSuccess: () => {
    confirming.value = null
    void refresh()
  },
  onError: (e) => (actionError.value = e.message),
})
const { mutate: cancel } = useMutation({
  mutationFn: async (r: Reservation) => {
    const { error } = await api.POST('/reservations/{id}/cancel', {
      params: { path: { id: r.id } },
    })
    if (error) throw new ApiError(error)
  },
  onSettled: refresh,
  onError: (e) => (actionError.value = e.message),
})
function startConfirm(r: Reservation) {
  confirming.value = r
  codes.value = []
  actionError.value = ''
}
</script>

<template>
  <v-container>
    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <h1 class="text-headline-small flex-grow-1">Брони</h1>
      <v-btn
        color="primary"
        :prepend-icon="mdiBookmarkPlusOutline"
        :to="{ name: 'reservation-new', query: { location: place.selectedId.value } }"
        data-test="new-reservation"
        >Отложить товар</v-btn
      >
    </div>
    <div class="d-flex flex-wrap align-center ga-3 mb-3">
      <LocationSelect
        v-model="place.selectedId.value"
        :locations="place.options.value"
        label="Магазин"
        density="compact"
        hide-details
        class="pk-place"
      />
      <v-switch v-model="openOnly" label="Только действующие" color="primary" hide-details />
    </div>
    <v-alert v-if="error" type="error" class="mb-3">{{ error.message }}</v-alert>
    <v-alert v-if="actionError" type="error" class="mb-3" closable @click:close="actionError = ''">
      {{ actionError }}
    </v-alert>

    <v-card>
      <v-progress-linear v-if="isPending" indeterminate />
      <v-list lines="three">
        <v-list-item v-for="r in data ?? []" :key="r.id" data-test="reservation">
          <v-list-item-title>
            <span class="pk-code">{{ r.number }}</span> · {{ r.name }}, {{ r.label }} ×
            {{ r.qty }}
          </v-list-item-title>
          <v-list-item-subtitle>
            {{ r.customerName }} · {{ r.customerPhone }} · до {{ formatDateTime(r.expiresAt) }}
            <template v-if="r.receiptNumber"> · чек № {{ r.receiptNumber }}</template>
          </v-list-item-subtitle>
          <v-list-item-subtitle v-if="r.marks.length" class="pk-code">
            {{ r.marks.map((m) => '…' + m.slice(-8)).join(', ') }}
          </v-list-item-subtitle>
          <template #append>
            <div class="d-flex flex-column align-end ga-2">
              <v-chip :color="statusColors[r.status]" size="small">{{
                statusLabels[r.status]
              }}</v-chip>
              <div class="d-flex ga-1">
                <v-btn
                  v-if="r.status === 'new'"
                  size="small"
                  color="primary"
                  variant="tonal"
                  data-test="confirm-reservation"
                  @click="startConfirm(r)"
                  >Подтвердить</v-btn
                >
                <v-btn
                  v-if="r.status === 'new' || r.status === 'confirmed'"
                  size="small"
                  variant="text"
                  @click="cancel(r)"
                  >Снять</v-btn
                >
              </div>
            </div>
          </template>
        </v-list-item>
        <v-list-item v-if="!isPending && !(data ?? []).length">
          <v-list-item-title class="text-medium-emphasis">Броней нет</v-list-item-title>
        </v-list-item>
      </v-list>
    </v-card>

    <v-dialog :model-value="!!confirming" max-width="480" @update:model-value="confirming = null">
      <v-card v-if="confirming" :title="`Подтвердить ${confirming.number}`">
        <v-card-text>
          <p class="mb-3">
            Отложите товар и отсканируйте
            {{
              marked
                ? `марку каждой бутылки (${confirming.qty} шт.)`
                : 'штрихкод товара — количество возьмётся из брони'
            }}.
          </p>
          <v-text-field
            v-model="code"
            label="Скан или код"
            autofocus
            data-test="reservation-code"
            @keyup.enter="addCode(code)"
          />
          <v-chip
            v-for="c in codes"
            :key="c"
            class="ma-1"
            closable
            @click:close="codes.splice(codes.indexOf(c), 1)"
          >
            …{{ c.slice(-10) }}
          </v-chip>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="confirming = null">Отмена</v-btn>
          <v-btn
            color="primary"
            variant="flat"
            :disabled="codes.length !== need"
            :loading="confirmBusy"
            data-test="confirm-codes"
            @click="confirmIt()"
            >Подтвердить</v-btn
          >
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>

<style scoped>
.pk-place {
  max-width: 320px;
}
</style>
