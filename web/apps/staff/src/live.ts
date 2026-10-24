import { live, type LiveEvent } from '@pikni/api'
import { useQueryClient, type QueryKey } from '@tanstack/vue-query'
import { onScopeDispose, toValue, watch, type MaybeRefOrGetter } from 'vue'

// useLive перезапрашивает запросы экрана, когда сервер сообщает об изменении по одной из тем
// (doc:{id}, location:{id}, product:{id}) — у всех, у кого экран открыт (УВ-1).
export function useLive(
  topics: MaybeRefOrGetter<string[]>,
  keys: MaybeRefOrGetter<QueryKey[]>,
  onEvent?: (event: LiveEvent) => void,
) {
  const client = useQueryClient()
  const refresh = (event?: LiveEvent) => {
    for (const queryKey of toValue(keys)) void client.invalidateQueries({ queryKey })
    if (event) onEvent?.(event)
  }
  let stops: (() => void)[] = []
  watch(
    () => toValue(topics).join(' '),
    () => {
      stops.forEach((s) => s())
      stops = toValue(topics).map((t) => live.subscribe(t, refresh))
    },
    { immediate: true },
  )
  const stopReconnect = live.onReconnect(() => refresh())
  onScopeDispose(() => {
    stops.forEach((s) => s())
    stopReconnect()
  })
}
