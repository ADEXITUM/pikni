import type { Location, Permission } from '@pikni/api'
import { useQuery } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'

import { networkRoles } from '@/labels'
import { locationsQuery } from '@/queries'
import { useMe } from '@/session'
import { ALL_LOCATIONS, useCurrentLocation } from '@/stores/currentLocation'

// useLocationChoice — точка, с которой работает экран. У ролей сети — любая действующая точка,
// у сотрудников точек — свои, где есть право. По умолчанию — точка, выбранная в шапке.
// Сервер всё равно проверяет точку: выбор здесь только подсказка.
export function useLocationChoice(permission: Permission, filter?: (l: Location) => boolean) {
  const { data: me } = useMe()
  const { data: locations } = useQuery(locationsQuery)
  const current = useCurrentLocation()

  const options = computed(() => {
    const m = me.value
    if (!m?.permissions.includes(permission)) return []
    const all = (locations.value ?? []).filter((l) => l.isActive && (!filter || filter(l)))
    return networkRoles.has(m.role) ? all : all.filter((l) => m.locationIds.includes(l.id))
  })
  const selectedId = ref<string>()
  watch(
    options,
    (opts) => {
      if (selectedId.value && opts.some((l) => l.id === selectedId.value)) return
      const header = current.selected !== ALL_LOCATIONS ? current.selected : null
      selectedId.value = opts.find((l) => l.id === header)?.id ?? opts[0]?.id
    },
    { immediate: true },
  )
  const selected = computed(() => options.value.find((l) => l.id === selectedId.value))
  const items = computed(() =>
    options.value.map((l) => ({ value: l.id, title: `${l.code} · ${l.name}` })),
  )
  return { options, items, selectedId, selected }
}

// Короткая запись количества со знаком для карточки движения: «+12», «−3».
export const signed = (n: number) => (n > 0 ? `+${n}` : n < 0 ? `−${-n}` : '0')
