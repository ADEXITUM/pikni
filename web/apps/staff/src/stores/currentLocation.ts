import type { Me } from '@pikni/api'
import { defineStore } from 'pinia'
import { ref } from 'vue'

import { load, save } from '@/preferences'

export const ALL_LOCATIONS = 'all'

export function locationOptions(me: Me): string[] {
  return me.locationIds.length > 1 ? [ALL_LOCATIONS, ...me.locationIds] : me.locationIds
}

const storageKey = (userId: string) => `pikni.location.${userId}`

// Текущая точка — фильтр интерфейса, запоминается на устройстве для каждого пользователя.
// Доступ к точке проверяет сервер, поэтому подмена значения в localStorage ничего не даёт.
export const useCurrentLocation = defineStore('currentLocation', () => {
  const selected = ref<string | null>(null)
  let userId = ''

  function restore(me: Me) {
    userId = me.id
    const options = locationOptions(me)
    const saved = load(storageKey(me.id))
    selected.value = saved && options.includes(saved) ? saved : (options[0] ?? null)
  }

  function select(id: string) {
    selected.value = id
    save(storageKey(userId), id)
  }

  return { selected, restore, select }
})
