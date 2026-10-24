<script setup lang="ts">
import { mdiFileDownloadOutline } from '@mdi/js'
import { api, ApiError, type ImportReport } from '@pikni/api'
import { useMutation } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'

import { confirm } from '@/confirm'

const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ imported: [] }>()

const file = ref<File | null>(null)
const preview = ref<ImportReport>()
const done = ref<ImportReport>()

// Расхождения с каталогом решаются, как конфликты при слиянии: по каждому — оставить значение
// каталога или взять из файла. По умолчанию — из файла: ради этого прайс обычно и загружают.
const takeNew = ref<Record<string, boolean>>({})
const accepted = computed(() => Object.keys(takeNew.value).filter((k) => takeNew.value[k]))
function takeAll(value: boolean) {
  takeNew.value = Object.fromEntries((preview.value?.changes ?? []).map((c) => [c.key, value]))
}

// Сервер ничего не хранит между шагами: для загрузки тот же файл отправляется ещё раз.
async function send(path: '/catalog/import/preview' | '/catalog/import') {
  const form = new FormData()
  form.append('file', file.value!)
  if (path === '/catalog/import') form.append('accept', JSON.stringify(accepted.value))
  const { data, error } = await api.POST(path, {
    body: { file: '' },
    bodySerializer: () => form,
  })
  if (error) throw new ApiError(error)
  return data
}

const {
  mutate: check,
  isPending: checking,
  error: checkError,
  reset: resetCheck,
} = useMutation({
  mutationFn: () => send('/catalog/import/preview'),
  onSuccess: (r) => {
    preview.value = r
    takeAll(true)
  },
})

const {
  mutate: load,
  isPending: loading,
  error: loadError,
  reset: resetLoad,
} = useMutation({
  mutationFn: () => send('/catalog/import'),
  onSuccess: (r) => {
    // Между предпросмотром и загрузкой данные могли измениться: тогда показываем новые ошибки.
    if (r.committed) {
      done.value = r
      emit('imported')
    } else {
      preview.value = r
    }
  },
})

watch(file, (f) => {
  preview.value = undefined
  resetCheck()
  resetLoad()
  if (f) check()
})

watch(open, (isOpen) => {
  if (!isOpen) return
  file.value = null
  preview.value = undefined
  done.value = undefined
})

// Перед загрузкой — итог: сколько создаётся и какие значения каталога меняются.
async function askLoad() {
  const r = preview.value
  if (!r) return
  const byKey = new Map(r.changes.map((c) => [c.key, c]))
  const ok = await confirm({
    title: 'Загрузить файл в каталог?',
    text: `Новых позиций: ${r.products}, новых вариантов: ${r.variants}, изменится значений: ${accepted.value.length}.`,
    changes: accepted.value.slice(0, 20).map((k) => {
      const c = byKey.get(k)!
      return {
        label: `${c.product}${c.variant ? ', ' + c.variant : ''}: ${c.field}`,
        old: c.old,
        new: c.new,
      }
    }),
    confirmText: 'Загрузить',
  })
  if (ok) load()
}

const failure = computed(() => checkError.value ?? loadError.value)
const canLoad = computed(() => {
  const r = preview.value
  return (
    r && r.errors.length === 0 && (r.products > 0 || r.variants > 0 || accepted.value.length > 0)
  )
})
const created = (r: ImportReport) =>
  [
    r.newProducers.length && `производители: ${r.newProducers.join(', ')}`,
    r.newBrands.length && `бренды: ${r.newBrands.join(', ')}`,
    r.newLines.length && `линейки: ${r.newLines.join(', ')}`,
  ].filter(Boolean)
</script>

<template>
  <v-dialog v-model="open" max-width="960">
    <v-card title="Импорт номенклатуры">
      <v-card-text>
        <template v-if="done">
          <v-alert type="success" variant="tonal" data-test="import-done">
            Новых позиций: {{ done.products }}, новых вариантов: {{ done.variants }}, изменено
            значений: {{ accepted.length }}.
          </v-alert>
        </template>
        <template v-else>
          <p class="text-body-medium mb-3">
            Заполните шаблон: одна строка — позиция с одним вариантом. Перед загрузкой система
            проверит каждую строку. Если хоть в одной ошибка, не загрузится ничего. Позиции, которые
            уже есть в каталоге, не дублируются: отличия покажутся списком.
          </p>
          <v-btn
            :prepend-icon="mdiFileDownloadOutline"
            variant="tonal"
            href="/api/v1/catalog/import/template"
            class="mb-4"
          >
            Скачать шаблон
          </v-btn>
          <v-file-input
            v-model="file"
            label="Файл XLSX или CSV"
            accept=".xlsx,.csv"
            :loading="checking"
            show-size
            data-test="import-file"
          />

          <v-alert v-if="failure" type="error" class="mt-2">
            {{ failure instanceof ApiError ? failure.message : 'Нет связи с сервером' }}
          </v-alert>

          <template v-if="preview">
            <div class="d-flex flex-wrap ga-2 my-3" data-test="import-summary">
              <v-chip size="small">строк: {{ preview.rows }}</v-chip>
              <v-chip size="small">новых позиций: {{ preview.products }}</v-chip>
              <v-chip size="small">уже в каталоге: {{ preview.existing }}</v-chip>
              <v-chip size="small">новых вариантов: {{ preview.variants }}</v-chip>
            </div>
            <template v-if="preview.changes.length && !preview.errors.length">
              <div class="d-flex flex-wrap align-center ga-2 mb-2">
                <span class="text-title-small"
                  >Отличия от каталога: {{ preview.changes.length }}</span
                >
                <v-spacer />
                <v-btn size="small" variant="tonal" data-test="take-all-new" @click="takeAll(true)">
                  Всё из файла
                </v-btn>
                <v-btn
                  size="small"
                  variant="tonal"
                  data-test="take-all-old"
                  @click="takeAll(false)"
                >
                  Всё оставить
                </v-btn>
              </div>
              <v-table density="compact" class="mb-3" data-test="import-changes">
                <thead>
                  <tr>
                    <th>Строка</th>
                    <th>Позиция</th>
                    <th>Поле</th>
                    <th>В каталоге</th>
                    <th>В файле</th>
                    <th>Что оставить</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="c in preview.changes" :key="c.key" :data-test="`change-${c.key}`">
                    <td>{{ c.row }}</td>
                    <td>
                      {{ c.product }}
                      <div v-if="c.variant" class="text-body-small text-medium-emphasis">
                        {{ c.variant }}
                      </div>
                    </td>
                    <td>{{ c.field }}</td>
                    <td :class="{ 'text-disabled': takeNew[c.key] }">{{ c.old }}</td>
                    <td :class="{ 'text-disabled': !takeNew[c.key] }">{{ c.new }}</td>
                    <td>
                      <v-btn-toggle
                        v-model="takeNew[c.key]"
                        mandatory
                        density="compact"
                        divided
                        variant="outlined"
                      >
                        <v-btn :value="false" size="small">Каталог</v-btn>
                        <v-btn :value="true" size="small">Файл</v-btn>
                      </v-btn-toggle>
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </template>
            <p v-if="created(preview).length" class="text-body-medium mb-3">
              Будут созданы — {{ created(preview).join('; ') }}. Проверьте, нет ли опечаток.
            </p>
            <template v-if="preview.errors.length">
              <v-alert type="error" variant="tonal" class="mb-2">
                Ошибок: {{ preview.errors.length }}. Исправьте файл и выберите его снова.
              </v-alert>
              <v-table density="compact" data-test="import-errors">
                <thead>
                  <tr>
                    <th>Строка</th>
                    <th>Ошибка</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="e in preview.errors" :key="`${e.row}-${e.message}`">
                    <td>{{ e.row || '—' }}</td>
                    <td>{{ e.message }}</td>
                  </tr>
                </tbody>
              </v-table>
            </template>
          </template>
        </template>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn variant="text" @click="open = false">{{ done ? 'Готово' : 'Отмена' }}</v-btn>
        <v-btn
          v-if="!done"
          color="primary"
          :disabled="!canLoad"
          :loading="loading"
          data-test="import-run"
          @click="askLoad()"
        >
          Загрузить
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
