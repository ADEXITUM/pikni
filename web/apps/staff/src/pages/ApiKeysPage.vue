<script setup lang="ts">
import { mdiCheck, mdiContentCopy, mdiKeyPlus } from '@mdi/js'
import { api, ApiError, type ApiKey } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'

import { confirm } from '@/confirm'
import { formatDateTime } from '@/format'

// Ключи API внешних систем (ИН-2): ограниченные права, журнал обращений, отзыв.
const queryClient = useQueryClient()
const { data: keys, error } = useQuery({
  queryKey: ['api-keys'],
  queryFn: async () => {
    const { data, error } = await api.GET('/api-keys')
    if (error) throw new ApiError(error)
    return data.items
  },
})
const scopeLabels: Record<ApiKey['scopes'][number], string> = {
  availability: 'Наличие',
  catalog: 'Каталог',
  import: 'Импорт номенклатуры',
}

const open = ref(false)
const name = ref('')
const scopes = ref<ApiKey['scopes']>(['availability'])
const issued = ref<string>()
const copied = ref(false)
const {
  mutate: create,
  isPending,
  error: createError,
} = useMutation({
  mutationFn: async () => {
    const { data, error } = await api.POST('/api-keys', {
      body: { name: name.value, scopes: scopes.value },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (data) => {
    issued.value = data.key
    copied.value = false
    name.value = ''
    void queryClient.invalidateQueries({ queryKey: ['api-keys'] })
  },
})
async function copy() {
  await navigator.clipboard.writeText(issued.value!)
  copied.value = true
}

async function revoke(k: ApiKey) {
  const ok = await confirm({
    title: `Отозвать ключ «${k.name}»?`,
    text: 'Внешняя система сразу потеряет доступ. Отзыв не отменяется — понадобится новый ключ.',
    confirmText: 'Отозвать',
    danger: true,
  })
  if (!ok) return
  await api.DELETE('/api-keys/{id}', { params: { path: { id: k.id } } })
  void queryClient.invalidateQueries({ queryKey: ['api-keys'] })
}

const logFor = ref<ApiKey>()
const { data: log } = useQuery({
  queryKey: ['api-key-log', () => logFor.value?.id],
  enabled: () => !!logFor.value,
  queryFn: async () => {
    const { data, error } = await api.GET('/api-keys/{id}/requests', {
      params: { path: { id: logFor.value!.id } },
    })
    if (error) throw new ApiError(error)
    return data.items
  },
})
</script>

<template>
  <v-container class="pk-keys">
    <h1 class="text-headline-small mb-1">Ключи API</h1>
    <p class="text-body-medium text-medium-emphasis mb-4">
      Внешняя система передаёт ключ в заголовке <code>Authorization: Bearer …</code> и получает
      доступ только к выбранным данным. Описание API —
      <a href="/api/docs" target="_blank">/api/docs</a>.
    </p>
    <v-btn
      :prepend-icon="mdiKeyPlus"
      color="primary"
      variant="flat"
      class="mb-4"
      data-test="new-key"
      @click="((open = true), (issued = undefined))"
      >Выпустить ключ</v-btn
    >
    <v-alert v-if="error" type="error" class="mb-3">{{ error.message }}</v-alert>
    <v-table v-if="keys?.length" density="comfortable" data-test="keys">
      <thead>
        <tr>
          <th>Название</th>
          <th>Права</th>
          <th>Выпущен</th>
          <th>Последнее обращение</th>
          <th />
        </tr>
      </thead>
      <tbody>
        <tr v-for="k in keys" :key="k.id" :class="{ 'text-disabled': k.revokedAt }">
          <td>
            {{ k.name }}
            <div class="pk-code text-body-small">pk_{{ k.prefix }}_…</div>
          </td>
          <td>{{ k.scopes.map((s) => scopeLabels[s]).join(', ') }}</td>
          <td>
            {{ formatDateTime(k.createdAt) }}
            <div class="text-body-small">{{ k.createdByName }}</div>
          </td>
          <td>{{ k.lastUsedAt ? formatDateTime(k.lastUsedAt) : '—' }}</td>
          <td class="text-end text-no-wrap">
            <v-btn variant="text" size="small" @click="logFor = k">Журнал</v-btn>
            <v-btn v-if="!k.revokedAt" variant="text" size="small" color="error" @click="revoke(k)"
              >Отозвать</v-btn
            >
            <span v-else class="text-body-small">отозван {{ formatDateTime(k.revokedAt) }}</span>
          </td>
        </tr>
      </tbody>
    </v-table>
    <p v-else-if="keys" class="text-body-medium text-medium-emphasis">Ключей ещё нет.</p>

    <v-dialog v-model="open" max-width="480">
      <v-card title="Новый ключ API">
        <v-card-text>
          <template v-if="issued">
            <v-alert type="warning" variant="tonal" class="mb-3">
              Ключ показывается один раз. Передайте его в систему-получатель.
            </v-alert>
            <div class="d-flex align-center ga-2">
              <code class="pk-key flex-grow-1" data-test="issued-key">{{ issued }}</code>
              <v-btn :icon="copied ? mdiCheck : mdiContentCopy" variant="tonal" @click="copy" />
            </div>
          </template>
          <template v-else>
            <v-text-field
              v-model="name"
              label="Для какой системы"
              class="mb-2"
              data-test="key-name"
            />
            <v-checkbox
              v-for="(label, s) in scopeLabels"
              :key="s"
              v-model="scopes"
              :value="s"
              :label="label"
              density="compact"
              hide-details
            />
            <v-alert v-if="createError" type="error" class="mt-2">{{
              createError.message
            }}</v-alert>
          </template>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn @click="open = false">{{ issued ? 'Готово' : 'Отмена' }}</v-btn>
          <v-btn
            v-if="!issued"
            color="primary"
            variant="flat"
            :disabled="!name.trim() || !scopes.length"
            :loading="isPending"
            data-test="create-key"
            @click="create()"
            >Выпустить</v-btn
          >
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog :model-value="!!logFor" max-width="640" @update:model-value="logFor = undefined">
      <v-card :title="`Журнал ключа «${logFor?.name}»`">
        <v-card-text>
          <p v-if="!log?.length" class="text-medium-emphasis">Обращений не было.</p>
          <v-table v-else density="compact">
            <tbody>
              <tr v-for="(r, i) in log" :key="i">
                <td>{{ formatDateTime(r.at) }}</td>
                <td>{{ r.method }} {{ r.operation }}</td>
                <td>{{ r.ip }}</td>
                <td>{{ r.ok ? 'разрешено' : 'отказано' }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-card-text>
      </v-card>
    </v-dialog>
  </v-container>
</template>

<style scoped>
.pk-keys {
  max-width: 1000px;
}
.pk-key {
  font-size: 0.85rem;
  word-break: break-all;
  padding: 8px 12px;
  border-radius: 8px;
  background: rgb(var(--v-theme-surface-light));
}
</style>
