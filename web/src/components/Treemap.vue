<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { layoutTreemap, colorFor } from '../utils/treemap'
import { formatBytes } from '../utils/format'

const props = defineProps({
  cells: { type: Array, default: () => [] }
})
const emit = defineEmits(['select'])

const box = ref(null)
const size = ref({ w: 800, h: 460 })
let observer = null

const rects = computed(() => {
  const items = props.cells.map((c) => ({ ...c, value: c.size }))
  return layoutTreemap(items, size.value.w, size.value.h)
})

function measure() {
  if (!box.value) return
  const r = box.value.getBoundingClientRect()
  size.value = { w: Math.max(1, Math.round(r.width)), h: Math.max(1, Math.round(r.height)) }
}

onMounted(() => {
  measure()
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(measure)
    observer.observe(box.value)
  } else {
    window.addEventListener('resize', measure)
  }
})

onUnmounted(() => {
  if (observer) observer.disconnect()
  else window.removeEventListener('resize', measure)
})
</script>

<template>
  <div ref="box" class="treemap">
    <div v-if="!rects.length" class="empty">暂无数据，请先执行目录扫描</div>
    <div
      v-for="(r, i) in rects"
      :key="r.item.path + i"
      class="cell"
      :title="`${r.item.name}\n${formatBytes(r.item.size)}`"
      :style="{
        left: r.x + 'px',
        top: r.y + 'px',
        width: Math.max(0, r.w - 1) + 'px',
        height: Math.max(0, r.h - 1) + 'px',
        background: colorFor(i)
      }"
      @click="emit('select', r.item)"
    >
      <span class="nm">{{ r.item.name }}</span>
      <span class="sz" v-if="r.w > 70 && r.h > 30">{{ formatBytes(r.item.size) }}</span>
    </div>
  </div>
</template>
