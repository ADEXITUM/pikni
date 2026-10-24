<script setup lang="ts" generic="Row extends object">
import { mdiDeleteOutline, mdiMagnify, mdiPlus } from '@mdi/js'
import { ApiError } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, reactive, ref } from 'vue'
import type { SubmitEventPromise } from 'vuetify'

import { confirm, confirmChanges, diffFields } from '@/confirm'
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
const editable = computed(() => props.canEdit && props.config.fields !== undefined)
const removable = computed(() => props.canEdit && props.config.remove !== undefined)

const headers = computed(() => [
  ...props.config.columns.map((c) => ({ title: c.title, key: c.key, value: c.value })),
  ...(props.config.hasActive ? [{ title: 'Статус', key: 'isActive', sortable: false }] : []),
  ...(removable.value ? [{ title: '', key: 'actions', sortable: false }] : []),
])

const { mutate: remove, error: removeError } = useMutation({
  mutationFn: (row: Row) => props.config.remove!(row),
  onSuccess: () => queryClient.invalidateQueries({ queryKey: key.value }),
})
const rowTitle = (row: Row) => {
  const first = props.config.columns[0]!
  return first.value ? first.value(row) : String((row as Record<string, unknown>)[first.key] ?? '')
}
async function askRemove(row: Row) {
  const ok = await confirm({
    title: 'Удалить запись?',
    text: `«${rowTitle(row)}» удалится из справочника «${props.config.title}».`,
    danger: true,
  })
  if (ok) remove(row)
}

const dialog = ref(false)
const editing = ref<Row | null>(null)
const form = reactive<RefValues>({})

function open(row: Row | null) {
  editing.value = row
  for (const f of props.config.fields ?? [])
    form[f.key] = row ? (props.config.toForm?.(row)[f.key] ?? '') : (f.initial ?? '')
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

// Значение поля так, как его видит человек: для списков — название, а не код.
function describe(values: RefValues): Record<string, unknown> {
  const out: Record<string, unknown> = { isActive: values.isActive }
  for (const f of visibleFields.value) {
    const v = values[f.key]
    out[f.key] = f.items ? (f.items().find((i) => i.value === v)?.title ?? v) : v
  }
  return out
}

async function submit(event: SubmitEventPromise) {
  if (!(await event).valid) return
  const row = editing.value
  if (row) {
    const labels: Record<string, string> = Object.fromEntries(
      visibleFields.value.map((f) => [f.key, f.label]),
    )
    if (props.config.hasActive) labels.isActive = 'Используется'
    const before = {
      ...props.config.toForm?.(row),
      isActive: (row as { isActive?: boolean }).isActive ?? true,
    }
    const changes = diffFields(describe(before), describe(form), labels)
    if (!changes.length) {
      dialog.value = false
      return
    }
    if (!(await confirmChanges(`Изменить «${rowTitle(row)}»?`, changes))) return
  }
  mutate()
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
    <v-alert v-if="removeError" type="error" class="mb-3">{{ removeError.message }}</v-alert>

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
        <template #[`item.actions`]="{ item }">
          <v-btn
            v-if="config.canRemove?.(item) ?? true"
            :icon="mdiDeleteOutline"
            variant="text"
            size="small"
            title="Удалить"
            data-test="ref-remove"
            @click.stop="askRemove(item)"
          />
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
                :type="f.inputType ?? 'text'"
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
