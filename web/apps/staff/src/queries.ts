import { api, ApiError, type Problem } from '@pikni/api'
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

async function items<T>(request: Promise<{ data?: { items: T[] }; error?: Problem }>) {
  const { data, error } = await request
  if (error) throw new ApiError(error)
  return data!.items
}

// Ключи ['ref', …] те же, что у вкладок «Справочников»: после правки там обновятся и списки
// выбора в карточке позиции.
export const countriesQuery = queryOptions({
  queryKey: ['ref', 'countries'],
  queryFn: () => items(api.GET('/countries')),
  staleTime: Infinity,
})

export const productKindsQuery = queryOptions({
  queryKey: ['ref', 'product-kinds'],
  queryFn: () => items(api.GET('/product-kinds')),
  staleTime: Infinity,
})

export const producersQuery = queryOptions({
  queryKey: ['ref', 'producers'],
  queryFn: () => items(api.GET('/producers')),
})

export const brandsQuery = queryOptions({
  queryKey: ['ref', 'brands'],
  queryFn: () => items(api.GET('/brands')),
})

export const productLinesQuery = queryOptions({
  queryKey: ['ref', 'product-lines'],
  queryFn: () => items(api.GET('/product-lines')),
})

export const originRegionsQuery = queryOptions({
  queryKey: ['ref', 'origin-regions'],
  queryFn: () => items(api.GET('/origin-regions')),
})

export const categoriesQuery = queryOptions({
  queryKey: ['categories'],
  queryFn: async () => {
    const { data, error } = await api.GET('/categories')
    if (error) throw new ApiError(error)
    return data
  },
})
