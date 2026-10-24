<script setup lang="ts">
import { api, ApiError, type RuSubject, type SaleScope } from '@pikni/api'
import { useMutation } from '@tanstack/vue-query'
import { reactive, watch } from 'vue'
import type { SubmitEventPromise } from 'vuetify'

import { saleScopeLabels } from '@/labels'

const props = defineProps<{ subjects: RuSubject[] }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const FEDERAL = 'federal'
const form = reactive({
  subject: FEDERAL,
  scope: 'all' as SaleScope,
  validFrom: '',
  from: '',
  to: '',
  source: '',
})
const subjectItems = () => [
  { value: FEDERAL, title: 'Вся Россия (федеральное)' },
  ...props.subjects.map((s) => ({ value: s.code, title: `${s.code} — ${s.name}` })),
]
const scopeItems = Object.entries(saleScopeLabels).map(([value, title]) => ({ value, title }))
const required = (v: string) => v.trim() !== '' || 'Обязательное поле'

const { mutate, isPending, error, reset } = useMutation({
  mutationFn: async () => {
    const { error } = await api.POST('/sale-rules', {
      body: {
        subjectCode: form.subject === FEDERAL ? undefined : form.subject,
        scope: form.scope,
        validFrom: form.validFrom,
        from: form.from,
        to: form.to,
        source: form.source,
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
  Object.assign(form, {
    subject: FEDERAL,
    scope: 'all',
    validFrom: '',
    from: '',
    to: '',
    source: '',
  })
})

async function submit(event: SubmitEventPromise) {
  if ((await event).valid) mutate()
}
</script>

<template>
  <v-dialog v-model="open" max-width="520">
    <v-card title="Новая редакция правила">
      <v-form @submit.prevent="submit">
        <v-card-text>
          <p class="text-body-medium text-medium-emphasis mb-4">
            Прежние редакции не меняются: на дату продажи действует последняя наступившая.
          </p>
          <v-select v-model="form.subject" label="Регион" :items="subjectItems()" class="mb-3" />
          <v-select v-model="form.scope" label="Продукция" :items="scopeItems" class="mb-3" />
          <v-text-field
            v-model="form.validFrom"
            label="Действует с"
            type="date"
            :rules="[required]"
            class="mb-3"
          />
          <div class="d-flex ga-3 mb-3">
            <v-text-field v-model="form.from" label="Продажа с" type="time" :rules="[required]" />
            <v-text-field v-model="form.to" label="до" type="time" :rules="[required]" />
          </div>
          <v-text-field
            v-model="form.source"
            label="Закон-основание"
            :rules="[required]"
            placeholder="Закон Амурской области от … № …-ОЗ, ст. …"
          />
          <v-alert v-if="error" type="error" class="mt-3">
            {{ error instanceof ApiError ? error.message : 'Нет связи с сервером' }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="open = false">Отмена</v-btn>
          <v-btn type="submit" color="primary" :loading="isPending">Добавить</v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>
