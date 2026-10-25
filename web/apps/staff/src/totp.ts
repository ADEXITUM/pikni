import { api, ApiError } from '@pikni/api'
import { qrSvg } from '@pikni/scanner'
import { useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'

import { meQuery } from '@/session'

// useTotpSetup — подключение второго фактора (ПБ-3): ключ и QR-код для приложения-аутентификатора,
// проверка первого кода, резервные коды. Общее для профиля и экрана обязательного подключения.
export function useTotpSetup() {
  const queryClient = useQueryClient()
  const setup = ref<{ secret: string; qr: string }>()
  const code = ref('')
  const recovery = ref<string[]>()
  const error = ref<string>()
  const busy = ref(false)

  async function start() {
    error.value = undefined
    const { data, error: problem } = await api.POST('/auth/totp/setup')
    if (problem) return (error.value = new ApiError(problem).message)
    setup.value = { secret: data.secret, qr: await qrSvg(data.url) }
  }

  async function enable() {
    busy.value = true
    error.value = undefined
    const { data, error: problem } = await api.POST('/auth/totp/enable', {
      body: { code: code.value },
    })
    busy.value = false
    if (problem) return (error.value = new ApiError(problem).message)
    recovery.value = data.recoveryCodes
    setup.value = undefined
    code.value = ''
    await queryClient.invalidateQueries({ queryKey: meQuery.queryKey })
  }

  function copyRecovery() {
    void navigator.clipboard.writeText(recovery.value!.join('\n'))
  }

  return { setup, code, recovery, error, busy, start, enable, copyRecovery }
}
