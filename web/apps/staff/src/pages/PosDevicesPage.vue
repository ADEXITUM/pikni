<script setup lang="ts">
import { mdiCancel, mdiPlus } from '@mdi/js'
import { api, ApiError, type PosDevice, type PosDeviceKey } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import ConfirmDialog from '@/components/ConfirmDialog.vue'
import PosDeviceKeyDialog from '@/components/PosDeviceKeyDialog.vue'
import { formatDateTime } from '@/format'
import { networkRoles } from '@/labels'
import { locationsQuery } from '@/queries'
import { useMe } from '@/session'

const queryClient = useQueryClient()
const { data: me } = useMe()
const { data: locations } = useQuery(locationsQuery)
// Управляющий регистрирует кассы только своего магазина; сервер всё равно проверит точку.
const stores = computed(() =>
  (locations.value ?? []).filter(
    (l) =>
      l.kind === 'store' &&
      l.isActive &&
      (!me.value || networkRoles.has(me.value.role) || me.value.locationIds.includes(l.id)),
  ),
)

const devicesKey = ['pos-devices']
const {
  data: devices,
  isPending,
  error,
} = useQuery({
  queryKey: devicesKey,
  queryFn: async () => {
    const { data, error } = await api.GET('/pos-devices')
    if (error) throw new ApiError(error)
    return data
  },
})

const headers = [
  { title: 'Касса', key: 'name' },
  {
    title: 'Точка',
    key: 'locationCode',
    value: (d: PosDevice) => `${d.locationCode} · ${d.locationName}`,
  },
  { title: 'Ключ', key: 'keyPrefix', sortable: false },
  { title: 'На связи', key: 'lastSeenAt' },
  { title: '', key: 'actions', sortable: false, align: 'end' as const },
]

const formOpen = ref(false)
const location = ref<string | null>(null)
const name = ref('Касса 1')
const issued = ref<PosDeviceKey | null>(null)

const refresh = () => queryClient.invalidateQueries({ queryKey: devicesKey })

const create = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/pos-devices', {
      body: { locationId: location.value!, name: name.value.trim() },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (data) => {
    formOpen.value = false
    issued.value = data
    void refresh()
  },
})

const revoking = ref<PosDevice | null>(null)
const revokeOpen = computed({
  get: () => revoking.value !== null,
  set: (open) => {
    if (!open) revoking.value = null
  },
})
const revoke = useMutation({
  mutationFn: async (d: PosDevice) => {
    const { error } = await api.POST('/pos-devices/{id}/revoke', { params: { path: { id: d.id } } })
    if (error) throw new ApiError(error)
  },
  onSuccess: () => (revoking.value = null),
  onSettled: refresh,
})

function openForm() {
  location.value = stores.value.length === 1 ? stores.value[0]!.id : null
  name.value = 'Касса 1'
  create.reset()
  formOpen.value = true
}
</script>

<template>
  <v-container>
    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <h1 class="text-headline-small flex-grow-1">Кассы</h1>
      <v-btn color="primary" :prepend-icon="mdiPlus" data-test="add-device" @click="openForm()">
        Зарегистрировать
      </v-btn>
    </div>
    <p class="text-body-medium text-medium-emphasis mb-4">
      Касса входит в систему по ключу, который выдаётся при регистрации. Кассир на кассе входит по
      логину и PIN — PIN задаётся в разделе «Сотрудники».
    </p>

    <v-alert v-if="error" type="error" class="mb-4">{{ error.message }}</v-alert>
    <v-alert v-if="revoke.error.value" type="error" class="mb-4">
      {{ revoke.error.value.message }}
    </v-alert>

    <v-card>
      <v-data-table
        :headers="headers"
        :items="devices ?? []"
        :loading="isPending"
        :items-per-page="-1"
        item-value="id"
        mobile-breakpoint="sm"
        no-data-text="Касс пока нет"
        hide-default-footer
      >
        <template #[`item.name`]="{ item }">
          <span :class="{ 'text-disabled': item.revokedAt }">{{ item.name }}</span>
          <v-chip v-if="item.revokedAt" size="x-small" class="ml-2">отключена</v-chip>
        </template>
        <template #[`item.keyPrefix`]="{ value }">
          <span class="pk-code">pos_{{ value }}_…</span>
        </template>
        <template #[`item.lastSeenAt`]="{ value }">
          {{ value ? formatDateTime(value) : '—' }}
        </template>
        <template #[`item.actions`]="{ item }">
          <v-btn
            v-if="!item.revokedAt"
            :prepend-icon="mdiCancel"
            variant="text"
            size="small"
            color="error"
            @click="revoking = item"
          >
            Отключить
          </v-btn>
        </template>
      </v-data-table>
    </v-card>

    <v-dialog v-model="formOpen" max-width="440">
      <v-card title="Регистрация кассы">
        <v-card-text>
          <v-select
            v-model="location"
            :items="stores"
            :item-title="(l) => `${l.code} · ${l.name}`"
            item-value="id"
            label="Магазин"
            class="mb-2"
          />
          <v-text-field
            v-model="name"
            label="Название"
            hint="Например, «Касса 1»"
            persistent-hint
          />
          <v-alert v-if="create.error.value" type="error" density="compact" class="mt-4">
            {{ create.error.value.message }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="formOpen = false">Отмена</v-btn>
          <v-btn
            color="primary"
            :disabled="!location || !name.trim()"
            :loading="create.isPending.value"
            @click="create.mutate()"
          >
            Зарегистрировать
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <PosDeviceKeyDialog v-model="issued" />
    <ConfirmDialog
      v-model="revokeOpen"
      title="Отключить кассу?"
      :text="`Ключ кассы «${revoking?.name}» сразу перестанет действовать. Чтобы снова работать на ней, кассу регистрируют заново.`"
      confirm-text="Отключить"
      :loading="revoke.isPending.value"
      @confirm="revoking && revoke.mutate(revoking)"
    />
  </v-container>
</template>
