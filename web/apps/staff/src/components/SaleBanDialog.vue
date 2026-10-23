<script setup lang="ts">
import { api, ApiError, type RuSubject, type SaleScope } from '@pikni/api'
import { useMutation } from '@tanstack/vue-query'
import { reactive, watch } from 'vue'
import type { SubmitEventPromise } from 'vuetify'

import { saleScopeLabels } from '@/labels'

const props = defineProps<{ subjects: RuSubject[] }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const form = reactive({
  subject: '',
  name: '',
  scope: 'all' as SaleScope,
  yearly: true,
  date: '',
  source: '',
})
const subjectItems = () =>
  props.subjects.map((s) => ({ value: s.code, title: `${s.code} — ${s.name}` }))
const scopeItems = Object.entries(saleScopeLabels).map(([value, title]) => ({ value, title }))
const required = (v: string) => v.trim() !== '' || 'Обязательное поле'

const { mutate, isPending, error, reset } = useMutation({
  mutationFn: async () => {
    // Из поля даты «ГГГГ-ММ-ДД» для ежегодного запрета нужна только часть «ММ-ДД».
    const { error } = await api.POST('/sale-bans', {
      body: {
        subjectCode: form.subject,
        name: form.name,
        scope: form.scope,
        source: form.source,
        ...(form.yearly ? { monthDay: form.date.slice(5) } : { date: form.date }),
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
    subject: props.subjects[0]?.code ?? '',
    name: '',
    scope: 'all',
    yearly: true,
    date: '',
    source: '',
  })
})

async function submit(event: SubmitEventPromise) {
  if ((await event).valid) mutate()
}
</script>

<template>
  <v-dialog v-model="open" max-width="520">
    <v-card title="День полного запрета">
      <v-form @submit.prevent="submit">
        <v-card-text>
          <v-select v-model="form.subject" label="Регион" :items="subjectItems()" class="mb-3" />
          <v-text-field
            v-model="form.name"
            label="Название"
            :rules="[required]"
            placeholder="День знаний"
            class="mb-3"
          />
          <v-select v-model="form.scope" label="Продукция" :items="scopeItems" class="mb-3" />
          <v-switch v-model="form.yearly" label="Каждый год в этот день" />
          <v-text-field
            v-model="form.date"
            :label="form.yearly ? 'День (год не важен)' : 'Дата'"
            type="date"
            :rules="[required]"
            class="mb-3"
          />
          <v-text-field v-model="form.source" label="Закон-основание" :rules="[required]" />
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
