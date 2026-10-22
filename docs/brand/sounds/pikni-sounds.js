/**
 * Звуки сканера «Пикни».
 *
 * Все звуки синтезируются Web Audio API прямо в браузере: ни одного звукового
 * файла, нет вопросов с лицензией, вес — несколько килобайт, задержка — единицы
 * миллисекунд. Параметры и обоснование — в ../sounds.md.
 *
 * Два тембра — два смысла:
 *   «ток» — щелчок по крышке чекушки, всё хорошо;
 *   «пум» — мягкий низкий удар, что-то не так.
 * Событие различается количеством и ритмом ударов, а не только высотой:
 * ритм слышно в шумном зале и его различают люди с плохим слухом на высоту.
 *
 * Использование в приложении:
 *   import { createPikniSounds } from '@/scanner/pikni-sounds'
 *   const sounds = createPikniSounds({ volume: 0.8 })
 *   button.addEventListener('click', () => sounds.unlock())  // первый жест пользователя
 *   sounds.play('accepted')
 */

/** События сканера → звук и узор вибрации (мс: вибрация, пауза, вибрация…). */
export const EVENTS = {
  accepted: { sound: 'tok', vibrate: [25], label: 'Принято' },
  acceptedOffline: { sound: 'tokMuted', vibrate: [25], label: 'Принято, ждёт отправки' },
  notInPlan: { sound: 'tokPum', vibrate: [25, 80, 90], label: 'Не по плану / излишек' },
  unknown: { sound: 'pumPum', vibrate: [90, 70, 90], label: 'Код не найден' },
  stop: { sound: 'pumLong', vibrate: [400], label: 'Стоп: повтор марки, марка не на балансе, продажа запрещена' },
  paired: { sound: 'tokUp', vibrate: [25, 60, 25], label: 'Телефон подключён к ПК' },
  unpaired: { sound: 'tokDown', vibrate: [60], label: 'Сопряжение разорвано' },
  posted: { sound: 'stamp', vibrate: [50], label: 'Документ проведён' },
  undone: { sound: 'tokLow', vibrate: [15], label: 'Скан отменён' },
}

const SILENCE = 0.0008 // экспоненциальная огибающая не может дойти до нуля

const noiseCache = new WeakMap()
function noiseBuffer (ctx) {
  let buf = noiseCache.get(ctx)
  if (!buf) {
    buf = ctx.createBuffer(1, Math.ceil(ctx.sampleRate * 0.1), ctx.sampleRate)
    const data = buf.getChannelData(0)
    let seed = 12345 // детерминированный шум: звук одинаков при каждом запуске
    for (let i = 0; i < data.length; i++) {
      seed = (seed * 1103515245 + 12345) & 0x7fffffff
      data[i] = (seed / 0x3fffffff) - 1
    }
    noiseCache.set(ctx, buf)
  }
  return buf
}

function envelope (ctx, t, peak, attack, decayEnd) {
  const g = ctx.createGain()
  g.gain.setValueAtTime(0, t)
  g.gain.linearRampToValueAtTime(peak, t + attack)
  g.gain.exponentialRampToValueAtTime(SILENCE, decayEnd)
  return g
}

/**
 * «Ток»: щелчок по крышке. Основной тон с быстрым спадом высоты,
 * негармонический обертон ×2,76 (сухой «стук», а не «писк») и щелчок шума.
 */
function tok (ctx, out, t, { f = 1050, gain = 0.9, lowpass = 0 } = {}) {
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
  const bodyEnv = envelope(ctx, t, gain, 0.002, t + 0.09)
  body.connect(bodyEnv).connect(dest)

  const overtone = ctx.createOscillator()
  overtone.type = 'sine'
  overtone.frequency.setValueAtTime(f * 2.76, t)
  const overEnv = envelope(ctx, t, gain * 0.35, 0.001, t + 0.045)
  overtone.connect(overEnv).connect(dest)

  const click = ctx.createBufferSource()
  click.buffer = noiseBuffer(ctx)
  const bp = ctx.createBiquadFilter()
  bp.type = 'bandpass'
  bp.frequency.value = 2600
  bp.Q.value = 1.2
  const clickEnv = envelope(ctx, t, gain * 0.4, 0.001, t + 0.015)
  click.connect(bp).connect(clickEnv).connect(dest)

  for (const node of [body, overtone]) { node.start(t); node.stop(t + 0.1) }
  click.start(t, 0, 0.02)
  return t + 0.1
}

/**
 * «Пум»: мягкий низкий удар. Динамик телефона почти не играет ниже 300 Гц,
 * поэтому основная энергия отдана гармоникам (2f, 3f, 4f), а мозг сам
 * «достраивает» низкую высоту (эффект недостающей основной частоты).
 */
function pum (ctx, out, t, { f = 233, glide = 0.8, dur = 0.22, gain = 0.95, partials = [0.45, 1, 0.7, 0.4, 0.2] } = {}) {
  const lp = ctx.createBiquadFilter()
  lp.type = 'lowpass'
  lp.frequency.value = 1400
  lp.connect(out)
  const env = envelope(ctx, t, gain, 0.008, t + dur)
  env.connect(lp)
  partials.forEach((amp, i) => {
    const k = i + 1
    const osc = ctx.createOscillator()
    osc.type = 'sine'
    osc.frequency.setValueAtTime(f * k, t)
    osc.frequency.exponentialRampToValueAtTime(f * glide * k, t + dur * 0.7)
    const g = ctx.createGain()
    g.gain.value = amp / partials.length * 2
    osc.connect(g).connect(env)
    osc.start(t)
    osc.stop(t + dur + 0.02)
  })
  return t + dur
}

/** «Пу-у-ум»: длинный низкий звук с лёгким вибрато. Стоп, дальше нельзя. */
function pumLong (ctx, out, t) {
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
    g.gain.value = amp / partials.length * 2
    osc.connect(g).connect(env)
    osc.start(t)
    osc.stop(t + 0.64)
  })
  lfo.start(t)
  lfo.stop(t + 0.64)
  return t + 0.62
}

/** «Шлёп»: штамп на документе. Глухой шумовой удар и короткий низкий «пум». */
function stamp (ctx, out, t) {
  const src = ctx.createBufferSource()
  src.buffer = noiseBuffer(ctx)
  const lp = ctx.createBiquadFilter()
  lp.type = 'lowpass'
  lp.frequency.value = 900
  const env = envelope(ctx, t, 0.8, 0.002, t + 0.07)
  src.connect(lp).connect(env).connect(out)
  src.start(t, 0, 0.08)
  pum(ctx, out, t + 0.004, { f: 130, glide: 0.85, dur: 0.12, gain: 0.55 })
  return t + 0.12
}

/** Звуки = композиции двух тембров. Значения в секундах от момента события. */
export const SOUNDS = {
  tok: (ctx, out, t) => tok(ctx, out, t),
  tokMuted: (ctx, out, t) => tok(ctx, out, t, { gain: 0.7, lowpass: 1300 }),
  tokLow: (ctx, out, t) => tok(ctx, out, t, { f: 620, gain: 0.6 }),
  tokPum: (ctx, out, t) => { tok(ctx, out, t); return pum(ctx, out, t + 0.12, { f: 220, glide: 0.78, dur: 0.2 }) },
  pumPum: (ctx, out, t) => { pum(ctx, out, t); return pum(ctx, out, t + 0.17, { f: 208 }) },
  pumLong,
  tokUp: (ctx, out, t) => { tok(ctx, out, t, { f: 760 }); return tok(ctx, out, t + 0.11, { f: 1140 }) },
  tokDown: (ctx, out, t) => { tok(ctx, out, t, { f: 1140 }); return tok(ctx, out, t + 0.11, { f: 760 }) },
  stamp,
}

/**
 * Создаёт проигрыватель. Аудиоконтекст создаётся лениво: браузеры разрешают
 * звук только после жеста пользователя, поэтому unlock() вызывают на первом
 * нажатии (например, «Начать сканирование»).
 */
export function createPikniSounds ({ volume = 0.8, muted = false, haptics = true } = {}) {
  let ctx = null
  let master = null

  function ensure () {
    if (!ctx) {
      const AC = globalThis.AudioContext || globalThis.webkitAudioContext
      ctx = new AC({ latencyHint: 'interactive' })
      const comp = ctx.createDynamicsCompressor() // страховка от перегрузки при наложении звуков
      comp.threshold.value = -6
      comp.ratio.value = 4
      master = ctx.createGain()
      master.gain.value = volume
      master.connect(comp).connect(ctx.destination)
    }
    return ctx
  }

  return {
    async unlock () {
      ensure()
      if (ctx.state !== 'running') await ctx.resume()
    },
    play (event) {
      const e = EVENTS[event]
      if (!e) throw new Error(`Неизвестное событие звука: ${event}`)
      if (haptics && typeof navigator !== 'undefined' && navigator.vibrate) navigator.vibrate(e.vibrate)
      if (muted) return
      ensure()
      if (ctx.state !== 'running') ctx.resume()
      SOUNDS[e.sound](ctx, master, ctx.currentTime + 0.005)
    },
    setVolume (v) {
      volume = Math.min(1, Math.max(0, v))
      if (master) master.gain.value = volume
    },
    setMuted (m) { muted = Boolean(m) },
    setHaptics (h) { haptics = Boolean(h) },
  }
}

/** Отрисовать событие в WAV (16 бит, моно) — для документации и проверки. */
export async function renderToWav (event, sampleRate = 44100) {
  const e = EVENTS[event]
  if (!e) throw new Error(`Неизвестное событие звука: ${event}`)
  const off = new OfflineAudioContext(1, Math.ceil(sampleRate * 0.8), sampleRate)
  const g = off.createGain()
  g.gain.value = 0.8
  g.connect(off.destination)
  SOUNDS[e.sound](off, g, 0.01)
  const buf = await off.startRendering()
  return encodeWav(buf.getChannelData(0), sampleRate)
}

function encodeWav (samples, sampleRate) {
  const bytes = new ArrayBuffer(44 + samples.length * 2)
  const v = new DataView(bytes)
  const str = (o, s) => [...s].forEach((c, i) => v.setUint8(o + i, c.charCodeAt(0)))
  str(0, 'RIFF'); v.setUint32(4, 36 + samples.length * 2, true); str(8, 'WAVE')
  str(12, 'fmt '); v.setUint32(16, 16, true); v.setUint16(20, 1, true); v.setUint16(22, 1, true)
  v.setUint32(24, sampleRate, true); v.setUint32(28, sampleRate * 2, true); v.setUint16(32, 2, true); v.setUint16(34, 16, true)
  str(36, 'data'); v.setUint32(40, samples.length * 2, true)
  samples.forEach((s, i) => v.setInt16(44 + i * 2, Math.max(-1, Math.min(1, s)) * 0x7fff, true))
  return new Blob([bytes], { type: 'audio/wav' })
}
