import { watch } from 'vue'

import { type ConfirmRequest, confirmState, settleConfirm } from '@/confirm'

// autoConfirm отвечает на окна подтверждения за пользователя и запоминает вопросы: тест
// проверяет и сам вопрос («было — станет»), и то, что без согласия ничего не сохраняется.
export function autoConfirm(answer = true) {
  const asked: ConfirmRequest[] = []
  const stop = watch(
    () => confirmState.open,
    (open) => {
      if (!open || !confirmState.request) return
      asked.push(confirmState.request)
      settleConfirm(answer)
    },
    { flush: 'sync' },
  )
  return { asked, stop }
}
