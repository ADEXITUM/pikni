<script setup lang="ts">
import { mdiCalendarRemove, mdiDeleteOutline, mdiPlus } from '@mdi/js'
import { api, ApiError, type SaleBan, type SaleRule } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import SaleBanDialog from '@/components/SaleBanDialog.vue'
import SaleRuleDialog from '@/components/SaleRuleDialog.vue'
import { saleScopeLabels } from '@/labels'
import { banDate, editionStatuses } from '@/sale'
import { useMe } from '@/session'

const queryClient = useQueryClient()
const { data: me } = useMe()
const canEdit = computed(() => me.value?.permissions.includes('refs.edit') ?? false)

const bookKey = ['sale-rules']
const {
  data: book,
  isError,
  error,
} = useQuery({
  queryKey: bookKey,
  queryFn: async () => {
    const { data, error } = await api.GET('/sale-rules')
    if (error) throw new ApiError(error)
    return data
  },
})
const { data: subjects } = useQuery({
  queryKey: ['ru-subjects'],
  queryFn: async () => {
    const { data, error } = await api.GET('/ru-subjects')
    if (error) throw new ApiError(error)
    return data.items
  },
  staleTime: Infinity,
})

const today = new Date().toLocaleDateString('sv')
const statuses = computed(() => editionStatuses(book.value?.rules ?? [], today))
const statusChip = {
  current: { text: 'действует', color: 'success' },
  planned: { text: 'вступит в силу', color: 'info' },
  past: { text: 'прежняя редакция', color: undefined },
}

// Сначала федеральные правила, затем регионы в порядке справочника.
const regions = computed(() => {
  const rules = book.value?.rules ?? []
  const bans = book.value?.bans ?? []
  const byScope = (list: SaleRule[]) =>
    [...list].sort(
      (a, b) => a.scope.localeCompare(b.scope) || b.validFrom.localeCompare(a.validFrom),
    )
  return [
    {
      code: '',
      title: 'Вся Россия',
      rules: byScope(rules.filter((r) => !r.subjectCode)),
      bans: [] as SaleBan[],
    },
    ...(subjects.value ?? []).map((s) => ({
      code: s.code,
      title: `${s.code} — ${s.name}`,
      rules: byScope(rules.filter((r) => r.subjectCode === s.code)),
      bans: bans.filter((b) => b.subjectCode === s.code),
    })),
  ]
})

const ruleOpen = ref(false)
const banOpen = ref(false)
const refresh = () => queryClient.invalidateQueries({ queryKey: bookKey })

const { mutate: remove, error: removeError } = useMutation({
  mutationFn: async (target: { kind: 'rule' | 'ban'; id: string }) => {
    const params = { params: { path: { id: target.id } } }
    const { error } =
      target.kind === 'rule'
        ? await api.DELETE('/sale-rules/{id}', params)
        : await api.DELETE('/sale-bans/{id}', params)
    if (error) throw new ApiError(error)
  },
  onSettled: refresh,
})

const date = (iso: string) => new Date(`${iso}T00:00`).toLocaleDateString('ru')
</script>

<template>
  <v-container class="pk-rules">
    <div class="d-flex flex-wrap align-center ga-3 mb-2">
      <h1 class="text-headline-small flex-grow-1">Правила продажи алкоголя</h1>
      <template v-if="canEdit">
        <v-btn :prepend-icon="mdiPlus" color="primary" @click="ruleOpen = true"
          >Новая редакция</v-btn
        >
        <v-btn :prepend-icon="mdiCalendarRemove" variant="tonal" @click="banOpen = true">
          День запрета
        </v-btn>
      </template>
    </div>
    <p class="text-body-medium text-medium-emphasis mb-4">
      Продавать можно только когда разрешают все действующие правила сразу: федеральное и
      региональное, общее и для крепкого. Время — местное время точки.
    </p>

    <v-alert v-if="isError" type="error" title="Не удалось загрузить правила" class="mb-4">
      {{ error?.message }}
    </v-alert>
    <v-alert v-if="removeError" type="error" class="mb-4">{{ removeError.message }}</v-alert>

    <v-card v-for="region in regions" :key="region.code" :title="region.title" class="mb-4">
      <v-table density="comfortable">
        <thead>
          <tr>
            <th>Продукция</th>
            <th>Окно продажи</th>
            <th>Действует с</th>
            <th>Основание</th>
            <th v-if="canEdit"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in region.rules" :key="r.id" :data-test="`rule-${r.id}`">
            <td>{{ saleScopeLabels[r.scope] }}</td>
            <td class="text-no-wrap">{{ r.from }}–{{ r.to }}</td>
            <td class="text-no-wrap">
              {{ date(r.validFrom) }}
              <v-chip
                size="x-small"
                :color="statusChip[statuses.get(r.id) ?? 'past'].color"
                class="ml-1"
              >
                {{ statusChip[statuses.get(r.id) ?? 'past'].text }}
              </v-chip>
            </td>
            <td class="text-body-medium">{{ r.source }}</td>
            <td v-if="canEdit" class="text-end">
              <v-btn
                v-if="statuses.get(r.id) === 'planned'"
                :icon="mdiDeleteOutline"
                variant="text"
                size="small"
                title="Удалить редакцию, которая ещё не вступила в силу"
                @click="remove({ kind: 'rule', id: r.id })"
              />
            </td>
          </tr>
          <tr v-if="region.rules.length === 0">
            <td colspan="5" class="text-medium-emphasis">
              Своих ограничений нет — действуют федеральные.
            </td>
          </tr>
        </tbody>
      </v-table>

      <template v-if="region.code">
        <v-divider />
        <v-card-subtitle class="pt-3">Дни полного запрета</v-card-subtitle>
        <v-list density="compact">
          <v-list-item
            v-for="b in region.bans"
            :key="b.id"
            :title="`${b.name} — ${banDate(b)}`"
            :subtitle="`${saleScopeLabels[b.scope]}. ${b.source}`"
          >
            <template v-if="canEdit" #append>
              <v-btn
                :icon="mdiDeleteOutline"
                variant="text"
                size="small"
                title="Удалить день запрета"
                @click="remove({ kind: 'ban', id: b.id })"
              />
            </template>
          </v-list-item>
          <v-list-item v-if="region.bans.length === 0" subtitle="Нет" />
        </v-list>
      </template>
    </v-card>

    <SaleRuleDialog v-model="ruleOpen" :subjects="subjects ?? []" @saved="refresh" />
    <SaleBanDialog v-model="banOpen" :subjects="subjects ?? []" @saved="refresh" />
  </v-container>
</template>

<style scoped>
.pk-rules {
  max-width: 1000px;
}
</style>
