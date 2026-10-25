<script setup lang="ts">
import type { Location } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import { subjectsQuery } from '@/queries'

// Выбор точки: поиск по коду, названию, городу и региону. Когда точек много (федеральная сеть),
// рядом — фильтры региона и города; при выборе нескольких точек — «все точки региона/города».
defineOptions({ inheritAttrs: false })
const model = defineModel<string | string[] | null | undefined>()
const props = defineProps<{ locations: Location[]; multiple?: boolean }>()

const { data: subjects } = useQuery(subjectsQuery)
const subjectName = computed(() => new Map((subjects.value ?? []).map((s) => [s.code, s.name])))
const many = computed(() => props.locations.length > 20)
const subject = ref<string | null>(null)
const city = ref<string | null>(null)

const subjectItems = computed(() =>
  [...new Set(props.locations.map((l) => l.subjectCode))]
    .map((code) => ({ value: code, title: subjectName.value.get(code) ?? code }))
    .sort((a, b) => a.title.localeCompare(b.title, 'ru')),
)
const cityItems = computed(() =>
  [
    ...new Set(
      props.locations
        .filter((l) => !subject.value || l.subjectCode === subject.value)
        .map((l) => l.city),
    ),
  ].sort((a, b) => a.localeCompare(b, 'ru')),
)
const narrowed = computed(() =>
  props.locations.filter(
    (l) =>
      (!subject.value || l.subjectCode === subject.value) && (!city.value || l.city === city.value),
  ),
)
// Выбранная точка остаётся в списке, даже если не подходит под фильтр региона и города.
const items = computed(() => {
  const chosen = new Set(
    Array.isArray(model.value) ? model.value : model.value ? [model.value] : [],
  )
  const shown = new Set(narrowed.value.map((l) => l.id))
  return props.locations
    .filter((l) => shown.has(l.id) || chosen.has(l.id))
    .map((l) => ({
      value: l.id,
      title: `${l.code} · ${l.name}`,
      search:
        `${l.code} ${l.name} ${l.city} ${subjectName.value.get(l.subjectCode) ?? ''}`.toLowerCase(),
      city: l.city,
    }))
})
const filter = (_: string, query: string, item?: { raw: { search: string } }) =>
  !!item && item.raw.search.includes(query.toLowerCase())

function selectAll() {
  model.value = narrowed.value.map((l) => l.id)
}
</script>

<template>
  <div class="pk-location-select" :class="$attrs.class">
    <template v-if="many">
      <v-autocomplete
        v-model="subject"
        :items="subjectItems"
        label="Регион"
        density="compact"
        hide-details
        clearable
        class="pk-narrow"
        @update:model-value="city = null"
      />
      <v-autocomplete
        v-model="city"
        :items="cityItems"
        label="Город"
        density="compact"
        hide-details
        clearable
        class="pk-narrow"
      />
    </template>
    <v-autocomplete
      v-bind="{ ...$attrs, class: undefined }"
      v-model="model"
      :items="items"
      :multiple="multiple"
      :custom-filter="filter"
      class="pk-main"
      no-data-text="Точек не нашлось"
    >
      <template #item="{ props: itemProps, item }">
        <v-list-item v-bind="itemProps" :subtitle="item.city" />
      </template>
    </v-autocomplete>
    <v-btn
      v-if="multiple && many && (subject || city)"
      variant="tonal"
      size="small"
      class="align-self-center"
      @click="selectAll"
      >Все {{ narrowed.length }}</v-btn
    >
  </div>
</template>

<style scoped>
.pk-location-select {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: flex-start;
}
.pk-narrow {
  flex: 1 1 160px;
  max-width: 220px;
}
.pk-main {
  flex: 2 1 240px;
}
</style>
