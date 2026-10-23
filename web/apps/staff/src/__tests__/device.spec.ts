import {
  mdiAndroid,
  mdiApple,
  mdiAppleSafari,
  mdiConsoleLine,
  mdiDevices,
  mdiGoogleChrome,
  mdiMicrosoftEdge,
  mdiMicrosoftWindows,
  mdiWeb,
} from '@mdi/js'
import { describe, expect, it } from 'vitest'

import { describeDevice } from '../device'
import { formatAgo } from '../format'

describe('describeDevice', () => {
  it.each([
    [
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36',
      'Chrome · Windows',
      mdiMicrosoftWindows,
      mdiGoogleChrome,
    ],
    [
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0',
      'Edge · Windows',
      mdiMicrosoftWindows,
      mdiMicrosoftEdge,
    ],
    [
      'Mozilla/5.0 (Linux; Android 13; TC21) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36',
      'Chrome · Android',
      mdiAndroid,
      mdiGoogleChrome,
    ],
    [
      'Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Mobile/15E148 Safari/604.1',
      'Safari · iOS',
      mdiApple,
      mdiAppleSafari,
    ],
    [
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 YaBrowser/25.8.0.0 Safari/537.36',
      'Яндекс Браузер · Windows',
      mdiMicrosoftWindows,
      mdiWeb,
    ],
    ['curl/8.21.0', 'Программа', mdiDevices, mdiConsoleLine],
  ])('%s', (ua, title, osIcon, browserIcon) => {
    const device = describeDevice(ua)
    expect(device.title).toBe(title)
    expect(device.osIcon).toBe(osIcon)
    expect(device.browserIcon).toBe(browserIcon)
  })

  it('без строки — неизвестное устройство', () => {
    expect(describeDevice(undefined).title).toBe('Неизвестное устройство')
  })
})

describe('formatAgo', () => {
  const now = Date.parse('2026-10-23T12:00:00Z')

  it.each([
    ['2026-10-23T11:59:30Z', 'только что'],
    ['2026-10-23T11:55:00Z', '5 минут назад'],
    ['2026-10-23T09:00:00Z', '3 часа назад'],
    ['2026-10-22T12:00:00Z', 'вчера'],
    ['2026-10-20T12:00:00Z', '3 дня назад'],
  ])('%s → %s', (iso, text) => {
    expect(formatAgo(iso, now)).toBe(text)
  })
})
