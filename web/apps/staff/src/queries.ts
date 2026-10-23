import { api, ApiError } from '@pikni/api'
import { queryOptions } from '@tanstack/vue-query'

export const locationsQuery = queryOptions({
  queryKey: ['locations'],
  queryFn: async () => {
    const { data, error } = await api.GET('/locations')
    if (error) throw new ApiError(error)
    return data.items
  },
  staleTime: 5 * 60 * 1000,
})
