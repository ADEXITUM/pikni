import { createPikniVuetify } from '@pikni/ui'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ScanCamera from '@pikni/ui/ScanCamera.vue'

const mediaDevices = (value: unknown) =>
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value })

describe('ScanCamera', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        disconnect() {}
      },
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('при запрете доступа объясняет, что делать, и повторяет попытку', async () => {
    const getUserMedia = vi
      .fn<MediaDevices['getUserMedia']>()
      .mockRejectedValue(new DOMException('denied', 'NotAllowedError'))
    mediaDevices({ getUserMedia })

    const wrapper = mount(ScanCamera, { global: { plugins: [createPikniVuetify()] } })
    await flushPromises()

    const error = wrapper.find('[data-test="camera-error"]')
    expect(error.text()).toContain('Разрешите его в настройках браузера')
    await error.find('button').trigger('click')
    await flushPromises()
    expect(getUserMedia).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('кнопка закрытия выключает камеру', async () => {
    mediaDevices(undefined)
    const wrapper = mount(ScanCamera, { global: { plugins: [createPikniVuetify()] } })
    await flushPromises()
    await wrapper.find('[aria-label="Выключить камеру"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })
})
