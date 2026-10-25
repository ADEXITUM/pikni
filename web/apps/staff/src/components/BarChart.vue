<script setup lang="ts">
import type { ChartPoint } from '@pikni/api'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

// Столбчатая диаграмма одного ряда: один цвет, без легенды — ряд называет заголовок. Тонкие
// столбцы со скруглённым концом, зазор между ними, подсказка при наведении и таблица для тех,
// кому график не подходит. horizontal — подписи слева, для категорий и точек.
const props = defineProps<{
  points: ChartPoint[]
  format: (v: number) => string
  label?: (s: string) => string
  horizontal?: boolean
}>()
const showTable = ref(false)
const hover = ref<number>()

// Ширина рисунка — ширина блока в пикселях: подписи не растягиваются и не сжимаются вместе с графиком.
const plot = ref<HTMLElement>()
const W = ref(640)
let observer: ResizeObserver | undefined
onMounted(() => {
  observer = new ResizeObserver(([e]) => e && (W.value = Math.max(e.contentRect.width, 200)))
  if (plot.value) observer.observe(plot.value)
})
onBeforeUnmount(() => observer?.disconnect())
const rowH = 26
const max = computed(() => Math.max(...props.points.map((p) => p.value), 0) || 1)
const label = (s: string) => (props.label ? props.label(s) : s)

// Вертикальные: столбец на день, подписи по оси — каждая n-я, чтобы не наползали.
const vH = 200
const pad = { l: 8, r: 8, t: 12, b: 24 }
const bandW = computed(() => (W.value - pad.l - pad.r) / Math.max(props.points.length, 1))
const barW = computed(() => Math.max(Math.min(bandW.value - 2, 24), 2))
const tickEvery = computed(() => Math.ceil(props.points.length / 8))
const height = computed(() => (props.horizontal ? props.points.length * rowH + 8 : vH))
const labelW = 120

// Конец столбца скруглён на 4 px, начало стоит на оси.
const hLen = (v: number) => Math.max(((W.value - labelW - 90) * v) / max.value, 2)
function hPath(i: number, v: number) {
  const w = hLen(v)
  const y = i * rowH + 6
  const h = rowH - 12
  const r = Math.min(4, w)
  return `M${labelW},${y} h${w - r} a${r},${r} 0 0 1 ${r},${r} v${h - 2 * r} a${r},${r} 0 0 1 -${r},${r} h-${w - r} z`
}
function vPath(i: number, v: number) {
  const h = Math.max(((vH - pad.t - pad.b) * v) / max.value, 2)
  const x = pad.l + i * bandW.value + (bandW.value - barW.value) / 2
  const r = Math.min(4, barW.value / 2, h)
  const base = vH - pad.b
  return `M${x},${base} v-${h - r} a${r},${r} 0 0 1 ${r},-${r} h${barW.value - 2 * r} a${r},${r} 0 0 1 ${r},${r} v${h - r} z`
}
</script>

<template>
  <div class="pk-chart">
    <div class="d-flex justify-end mb-1">
      <v-btn size="x-small" variant="text" @click="showTable = !showTable">{{
        showTable ? 'Графиком' : 'Таблицей'
      }}</v-btn>
    </div>
    <v-table v-if="showTable" density="compact">
      <tbody>
        <tr v-for="p in points" :key="p.label">
          <td>{{ label(p.label) }}</td>
          <td class="text-end">{{ format(p.value) }}</td>
        </tr>
      </tbody>
    </v-table>
    <p
      v-if="!showTable && points.every((p) => p.value === 0)"
      class="text-body-medium text-medium-emphasis"
    >
      Нет данных за период.
    </p>
    <div v-show="!showTable && points.some((p) => p.value !== 0)" ref="plot" class="pk-plot">
      <svg
        :viewBox="`0 0 ${W} ${height}`"
        role="img"
        class="pk-svg"
        @mouseleave="hover = undefined"
      >
        <template v-if="horizontal">
          <g v-for="(p, i) in points" :key="p.label" @mouseenter="hover = i">
            <rect :x="0" :y="i * rowH" :width="W" :height="rowH" fill="transparent" />
            <text :x="labelW - 8" :y="i * rowH + rowH / 2 + 4" text-anchor="end" class="pk-label">
              {{ label(p.label) }}
            </text>
            <path
              :d="hPath(i, p.value)"
              class="pk-bar"
              :class="{ 'pk-dim': hover !== undefined && hover !== i }"
            />
            <text :x="labelW + hLen(p.value) + 6" :y="i * rowH + rowH / 2 + 4" class="pk-value">
              {{ format(p.value) }}
            </text>
          </g>
        </template>
        <template v-else>
          <line :x1="pad.l" :x2="W - pad.r" :y1="vH - pad.b" :y2="vH - pad.b" class="pk-axis" />
          <g v-for="(p, i) in points" :key="p.label" @mouseenter="hover = i">
            <rect :x="pad.l + i * bandW" :y="0" :width="bandW" :height="vH" fill="transparent" />
            <path
              v-if="p.value > 0"
              :d="vPath(i, p.value)"
              class="pk-bar"
              :class="{ 'pk-dim': hover !== undefined && hover !== i }"
            />
            <text
              v-if="i % tickEvery === 0"
              :x="pad.l + i * bandW + bandW / 2"
              :y="vH - 6"
              text-anchor="middle"
              class="pk-label"
            >
              {{ label(p.label) }}
            </text>
          </g>
        </template>
      </svg>
      <div v-if="hover !== undefined && points[hover]" class="pk-tip" role="status">
        {{ label(points[hover]!.label) }}: <b>{{ format(points[hover]!.value) }}</b>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pk-svg {
  width: 100%;
  height: auto;
  display: block;
}
.pk-plot {
  position: relative;
}
.pk-bar {
  fill: rgb(var(--v-theme-primary));
  transition: opacity 0.15s;
}
.pk-dim {
  opacity: 0.45;
}
.pk-axis {
  stroke: rgba(var(--v-border-color), 0.4);
  stroke-width: 1;
}
.pk-label,
.pk-value {
  font-size: 12px;
  fill: rgba(var(--v-theme-on-surface), var(--v-medium-emphasis-opacity));
}
.pk-value {
  fill: rgb(var(--v-theme-on-surface));
}
.pk-tip {
  position: absolute;
  top: 0;
  left: 0;
  padding: 4px 8px;
  border-radius: 6px;
  font-size: 13px;
  background: rgb(var(--v-theme-surface));
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  pointer-events: none;
}
</style>
