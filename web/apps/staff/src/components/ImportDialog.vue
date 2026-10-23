<script setup lang="ts">
import { mdiFileDownloadOutline } from '@mdi/js'
import { api, ApiError, type ImportReport } from '@pikni/api'
import { useMutation } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'

const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ imported: [] }>()

const file = ref<File | null>(null)
const preview = ref<ImportReport>()
const done = ref<ImportReport>()

// Сервер ничего не хранит между шагами: для загрузки тот же файл отправляется ещё раз.
async function send(path: '/catalog/import/preview' | '/catalog/import') {
  const form = new FormData()
  form.append('file', file.value!)
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
  onSuccess: (r) => (preview.value = r),
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

const failure = computed(() => checkError.value ?? loadError.value)
const canLoad = computed(
  () => preview.value && preview.value.errors.length === 0 && preview.value.products > 0,
)
const created = (r: ImportReport) =>
  [
    r.newProducers.length && `производители: ${r.newProducers.join(', ')}`,
    r.newBrands.length && `бренды: ${r.newBrands.join(', ')}`,
    r.newLines.length && `линейки: ${r.newLines.join(', ')}`,
  ].filter(Boolean)
</script>

<template>
  <v-dialog v-model="open" max-width="720">
    <v-card title="Импорт номенклатуры">
      <v-card-text>
        <template v-if="done">
          <v-alert type="success" variant="tonal" data-test="import-done">
            Загружено позиций: {{ done.products }}, вариантов: {{ done.variants }}.
          </v-alert>
        </template>
        <template v-else>
          <p class="text-body-2 mb-3">
            Заполните шаблон: одна строка — позиция с одним вариантом. Перед загрузкой система
            проверит каждую строку. Если хоть в одной ошибка, не загрузится ничего.
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
              <v-chip size="small">позиций: {{ preview.products }}</v-chip>
              <v-chip size="small">вариантов: {{ preview.variants }}</v-chip>
            </div>
            <p v-if="created(preview).length" class="text-body-2 mb-3">
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
          @click="load()"
        >
          Загрузить
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
