<script setup lang="ts">
import { mdiArrowLeft, mdiChevronRight } from '@mdi/js'
import { api, ApiError, type Location, type LocationKind, type OpeningHours } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, reactive, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { SubmitEventPromise } from 'vuetify'

import OpeningHoursEditor from '@/components/OpeningHoursEditor.vue'
import UnsavedBar from '@/components/UnsavedBar.vue'
import { confirmChanges, diffFields } from '@/confirm'
import { formatHours } from '@/hours'
import { locationKindLabels, russianTimezones, zoneRegisters } from '@/labels'
import { describeVerdict } from '@/sale'
import { useMe } from '@/session'
import { useUnsavedGuard } from '@/unsaved'
import { subjectsQuery } from '@/queries'

const route = useRoute()
const router = useRouter()
const queryClient = useQueryClient()
const { data: me } = useMe()

const isNew = computed(() => route.name === 'location-new')
const id = computed(() => String(route.params.id ?? ''))
const canEdit = computed(() => me.value?.permissions.includes('refs.edit') ?? false)

const {
  data: location,
  isError,
  error: loadError,
} = useQuery({
  queryKey: ['location', id],
  queryFn: async () => {
    const { data, error } = await api.GET('/locations/{id}', { params: { path: { id: id.value } } })
    if (error) throw new ApiError(error)
    return data
  },
  enabled: computed(() => !isNew.value),
})

// Обновляется раз в минуту: окно продажи может закрыться, пока страница открыта.
const { data: sale } = useQuery({
  queryKey: ['sale-check', id],
  queryFn: async () => {
    const { data, error } = await api.GET('/locations/{id}/sale-check', {
      params: { path: { id: id.value } },
    })
    if (error) throw new ApiError(error)
    return data
  },
  enabled: computed(() => !isNew.value),
  refetchInterval: 60_000,
})

// Сама матрица — на отдельной странице: в магазине тысячи вариантов. Здесь только их число.
const { data: assortmentTotal } = useQuery({
  queryKey: ['assortment', id, 'total'],
  queryFn: async () => {
    const { data, error } = await api.GET('/locations/{id}/assortment', {
      params: { path: { id: id.value }, query: { limit: 1 } },
    })
    if (error) throw new ApiError(error)
    return data.total
  },
  enabled: computed(() => !isNew.value),
})
const assortmentSummary = computed(() => {
  if (assortmentTotal.value === undefined) return 'Что точка должна держать в наличии'
  return assortmentTotal.value ? `Вариантов: ${assortmentTotal.value}` : 'Пока пуста'
})

const { data: subjects } = useQuery(subjectsQuery)
const subjectItems = computed(() =>
  (subjects.value ?? []).map((s) => ({ value: s.code, title: `${s.code} — ${s.name}` })),
)
const kindItems = Object.entries(locationKindLabels).map(([value, title]) => ({ value, title }))

// Числа и даты в полях ввода — строки; пустая строка значит «не указано».
const form = reactive({
  code: '',
  kind: 'store' as LocationKind,
  name: '',
  city: '',
  timezone: 'Asia/Yakutsk',
  subjectCode: '28',
  address: '',
  lat: '',
  lon: '',
  fsrarId: '',
  kpp: '',
  licenseNo: '',
  licenseValidTo: '',
  openingHours: {} as OpeningHours,
  isActive: true,
})

const formOf = (l: Location): typeof form => ({
  code: l.code,
  kind: l.kind,
  name: l.name,
  city: l.city,
  timezone: l.timezone,
  subjectCode: l.subjectCode,
  address: l.address ?? '',
  lat: l.lat?.toString() ?? '',
  lon: l.lon?.toString() ?? '',
  fsrarId: l.fsrarId ?? '',
  kpp: l.kpp ?? '',
  licenseNo: l.licenseNo ?? '',
  licenseValidTo: l.licenseValidTo ?? '',
  openingHours: { ...l.openingHours },
  isActive: l.isActive,
})

watch(
  location,
  (l) => {
    if (l) Object.assign(form, formOf(l))
  },
  { immediate: true },
)

// Правка показывается до сохранения: «было — станет» по каждому полю.
const changeLabels = {
  name: 'Название',
  city: 'Город',
  timezone: 'Часовой пояс',
  subject: 'Регион',
  address: 'Адрес',
  coordinates: 'Координаты',
  fsrarId: 'ФСРАР ID',
  kpp: 'КПП',
  licenseNo: 'Лицензия',
  licenseValidTo: 'Лицензия до',
  hours: 'Режим работы',
  active: 'Точка действует',
}
const describe = (f: typeof form): Record<string, unknown> => ({
  name: f.name.trim(),
  city: f.city.trim(),
  timezone: f.timezone,
  subject: subjectItems.value.find((s) => s.value === f.subjectCode)?.title ?? f.subjectCode,
  address: f.address.trim(),
  coordinates: f.lat.trim() && f.lon.trim() ? `${f.lat.trim()}, ${f.lon.trim()}` : '',
  fsrarId: f.fsrarId.trim(),
  kpp: f.kpp.trim(),
  licenseNo: f.licenseNo.trim(),
  licenseValidTo: f.licenseValidTo,
  hours: formatHours(f.openingHours),
  active: f.isActive,
})
const changes = computed(() =>
  location.value ? diffFields(describe(formOf(location.value)), describe(form), changeLabels) : [],
)
const dirty = computed(() => !isNew.value && canEdit.value && changes.value.length > 0)
useUnsavedGuard(dirty, 'карточке точки')
const discard = () => location.value && Object.assign(form, formOf(location.value))

const optional = (v: string) => v.trim() || undefined
const number = (v: string) => (v.trim() === '' ? undefined : Number(v.replace(',', '.')))

const required = (v: string) => v.trim() !== '' || 'Обязательное поле'
const digits = (n: number, name: string) => (v: string) =>
  v.trim() === '' || new RegExp(`^\\d{${n}}$`).test(v.trim()) || `${name} — ${n} цифр`

const {
  mutate: save,
  isPending: saving,
  error: saveError,
} = useMutation({
  mutationFn: async (): Promise<Location> => {
    const fields = {
      name: form.name,
      city: form.city,
      timezone: form.timezone,
      subjectCode: form.subjectCode,
      address: optional(form.address),
      lat: number(form.lat),
      lon: number(form.lon),
      fsrarId: optional(form.fsrarId),
      kpp: optional(form.kpp),
      licenseNo: optional(form.licenseNo),
      licenseValidTo: optional(form.licenseValidTo),
      openingHours: form.openingHours,
    }
    const { data, error } = isNew.value
      ? await api.POST('/locations', { body: { ...fields, code: form.code, kind: form.kind } })
      : await api.PUT('/locations/{id}', {
          params: { path: { id: id.value } },
          body: { ...fields, isActive: form.isActive, version: location.value!.version },
        })
    if (error) throw new ApiError(error)
    return data
  },
  onSuccess: async (saved) => {
    queryClient.setQueryData(['location', saved.id], saved)
    await queryClient.invalidateQueries({ queryKey: ['locations'] })
    if (isNew.value) await router.replace({ name: 'location', params: { id: saved.id } })
  },
})

const title = computed(() =>
  isNew.value
    ? 'Новая точка'
    : location.value
      ? `${location.value.code} · ${location.value.name}`
      : '',
)

async function submit(event: SubmitEventPromise) {
  if (!(await event).valid) return
  if (!isNew.value && !(await confirmChanges('Сохранить изменения точки?', changes.value))) return
  save()
}
</script>

<template>
  <v-container class="pk-location">
    <div class="d-flex align-center ga-2 mb-4">
      <v-btn
        :icon="mdiArrowLeft"
        variant="text"
        :to="{ name: 'locations' }"
        title="К списку точек"
      />
      <h1 class="text-headline-small">{{ title }}</h1>
      <v-chip v-if="location && !location.isActive" color="error" size="small">Не действует</v-chip>
    </div>

    <v-alert v-if="isError" type="error" title="Не удалось загрузить точку" class="mb-4">
      {{ loadError?.message }}
    </v-alert>

    <v-card v-if="sale" class="mb-4" data-test="sale-now">
      <v-card-item
        title="Продажа алкоголя сейчас"
        :subtitle="`местное время ${sale.localTime.slice(11, 16)}`"
      />
      <v-list density="compact">
        <v-list-item
          title="Крепче 15 %"
          :subtitle="describeVerdict(sale.strong)"
          :base-color="sale.strong.allowed ? 'success' : 'error'"
        />
        <v-list-item
          title="Пиво, вино и прочее до 15 %"
          :subtitle="describeVerdict(sale.other)"
          :base-color="sale.other.allowed ? 'success' : 'error'"
        />
      </v-list>
    </v-card>

    <v-form v-if="isNew || location" :readonly="!canEdit" @submit.prevent="submit">
      <v-card title="Основное" class="mb-4">
        <v-card-text>
          <v-row density="compact">
            <v-col cols="12" sm="4">
              <v-text-field
                v-model="form.code"
                label="Код"
                :disabled="!isNew || !canEdit"
                :rules="[required]"
                data-test="code"
              />
            </v-col>
            <v-col cols="12" sm="8">
              <v-select
                v-model="form.kind"
                label="Тип"
                :items="kindItems"
                :disabled="!isNew || !canEdit"
                :hint="
                  isNew ? 'Тип не меняется после создания: от него зависят зоны учёта' : undefined
                "
                persistent-hint
              />
            </v-col>
            <v-col cols="12">
              <v-text-field
                v-model="form.name"
                label="Название"
                :rules="[required]"
                data-test="name"
              />
            </v-col>
            <v-col cols="12" sm="4">
              <v-text-field
                v-model="form.city"
                label="Город"
                :rules="[required]"
                data-test="city"
              />
            </v-col>
            <v-col cols="12" sm="4">
              <v-select v-model="form.subjectCode" label="Регион" :items="subjectItems" />
            </v-col>
            <v-col cols="12" sm="4">
              <v-select v-model="form.timezone" label="Часовой пояс" :items="russianTimezones" />
            </v-col>
            <v-col cols="12">
              <v-text-field v-model="form.address" label="Адрес" />
            </v-col>
            <v-col cols="6">
              <v-text-field v-model="form.lat" label="Широта" inputmode="decimal" data-test="lat" />
            </v-col>
            <v-col cols="6">
              <v-text-field
                v-model="form.lon"
                label="Долгота"
                inputmode="decimal"
                data-test="lon"
              />
            </v-col>
            <v-col v-if="!isNew" cols="12" sm="6">
              <v-switch v-model="form.isActive" label="Точка действует" />
            </v-col>
          </v-row>
        </v-card-text>
      </v-card>

      <v-card title="Реквизиты ЕГАИС" class="mb-4">
        <v-card-text>
          <v-row density="compact">
            <v-col cols="12" sm="6">
              <v-text-field
                v-model="form.fsrarId"
                label="ФСРАР ID"
                inputmode="numeric"
                :rules="[digits(12, 'ФСРАР ID')]"
              />
            </v-col>
            <v-col cols="12" sm="6">
              <v-text-field
                v-model="form.kpp"
                label="КПП"
                inputmode="numeric"
                :rules="[digits(9, 'КПП')]"
              />
            </v-col>
            <v-col cols="12" sm="6">
              <v-text-field v-model="form.licenseNo" label="Номер лицензии" />
            </v-col>
            <v-col cols="12" sm="6">
              <v-text-field
                v-model="form.licenseValidTo"
                label="Лицензия действует до"
                type="date"
              />
            </v-col>
          </v-row>
        </v-card-text>
      </v-card>

      <v-card title="Режим работы" class="mb-4">
        <v-card-text>
          <OpeningHoursEditor v-model="form.openingHours" :readonly="!canEdit" />
        </v-card-text>
      </v-card>

      <v-card v-if="location" title="Зоны учёта" class="mb-4">
        <v-list density="compact">
          <v-list-item
            v-for="zone in location.zones"
            :key="zone.id"
            :title="zone.name"
            :subtitle="zoneRegisters[zone.kind]"
          />
        </v-list>
      </v-card>

      <v-alert v-if="saveError" type="error" class="mb-4">
        {{ saveError instanceof ApiError ? saveError.message : 'Нет связи с сервером' }}
      </v-alert>

      <div v-if="canEdit && isNew" class="d-flex justify-end">
        <v-btn type="submit" color="primary" size="large" :loading="saving" data-test="save">
          Создать точку
        </v-btn>
      </div>
      <UnsavedBar v-if="dirty" :saving="saving" @discard="discard" />
    </v-form>

    <v-card
      v-if="location"
      class="mt-4"
      :to="{ name: 'location-assortment', params: { id: location.id } }"
      data-test="assortment-link"
    >
      <v-card-item
        title="Ассортиментная матрица"
        :subtitle="assortmentSummary"
        :append-icon="mdiChevronRight"
      />
    </v-card>
  </v-container>
</template>

<style scoped>
.pk-location {
  max-width: 860px;
}
</style>
