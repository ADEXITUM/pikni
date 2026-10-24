// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'

import { GS } from '../codes'
import { createWedgeDetector } from '../wedge'

function press(
  detector: ReturnType<typeof createWedgeDetector>,
  keys: string[],
  interval: number,
  target: EventTarget = document.body,
  start = 1000,
) {
  let now = start
  for (const key of keys) {
    const init: KeyboardEventInit = key === 'GS' ? { key: ']', ctrlKey: true } : { key }
    const e = new KeyboardEvent('keydown', { ...init, cancelable: true })
    Object.defineProperty(e, 'target', { value: target })
    detector(e, now)
    now += interval
  }
  return now
}

const chars = (s: string) => [...s]

describe('createWedgeDetector', () => {
  it('быстрый ввод с Enter — скан, медленный набор человеком — нет', () => {
    const onScan = vi.fn()
    const detector = createWedgeDetector({ onScan })

    press(detector, [...chars('4006381333931'), 'Enter'], 10)
    expect(onScan).toHaveBeenCalledWith('4006381333931')

    press(detector, [...chars('4006381333931'), 'Enter'], 120, document.body, 5000)
    expect(onScan).toHaveBeenCalledTimes(1)
  })

  it('короткий быстрый ввод сканом не считается', () => {
    const onScan = vi.fn()
    press(createWedgeDetector({ onScan }), [...chars('123'), 'Enter'], 5)
    expect(onScan).not.toHaveBeenCalled()
  })

  it('Ctrl+] превращается в разделитель GS', () => {
    const onScan = vi.fn()
    press(
      createWedgeDetector({ onScan }),
      [...chars('0104602124002104215i.7D(U'), 'GS', ...chars('9333kC'), 'Enter'],
      5,
    )
    expect(onScan).toHaveBeenCalledWith(`0104602124002104215i.7D(U${GS}9333kC`)
  })

  it('символы скана, попавшие в поле ввода, убираются', () => {
    const input = document.createElement('input')
    input.value = 'черновик'
    const onInput = vi.fn()
    input.addEventListener('input', onInput)
    // Браузер успел вписать символы в поле; детектор возвращает прежнее значение.
    const detector = createWedgeDetector({ onScan: vi.fn() })
    const end = press(detector, chars('4006381333931'), 10, input)
    input.value = 'черновик4006381333931'
    press(detector, ['Enter'], 0, input, end)

    expect(input.value).toBe('черновик')
    expect(onInput).toHaveBeenCalled()
  })

  it('с ignoreFields скан в поле ввода остаётся обычным вводом', () => {
    const input = document.createElement('input')
    const onScan = vi.fn<(raw: string) => void>()
    const detector = createWedgeDetector({ onScan, ignoreFields: true })
    const end = press(detector, chars('4006381333931'), 10, input)
    press(detector, ['Enter'], 0, input, end)
    expect(onScan).not.toHaveBeenCalled()
  })
})
