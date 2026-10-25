<script setup lang="ts">
import { mdiRefresh } from '@mdi/js'
import { api, ApiError, type ReconciliationProduct } from '@pikni/api'
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'

import { formatDateTime } from '@/format'
import { markStatusLabels } from '@/labels'
import { useLocationChoice } from '@/stock'

// Сверка с ЕГАИС (СО-9): остатки точки в ЕГАИС против учёта системы. Система запрашивает их
// раз в сутки сама; кнопка — внеочередной запрос, ответ УТМ приходит обычно за минуту.
const place = useLocationChoice('egais.reconcile', (l) => !!l.fsrarId)
const queryClient = useQueryClient()
const tab = ref<'backroom' | 'floor' | 'marks'>('backroom')
const key = computed(() => ['reconciliation', place.selectedId.value])

const { data, error, isPending } = useQuery({
  queryKey: key,
  enabled: computed(() => !!place.selectedId.value),
  queryFn: async () => {
    const { data, error } = await api.GET('/egais/reconciliation/{locationId}', {
      params: { path: { locationId: place.selectedId.value! } },
    })
    if (error) throw new ApiError(error)
    return data
  },
  // Пока ответ на запрос не пришёл, страница проверяет его каждые 10 с.
  refetchInterval: (q) => {
    const d = q.state.data
    return d?.requestedAt && (!d.takenAt || d.takenAt < d.requestedAt) ? 10_000 : false
  },
})
const waiting = computed(
  () =>
    !!data.value?.requestedAt &&
    (!data.value.takenAt || data.value.takenAt < data.value.requestedAt),
)

const {
  mutate: request,
  isPending: requesting,
  error: requestError,
} = useMutation({
  mutationFn: async () => {
    const { error } = await api.POST('/egais/reconciliation/{locationId}', {
      params: { path: { locationId: place.selectedId.value! } },
    })
    if (error) throw new ApiError(error)
  },
  onSuccess: () => queryClient.invalidateQueries({ queryKey: key.value }),
})

const products = (register: 1 | 2) =>
  (data.value?.products ?? []).filter((p) => p.register === register)
const backroom = computed(() => products(1))
const floor = computed(() => products(2))
const onlyOurs = computed(() => (data.value?.marks ?? []).filter((m) => m.onlyOurs))
const onlyEgais = computed(() => (data.value?.marks ?? []).filter((m) => !m.onlyOurs))

// Подсказка, что делать с расхождением, — по его знаку и регистру.
function hint(p: ReconciliationProduct) {
  if (p.ours > p.egais) {
    return p.register === 2
      ? 'В ЕГАИС меньше: проверьте, ушёл ли перевод в зал и чеки; при излишке — ревизия.'
      : 'В ЕГАИС меньше: проверьте, подтверждены ли ТТН и не висит ли акт расхождения.'
  }
  return p.register === 2
    ? 'В ЕГАИС больше: вероятно, продажа или списание не дошли до ЕГАИС — проверьте обмен.'
    : 'В ЕГАИС больше: не принятая у нас ТТН или недостача — проверьте поступления и ревизию.'
}
const short = (raw: string) => (raw.length > 32 ? `${raw.slice(0, 32)}…` : raw)
</script>

<template>
  <v-container class="pk-reconcile">
    <h1 class="text-headline-small mb-1">Сверка с ЕГАИС</h1>
    <p class="text-body-medium text-medium-emphasis mb-4">
      Остатки точки в ЕГАИС сравниваются с учётом системы: регистр 1 — склад, регистр 2 — торговый
      зал, марки — поштучно. Показаны только расхождения.
    </p>

    <div class="d-flex flex-wrap align-center ga-3 mb-4">
      <v-select
        v-model="place.selectedId.value"
        :items="place.items.value"
        label="Точка"
        hide-details
        density="compact"
        class="pk-place"
      />
      <v-btn
        :prepend-icon="mdiRefresh"
        variant="tonal"
        :loading="requesting"
        :disabled="!place.selectedId.value || waiting"
        data-test="request-rests"
        @click="request()"
        >Запросить остатки</v-btn
      >
    </div>
    <p v-if="!place.options.value.length" class="text-body-medium text-medium-emphasis">
      Нет точек с ФСРАР ИД, доступных для сверки.
    </p>
    <v-alert v-if="error || requestError" type="error" class="mb-3">
      {{ (error ?? requestError)?.message }}
    </v-alert>
    <v-progress-linear v-if="isPending && place.selectedId.value" indeterminate />

    <template v-if="data">
      <p class="text-body-medium mb-3" data-test="snapshot">
        <template v-if="data.takenAt"
          >Остатки ЕГАИС на {{ formatDateTime(data.takenAt) }}.</template
        >
        <template v-else>Остатки ещё не запрашивались.</template>
        <template v-if="waiting">
          Ждём ответа УТМ на запрос от {{ formatDateTime(data.requestedAt!) }}…</template
        >
      </p>

      <template v-if="data.takenAt">
        <v-tabs v-model="tab" class="mb-3">
          <v-tab value="backroom">Склад · {{ backroom.length }}</v-tab>
          <v-tab value="floor">Зал · {{ floor.length }}</v-tab>
          <v-tab value="marks">Марки · {{ data.marks.length }}</v-tab>
        </v-tabs>

        <template v-if="tab !== 'marks'">
          <p
            v-if="!(tab === 'backroom' ? backroom : floor).length"
            class="text-body-medium text-success"
          >
            Расхождений нет.
          </p>
          <v-table v-else density="comfortable" data-test="products">
            <thead>
              <tr>
                <th>Продукция</th>
                <th class="text-end">У нас</th>
                <th class="text-end">ЕГАИС</th>
                <th class="text-end">Разница</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in tab === 'backroom' ? backroom : floor" :key="p.alcocode">
                <td>
                  <div>{{ p.name }}</div>
                  <div class="text-body-small text-medium-emphasis">
                    <span class="pk-code">{{ p.alcocode }}</span> · {{ hint(p) }}
                  </div>
                </td>
                <td class="text-end">{{ p.ours }}</td>
                <td class="text-end">{{ p.egais }}</td>
                <td class="text-end font-weight-bold">
                  {{ p.ours > p.egais ? '+' : '' }}{{ p.ours - p.egais }}
                </td>
              </tr>
            </tbody>
          </v-table>
        </template>

        <template v-else>
          <p v-if="!data.marks.length" class="text-body-medium text-success">Расхождений нет.</p>
          <template v-if="onlyOurs.length">
            <h2 class="text-title-medium mb-1">
              Есть у нас, нет на балансе ЕГАИС · {{ onlyOurs.length }}
            </h2>
            <p class="text-body-small text-medium-emphasis mb-2">
              Продажа или списание прошли в ЕГАИС, но не у нас, либо поступление не подтверждено.
              Найдите бутылку ревизией.
            </p>
            <v-list density="compact" class="mb-4" data-test="marks-ours">
              <v-list-item
                v-for="m in onlyOurs"
                :key="m.code"
                :title="m.name"
                :subtitle="short(m.code)"
                :to="{ name: 'scan', query: { code: m.code } }"
              />
            </v-list>
          </template>
          <template v-if="onlyEgais.length">
            <h2 class="text-title-medium mb-1">Есть в ЕГАИС, нет у нас · {{ onlyEgais.length }}</h2>
            <p class="text-body-small text-medium-emphasis mb-2">
              Марка числится на балансе точки, а в учёте её нет: бутылку продали без марки в чеке
              или она не была поставлена на учёт. Найденную ставят на учёт ревизией, пропавшую —
              списывают.
            </p>
            <v-list density="compact" data-test="marks-egais">
              <v-list-item v-for="m in onlyEgais" :key="m.code" :title="m.name">
                <v-list-item-subtitle>
                  <span class="pk-code">{{ short(m.code) }}</span>
                  <template v-if="m.status">
                    · у нас: {{ markStatusLabels[m.status].toLowerCase() }}
                    <router-link
                      v-if="m.documentId"
                      :to="{ name: 'document', params: { id: m.documentId } }"
                      >{{ m.documentNumber }}</router-link
                    ><template v-if="m.status === 'in_transit'">
                      — ЕГАИС спишет её с баланса, когда получатель подтвердит ТТН</template
                    >
                  </template>
                </v-list-item-subtitle>
              </v-list-item>
            </v-list>
          </template>
        </template>
      </template>
    </template>
  </v-container>
</template>

<style scoped>
.pk-reconcile {
  max-width: 960px;
}
.pk-place {
  max-width: 360px;
}
</style>
