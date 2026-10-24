<script setup lang="ts">
import { api, ApiError, type Category } from '@pikni/api'
import { useMutation } from '@tanstack/vue-query'
import { computed, reactive, watch } from 'vue'
import type { SubmitEventPromise } from 'vuetify'

// Без category — новая категория внутри parent (без parent — верхнего уровня).
const props = defineProps<{ category?: Category; parent?: Category; nextSort: number }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [category: Category] }>()

const form = reactive({ name: '', sort: 0, isActive: true, alcohol: false })
// Признак алкоголя задаётся у категории верхнего уровня, подкатегории его наследуют.
const isRoot = computed(() => (props.category ? !props.category.parentId : !props.parent))
const inheritedAlcohol = computed(() => props.category?.alcohol ?? props.parent?.alcohol ?? false)
const required = (v: string) => v.trim() !== '' || 'Обязательное поле'

const { mutate, isPending, error, reset } = useMutation({
  mutationFn: async () => {
    const fields = { name: form.name.trim(), sort: Number(form.sort) }
    const alcohol = isRoot.value ? form.alcohol : undefined
    const { data, error } = props.category
      ? await api.PUT('/categories/{id}', {
          params: { path: { id: props.category.id } },
          body: { ...fields, isActive: form.isActive, alcohol },
        })
      : await api.POST('/categories', {
          body: { ...fields, parentId: props.parent?.id, alcohol },
        })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: (category) => {
    open.value = false
    emit('saved', category)
  },
})

watch(open, (isOpen) => {
  if (!isOpen) return
  reset()
  const c = props.category
  Object.assign(form, {
    name: c?.name ?? '',
    sort: c?.sort ?? props.nextSort,
    isActive: c?.isActive ?? true,
    alcohol: c?.alcohol ?? false,
  })
})

async function submit(event: SubmitEventPromise) {
  if ((await event).valid) mutate()
}
</script>

<template>
  <v-dialog v-model="open" max-width="480">
    <v-card
      :title="category ? 'Изменить категорию' : 'Новая категория'"
      :subtitle="!category && parent ? `Внутри «${parent.name}»` : undefined"
    >
      <v-form @submit.prevent="submit">
        <v-card-text>
          <v-text-field
            v-model="form.name"
            label="Название"
            :rules="[required]"
            data-test="category-name"
            class="mb-3"
          />
          <v-text-field
            v-model.number="form.sort"
            label="Порядок"
            type="number"
            hint="Чем меньше число, тем выше в списке"
            persistent-hint
            class="mb-3"
          />
          <v-switch
            v-if="isRoot"
            v-model="form.alcohol"
            label="Алкогольная продукция"
            hint="У позиций есть вид продукции ЕГАИС, крепость, алкокод и МРЦ. Действует на все подкатегории"
            persistent-hint
            class="mb-3"
            data-test="category-alcohol"
          />
          <p v-else class="text-body-medium text-medium-emphasis mb-3">
            {{ inheritedAlcohol ? 'Алкогольная продукция' : 'Не алкогольная продукция' }} — как у
            категории верхнего уровня.
          </p>
          <v-switch
            v-if="category"
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
          <v-btn type="submit" color="primary" :loading="isPending" data-test="category-save">
            Сохранить
          </v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>
