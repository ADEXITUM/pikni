<script setup lang="ts">
import { api, ApiError, type Role, type User } from '@pikni/api'
import { useMutation, useQuery } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'
import type { SubmitEventPromise } from 'vuetify'

import { confirmChanges, diffFields } from '@/confirm'
import { networkRoles, roleLabels } from '@/labels'
import { locationsQuery } from '@/queries'
import { useMe } from '@/session'

// user — сотрудник для редактирования; null — создание нового.
const props = defineProps<{ user: User | null }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ created: [user: User, password: string]; saved: [user: User] }>()

const { data: me } = useMe()
const { data: locations } = useQuery(locationsQuery)

const login = ref('')
const fullName = ref('')
const role = ref<Role>('cashier')
const locationIds = ref<string[]>([])

const isSelf = computed(() => props.user !== null && props.user.id === me.value?.id)
const needsLocations = computed(() => !networkRoles.has(role.value))
const roleItems = Object.entries(roleLabels).map(([value, title]) => ({ value, title }))
const locationItems = computed(() =>
  (locations.value ?? []).map((l) => ({ value: l.id, title: `${l.code} · ${l.name}` })),
)

const loginRule = (v: string) =>
  /^[A-Za-z0-9._-]{3,32}$/.test(v.trim()) || 'От 3 до 32 символов: латиница, цифры, . - _'
const requiredRule = (v: string) => v.trim() !== '' || 'Обязательное поле'
const locationsRule = (v: string[]) => v.length > 0 || 'Назначьте хотя бы одну точку'

const { mutate, isPending, error, reset } = useMutation({
  mutationFn: async () => {
    const profile = {
      fullName: fullName.value,
      role: role.value,
      locationIds: needsLocations.value ? locationIds.value : [],
    }
    if (props.user) {
      const { data, error } = await api.PUT('/users/{id}', {
        params: { path: { id: props.user.id } },
        body: { ...profile, version: props.user.version },
      })
      if (error) throw new ApiError(error)
      emit('saved', data)
    } else {
      const { data, error } = await api.POST('/users', { body: { ...profile, login: login.value } })
      if (error) throw new ApiError(error)
      emit('created', data.user, data.temporaryPassword)
    }
    open.value = false
  },
})

watch(open, (isOpen) => {
  if (!isOpen) return
  reset()
  login.value = props.user?.login ?? ''
  fullName.value = props.user?.fullName ?? ''
  role.value = props.user?.role ?? 'cashier'
  locationIds.value = [...(props.user?.locationIds ?? [])]
})

const describe = (v: { fullName: string; role: Role; locationIds: string[] }) => ({
  fullName: v.fullName.trim(),
  role: roleLabels[v.role],
  locations: networkRoles.has(v.role)
    ? 'вся сеть'
    : v.locationIds.map((id) => locations.value?.find((l) => l.id === id)?.code ?? id).sort(),
})

async function submit(event: SubmitEventPromise) {
  if (!(await event).valid) return
  if (props.user) {
    const changes = diffFields(
      describe(props.user),
      describe({ fullName: fullName.value, role: role.value, locationIds: locationIds.value }),
      { fullName: 'ФИО', role: 'Роль', locations: 'Точки' },
    )
    if (!changes.length) {
      open.value = false
      return
    }
    if (!(await confirmChanges(`Изменить сотрудника ${props.user.login}?`, changes))) return
  }
  mutate()
}
</script>

<template>
  <v-dialog v-model="open" max-width="520">
    <v-card :title="user ? 'Изменить сотрудника' : 'Новый сотрудник'">
      <v-form @submit.prevent="submit">
        <v-card-text>
          <v-text-field
            v-model="login"
            label="Логин"
            :disabled="user !== null"
            :rules="user ? [] : [loginRule]"
            autocomplete="off"
            class="mb-3"
            data-test="login"
          />
          <v-text-field
            v-model="fullName"
            label="ФИО"
            :rules="[requiredRule]"
            class="mb-3"
            data-test="full-name"
          />
          <v-select
            v-model="role"
            label="Роль"
            :items="roleItems"
            :disabled="isSelf"
            :hint="isSelf ? 'Свою роль сменить нельзя' : undefined"
            persistent-hint
            class="mb-3"
            data-test="role"
          />
          <v-autocomplete
            v-if="needsLocations"
            v-model="locationIds"
            label="Точки"
            :items="locationItems"
            :rules="[locationsRule]"
            multiple
            chips
            closable-chips
            data-test="locations"
          />
          <v-alert v-if="error" type="error" class="mt-2">
            {{ error instanceof ApiError ? error.message : 'Нет связи с сервером' }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="open = false">Отмена</v-btn>
          <v-btn type="submit" color="primary" :loading="isPending" data-test="save">
            {{ user ? 'Сохранить' : 'Создать' }}
          </v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>
