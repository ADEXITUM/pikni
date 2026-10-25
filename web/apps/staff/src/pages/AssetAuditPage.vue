<script setup lang="ts">
import {
  mdiArrowLeft,
  mdiBarcodeScan,
  mdiCameraOutline,
  mdiCheckCircleOutline,
  mdiHelpCircleOutline,
  mdiMagnify,
  mdiSwapHorizontal,
} from '@mdi/js'
import { api, ApiError, type AssetAuditResult } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'

import ScanCamera from '@pikni/ui/ScanCamera.vue'
import { confirm } from '@/confirm'
import { playFeedback } from '@/feedback'
import { formatDateTime } from '@/format'
import { load, save } from '@/preferences'
import { useScanner } from '@/scanning'

// Инвентаризация имущества (ИМ-4): точку обходят со смартфоном и сканируют наклейки. Итог по
// каждому объекту — найден, не найден или найден здесь, хотя числится в другой точке.
const route = useRoute()
const id = computed(() => String(route.params.id))
const key = computed(() => ['asset-audit', id.value])
const queryClient = useQueryClient()

const { data: state, error: loadError } = useQuery({
  queryKey: key,
  queryFn: async () => {
    const { data, error } = await api.GET('/asset-audits/{id}', {
      params: { path: { id: id.value } },
    })
    if (error) throw new ApiError(error)
    return data
  },
})
const counting = computed(() => state.value?.audit.status === 'counting')
const groups = computed(() => {
  const rows = state.value?.results ?? []
  const by = (r: AssetAuditResult['result']) => rows.filter((x) => x.result === r)
  return { found: by('found'), notFound: by('not_found'), elsewhere: by('elsewhere') }
})

const last = ref<{ ok: boolean; text: string }>()
const { submit } = useScanner(async ({ event }) => {
  if (!counting.value) return
  try {
    const { data, error } = await api.POST('/asset-audits/{id}/scans', {
      params: { path: { id: id.value } },
      body: { raw: event.raw },
    })
    if (error) throw new ApiError(error)
    await queryClient.invalidateQueries({ queryKey: key.value })
    const r = state.value?.results.find((x) => x.assetId === data.assetId)
    const name = r ? `${r.number} · ${r.name}` : event.raw
    last.value = {
      ok: true,
      text: data.duplicate
        ? `${name} — уже отмечен`
        : data.result === 'elsewhere'
          ? `${name} — числится в ${r?.locationName}`
          : `${name} — найден`,
    }
    playFeedback(data.duplicate || data.result === 'elsewhere' ? 'notInPlan' : 'accepted')
  } catch (e) {
    last.value = { ok: false, text: e instanceof ApiError ? e.message : 'Нет связи с сервером' }
    playFeedback('unknown')
  }
})

const stored = load('pikni.scan.camera')
const cameraOn = ref(
  stored === null ? (window.matchMedia?.('(pointer: coarse)').matches ?? false) : stored === '1',
)
function setCamera(on: boolean) {
  cameraOn.value = on
  save('pikni.scan.camera', on ? '1' : '0')
}
const manual = ref('')
function submitManual() {
  if (!manual.value.trim()) return
  submit(manual.value, 'manual')
  manual.value = ''
}

const relocate = ref(true)
const {
  mutate: finish,
  isPending,
  error,
} = useMutation({
  mutationFn: async (action: 'finish' | 'cancel') => {
    const { data, error } = await api.POST('/asset-audits/{id}/{action}', {
      params: {
        path: { id: id.value, action },
        query: { relocate: action === 'finish' && relocate.value },
      },
    })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (data) => {
    queryClient.setQueryData(key.value, data)
    void queryClient.invalidateQueries({ queryKey: ['asset-audits'] })
    void queryClient.invalidateQueries({ queryKey: ['assets'] })
  },
})
async function askFinish() {
  const g = groups.value
  const ok = await confirm({
    title: 'Завершить инвентаризацию?',
    text: `Найдено ${g.found.length}, не найдено ${g.notFound.length}, из других точек ${g.elsewhere.length}.`,
    confirmText: 'Завершить',
  })
  if (ok) finish('finish')
}
</script>

<template>
  <v-container class="pk-audit">
    <v-btn :prepend-icon="mdiArrowLeft" :to="{ name: 'assets' }" variant="text" class="mb-2 ml-n3"
      >Имущество</v-btn
    >
    <v-alert v-if="loadError" type="error">{{ loadError.message }}</v-alert>
    <template v-if="state">
      <h1 class="text-headline-small mb-1">Инвентаризация имущества</h1>
      <p class="text-body-medium text-medium-emphasis mb-4">
        {{ state.audit.locationName }} · начата {{ formatDateTime(state.audit.startedAt) }},
        {{ state.audit.startedByName
        }}<template v-if="state.audit.finishedAt">
          · {{ state.audit.status === 'finished' ? 'завершена' : 'отменена' }}
          {{ formatDateTime(state.audit.finishedAt) }}</template
        >
      </p>

      <template v-if="counting">
        <ScanCamera
          v-if="cameraOn"
          class="mb-4"
          @scan="(c) => submit(c.raw, 'camera', c.format)"
          @close="setCamera(false)"
        />
        <v-btn
          v-else
          :prepend-icon="mdiCameraOutline"
          variant="tonal"
          class="mb-4"
          @click="setCamera(true)"
          >Включить камеру</v-btn
        >
        <v-form class="mb-3" @submit.prevent="submitManual">
          <v-text-field
            v-model="manual"
            :prepend-inner-icon="mdiBarcodeScan"
            :append-inner-icon="mdiMagnify"
            label="Инвентарный номер"
            hide-details
            data-test="manual"
            @click:append-inner="submitManual"
          />
        </v-form>
        <v-alert
          v-if="last"
          :type="last.ok ? 'success' : 'error'"
          variant="tonal"
          class="mb-4"
          data-test="last-scan"
          >{{ last.text }}</v-alert
        >
      </template>

      <div class="pk-summary mb-4" data-test="summary">
        <div>
          <v-icon :icon="mdiCheckCircleOutline" color="success" /> Найдено
          <b>{{ groups.found.length }}</b>
        </div>
        <div>
          <v-icon :icon="mdiHelpCircleOutline" color="error" /> Не найдено
          <b>{{ groups.notFound.length }}</b>
        </div>
        <div>
          <v-icon :icon="mdiSwapHorizontal" color="warning" /> Из других точек
          <b>{{ groups.elsewhere.length }}</b>
        </div>
      </div>

      <template v-for="(rows, kind) in groups" :key="kind">
        <template v-if="rows.length">
          <h2 class="text-title-medium mb-1">
            {{
              kind === 'found'
                ? 'Найдены'
                : kind === 'notFound'
                  ? 'Не найдены'
                  : 'Числятся в других точках'
            }}
          </h2>
          <v-list density="compact" class="mb-4" :data-test="`group-${kind}`">
            <v-list-item
              v-for="r in rows"
              :key="r.assetId"
              :to="{ name: 'asset', params: { id: r.assetId } }"
              :title="`${r.number} · ${r.name}`"
              :subtitle="
                kind === 'elsewhere'
                  ? `числится: ${r.locationName}`
                  : `${r.categoryName}${r.responsibleName ? ' · МОЛ ' + r.responsibleName : ''}`
              "
            />
          </v-list>
        </template>
      </template>

      <v-alert v-if="error" type="error" class="mb-3">{{ error.message }}</v-alert>
      <div v-if="counting" class="pk-actions">
        <v-btn variant="text" :loading="isPending" @click="finish('cancel')">Отменить</v-btn>
        <v-spacer />
        <v-checkbox
          v-if="groups.elsewhere.length"
          v-model="relocate"
          label="Перевести найденное из других точек сюда"
          density="compact"
          hide-details
        />
        <v-btn
          color="primary"
          variant="flat"
          :loading="isPending"
          data-test="finish"
          @click="askFinish"
          >Завершить</v-btn
        >
      </div>
    </template>
  </v-container>
</template>

<style scoped>
.pk-audit {
  max-width: 900px;
}
.pk-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 24px;
}
.pk-actions {
  position: sticky;
  bottom: 12px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  padding: 8px;
  border-radius: 8px;
  background: rgb(var(--v-theme-surface));
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
}
</style>
