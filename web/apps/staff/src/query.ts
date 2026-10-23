import { ApiError } from '@pikni/api'
import { QueryClient } from '@tanstack/vue-query'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Ответы 4xx повторять бесполезно: ни права, ни данные от повтора не появятся.
      retry: (failures, error) =>
        failures < 2 && !(error instanceof ApiError && error.problem.status < 500),
    },
  },
})
