// Хранилище кассы на устройстве. В приватном окне или при запрете сайта на хранение
// localStorage бросает исключение — тогда касса работает, но не переживает перезагрузку.
export interface Store {
  get(key: string): string | null
  set(key: string, value: string | null): void
}

export const local: Store = {
  get(key) {
    try {
      return localStorage.getItem(key)
    } catch {
      return null
    }
  },
  set(key, value) {
    try {
      if (value === null) localStorage.removeItem(key)
      else localStorage.setItem(key, value)
    } catch {
      // хранилище недоступно
    }
  },
}

export function memoryStore(): Store {
  const data = new Map<string, string>()
  return {
    get: (key) => data.get(key) ?? null,
    set: (key, value) => (value === null ? data.delete(key) : data.set(key, value)),
  }
}

export function readJSON<T>(store: Store, key: string, fallback: T): T {
  const raw = store.get(key)
  if (raw === null) return fallback
  try {
    return JSON.parse(raw) as T
  } catch {
    return fallback
  }
}
