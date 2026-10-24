<script setup lang="ts">
import 'leaflet/dist/leaflet.css'

import L from 'leaflet'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

// Точки на карте OpenStreetMap (НЛ-2). Подложку отдаёт tile.openstreetmap.org: без интернета
// карта пустая, но точки и подписи остаются.
export interface MapPoint {
  id: string
  lat: number
  lon: number
  title: string
  subtitle: string
  highlight?: boolean
}

const props = defineProps<{ points: MapPoint[] }>()
const box = ref<HTMLElement>()
let map: L.Map | undefined
let layer: L.LayerGroup | undefined

function draw() {
  if (!map || !layer) return
  layer.clearLayers()
  for (const p of props.points) {
    L.circleMarker([p.lat, p.lon], {
      radius: p.highlight ? 10 : 8,
      color: p.highlight ? '#2b3a8f' : '#3f7d4c',
      fillOpacity: 0.85,
      weight: 2,
    })
      .bindTooltip(`<b>${p.title}</b><br>${p.subtitle}`)
      .addTo(layer)
  }
  if (props.points.length === 1) map.setView([props.points[0]!.lat, props.points[0]!.lon], 12)
  else if (props.points.length > 1)
    map.fitBounds(L.latLngBounds(props.points.map((p) => [p.lat, p.lon])), { padding: [32, 32] })
}

onMounted(() => {
  map = L.map(box.value!, { scrollWheelZoom: false }).setView([48.5, 131], 5)
  L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
    maxZoom: 18,
    attribution: '© участники OpenStreetMap',
  }).addTo(map)
  layer = L.layerGroup().addTo(map)
  draw()
})
watch(() => props.points, draw, { deep: true })
onBeforeUnmount(() => map?.remove())
</script>

<template>
  <div ref="box" class="pk-map" data-test="map" />
</template>

<style scoped>
.pk-map {
  height: 320px;
  border-radius: 12px;
  z-index: 0;
}
</style>
