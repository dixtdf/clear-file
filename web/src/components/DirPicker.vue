<script setup>
import { ref, watch } from 'vue'
import { api } from '../api/client'
import { formatBytes } from '../utils/format'

const props = defineProps({
  open: Boolean,
  initial: { type: String, default: '/mnt' },
  title: { type: String, default: '选择目录' }
})
const emit = defineEmits(['select', 'close'])

const current = ref(props.initial || '/mnt')
const crumbs = ref([])
const dirs = ref([])
const parent = ref('')
const loading = ref(false)
const error = ref('')

watch(
  () => [props.open, props.initial],
  () => {
    if (props.open) load(props.initial || '/mnt')
  },
  { immediate: true }
)

async function load(path) {
  loading.value = true
  error.value = ''
  try {
    const res = await api.listFiles({ path, pageSize: 2000, sort: 'name', order: 'asc' })
    current.value = res.path
    crumbs.value = res.breadcrumbs || []
    parent.value = res.parent || ''
    dirs.value = (res.entries || []).filter((e) => e.isDir)
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div v-if="open" class="modal-mask" @click.self="emit('close')">
    <div class="modal wide">
      <div class="modal-head">{{ title }}</div>
      <div class="modal-body">
        <div class="breadcrumb" style="margin-bottom: 10px">
          <template v-for="(c, i) in crumbs" :key="c.path">
            <span v-if="i > 0" class="sep">/</span>
            <span class="crumb" @click="load(c.path)">{{ c.name }}</span>
          </template>
        </div>
        <div v-if="error" class="chip danger">{{ error }}</div>
        <div v-else-if="loading" class="empty">加载中…</div>
        <div v-else>
          <div v-if="parent" class="node tree" style="cursor: pointer" @click="load(parent)">.. 返回上级</div>
          <div v-if="!dirs.length" class="empty">该目录下没有子目录</div>
          <div
            v-for="d in dirs"
            :key="d.path"
            class="node tree"
            @click="load(d.path)"
          >
            <span>📁</span>
            <span class="nm">{{ d.name }}</span>
            <span class="sz">{{ formatBytes(d.size) }}</span>
          </div>
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn" @click="emit('close')">取消</button>
        <button class="btn primary" @click="emit('select', current)">选择 {{ current }}</button>
      </div>
    </div>
  </div>
</template>
