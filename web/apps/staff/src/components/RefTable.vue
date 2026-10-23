<script setup lang="ts" generic="Row extends object">
import { mdiMagnify, mdiPlus } from '@mdi/js'
import { ApiError } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, reactive, ref } from 'vue'
import type { SubmitEventPromise } from 'vuetify'

import type { RefConfig, RefValues } from '@/refs'

const props = defineProps<{ config: RefConfig<Row>; canEdit: boolean }>()

const queryClient = useQueryClient()
const key = computed(() => ['ref', props.config.key])
const {
  data: rows,
  isPending,
  isError,
  error,
} = useQuery({
  queryKey: key,
  queryFn: () => props.config.load(),
})

const search = ref('')
const headers = computed(() => [
  ...props.config.columns.map((c) => ({ title: c.title, key: c.key, value: c.value })),
  ...(props.config.hasActive ? [{ title: 'Статус', key: 'isActive', sortable: false }] : []),
])

const editable = computed(() => props.canEdit && props.config.fields !== undefined)
const dialog = ref(false)
const editing = ref<Row | null>(null)
const form = reactive<RefValues>({})

function open(row: Row | null) {
  editing.value = row
  for (const f of props.config.fields ?? [])
    form[f.key] = row ? (props.config.toForm?.(row)[f.key] ?? '') : ''
  form.isActive = row ? ((row as { isActive?: boolean }).isActive ?? true) : true
  reset()
  dialog.value = true
}

function onRow(_: Event, { item }: { item: Row }) {
  if (editable.value && props.config.update) open(item)
}

const visibleFields = computed(() =>
  (props.config.fields ?? []).filter((f) => !(f.createOnly && editing.value)),
)
const required = (v: unknown) => (v !== '' && v !== null && v !== undefined) || 'Обязательное поле'

const {
  mutate,
  isPending: saving,
  error: saveError,
  reset,
} = useMutation({
  mutationFn: async () => {
    if (editing.value) await props.config.update!(editing.value, { ...form })
    else await props.config.create!({ ...form })
  },
  onSuccess: async () => {
    dialog.value = false
    await queryClient.invalidateQueries({ queryKey: key.value })
  },
})

async function submit(event: SubmitEventPromise) {
  if ((await event).valid) mutate()
}
</script>

<template>
  <div>
    <div class="d-flex flex-wrap align-center ga-3 mb-3">
      <v-text-field
        v-model="search"
        :prepend-inner-icon="mdiMagnify"
        placeholder="Поиск"
        density="compact"
        class="pk-ref-search"
        clearable
      />
      <v-spacer />
      <v-btn
        v-if="editable && config.create"
        color="primary"
        :prepend-icon="mdiPlus"
        data-test="ref-add"
        @click="open(null)"
      >
        Добавить
      </v-btn>
    </div>

    <v-alert v-if="isError" type="error" class="mb-3">{{ error?.message }}</v-alert>

    <v-card>
      <v-data-table
        :headers="headers"
        :items="rows ?? []"
        :loading="isPending"
        :search="search"
        :items-per-page="50"
        :item-value="config.rowKey"
        :hover="editable"
        mobile-breakpoint="sm"
        no-data-text="Записей нет"
        @click:row="onRow"
      >
        <template #[`item.isActive`]="{ value }">
          <v-chip size="small" :color="value ? 'success' : undefined">
            {{ value ? 'используется' : 'в архиве' }}
          </v-chip>
        </template>
      </v-data-table>
    </v-card>

    <v-dialog v-model="dialog" max-width="520">
      <v-card :title="editing ? 'Изменить запись' : 'Новая запись'">
        <v-form @submit.prevent="submit">
          <v-card-text>
            <template v-for="f in visibleFields" :key="f.key">
              <v-select
                v-if="f.type === 'select'"
                v-model="form[f.key]"
                :label="f.label"
                :items="f.items?.() ?? []"
                :rules="f.optional ? [] : [required]"
                class="mb-3"
              />
              <v-text-field
                v-else
                v-model="form[f.key]"
                :label="f.label"
                :hint="f.hint"
                persistent-hint
                :rules="f.optional ? [] : [required]"
                :data-test="`field-${f.key}`"
                class="mb-3"
              />
            </template>
            <v-switch
              v-if="editing && config.hasActive"
              v-model="form.isActive"
              label="Используется (выключено — в архиве)"
            />
            <v-alert v-if="saveError" type="error" class="mt-2">
              {{ saveError instanceof ApiError ? saveError.message : 'Нет связи с сервером' }}
            </v-alert>
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn variant="text" @click="dialog = false">Отмена</v-btn>
            <v-btn type="submit" color="primary" :loading="saving" data-test="ref-save">
              Сохранить
            </v-btn>
          </v-card-actions>
        </v-form>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.pk-ref-search {
  max-width: 320px;
}
</style>
