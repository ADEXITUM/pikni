import { api, ApiError, type AssetEvent, type AssetStatus } from '@pikni/api'
import { queryOptions } from '@tanstack/vue-query'

export const assetStatusLabels: Record<AssetStatus, string> = {
  in_use: 'В эксплуатации',
  repair: 'В ремонте',
  written_off: 'Списан',
}

export const assetStatusColors: Record<AssetStatus, string> = {
  in_use: 'success',
  repair: 'warning',
  written_off: 'secondary',
}

export const assetCategoriesQuery = queryOptions({
  queryKey: ['ref', 'asset-categories'],
  queryFn: async () => {
    const { data, error } = await api.GET('/asset-categories')
    if (error) throw new ApiError(error)
    return data.items
  },
  staleTime: Infinity,
})

// Строка истории объекта (ИМ-3) по-русски.
export function describeEvent(e: AssetEvent): string {
  const status = (s?: string) => (s ? assetStatusLabels[s as AssetStatus].toLowerCase() : '')
  switch (e.kind) {
    case 'created':
      return `Принят к учёту в ${e.toLocationName}${e.toUserName ? `, МОЛ — ${e.toUserName}` : ''}`
    case 'moved':
      return `Перемещён: ${e.fromLocationName} → ${e.toLocationName}`
    case 'responsible':
      return `МОЛ: ${e.fromUserName ?? 'не назначен'} → ${e.toUserName ?? 'не назначен'}`
    case 'status':
      return `Состояние: ${status(e.fromStatus)} → ${status(e.toStatus)}`
    case 'writeoff_requested':
      return 'Оформлено списание'
    case 'writeoff_rejected':
      return 'Списание отклонено'
    case 'written_off':
      return 'Списан'
    case 'found_elsewhere':
      return `Найден при инвентаризации: числился в ${e.fromLocationName}, переведён в ${e.toLocationName}`
  }
}
