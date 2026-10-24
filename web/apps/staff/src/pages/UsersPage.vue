<script setup lang="ts">
import {
  mdiAccountLockOutline,
  mdiAccountLockOpenOutline,
  mdiDialpad,
  mdiDotsVertical,
  mdiKeyChange,
  mdiLogoutVariant,
  mdiMagnify,
  mdiPencilOutline,
  mdiPlus,
} from '@mdi/js'
import { api, ApiError, type Problem, type User } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import ConfirmDialog from '@/components/ConfirmDialog.vue'
import PinDialog from '@/components/PinDialog.vue'
import TemporaryPasswordDialog from '@/components/TemporaryPasswordDialog.vue'
import UserFormDialog from '@/components/UserFormDialog.vue'
import { roleLabels } from '@/labels'
import { locationsQuery } from '@/queries'
import { useMe } from '@/session'

const queryClient = useQueryClient()
const { data: me } = useMe()
const { data: locations } = useQuery(locationsQuery)

const usersKey = ['users']
const {
  data: users,
  isPending,
  isError,
  error,
  refetch,
} = useQuery({
  queryKey: usersKey,
  queryFn: async () => {
    const { data, error } = await api.GET('/users')
    if (error) throw new ApiError(error)
    return data.items
  },
})

const search = ref('')
const headers = [
  { title: 'Сотрудник', key: 'fullName' },
  { title: 'Логин', key: 'login' },
  { title: 'Роль', key: 'role', value: (u: User) => roleLabels[u.role] },
  { title: 'Точки', key: 'locationIds', sortable: false },
  { title: 'Статус', key: 'status', sortable: false },
  { title: '', key: 'actions', sortable: false, align: 'end' as const },
]

const codes = computed(() => new Map((locations.value ?? []).map((l) => [l.id, l.code])))

function status(u: User) {
  if (u.isBlocked) return { text: 'Заблокирован', color: 'error' }
  if (u.lockedUntil && new Date(u.lockedUntil) > new Date()) {
    const until = new Date(u.lockedUntil).toLocaleTimeString('ru', {
      hour: '2-digit',
      minute: '2-digit',
    })
    return { text: `Вход закрыт до ${until}`, color: 'warning' }
  }
  if (u.mustChangePassword) return { text: 'Временный пароль', color: 'info' }
  return { text: 'Активен', color: 'success' }
}

const formOpen = ref(false)
const editing = ref<User | null>(null)
const issued = ref<{ login: string; password: string } | null>(null)
const pinFor = ref<User | null>(null)
// PIN нужен тем, кто продаёт на кассе (право pos.sell).
const sellers: User['role'][] = ['cashier', 'store_manager']

function openForm(user: User | null) {
  editing.value = user
  formOpen.value = true
}

const refresh = () => queryClient.invalidateQueries({ queryKey: usersKey })

function onCreated(user: User, password: string) {
  issued.value = { login: user.login, password }
  void refresh()
}

// Действия, которые нельзя отменить одним кликом, сначала подтверждаются.
interface Pending {
  title: string
  text: string
  confirmText: string
  run: () => Promise<void>
}
const pending = ref<Pending | null>(null)
const confirmOpen = computed({
  get: () => pending.value !== null,
  set: (open) => {
    if (!open) pending.value = null
  },
})

async function call(request: Promise<{ error?: Problem }>) {
  const { error } = await request
  if (error) throw new ApiError(error)
}

const path = (u: User) => ({ params: { path: { id: u.id } } })

const actions = {
  block: (u: User): Pending => ({
    title: 'Заблокировать сотрудника?',
    text: `${u.fullName} не сможет войти, все его сеансы сразу завершатся.`,
    confirmText: 'Заблокировать',
    run: () => call(api.POST('/users/{id}/block', path(u))),
  }),
  reset: (u: User): Pending => ({
    title: 'Сбросить пароль?',
    text: `Старый пароль ${u.fullName} перестанет действовать, сеансы завершатся. Вы получите временный пароль.`,
    confirmText: 'Сбросить',
    run: async () => {
      const { data, error } = await api.POST('/users/{id}/password', path(u))
      if (error) throw new ApiError(error)
      issued.value = { login: u.login, password: data.temporaryPassword }
    },
  }),
  endSessions: (u: User): Pending => ({
    title: 'Завершить все сеансы?',
    text: `${u.fullName} придётся войти заново на всех устройствах.`,
    confirmText: 'Завершить',
    run: () => call(api.DELETE('/users/{id}/sessions', path(u))),
  }),
}

const {
  mutate: run,
  isPending: running,
  error: actionError,
} = useMutation({
  mutationFn: (action: () => Promise<void>) => action(),
  onSuccess: () => (pending.value = null),
  onSettled: refresh,
})

function unblock(u: User) {
  run(() => call(api.POST('/users/{id}/unblock', path(u))))
}
</script>

<template>
  <v-container>
    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <h1 class="text-headline-small flex-grow-1">Сотрудники</h1>
      <v-text-field
        v-model="search"
        :prepend-inner-icon="mdiMagnify"
        placeholder="Поиск по ФИО или логину"
        density="compact"
        class="pk-search"
        clearable
      />
      <v-btn color="primary" :prepend-icon="mdiPlus" data-test="add-user" @click="openForm(null)">
        Добавить
      </v-btn>
    </div>

    <v-alert v-if="isError" type="error" title="Не удалось загрузить сотрудников" class="mb-4">
      {{ error?.message }}
      <template #append>
        <v-btn variant="text" @click="refetch()">Повторить</v-btn>
      </template>
    </v-alert>
    <v-alert v-if="actionError" type="error" class="mb-4">{{ actionError.message }}</v-alert>

    <v-card>
      <v-data-table
        :headers="headers"
        :items="users ?? []"
        :loading="isPending"
        :search="search"
        :filter-keys="['fullName', 'login']"
        :items-per-page="-1"
        item-value="id"
        mobile-breakpoint="sm"
        hide-default-footer
      >
        <template #[`item.login`]="{ value }">
          <span class="pk-code text-no-wrap">{{ value }}</span>
        </template>
        <template #[`item.locationIds`]="{ item }">
          {{ item.locationIds.map((id) => codes.get(id) ?? '…').join(', ') || 'вся сеть' }}
        </template>
        <template #[`item.status`]="{ item }">
          <v-chip size="small" :color="status(item).color">{{ status(item).text }}</v-chip>
        </template>
        <template #[`item.actions`]="{ item }">
          <v-menu>
            <template #activator="{ props }">
              <v-btn
                v-bind="props"
                :icon="mdiDotsVertical"
                variant="text"
                size="small"
                title="Действия"
              />
            </template>
            <v-list density="compact">
              <v-list-item
                :prepend-icon="mdiPencilOutline"
                title="Изменить"
                @click="openForm(item)"
              />
              <v-list-item
                :prepend-icon="mdiKeyChange"
                title="Сбросить пароль"
                @click="pending = actions.reset(item)"
              />
              <v-list-item
                v-if="sellers.includes(item.role)"
                :prepend-icon="mdiDialpad"
                title="PIN для кассы"
                @click="pinFor = item"
              />
              <v-list-item
                :prepend-icon="mdiLogoutVariant"
                title="Завершить сеансы"
                @click="pending = actions.endSessions(item)"
              />
              <v-list-item
                v-if="item.isBlocked"
                :prepend-icon="mdiAccountLockOpenOutline"
                title="Разблокировать"
                @click="unblock(item)"
              />
              <v-list-item
                v-else-if="item.id !== me?.id"
                :prepend-icon="mdiAccountLockOutline"
                title="Заблокировать"
                base-color="error"
                @click="pending = actions.block(item)"
              />
            </v-list>
          </v-menu>
        </template>
      </v-data-table>
    </v-card>

    <UserFormDialog v-model="formOpen" :user="editing" @created="onCreated" @saved="refresh" />
    <TemporaryPasswordDialog v-model="issued" />
    <PinDialog v-model="pinFor" />
    <ConfirmDialog
      v-model="confirmOpen"
      :title="pending?.title ?? ''"
      :text="pending?.text ?? ''"
      :confirm-text="pending?.confirmText ?? ''"
      :loading="running"
      @confirm="pending && run(pending.run)"
    />
  </v-container>
</template>

<style scoped>
.pk-search {
  max-width: 320px;
  min-width: 220px;
}
</style>
