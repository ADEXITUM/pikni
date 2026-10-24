import type { DecodedCode, Decoder } from './decode'
import type { Rect } from './frame'

export type CameraErrorCode = 'insecure' | 'denied' | 'not_found' | 'busy' | 'failed'

export class CameraError extends Error {
  constructor(
    readonly code: CameraErrorCode,
    readonly reason?: unknown,
  ) {
    super(`camera: ${code}`)
  }
}

export interface Zoom {
  min: number
  max: number
  step: number
  value: number
}

export interface CameraFeatures {
  torch: boolean
  zoom?: Zoom
}

// Фонарик и увеличение есть не во всех браузерах, поэтому их нет в типах DOM.
interface ExtendedCapabilities extends MediaTrackCapabilities {
  torch?: boolean
  zoom?: { min: number; max: number; step?: number }
}

export type Capture = (video: HTMLVideoElement, region: Rect) => ImageData | undefined

export interface CameraOptions {
  video: HTMLVideoElement
  // region — область кадра под рамкой видоискателя, в пикселях видео.
  region: () => Rect | undefined
  onScan: (code: DecodedCode) => void
  decoder?: Decoder & { terminate?: () => void }
  capture?: Capture
  fps?: number
}

export function createCamera({
  video,
  region,
  onScan,
  decoder = workerDecoder(),
  capture = canvasCapture(),
  fps = 12,
}: CameraOptions) {
  let stream: MediaStream | undefined
  let running = false
  let busy = false
  let last = 0
  // Номер запуска: если камеру выключили, пока браузер спрашивал разрешение, поздний поток
  // сразу останавливается.
  let generation = 0

  const track = () => stream?.getVideoTracks()[0]

  async function open(deviceId?: string): Promise<MediaStream> {
    const size = { width: { ideal: 1920 }, height: { ideal: 1080 } }
    try {
      return await navigator.mediaDevices.getUserMedia({
        audio: false,
        video: deviceId
          ? { ...size, deviceId: { exact: deviceId } }
          : { ...size, facingMode: { ideal: 'environment' } },
      })
    } catch (e) {
      const err = cameraError(e)
      // Сохранённая камера могла пропасть (отключили веб-камеру) — берём камеру по умолчанию.
      if (deviceId && err.code === 'not_found') return open()
      throw err
    }
  }

  async function start(deviceId?: string) {
    stop()
    const run = generation
    if (!navigator.mediaDevices?.getUserMedia) {
      throw new CameraError(window.isSecureContext === false ? 'insecure' : 'not_found')
    }
    const s = await open(deviceId)
    if (run !== generation) {
      s.getTracks().forEach((t) => t.stop())
      return
    }
    stream = s
    video.muted = true
    video.playsInline = true
    video.srcObject = s
    try {
      await video.play()
    } catch (e) {
      if (run === generation) throw cameraError(e)
      return
    }
    running = true
    schedule()
  }

  function stop() {
    generation++
    running = false
    stream?.getTracks().forEach((t) => t.stop())
    stream = undefined
    video.srcObject = null
  }

  function schedule() {
    if (!running) return
    if (video.requestVideoFrameCallback) video.requestVideoFrameCallback(tick)
    else requestAnimationFrame(tick)
  }

  // Распознаётся 10–15 кадров в секунду и только один кадр за раз: пока модуль занят,
  // новые кадры пропускаются, очередь не копится.
  function tick() {
    schedule()
    const now = performance.now()
    if (busy || now - last < 1000 / fps || video.readyState < video.HAVE_CURRENT_DATA) return
    const r = region()
    const image = r && r.width >= 1 && r.height >= 1 ? capture(video, r) : undefined
    if (!image) return
    last = now
    busy = true
    decoder(image)
      .then((codes) => {
        if (running) for (const code of codes) onScan(code)
      })
      .catch(() => {})
      .finally(() => (busy = false))
  }

  function features(): CameraFeatures {
    const t = track()
    const caps = (t?.getCapabilities?.() ?? {}) as ExtendedCapabilities
    const settings = (t?.getSettings() ?? {}) as MediaTrackSettings & { zoom?: number }
    const zoom = caps.zoom && caps.zoom.max > caps.zoom.min ? caps.zoom : undefined
    return {
      torch: caps.torch === true,
      zoom: zoom && {
        min: zoom.min,
        max: zoom.max,
        step: zoom.step || 0.1,
        value: settings.zoom ?? zoom.min,
      },
    }
  }

  const apply = (c: Record<string, unknown>) =>
    track()?.applyConstraints({ advanced: [c as MediaTrackConstraintSet] })

  async function devices(): Promise<MediaDeviceInfo[]> {
    const all = (await navigator.mediaDevices?.enumerateDevices()) ?? []
    return all.filter((d) => d.kind === 'videoinput')
  }

  return {
    start,
    stop,
    devices,
    features,
    deviceId: () => track()?.getSettings().deviceId,
    setTorch: (on: boolean) => apply({ torch: on }),
    setZoom: (value: number) => apply({ zoom: value }),
    dispose() {
      stop()
      decoder.terminate?.()
    },
  }
}

export type Camera = ReturnType<typeof createCamera>

function cameraError(e: unknown): CameraError {
  const name = (e as { name?: string } | undefined)?.name
  if (name === 'NotAllowedError' || name === 'SecurityError') return new CameraError('denied', e)
  if (name === 'NotFoundError' || name === 'OverconstrainedError') {
    return new CameraError('not_found', e)
  }
  if (name === 'NotReadableError' || name === 'AbortError') return new CameraError('busy', e)
  return new CameraError('failed', e)
}

// canvasCapture вырезает область кадра. Сторона ограничена 1280 пикселями: мелкий DataMatrix
// марки ещё читается, а распознавание укладывается в доли кадра.
function canvasCapture(maxSide = 1280): Capture {
  let ctx: CanvasRenderingContext2D | null | undefined
  return (video, r) => {
    ctx ??= document.createElement('canvas').getContext('2d', { willReadFrequently: true })
    if (!ctx) return undefined
    const k = Math.min(1, maxSide / Math.max(r.width, r.height))
    const width = Math.round(r.width * k)
    const height = Math.round(r.height * k)
    if (ctx.canvas.width !== width || ctx.canvas.height !== height) {
      ctx.canvas.width = width
      ctx.canvas.height = height
    }
    ctx.drawImage(video, r.x, r.y, r.width, r.height, 0, 0, width, height)
    return ctx.getImageData(0, 0, width, height)
  }
}

// workerDecoder распознаёт в отдельном потоке: модуль WebAssembly не тормозит интерфейс,
// а сам загружается только при первом кадре — то есть только на экранах сканирования.
export function workerDecoder(): Decoder & { terminate: () => void } {
  let worker: Worker | undefined
  let seq = 0
  const pending = new Map<number, (codes: DecodedCode[]) => void>()

  const decode = (image: ImageData) =>
    new Promise<DecodedCode[]>((resolve) => {
      if (!worker) {
        worker = new Worker(new URL('./decoder.worker.ts', import.meta.url), { type: 'module' })
        worker.onmessage = (e: MessageEvent<{ id: number; codes?: DecodedCode[] }>) => {
          pending.get(e.data.id)?.(e.data.codes ?? [])
          pending.delete(e.data.id)
        }
        // Модуль не загрузился: кадры не должны ждать ответа вечно.
        worker.onerror = () => {
          pending.forEach((done) => done([]))
          pending.clear()
        }
      }
      const id = ++seq
      pending.set(id, resolve)
      worker.postMessage({ id, image }, [image.data.buffer])
    })

  return Object.assign(decode, {
    terminate() {
      worker?.terminate()
      worker = undefined
      pending.forEach((resolve) => resolve([]))
      pending.clear()
    },
  })
}
