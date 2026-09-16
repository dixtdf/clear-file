<script setup>
import { computed } from 'vue'
import { formatBytes, formatDuration, formatNumber } from '../utils/format'

const props = defineProps({
  task: { type: Object, default: null },
  showCancel: { type: Boolean, default: true }
})
const emit = defineEmits(['cancel'])

const statusText = {
  pending: '等待',
  running: '扫描中',
  completed: '完成',
  canceled: '已取消',
  failed: '失败'
}

const p = computed(() => (props.task && props.task.progress) || {})
const status = computed(() => (props.task && props.task.status) || '')
const chipClass = computed(() => {
  if (status.value === 'completed') return 'chip ok'
  if (status.value === 'failed') return 'chip danger'
  if (status.value === 'canceled') return 'chip warn'
  return 'chip primary'
})
const percent = computed(() => (p.value.percent >= 0 ? Math.min(100, p.value.percent) : -1))
</script>

<template>
  <div v-if="task" class="card">
    <div class="card-head">
      <span class="card-title">扫描任务</span>
      <span :class="chipClass">{{ statusText[status] || status }}</span>
      <span class="muted small mono">{{ task.id.slice(0, 8) }}</span>
      <div class="grow"></div>
      <span v-if="status === 'running'" class="muted small">
        文件 {{ formatNumber(p.files) }} · 目录 {{ formatNumber(p.dirs) }} · 已扫描 {{ formatBytes(p.bytes) }}
      </span>
      <button
        v-if="showCancel && (status === 'running' || status === 'pending')"
        class="btn sm"
        @click="emit('cancel')"
      >
        取消扫描
      </button>
    </div>
    <div class="card-body">
      <div class="progress" :class="{ unknown: percent < 0 && status === 'running' }">
        <span :style="{ width: (percent >= 0 ? percent : 30) + '%' }"></span>
      </div>
      <div class="row" style="margin-top: 10px; font-size: 12.5px">
        <span class="muted">耗时 {{ formatDuration(p.elapsedMs) }}</span>
        <span v-if="p.speed > 0" class="muted">速度 {{ formatBytes(p.speed) }}/s</span>
        <span v-if="p.found > 0" class="muted">已发现 {{ formatNumber(p.found) }} 项</span>
        <span v-if="task.error" class="chip danger">{{ task.error }}</span>
      </div>
      <div v-if="p.current" class="mono muted" style="margin-top: 6px; overflow: hidden; text-overflow: ellipsis">
        {{ p.current }}
      </div>
    </div>
  </div>
</template>
