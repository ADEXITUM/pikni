<script setup lang="ts">
import { mdiArchiveArrowDownOutline, mdiArchiveArrowUpOutline, mdiPlus } from '@mdi/js'
import { api, ApiError, type AttributeType, type CategoryAttribute } from '@pikni/api'
import { useMutation } from '@tanstack/vue-query'
import { reactive, ref, watch } from 'vue'
import type { SubmitEventPromise } from 'vuetify'

import { attributeTypeLabels } from '@/categories'
import { confirmChanges, diffFields } from '@/confirm'

// Без attribute — новая характеристика категории categoryId.
const props = defineProps<{ categoryId: string; attribute?: CategoryAttribute; nextSort: number }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

interface Option {
  value: string
  archived: boolean
  saved: boolean
}

const form = reactive({
  name: '',
  code: '',
  type: 'enum' as AttributeType,
  unit: '',
  options: [] as Option[],
  filterable: false,
  usedForAnalogs: false,
  sort: 0,
  isActive: true,
})
const newOption = ref('')
const optionError = ref('')

const typeItems = Object.entries(attributeTypeLabels).map(([value, title]) => ({ value, title }))
const required = (v: string) => v.trim() !== '' || 'Обязательное поле'
const codeRule = (v: string) =>
  /^[a-z][a-z0-9_]{1,31}$/.test(v.trim()) ||
  'От 2 до 32 символов: латиница в нижнем регистре, цифры и _, первая — буква'

// Удалить можно только значение, добавленное в этом окне. Сохранённое уже может стоять
// у позиций, поэтому его только отправляют в архив.
function addOption() {
  const value = newOption.value.trim()
  if (!value) return
  if (form.options.some((o) => o.value.toLowerCase() === value.toLowerCase())) {
    optionError.value = 'Такое значение уже есть'
    return
  }
  form.options.push({ value, archived: false, saved: false })
  newOption.value = ''
  optionError.value = ''
}

const { mutate, isPending, error, reset } = useMutation({
  mutationFn: async () => {
    const fields = {
      name: form.name.trim(),
      unit: form.type === 'number' ? form.unit.trim() || undefined : undefined,
      filterable: form.filterable,
      usedForAnalogs: form.usedForAnalogs,
      sort: Number(form.sort),
    }
    const { error } = props.attribute
      ? await api.PUT('/category-attributes/{id}', {
          params: { path: { id: props.attribute.id } },
          body: {
            ...fields,
            options: form.options.map(({ value, archived }) => ({ value, archived })),
            isActive: form.isActive,
          },
        })
      : await api.POST('/categories/{id}/attributes', {
          params: { path: { id: props.categoryId } },
          body: {
            ...fields,
            code: form.code.trim(),
            type: form.type,
            options: form.options.map((o) => o.value),
          },
        })
    if (error) throw new ApiError(error)
  },
  onSuccess: () => {
    open.value = false
    emit('saved')
  },
})

watch(open, (isOpen) => {
  if (!isOpen) return
  reset()
  const a = props.attribute
  Object.assign(form, {
    name: a?.name ?? '',
    code: a?.code ?? '',
    type: a?.type ?? 'enum',
    unit: a?.unit ?? '',
    options: (a?.options ?? []).map((o) => ({ ...o, saved: true })),
    filterable: a?.filterable ?? false,
    usedForAnalogs: a?.usedForAnalogs ?? false,
    sort: a?.sort ?? props.nextSort,
    isActive: a?.isActive ?? true,
  })
  newOption.value = ''
  optionError.value = ''
})

// Значение, набранное без нажатия «добавить», тоже попадает в список.
async function submit(event: SubmitEventPromise) {
  optionError.value = ''
  addOption()
  if (!optionError.value && form.type === 'enum' && !form.options.some((o) => !o.archived)) {
    optionError.value = 'Нужно хотя бы одно действующее значение'
  }
  if (!(await event).valid || optionError.value) return
  const a = props.attribute
  if (a) {
    const describe = (v: {
      name: string
      unit?: string | null
      options: { value: string; archived: boolean }[]
      filterable: boolean
      usedForAnalogs: boolean
      sort: number
      isActive: boolean
    }) => ({
      name: v.name.trim(),
      unit: v.unit?.trim() ?? '',
      options: v.options.filter((o) => !o.archived).map((o) => o.value),
      archived: v.options.filter((o) => o.archived).map((o) => o.value),
      filterable: v.filterable,
      usedForAnalogs: v.usedForAnalogs,
      sort: String(v.sort),
      isActive: v.isActive,
    })
    const changes = diffFields(describe(a), describe({ ...form, sort: Number(form.sort) }), {
      name: 'Название',
      unit: 'Единица',
      options: 'Значения',
      archived: 'Значения в архиве',
      filterable: 'В фильтрах',
      usedForAnalogs: 'Для подбора аналогов',
      sort: 'Порядок',
      isActive: 'Используется',
    })
    if (!changes.length) {
      open.value = false
      return
    }
    if (!(await confirmChanges(`Изменить характеристику «${a.name}»?`, changes))) return
  }
  mutate()
}
</script>

<template>
  <v-dialog v-model="open" max-width="560">
    <v-card :title="attribute ? 'Изменить характеристику' : 'Новая характеристика'">
      <v-form @submit.prevent="submit">
        <v-card-text>
          <v-text-field
            v-model="form.name"
            label="Название"
            :rules="[required]"
            data-test="attr-name"
            class="mb-3"
          />
          <v-row density="compact">
            <v-col cols="12" sm="6">
              <v-text-field
                v-model="form.code"
                label="Код"
                :rules="attribute ? [] : [codeRule]"
                :disabled="!!attribute"
                hint="Для импорта и API, не меняется"
                persistent-hint
                data-test="attr-code"
              />
            </v-col>
            <v-col cols="12" sm="6">
              <v-select
                v-model="form.type"
                label="Тип"
                :items="typeItems"
                :disabled="!!attribute"
                hint="Не меняется после создания"
                persistent-hint
                data-test="attr-type"
              />
            </v-col>
          </v-row>

          <v-text-field
            v-if="form.type === 'number'"
            v-model="form.unit"
            label="Единица измерения"
            placeholder="лет"
            class="mt-3"
          />

          <template v-if="form.type === 'enum'">
            <div class="text-title-small mt-4 mb-2">Значения списка</div>
            <div class="d-flex flex-wrap ga-2 mb-3">
              <v-chip
                v-for="(o, i) in form.options"
                :key="o.value"
                :variant="o.archived ? 'outlined' : 'tonal'"
                :class="{ 'pk-archived': o.archived }"
                :closable="!o.saved"
                :data-test="`option-${o.value}`"
                @click:close="form.options.splice(i, 1)"
              >
                {{ o.value }}
                <template v-if="o.saved" #append>
                  <v-icon
                    :icon="o.archived ? mdiArchiveArrowUpOutline : mdiArchiveArrowDownOutline"
                    :title="o.archived ? 'Вернуть из архива' : 'Отправить в архив'"
                    size="small"
                    class="ml-1"
                    data-test="option-archive"
                    @click="o.archived = !o.archived"
                  />
                </template>
              </v-chip>
            </div>
            <v-text-field
              v-model="newOption"
              label="Новое значение"
              :error-messages="optionError"
              :append-inner-icon="mdiPlus"
              :hint="
                attribute
                  ? 'Сохранённые значения не удаляются: архивное остаётся у заведённых позиций, но не предлагается для новых'
                  : 'Enter — добавить'
              "
              persistent-hint
              data-test="attr-new-option"
              @click:append-inner="addOption"
              @keydown.enter.prevent="addOption"
            />
          </template>

          <v-switch
            v-model="form.filterable"
            label="Показывать в фильтрах поиска"
            hide-details
            class="mt-2"
          />
          <v-switch
            v-model="form.usedForAnalogs"
            label="Учитывать при подборе аналогов"
            hide-details
          />
          <v-text-field v-model.number="form.sort" label="Порядок" type="number" class="mt-3" />
          <v-switch
            v-if="attribute"
            v-model="form.isActive"
            label="Используется (выключено — в архиве)"
          />
          <v-alert v-if="error" type="error" class="mt-2">
            {{ error instanceof ApiError ? error.message : 'Нет связи с сервером' }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="open = false">Отмена</v-btn>
          <v-btn type="submit" color="primary" :loading="isPending" data-test="attr-save">
            Сохранить
          </v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.pk-archived {
  text-decoration: line-through;
  opacity: 0.6;
}
</style>
