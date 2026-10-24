// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'

import { CameraError, createCamera, type CameraErrorCode } from '../camera'
import type { DecodedCode } from '../decode'

function fakeTrack(capabilities = {}) {
  return {
    stop: vi.fn(),
    getCapabilities: () => capabilities,
    getSettings: () => ({ deviceId: 'back' }),
    applyConstraints: vi.fn<(c: MediaTrackConstraints) => Promise<void>>().mockResolvedValue(),
  }
}

const streamOf = (track: ReturnType<typeof fakeTrack>) =>
  ({ getVideoTracks: () => [track], getTracks: () => [track] }) as unknown as MediaStream

function setup(getUserMedia: (c: MediaStreamConstraints) => Promise<MediaStream>) {
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia: vi.fn(getUserMedia), enumerateDevices: vi.fn() },
  })
  const video = document.createElement('video')
  video.play = vi.fn<() => Promise<void>>().mockResolvedValue()
  Object.defineProperty(video, 'readyState', { value: 4 })
  const frames: (() => void)[] = []
  video.requestVideoFrameCallback = (cb) => frames.push(() => cb(0, {} as never))
  // Кадры идут через 100 мс — чаще, чем модуль распознаёт при 12 кадрах в секунду, не будет.
  let now = 0
  vi.spyOn(performance, 'now').mockImplementation(() => (now += 100))
  const nextFrame = () => frames.shift()?.()
  return { video, nextFrame, getUserMedia: vi.mocked(navigator.mediaDevices.getUserMedia) }
}

const camera = (video: HTMLVideoElement, extra: Partial<Parameters<typeof createCamera>[0]> = {}) =>
  createCamera({
    video,
    region: () => ({ x: 0, y: 0, width: 640, height: 480 }),
    onScan: () => {},
    decoder: () => Promise.resolve([]),
    capture: () => ({}) as ImageData,
    ...extra,
  })

async function errorCode(p: Promise<unknown>): Promise<CameraErrorCode | undefined> {
  try {
    await p
  } catch (e) {
    return e instanceof CameraError ? e.code : undefined
  }
}

describe('камера', () => {
  afterEach(() => vi.restoreAllMocks())

  it('по умолчанию берёт заднюю камеру; фонарик и увеличение доступны, если их умеет устройство', async () => {
    const track = fakeTrack({ torch: true, zoom: { min: 1, max: 5 } })
    const { video, getUserMedia } = setup(() => Promise.resolve(streamOf(track)))
    const cam = camera(video)

    await cam.start()

    expect(getUserMedia.mock.calls[0]![0].video).toMatchObject({
      facingMode: { ideal: 'environment' },
      width: { ideal: 1920 },
    })
    expect(cam.features()).toEqual({ torch: true, zoom: { min: 1, max: 5, step: 0.1, value: 1 } })
    await cam.setTorch(true)
    expect(track.applyConstraints).toHaveBeenCalledWith({ advanced: [{ torch: true }] })
  })

  it('без фонарика и увеличения кнопок для них нет', async () => {
    const { video } = setup(() => Promise.resolve(streamOf(fakeTrack())))
    const cam = camera(video)
    await cam.start()
    expect(cam.features()).toEqual({ torch: false, zoom: undefined })
  })

  it('отказ в доступе, занятая камера и страница без HTTPS дают понятные коды ошибок', async () => {
    const denied = setup(() => Promise.reject(new DOMException('no', 'NotAllowedError')))
    expect(await errorCode(camera(denied.video).start())).toBe('denied')

    const busy = setup(() => Promise.reject(new DOMException('in use', 'NotReadableError')))
    expect(await errorCode(camera(busy.video).start())).toBe('busy')

    Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: undefined })
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: false })
    expect(await errorCode(camera(document.createElement('video')).start())).toBe('insecure')
  })

  it('пропавшая сохранённая камера заменяется камерой по умолчанию', async () => {
    const { video, getUserMedia } = setup((c) =>
      typeof c.video === 'object' && c.video.deviceId
        ? Promise.reject(new DOMException('gone', 'OverconstrainedError'))
        : Promise.resolve(streamOf(fakeTrack())),
    )
    await camera(video).start('usb-webcam')
    expect(getUserMedia).toHaveBeenCalledTimes(2)
  })

  it('распознаёт кадр под рамкой; пока модуль занят, кадры пропускаются; после остановки сканов нет', async () => {
    const { video, nextFrame } = setup(() => Promise.resolve(streamOf(fakeTrack())))
    let finish: (codes: DecodedCode[]) => void = () => {}
    const decoder = vi.fn(() => new Promise<DecodedCode[]>((resolve) => (finish = resolve)))
    const capture = vi.fn(() => ({}) as ImageData)
    const onScan = vi.fn()
    const cam = camera(video, { decoder, capture, onScan })
    await cam.start()

    nextFrame()
    expect(capture).toHaveBeenCalledWith(video, { x: 0, y: 0, width: 640, height: 480 })
    nextFrame()
    expect(decoder).toHaveBeenCalledTimes(1)

    finish([{ raw: '4602124002104', format: 'ean_13' }])
    await vi.waitFor(() =>
      expect(onScan).toHaveBeenCalledWith({ raw: '4602124002104', format: 'ean_13' }),
    )

    nextFrame()
    expect(decoder).toHaveBeenCalledTimes(2)
    cam.stop()
    finish([{ raw: '4006381333931', format: 'ean_13' }])
    await Promise.resolve()
    expect(onScan).toHaveBeenCalledTimes(1)
  })
})
