<script setup lang="ts">
import {
  mdiCameraFlipOutline,
  mdiClose,
  mdiCropLandscape,
  mdiCropSquare,
  mdiFlashlight,
  mdiFlashlightOff,
} from '@mdi/js'
import {
  type Camera,
  CameraError,
  type CameraErrorCode,
  type CameraFeatures,
  createCamera,
  type DecodedCode,
  frameRect,
  type FrameShape,
  videoRegion,
} from '@pikni/scanner'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'

import { load, save } from '@/preferences'

// flash — цвет исхода последнего скана: уголки рамки вспыхивают им, и результат виден, даже
// когда взгляд на камере, а не на карточке.
const props = withDefaults(
  defineProps<{ shape?: FrameShape; flash?: { color: string; key: string } }>(),
  { shape: 'wide', flash: undefined },
)
const emit = defineEmits<{ scan: [code: DecodedCode]; close: [] }>()

const box = ref<HTMLElement>()
const video = ref<HTMLVideoElement>()
const view = reactive({ width: 0, height: 0 })
const shape = ref<FrameShape>(props.shape)
const frame = computed(() => frameRect(view, shape.value))

const state = ref<'starting' | 'running' | 'error'>('starting')
const error = ref<CameraErrorCode>('failed')
const features = ref<CameraFeatures>({ torch: false })
const torch = ref(false)
const zoom = ref(1)
const cameras = ref<MediaDeviceInfo[]>([])
const deviceId = ref<string>()

const errors: Record<CameraErrorCode, string> = {
  denied:
    'Нет доступа к камере. Разрешите его в настройках браузера для этого сайта и нажмите «Повторить».',
  insecure:
    'Камера работает только по защищённому соединению: откройте адрес, начинающийся с https://.',
  not_found: 'Камера не найдена. Подключите камеру или введите код вручную.',
  busy: 'Камера занята другим приложением. Закройте его и нажмите «Повторить».',
  failed: 'Не удалось включить камеру. Нажмите «Повторить» или введите код вручную.',
}

let camera: Camera | undefined
let observer: ResizeObserver | undefined

async function start(id = deviceId.value) {
  if (!camera) return
  state.value = 'starting'
  torch.value = false
  try {
    await camera.start(id)
    features.value = camera.features()
    zoom.value = features.value.zoom?.value ?? 1
    deviceId.value = camera.deviceId()
    // Названия камер браузер отдаёт только после разрешения доступа.
    cameras.value = await camera.devices()
    state.value = 'running'
  } catch (e) {
    error.value = e instanceof CameraError ? e.code : 'failed'
    state.value = 'error'
  }
}

function choose(id: string) {
  save('pikni.camera', id)
  void start(id)
}

async function toggleTorch() {
  torch.value = !torch.value
  await camera?.setTorch(torch.value)
}

function setZoom(value: number) {
  zoom.value = value
  void camera?.setZoom(value)
}

// Скрытая вкладка камеру не держит: она нужна другим приложениям и садит батарею.
function onVisibility() {
  if (document.hidden) camera?.stop()
  else void start()
}

onMounted(() => {
  const el = video.value!
  camera = createCamera({
    video: el,
    region: () =>
      el.videoWidth
        ? videoRegion({ width: el.videoWidth, height: el.videoHeight }, view, frame.value)
        : undefined,
    onScan: (code) => emit('scan', code),
  })
  observer = new ResizeObserver(([entry]) => {
    view.width = entry!.contentRect.width
    view.height = entry!.contentRect.height
  })
  observer.observe(box.value!)
  document.addEventListener('visibilitychange', onVisibility)
  void start(load('pikni.camera') ?? undefined)
})

onBeforeUnmount(() => {
  clearTimeout(flashTimer)
  document.removeEventListener('visibilitychange', onVisibility)
  observer?.disconnect()
  camera?.dispose()
})

const frameStyle = computed(() => ({
  left: `${frame.value.x}px`,
  top: `${frame.value.y}px`,
  width: `${frame.value.width}px`,
  height: `${frame.value.height}px`,
}))
const cameraLabel = (d: MediaDeviceInfo, i: number) => d.label || `Камера ${i + 1}`

const flashColor = ref<string>()
let flashTimer: ReturnType<typeof setTimeout> | undefined
watch(
  () => props.flash?.key,
  () => {
    flashColor.value = props.flash?.color
    clearTimeout(flashTimer)
    flashTimer = setTimeout(() => (flashColor.value = undefined), 700)
  },
)
const cornerColor = computed(() =>
  flashColor.value ? `rgb(var(--v-theme-${flashColor.value}))` : '#fff',
)
</script>

<template>
  <div ref="box" class="pk-camera" data-test="camera">
    <video ref="video" class="pk-camera__video" muted playsinline />

    <div v-if="state === 'running'" class="pk-camera__frame" :style="frameStyle" aria-hidden="true">
      <span v-for="c in ['tl', 'tr', 'bl', 'br']" :key="c" :class="`pk-camera__corner is-${c}`" />
    </div>

    <div v-if="state === 'starting'" class="pk-camera__overlay">
      <v-progress-circular indeterminate color="white" />
      <span>Включаем камеру…</span>
    </div>
    <div v-else-if="state === 'error'" class="pk-camera__overlay" data-test="camera-error">
      <p>{{ errors[error] }}</p>
      <v-btn variant="outlined" color="white" @click="start()">Повторить</v-btn>
    </div>

    <v-btn
      class="pk-camera__close"
      :icon="mdiClose"
      variant="text"
      color="white"
      size="small"
      aria-label="Выключить камеру"
      @click="emit('close')"
    />

    <div v-if="state === 'running'" class="pk-camera__controls">
      <v-slider
        v-if="features.zoom"
        :model-value="zoom"
        :min="features.zoom.min"
        :max="features.zoom.max"
        :step="features.zoom.step"
        color="white"
        hide-details
        density="compact"
        aria-label="Увеличение"
        class="pk-camera__zoom"
        @update:model-value="setZoom"
      />
      <div class="pk-camera__buttons">
        <v-menu v-if="cameras.length > 1">
          <template #activator="{ props: menu }">
            <v-btn
              v-bind="menu"
              :icon="mdiCameraFlipOutline"
              variant="text"
              color="white"
              aria-label="Выбрать камеру"
            />
          </template>
          <v-list density="compact">
            <v-list-item
              v-for="(d, i) in cameras"
              :key="d.deviceId"
              :title="cameraLabel(d, i)"
              :active="d.deviceId === deviceId"
              @click="choose(d.deviceId)"
            />
          </v-list>
        </v-menu>
        <v-btn
          :icon="shape === 'wide' ? mdiCropSquare : mdiCropLandscape"
          variant="text"
          color="white"
          :aria-label="shape === 'wide' ? 'Квадратная рамка' : 'Широкая рамка'"
          @click="shape = shape === 'wide' ? 'square' : 'wide'"
        />
        <v-btn
          v-if="features.torch"
          :icon="torch ? mdiFlashlightOff : mdiFlashlight"
          variant="text"
          color="white"
          :aria-label="torch ? 'Выключить фонарик' : 'Включить фонарик'"
          :aria-pressed="torch"
          @click="toggleTorch"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.pk-camera {
  position: relative;
  aspect-ratio: 4 / 3;
  max-height: 60vh;
  margin-inline: auto;
  overflow: hidden;
  border-radius: 8px;
  background: #000;
  color: #fff;
}

/* На телефоне окно квадратное: под ним должна помещаться карточка скана. */
@media (max-width: 599px) {
  .pk-camera {
    aspect-ratio: 1;
  }
}

.pk-camera__video {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

/* Затемнение вокруг рамки — тенью самой рамки. */
.pk-camera__frame {
  position: absolute;
  box-shadow: 0 0 0 100vmax rgb(0 0 0 / 0.45);
  transition:
    width 0.2s,
    height 0.2s,
    left 0.2s,
    top 0.2s;
}

/* Четыре уголка видоискателя — как на этикетке персонажа. */
.pk-camera__corner {
  position: absolute;
  width: 28px;
  height: 28px;
  border: 0 solid v-bind(cornerColor);
  transition: border-color 0.15s;
  filter: drop-shadow(0 0 1px rgb(0 0 0 / 0.8));
}

.pk-camera__corner.is-tl {
  top: 0;
  left: 0;
  border-top-width: 4px;
  border-left-width: 4px;
  border-top-left-radius: 6px;
}

.pk-camera__corner.is-tr {
  top: 0;
  right: 0;
  border-top-width: 4px;
  border-right-width: 4px;
  border-top-right-radius: 6px;
}

.pk-camera__corner.is-bl {
  bottom: 0;
  left: 0;
  border-bottom-width: 4px;
  border-left-width: 4px;
  border-bottom-left-radius: 6px;
}

.pk-camera__corner.is-br {
  right: 0;
  bottom: 0;
  border-right-width: 4px;
  border-bottom-width: 4px;
  border-bottom-right-radius: 6px;
}

.pk-camera__overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  padding: 24px;
  text-align: center;
}

.pk-camera__close {
  position: absolute;
  top: 4px;
  right: 4px;
}

.pk-camera__controls {
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  padding: 4px 8px;
  background: linear-gradient(transparent, rgb(0 0 0 / 0.6));
}

.pk-camera__buttons {
  display: flex;
  justify-content: center;
  gap: 8px;
}

.pk-camera__zoom {
  max-width: 320px;
  margin-inline: auto;
}
</style>
