import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'

import { useScanner, type Scan } from '../scanning'

function mountWith(onScan: (s: Scan) => void) {
  let submit!: ReturnType<typeof useScanner>['submit']
  const wrapper = mount(
    defineComponent({
      setup() {
        submit = useScanner(onScan).submit
        return () => h('div')
      },
    }),
  )
  return { wrapper, submit: (raw: string, source: 'manual' | 'camera') => submit(raw, source) }
}

const type = (text: string) => {
  for (const key of [...text, 'Enter']) window.dispatchEvent(new KeyboardEvent('keydown', { key }))
}

describe('useScanner', () => {
  it('ловит аппаратный сканер без фокуса, пока экран открыт', () => {
    const onScan = vi.fn<(s: Scan) => void>()
    const { wrapper } = mountWith(onScan)

    type('4006381333931')
    expect(onScan).toHaveBeenCalledWith(
      expect.objectContaining({
        event: expect.objectContaining({ raw: '4006381333931', source: 'wedge' }),
        code: { kind: 'product', code: '4006381333931' },
      }),
    )

    wrapper.unmount()
    type('73513537')
    expect(onScan).toHaveBeenCalledTimes(1)
  })

  it('повтор с камеры гасится, ручной ввод — нет', () => {
    const onScan = vi.fn<(s: Scan) => void>()
    const { submit } = mountWith(onScan)

    submit('4006381333931', 'camera')
    submit('4006381333931', 'camera')
    submit('4006381333931', 'manual')
    expect(onScan).toHaveBeenCalledTimes(2)
  })
})
