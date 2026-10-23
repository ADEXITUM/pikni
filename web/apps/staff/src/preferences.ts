// Настройки этого устройства. localStorage бывает недоступен (приватный режим, запрет
// хранения для сайта) — тогда настройки просто не запоминаются.
export function load(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

export function save(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // не запомнится до следующего запуска
  }
}

export type ThemePreference = 'light' | 'dark' | 'system'

export function loadTheme(): ThemePreference {
  const value = load('pikni.theme')
  return value === 'light' || value === 'dark' ? value : 'system'
}

export function saveTheme(value: ThemePreference) {
  save('pikni.theme', value)
}
