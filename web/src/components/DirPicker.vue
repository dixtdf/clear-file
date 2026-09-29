<script setup>
import { computed, ref, watch } from 'vue'
import { api } from '../api/client'
import { formatTime } from '../utils/format'

const props = defineProps({
  open: Boolean,
  initial: { type: String, default: '' },
  title: { type: String, default: '选择目录' }
})
const emit = defineEmits(['select', 'close'])

const current = ref('')
const pathInput = ref('')
const crumbs = ref([])
const dirs = ref([])
const parent = ref('')
const page = ref(1)
const total = ref(0)
const loading = ref(false)
const error = ref('')
const pageSize = 100
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
let requestId = 0

watch(
  () => [props.open, props.initial],
  () => {
    if (props.open) load(props.initial || '')
    else requestId++
  },
  { immediate: true }
)

async function load(path, targetPage = 1) {
  const id = ++requestId
  loading.value = true
  error.value = ''
  try {
    const res = await api.listFiles({ path, dirsOnly: true, page: targetPage, pageSize, sort: 'name', order: 'asc' })
    if (id !== requestId) return
    current.value = res.path
    pathInput.value = res.path
    crumbs.value = res.breadcrumbs || []
    parent.value = res.parent || ''
    dirs.value = res.entries || []
    page.value = res.page
    total.value = res.total
  } catch (e) {
    if (id === requestId) error.value = e.message
  } finally {
    if (id === requestId) loading.value = false
  }
}

function goToInput() {
  load(pathInput.value.trim())
}
</script>

<template>
  <div v-if="open" class="modal-mask" @click.self="emit('close')">
    <div class="modal wide dir-picker" role="dialog" aria-modal="true" :aria-label="title">
      <div class="modal-head dir-picker-head">
        <div>
          <div class="dir-picker-title">{{ title }}</div>
          <div class="dir-picker-subtitle">浏览文件夹，选择要扫描的目录</div>
        </div>
        <button class="dir-picker-close" type="button" aria-label="关闭" @click="emit('close')">×</button>
      </div>

      <div class="modal-body dir-picker-body">
        <div class="dir-picker-location">
          <div class="dir-picker-location-label">当前位置</div>
          <nav class="breadcrumb dir-picker-breadcrumb" aria-label="目录路径">
            <template v-for="(c, i) in crumbs" :key="c.path">
              <span v-if="i > 0" class="sep">/</span>
              <button type="button" class="crumb" @click="load(c.path)">{{ c.name }}</button>
            </template>
          </nav>
        </div>

        <form class="dir-picker-jump" @submit.prevent="goToInput">
          <input v-model="pathInput" class="input mono" aria-label="目录路径" placeholder="输入目录路径" />
          <button class="btn" type="submit" :disabled="loading">前往</button>
        </form>

        <div class="dir-picker-list-head">
          <span>文件夹</span>
          <span>{{ total }} 个目录</span>
        </div>
        <div class="dir-picker-list" :aria-busy="loading">
          <div v-if="error" class="dir-picker-message danger">{{ error }}</div>
          <div v-else-if="loading" class="dir-picker-message">正在加载目录…</div>
          <template v-else>
            <button v-if="parent" type="button" class="dir-picker-row parent" @click="load(parent)">
              <span class="dir-picker-icon up">↑</span>
              <span class="dir-picker-row-main"><strong>返回上级</strong><small>{{ parent }}</small></span>
              <span class="dir-picker-arrow">›</span>
            </button>
            <div v-if="!dirs.length" class="dir-picker-message">这里没有子目录，可直接选择当前目录</div>
            <button v-for="d in dirs" :key="d.path" type="button" class="dir-picker-row" :title="d.path" @click="load(d.path)">
              <span class="dir-picker-icon">📁</span>
              <span class="dir-picker-row-main"><strong>{{ d.name }}</strong><small>文件夹 · {{ formatTime(d.mtime) }}</small></span>
              <span class="dir-picker-arrow">›</span>
            </button>
          </template>
        </div>
        <div v-if="pageCount > 1" class="dir-picker-pages">
          <button class="btn sm" type="button" :disabled="loading || page <= 1" @click="load(current, page - 1)">上一页</button>
          <span>{{ page }} / {{ pageCount }}</span>
          <button class="btn sm" type="button" :disabled="loading || page >= pageCount" @click="load(current, page + 1)">下一页</button>
        </div>
      </div>

      <div class="modal-foot dir-picker-foot">
        <div class="dir-picker-selection"><span>已选目录</span><strong :title="current">{{ current || '—' }}</strong></div>
        <button class="btn" type="button" @click="emit('close')">取消</button>
        <button class="btn primary" type="button" :disabled="loading || !!error || !current" @click="emit('select', current)">选择当前目录</button>
      </div>
    </div>
  </div>
</template>
