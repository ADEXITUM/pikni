import {
  mdiAndroid,
  mdiApple,
  mdiAppleSafari,
  mdiConsoleLine,
  mdiDevices,
  mdiFirefox,
  mdiGoogleChrome,
  mdiLinux,
  mdiMicrosoftEdge,
  mdiMicrosoftWindows,
  mdiOpera,
  mdiWeb,
} from '@mdi/js'

export interface Device {
  title: string
  osIcon: string
  browserIcon: string
}

type Rule = [pattern: RegExp, name: string, icon: string]

// Порядок важен: Edge, Opera и Яндекс Браузер тоже называют себя Chrome, а Chrome — Safari.
const browsers: Rule[] = [
  [/curl|wget|python|go-http-client|okhttp/i, 'Программа', mdiConsoleLine],
  [/YaBrowser\//, 'Яндекс Браузер', mdiWeb],
  [/Edg(A|iOS)?\//, 'Edge', mdiMicrosoftEdge],
  [/OPR\//, 'Opera', mdiOpera],
  [/Firefox\/|FxiOS\//, 'Firefox', mdiFirefox],
  [/Chrome\/|CriOS\//, 'Chrome', mdiGoogleChrome],
  [/Safari\//, 'Safari', mdiAppleSafari],
]

// Android раньше Linux: в его строке есть оба слова. Так же iOS раньше macOS.
const systems: Rule[] = [
  [/Windows/, 'Windows', mdiMicrosoftWindows],
  [/Android/, 'Android', mdiAndroid],
  [/iPhone|iPad|iPod/, 'iOS', mdiApple],
  [/Macintosh|Mac OS X/, 'macOS', mdiApple],
  [/CrOS/, 'ChromeOS', mdiGoogleChrome],
  [/Linux/, 'Linux', mdiLinux],
]

export function describeDevice(userAgent?: string | null): Device {
  const find = (rules: Rule[]) => (userAgent ? rules.find(([re]) => re.test(userAgent)) : undefined)
  const browser = find(browsers)
  const os = find(systems)

  const title = [browser?.[1], os?.[1]].filter(Boolean).join(' · ') || 'Неизвестное устройство'
  return { title, osIcon: os?.[2] ?? mdiDevices, browserIcon: browser?.[2] ?? mdiWeb }
}
