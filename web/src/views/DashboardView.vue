<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api/client'
import { formatBytes, formatDuration, formatNumber } from '../utils/format'

const router = useRouter()
const info = ref(null)
const tasks = ref([])
const error = ref('')

const disk = computed(() => (info.value && info.value.disk) || {})
const percent = computed(() => Math.min(100, Number(disk.value.usedPercent) || 0))

const entries = [
  { title: '文件管理', desc: '浏览、筛选、批量删除', path: '/files', icon: '📁' },
  { title: '文件清理', desc: '空文件 / 空目录 / 小文件', path: '/cleanup', icon: '🧹' },
  { title: '查找重复文件', desc: '大小 + 校验 + 完整 Hash', path: '/duplicates', icon: '🧬' },
  { title: '磁盘空间分析', desc: '目录树 + Treemap', path: '/disk', icon: '📊' }
]

const statusText = {
  pending: '等待',
  running: '扫描中',
  completed: '完成',
  canceled: '已取消',
  failed: '失败'
}

onMounted(async () => {
  try {
    info.value = await api.systemInfo()
  } catch (e) {
    error.value = e.message
  }
  try {
    const res = await api.tasks()
    tasks.value = (res.tasks || []).slice(0, 8)
  } catch {
    /* ignore */
  }
})
</script>

<template>
  <div>
    <div v-if="error" class="card"><div class="card-body">{{ error }}</div></div>

    <div class="grid cols-4">
      <div class="card stat">
        <div class="label">挂载目录</div>
        <div class="value mono" style="font-size: 17px">{{ info ? info.root : '-' }}</div>
        <div class="sub">仅此目录可访问</div>
      </div>
      <div class="card stat">
        <div class="label">磁盘容量</div>
        <div class="value">{{ formatBytes(disk.total) }}</div>
        <div class="sub">已用 {{ formatBytes(disk.used) }}</div>
      </div>
      <div class="card stat">
        <div class="label">剩余空间</div>
        <div class="value">{{ formatBytes(disk.free) }}</div>
        <div class="sub">可用 {{ formatBytes(disk.avail) }}</div>
      </div>
      <div class="card stat">
        <div class="label">使用率</div>
        <div class="value">{{ percent.toFixed(1) }}%</div>
        <div class="progress" style="margin-top: 8px"><span :style="{ width: percent + '%' }"></span></div>
      </div>
    </div>

    <div class="grid cols-4" style="margin-top: 16px">
      <div v-for="e in entries" :key="e.path" class="card stat" style="cursor: pointer" @click="router.push(e.path)">
        <div style="font-size: 24px">{{ e.icon }}</div>
        <div class="value" style="font-size: 16px">{{ e.title }}</div>
        <div class="sub">{{ e.desc }}</div>
      </div>
    </div>

    <div class="card">
      <div class="card-head">
        <span class="card-title">最近扫描任务</span>
        <div class="grow"></div>
        <span class="muted small">启动后每次扫描都会保留在这里</span>
      </div>
      <div v-if="!tasks.length" class="empty">还没有扫描记录</div>
      <div v-else class="table-wrap">
        <table class="data">
          <thead>
            <tr>
              <th>类型</th>
              <th>状态</th>
              <th>文件</th>
              <th>目录</th>
              <th>已扫描</th>
              <th>耗时</th>
              <th>结果</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="t in tasks" :key="t.id">
              <td class="mono">{{ t.type }}</td>
              <td>
                <span class="chip" :class="t.status === 'completed' ? 'ok' : t.status === 'failed' ? 'danger' : t.status === 'running' ? 'primary' : ''">
                  {{ statusText[t.status] || t.status }}
                </span>
              </td>
              <td>{{ formatNumber(t.progress.files) }}</td>
              <td>{{ formatNumber(t.progress.dirs) }}</td>
              <td>{{ formatBytes(t.progress.bytes) }}</td>
              <td class="muted">{{ formatDuration(t.progress.elapsedMs) }}</td>
              <td>{{ formatNumber(t.resultCount) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
