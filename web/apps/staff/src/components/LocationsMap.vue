<script setup lang="ts">
import 'leaflet/dist/leaflet.css'

import L from 'leaflet'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

// Точки на карте OpenStreetMap (НЛ-2). Подложку отдаёт tile.openstreetmap.org: без интернета
// карта пустая, но точки и подписи остаются. Близкие точки собираются в кружок с числом:
// в федеральной сети иначе тысячи маркеров лежат друг на друге.
export interface MapPoint {
  id: string
  lat: number
  lon: number
  title: string
  subtitle: string
  highlight?: boolean
}

const props = defineProps<{ points: MapPoint[] }>()
const emit = defineEmits<{ select: [id: string] }>()
const box = ref<HTMLElement>()
let map: L.Map | undefined
let layer: L.LayerGroup | undefined

const CELL = 56

function marker(p: MapPoint) {
  return L.circleMarker([p.lat, p.lon], {
    radius: p.highlight ? 10 : 8,
    color: p.highlight ? '#2b3a8f' : '#3f7d4c',
    fillOpacity: 0.85,
    weight: 2,
  })
    .bindTooltip(`<b>${p.title}</b><br>${p.subtitle}`)
    .on('click', () => emit('select', p.id))
}

function cluster(group: MapPoint[]) {
  const lat = group.reduce((s, p) => s + p.lat, 0) / group.length
  const lon = group.reduce((s, p) => s + p.lon, 0) / group.length
  const size = group.length < 10 ? 34 : group.length < 100 ? 40 : 48
  const bounds = L.latLngBounds(group.map((p) => [p.lat, p.lon]))
  return L.marker([lat, lon], {
    icon: L.divIcon({
      className: 'pk-cluster',
      html: `<span>${group.length}</span>`,
      iconSize: [size, size],
    }),
  })
    .bindTooltip(`${group.length} точек — нажмите, чтобы приблизить`)
    .on('click', () => map?.fitBounds(bounds, { padding: [40, 40], maxZoom: 15 }))
}

function draw() {
  if (!map || !layer) return
  layer.clearLayers()
  const zoom = map.getZoom()
  const cells = new Map<string, MapPoint[]>()
  for (const p of props.points) {
    if (p.highlight) {
      marker(p).addTo(layer)
      continue
    }
    const px = map.project([p.lat, p.lon], zoom)
    const key = `${Math.floor(px.x / CELL)}:${Math.floor(px.y / CELL)}`
    cells.set(key, [...(cells.get(key) ?? []), p])
  }
  for (const group of cells.values())
    (group.length === 1 ? marker(group[0]!) : cluster(group)).addTo(layer)
}

function fit() {
  if (!map) return
  const pts = props.points
  if (pts.length === 1) map.setView([pts[0]!.lat, pts[0]!.lon], 12)
  else if (pts.length > 1)
    map.fitBounds(L.latLngBounds(pts.map((p) => [p.lat, p.lon])), {
      padding: [32, 32],
      maxZoom: 14,
    })
  draw()
}

onMounted(() => {
  map = L.map(box.value!, { scrollWheelZoom: false, attributionControl: false }).setView(
    [55.75, 37.62],
    4,
  )
  L.control.attribution({ prefix: 'Leaflet' }).addTo(map)
  L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
    maxZoom: 18,
    attribution: '© участники OpenStreetMap',
  }).addTo(map)
  layer = L.layerGroup().addTo(map)
  map.on('zoomend', draw)
  fit()
})
watch(() => props.points, fit, { deep: true })
onBeforeUnmount(() => map?.remove())
</script>

<template>
  <div ref="box" class="pk-map" data-test="map" />
</template>

<style scoped>
.pk-map {
  height: 360px;
  border-radius: 12px;
  z-index: 0;
}
.pk-map :deep(.pk-cluster) {
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: rgba(63, 125, 76, 0.85);
  box-shadow: 0 0 0 5px rgba(63, 125, 76, 0.3);
  color: #fff;
  font-weight: 600;
  font-size: 0.8rem;
}
</style>
