// Звуки сканера «Пикни» — синтез на Web Audio API: ни одного звукового файла, задержка —
// единицы миллисекунд. Параметры и обоснование — docs/brand/sounds.md, страница
// прослушивания — docs/brand/sounds/demo.html (тот же синтез на JavaScript).
//
// «Ток» — щелчок по крышке чекушки: всё хорошо. «Пум» — мягкий низкий удар: что-то не так.
// Событие различается числом и ритмом ударов — ритм слышно в шумном зале.

type Sound =
  'tok' | 'tokMuted' | 'tokLow' | 'tokPum' | 'pumPum' | 'pumLong' | 'tokUp' | 'tokDown' | 'stamp'

// Событие → звук и узор вибрации (мс: вибрация, пауза, вибрация…).
export const soundEvents = {
  accepted: { sound: 'tok', vibrate: [25] },
  acceptedOffline: { sound: 'tokMuted', vibrate: [25] },
  notInPlan: { sound: 'tokPum', vibrate: [25, 80, 90] },
  unknown: { sound: 'pumPum', vibrate: [90, 70, 90] },
  stop: { sound: 'pumLong', vibrate: [400] },
  paired: { sound: 'tokUp', vibrate: [25, 60, 25] },
  unpaired: { sound: 'tokDown', vibrate: [60] },
  posted: { sound: 'stamp', vibrate: [50] },
  undone: { sound: 'tokLow', vibrate: [15] },
} satisfies Record<string, { sound: Sound; vibrate: number[] }>

export type SoundEvent = keyof typeof soundEvents

const SILENCE = 0.0008 // экспоненциальная огибающая не может дойти до нуля

const noiseCache = new WeakMap<BaseAudioContext, AudioBuffer>()
function noiseBuffer(ctx: BaseAudioContext) {
  let buf = noiseCache.get(ctx)
  if (!buf) {
    buf = ctx.createBuffer(1, Math.ceil(ctx.sampleRate * 0.1), ctx.sampleRate)
    const data = buf.getChannelData(0)
    let seed = 12345 // детерминированный шум: звук одинаков при каждом запуске
    for (let i = 0; i < data.length; i++) {
      seed = (seed * 1103515245 + 12345) & 0x7fffffff
      data[i] = seed / 0x3fffffff - 1
    }
    noiseCache.set(ctx, buf)
  }
  return buf
}

function envelope(ctx: BaseAudioContext, t: number, peak: number, attack: number, end: number) {
  const g = ctx.createGain()
  g.gain.setValueAtTime(0, t)
  g.gain.linearRampToValueAtTime(peak, t + attack)
  g.gain.exponentialRampToValueAtTime(SILENCE, end)
  return g
}

// «Ток»: основной тон с быстрым спадом высоты, негармонический обертон ×2,76 (сухой «стук»,
// а не «писк») и щелчок шума.
function tok(
  ctx: BaseAudioContext,
  out: AudioNode,
  t: number,
  { f = 1050, gain = 0.9, lowpass = 0 } = {},
) {
  let dest = out
  if (lowpass) {
    const lp = ctx.createBiquadFilter()
    lp.type = 'lowpass'
    lp.frequency.value = lowpass
    lp.connect(out)
    dest = lp
  }
  const body = ctx.createOscillator()
  body.type = 'triangle'
  body.frequency.setValueAtTime(f * 1.25, t)
  body.frequency.exponentialRampToValueAtTime(f, t + 0.012)
  body.connect(envelope(ctx, t, gain, 0.002, t + 0.09)).connect(dest)

  const overtone = ctx.createOscillator()
  overtone.frequency.setValueAtTime(f * 2.76, t)
  overtone.connect(envelope(ctx, t, gain * 0.35, 0.001, t + 0.045)).connect(dest)

  const click = ctx.createBufferSource()
  click.buffer = noiseBuffer(ctx)
  const bp = ctx.createBiquadFilter()
  bp.type = 'bandpass'
  bp.frequency.value = 2600
  bp.Q.value = 1.2
  click
    .connect(bp)
    .connect(envelope(ctx, t, gain * 0.4, 0.001, t + 0.015))
    .connect(dest)

  for (const node of [body, overtone]) {
    node.start(t)
    node.stop(t + 0.1)
  }
  click.start(t, 0, 0.02)
}

// «Пум»: динамик телефона почти не играет ниже 300 Гц, поэтому энергия отдана гармоникам,
// а низкую высоту слух «достраивает» сам (эффект недостающей основной частоты).
function pum(
  ctx: BaseAudioContext,
  out: AudioNode,
  t: number,
  { f = 233, glide = 0.8, dur = 0.22, gain = 0.95, partials = [0.45, 1, 0.7, 0.4, 0.2] } = {},
) {
  const lp = ctx.createBiquadFilter()
  lp.type = 'lowpass'
  lp.frequency.value = 1400
  lp.connect(out)
  const env = envelope(ctx, t, gain, 0.008, t + dur)
  env.connect(lp)
  partials.forEach((amp, i) => {
    const k = i + 1
    const osc = ctx.createOscillator()
    osc.frequency.setValueAtTime(f * k, t)
    osc.frequency.exponentialRampToValueAtTime(f * glide * k, t + dur * 0.7)
    const g = ctx.createGain()
    g.gain.value = (amp / partials.length) * 2
    osc.connect(g).connect(env)
    osc.start(t)
    osc.stop(t + dur + 0.02)
  })
}

// «Пу-у-ум»: длинный низкий звук с лёгким вибрато — стоп, дальше нельзя.
function pumLong(ctx: BaseAudioContext, out: AudioNode, t: number) {
  const f = 165
  const partials = [0.4, 1, 0.75, 0.5, 0.3, 0.15]
  const lp = ctx.createBiquadFilter()
  lp.type = 'lowpass'
  lp.frequency.value = 1200
  lp.connect(out)
  const env = ctx.createGain()
  env.gain.setValueAtTime(0, t)
  env.gain.linearRampToValueAtTime(0.9, t + 0.015)
  env.gain.setValueAtTime(0.9, t + 0.42)
  env.gain.exponentialRampToValueAtTime(SILENCE, t + 0.62)
  env.connect(lp)
  const lfo = ctx.createOscillator()
  lfo.frequency.value = 5.5
  partials.forEach((amp, i) => {
    const k = i + 1
    const osc = ctx.createOscillator()
    osc.frequency.setValueAtTime(f * k, t)
    osc.frequency.exponentialRampToValueAtTime(f * 0.92 * k, t + 0.6)
    const depth = ctx.createGain()
    depth.gain.value = f * k * 0.008 // вибрато ±0,8 %
    lfo.connect(depth).connect(osc.frequency)
    const g = ctx.createGain()
    g.gain.value = (amp / partials.length) * 2
    osc.connect(g).connect(env)
    osc.start(t)
    osc.stop(t + 0.64)
  })
  lfo.start(t)
  lfo.stop(t + 0.64)
}

// «Шлёп»: штамп на документе — глухой шумовой удар и короткий низкий «пум».
function stamp(ctx: BaseAudioContext, out: AudioNode, t: number) {
  const src = ctx.createBufferSource()
  src.buffer = noiseBuffer(ctx)
  const lp = ctx.createBiquadFilter()
  lp.type = 'lowpass'
  lp.frequency.value = 900
  src
    .connect(lp)
    .connect(envelope(ctx, t, 0.8, 0.002, t + 0.07))
    .connect(out)
  src.start(t, 0, 0.08)
  pum(ctx, out, t + 0.004, { f: 130, glide: 0.85, dur: 0.12, gain: 0.55 })
}

const sounds: Record<Sound, (ctx: BaseAudioContext, out: AudioNode, t: number) => void> = {
  tok: (ctx, out, t) => tok(ctx, out, t),
  tokMuted: (ctx, out, t) => tok(ctx, out, t, { gain: 0.7, lowpass: 1300 }),
  tokLow: (ctx, out, t) => tok(ctx, out, t, { f: 620, gain: 0.6 }),
  tokPum: (ctx, out, t) => {
    tok(ctx, out, t)
    pum(ctx, out, t + 0.12, { f: 220, glide: 0.78, dur: 0.2 })
  },
  pumPum: (ctx, out, t) => {
    pum(ctx, out, t)
    pum(ctx, out, t + 0.17, { f: 208 })
  },
  pumLong,
  tokUp: (ctx, out, t) => {
    tok(ctx, out, t, { f: 760 })
    tok(ctx, out, t + 0.11, { f: 1140 })
  },
  tokDown: (ctx, out, t) => {
    tok(ctx, out, t, { f: 1140 })
    tok(ctx, out, t + 0.11, { f: 760 })
  },
  stamp,
}

export interface SoundSettings {
  volume: number
  muted: boolean
  haptics: boolean
}

// createSounds создаёт проигрыватель. Аудиоконтекст создаётся при первом звуке, а браузер
// разрешает звук только после жеста пользователя — поэтому unlock() вызывают на первом
// нажатии или клавише. Вибрации на iOS нет: остаются звук и цвет.
export function createSounds(settings: SoundSettings) {
  let ctx: AudioContext | undefined
  let master: GainNode | undefined

  function ensure() {
    if (!ctx) {
      ctx = new AudioContext({ latencyHint: 'interactive' })
      const comp = ctx.createDynamicsCompressor() // страховка от перегрузки при наложении звуков
      comp.threshold.value = -6
      comp.ratio.value = 4
      master = ctx.createGain()
      master.connect(comp).connect(ctx.destination)
    }
    master!.gain.value = settings.volume
    if (ctx.state !== 'running') void ctx.resume()
    return ctx
  }

  return {
    unlock() {
      if (typeof AudioContext !== 'undefined') ensure()
    },
    play(event: SoundEvent) {
      const e = soundEvents[event]
      if (settings.haptics) navigator.vibrate?.(e.vibrate)
      if (settings.muted || typeof AudioContext === 'undefined') return
      const c = ensure()
      sounds[e.sound](c, master!, c.currentTime + 0.005)
    },
  }
}
