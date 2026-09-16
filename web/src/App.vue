<script setup>
import { computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from './api/client'
import { useTaskPoller } from './composables/useTask'
import { useToasts, dismiss } from './composables/useToast'
import { formatBytes, formatDuration } from './utils/format'

const route = useRoute()
const router = useRouter()
const { tasks, start } = useTaskPoller(2500)
const { items: toasts } = useToasts()

const nav = [
  { name: 'dashboard', path: '/', label: '首页', icon: '🏠' },
  { name: 'files', path: '/files', label: '文件管理', icon: '📁' },
  { name: 'cleanup', path: '/cleanup', label: '文件清理', icon: '🧹' },
  { name: 'duplicates', path: '/duplicates', label: '重复文件', icon: '🧬' },
  { name: 'disk', path: '/disk', label: '空间分析', icon: '📊' }
]

const running = computed(() => tasks.value.filter((t) => t.status === 'running' || t.status === 'pending'))
const title = computed(() => nav.find((n) => n.name === route.name)?.label || '文件清理器')

async function cancelTask(id) {
  try {
    await api.cancelTask(id)
  } catch (e) {
    /* the task may already be finished */
  }
}

onMounted(() => start())
</script>

<template>
  <div class="app">
    <aside class="sidebar">
      <div class="brand">
        <span class="dot">🧽</span>
        <span>文件清理器</span>
      </div>
      <div
        v-for="item in nav"
        :key="item.name"
        class="nav-item"
        :class="{ active: route.name === item.name }"
        @click="router.push(item.path)"
      >
        <span>{{ item.icon }}</span>
        <span>{{ item.label }}</span>
      </div>
      <div class="sidebar-foot">仅可访问 /mnt 挂载目录</div>
    </aside>

    <div class="main">
      <header class="topbar">
        <h1>{{ title }}</h1>
        <div class="grow"></div>
        <span v-if="running.length" class="chip primary">{{ running.length }} 个任务进行中</span>
      </header>
      <section class="content">
        <router-view />
      </section>
    </div>

    <div v-if="running.length" class="taskbar">
      <span class="muted">后台任务</span>
      <div v-for="t in running" :key="t.id" class="task">
        <span>{{ t.type }}</span>
        <div class="mini"><span :style="{ width: (t.progress.percent >= 0 ? t.progress.percent : 30) + '%' }"></span></div>
        <span class="muted">
          {{ formatBytes(t.progress.bytes) }} · {{ formatDuration(t.progress.elapsedMs) }}
        </span>
        <button class="btn sm ghost" @click="cancelTask(t.id)">取消</button>
      </div>
    </div>

    <div class="toasts">
      <div
        v-for="t in toasts"
        :key="t.id"
        class="toast"
        :class="t.type"
        @click="dismiss(t.id)"
      >
        {{ t.message }}
      </div>
    </div>
  </div>
</template>
