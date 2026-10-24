import { onBeforeUnmount, onMounted, type Ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'

import { confirm } from '@/confirm'

// useUnsavedGuard не даёт молча потерять правку формы: при переходе на другой экран
// спрашивает подтверждение, при закрытии вкладки — браузерное предупреждение.
export function useUnsavedGuard(dirty: Ref<boolean>, what: string) {
  onBeforeRouteLeave(() => {
    if (!dirty.value) return true
    return confirm({
      title: 'Уйти без сохранения?',
      text: `Изменения в ${what} пропадут.`,
      confirmText: 'Уйти',
      danger: true,
    })
  })

  const onUnload = (e: BeforeUnloadEvent) => {
    if (dirty.value) e.preventDefault()
  }
  onMounted(() => window.addEventListener('beforeunload', onUnload))
  onBeforeUnmount(() => window.removeEventListener('beforeunload', onUnload))
}
