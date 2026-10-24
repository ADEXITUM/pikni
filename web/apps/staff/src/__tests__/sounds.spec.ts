import { createSounds } from '@pikni/ui'
import { afterEach, describe, expect, it, vi } from 'vitest'

// Поддельный Web Audio: любой вызов возвращает тот же узел, считаются только запуски звуков.
function fakeAudio() {
  const started = { count: 0 }
  const node: object = new Proxy(() => {}, {
    get: (_, key) => {
      if (key === 'start') return () => started.count++
      if (key === 'getChannelData') return () => new Float32Array(16)
      if (key === 'sampleRate') return 44100
      if (key === 'currentTime') return 0
      if (key === 'state') return 'running'
      return node
    },
    apply: () => node,
    set: () => true,
  })
  const created = vi.fn<() => void>()
  class AudioContext {
    constructor() {
      created()
      return node as AudioContext
    }
  }
  return { AudioContext, created, started }
}

describe('звуки сканера', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('«не по плану» — ток-пум и узор вибрации в том же ритме', () => {
    const audio = fakeAudio()
    const vibrate = vi.fn<(pattern: number[]) => boolean>()
    vi.stubGlobal('AudioContext', audio.AudioContext)
    vi.stubGlobal('navigator', { vibrate })

    const sounds = createSounds({ volume: 0.8, muted: false, haptics: true })
    sounds.play('notInPlan')
    sounds.play('accepted')

    expect(vibrate).toHaveBeenCalledWith([25, 80, 90])
    expect(audio.created).toHaveBeenCalledTimes(1)
    expect(audio.started.count).toBeGreaterThan(0)
  })

  it('без звука и без вибрации устройство молчит', () => {
    const audio = fakeAudio()
    const vibrate = vi.fn<(pattern: number[]) => boolean>()
    vi.stubGlobal('AudioContext', audio.AudioContext)
    vi.stubGlobal('navigator', { vibrate })

    createSounds({ volume: 0.8, muted: true, haptics: false }).play('stop')

    expect(vibrate).not.toHaveBeenCalled()
    expect(audio.created).not.toHaveBeenCalled()
  })
})
