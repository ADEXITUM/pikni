<script setup lang="ts">
import { mdiCameraPlusOutline, mdiClose } from '@mdi/js'
import { api, ApiError } from '@pikni/api'
import { ref } from 'vue'

// Фотографии к списанию (бой, СО-5). Снимок уменьшается на устройстве до 1600 px и уходит JPEG —
// с телефона это сотни килобайт, а не десяток мегабайт.
const props = defineProps<{
  documentId: string
  photos: { id: string; createdAt: string }[]
  canEdit: boolean
}>()
const emit = defineEmits<{ changed: [] }>()
const input = ref<HTMLInputElement>()
const busy = ref(false)
const error = ref('')

async function shrink(file: File): Promise<string> {
  const bitmap = await createImageBitmap(file)
  const scale = Math.min(1, 1600 / Math.max(bitmap.width, bitmap.height))
  const canvas = document.createElement('canvas')
  canvas.width = Math.round(bitmap.width * scale)
  canvas.height = Math.round(bitmap.height * scale)
  canvas.getContext('2d')!.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
  const blob = await new Promise<Blob>((ok, fail) =>
    canvas.toBlob((b) => (b ? ok(b) : fail(new Error('Снимок не прочитан'))), 'image/jpeg', 0.85),
  )
  const bytes = new Uint8Array(await blob.arrayBuffer())
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000)
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return btoa(binary)
}

async function upload(e: Event) {
  const files = [...((e.target as HTMLInputElement).files ?? [])]
  if (!files.length) return
  busy.value = true
  error.value = ''
  try {
    for (const file of files) {
      const { error: problem } = await api.POST('/documents/{id}/photos', {
        params: { path: { id: props.documentId } },
        body: { data: await shrink(file) },
      })
      if (problem) throw new ApiError(problem)
    }
    emit('changed')
  } catch (err) {
    error.value = (err as Error).message
  } finally {
    busy.value = false
    if (input.value) input.value.value = ''
  }
}

async function remove(photo: string) {
  const { error: problem } = await api.DELETE('/documents/{id}/photos/{photoId}', {
    params: { path: { id: props.documentId, photoId: photo } },
  })
  if (problem) error.value = new ApiError(problem).message
  else emit('changed')
}
</script>

<template>
  <div class="mb-6" data-test="photos">
    <h2 class="text-title-large mb-2">Фото</h2>
    <div class="d-flex flex-wrap ga-2">
      <div v-for="p in photos" :key="p.id" class="pk-photo">
        <a :href="`/api/v1/photos/${p.id}`" target="_blank">
          <img :src="`/api/v1/photos/${p.id}`" alt="фото к списанию" />
        </a>
        <v-btn
          v-if="canEdit"
          :icon="mdiClose"
          size="x-small"
          class="pk-remove"
          title="Убрать"
          @click="remove(p.id)"
        />
      </div>
      <v-btn
        v-if="canEdit"
        :prepend-icon="mdiCameraPlusOutline"
        variant="tonal"
        height="96"
        :loading="busy"
        data-test="add-photo"
        @click="input?.click()"
        >Добавить фото</v-btn
      >
      <span v-if="!photos.length && !canEdit" class="text-medium-emphasis">Фото нет</span>
    </div>
    <input
      ref="input"
      type="file"
      accept="image/*"
      capture="environment"
      multiple
      hidden
      data-test="photo-input"
      @change="upload"
    />
    <v-alert v-if="error" type="error" density="compact" class="mt-2">{{ error }}</v-alert>
  </div>
</template>

<style scoped>
.pk-photo {
  position: relative;
}
.pk-photo img {
  width: 96px;
  height: 96px;
  object-fit: cover;
  border-radius: 8px;
  display: block;
}
.pk-remove {
  position: absolute;
  top: 4px;
  right: 4px;
}
</style>
