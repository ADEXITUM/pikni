import { createSounds, type SoundEvent, type SoundSettings } from '@pikni/ui'
import { reactive, watch } from 'vue'

import { load, save } from '@/preferences'

// Звук и вибрация сканов (СК-3). Настройки хранятся на устройстве: в зале громче, в офисе тише.
const volume = Number(load('pikni.sound.volume'))
export const soundSettings = reactive<SoundSettings>({
  volume: Number.isFinite(volume) && load('pikni.sound.volume') !== null ? volume : 0.8,
  muted: load('pikni.sound.muted') === '1',
  haptics: load('pikni.sound.haptics') !== '0',
})

watch(soundSettings, (s) => {
  save('pikni.sound.volume', String(s.volume))
  save('pikni.sound.muted', s.muted ? '1' : '0')
  save('pikni.sound.haptics', s.haptics ? '1' : '0')
})

const sounds = createSounds(soundSettings)

// Браузер разрешает звук только после жеста пользователя. Первое касание или клавиша —
// в том числе «нажатия» ручного сканера — включают звук для всего сеанса.
function unlock() {
  sounds.unlock()
  window.removeEventListener('pointerdown', unlock, true)
  window.removeEventListener('keydown', unlock, true)
}
window.addEventListener('pointerdown', unlock, true)
window.addEventListener('keydown', unlock, true)

export const playFeedback = (event: SoundEvent) => sounds.play(event)
