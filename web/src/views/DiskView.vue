<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api/client'
import DirPicker from '../components/DirPicker.vue'
import TaskProgress from '../components/TaskProgress.vue'
import Treemap from '../components/Treemap.vue'
import { useTaskStream } from '../composables/useTask'
import { toast } from '../composables/useToast'
import { formatBytes, formatNumber } from '../utils/format'

const scanPath = ref('/mnt')
const pickerOpen = ref(false)

const { task, watch: watchTask } = useTaskStream()
const currentPath = ref('')
const crumbs = ref([])
const cells = ref([])
const totalSize = ref(0)
const treeNode = ref(null)
const topFiles = ref([])

const dialog = reactive({ open: false, busy: false, preview: null, paths: [] })
const selected = ref([])
const selectedSize = computed(() => selected.value.reduce((s, e) => s + (e.size || 0), 0))

async function startScan() {
  cells.value = []
  treeNode.value = null
  currentPath.value = ''
  try {
    const res = await api.scanDisk({ path: scanPath.value })
    watchTask(res.taskId, { onDone: () => loadTreemap(res.taskId, '') })
  } catch (e) {
    toast(e.message, 'error')
  }
}

function taskId() {
  return task.value ? task.value.id : null
}

async function loadTreemap(id, path) {
  try {
    const res = await api.diskTreemap(id, path, 300)
    cells.value = res.cells || []
    totalSize.value = res.total
    currentPath.value = path
    crumbs.value = path
      ? path.split('/').filter(Boolean).map((seg, i, arr) => ({
          name: seg,
          path: '/' + arr.slice(0, i + 1).join('/')
        }))
      : []
    const t = await api.diskTree(id, path, 2)
    treeNode.value = t.node
    topFiles.value = (t.topFiles || []).slice(0, 50)
  } catch (e) {
    toast(e.message, 'error')
  }
}

function onCellClick(cell) {
  if (cell.path === '__other__') return
  if (cell.isDir) {
    loadTreemap(taskId(), cell.path)
  } else {
    toast(`${cell.name} · ${formatBytes(cell.size)}`)
  }
}

function onTreeClick(node) {
  if (node.isDir) loadTreemap(taskId(), node.path)
}

function pickTopFile(f) {
  selected.value = [f]
  dialog.paths = [f.path]
  askDelete([f])
}

async function askDelete(list) {
  dialog.busy = true
  dialog.open = true
  try {
    dialog.preview = await api.deletePreview(list.map((e) => e.path), [])
  } catch (e) {
    dialog.open = false
    toast(e.message, 'error')
  } finally {
    dialog.busy = false
  }
}

async function confirmDelete() {
  dialog.busy = true
  try {
    const id = taskId() || ''
    const res = await api.deleteFiles(dialog.preview.paths, [], id)
    toast(`已删除 ${res.count} 项，释放 ${formatBytes(res.freedBytes)}`, 'ok')
    dialog.open = false
    // The server pruned the deleted file out of the tree, so this refresh is
    // accurate without re-scanning.
    if (taskId()) loadTreemap(taskId(), currentPath.value)
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    dialog.busy = false
  }
}

async function cancelScan() {
  if (task.value) await api.cancelTask(task.value.id)
}

const maxChildSize = computed(() => {
  const list = (treeNode.value && treeNode.value.children) || []
  return list.reduce((m, c) => Math.max(m, c.size), 0) || 1
})

onMounted(() => {})
</script>

<template>
  <div>
    <div class="card">
      <div class="toolbar">
        <div class="field">
          <label>扫描目录</label>
          <div class="row">
            <input v-model="scanPath" class="input mono" style="width: 320px" />
            <button class="btn" @click="pickerOpen = true">浏览</button>
          </div>
        </div>
        <div class="field">
          <label>&nbsp;</label>
          <button class="btn primary" @click="startScan">扫描目录大小</button>
        </div>
        <div class="grow"></div>
        <span class="muted small">只有点击扫描后才会递归统计，平时浏览不会产生额外 IO</span>
      </div>
    </div>

    <TaskProgress :task="task" @cancel="cancelScan" />

    <div v-if="task && task.status === 'completed'" class="card">
      <div class="card-head">
        <span class="card-title">Treemap</span>
        <span class="chip">合计 {{ formatBytes(totalSize) }}</span>
        <div class="breadcrumb grow">
          <span class="crumb" @click="loadTreemap(task.id, '')">根目录</span>
          <template v-for="c in crumbs" :key="c.path">
            <span class="sep">/</span>
            <span class="crumb" @click="loadTreemap(task.id, c.path)">{{ c.name }}</span>
          </template>
        </div>
        <span class="muted small">点击方块进入子目录</span>
      </div>
      <div class="card-body">
        <Treemap :cells="cells" @select="onCellClick" />
      </div>
    </div>

    <div v-if="task && task.status === 'completed'" class="grid cols-3" style="margin-top: 16px">
      <div class="card">
        <div class="card-head">
          <span class="card-title">目录树</span>
          <span class="muted small">当前：{{ currentPath || '根目录' }}</span>
        </div>
        <div class="card-body tree">
          <div v-if="!treeNode || !treeNode.children || !treeNode.children.length" class="empty">没有子项</div>
          <div
            v-for="c in (treeNode && treeNode.children) || []"
            :key="c.path"
            class="node"
            @click="onTreeClick(c)"
          >
            <span>{{ c.isDir ? '📁' : '📄' }}</span>
            <span class="nm">{{ c.name }}</span>
            <span class="bar" :style="{ width: Math.max(4, (c.size / maxChildSize) * 120) + 'px' }"></span>
            <span class="sz">{{ formatBytes(c.size) }}</span>
          </div>
        </div>
      </div>

      <div class="card" style="grid-column: span 2">
        <div class="card-head">
          <span class="card-title">最大的文件</span>
          <span class="muted small">Top {{ topFiles.length }}</span>
        </div>
        <div class="table-wrap">
          <table class="data">
            <thead>
              <tr>
                <th>文件</th>
                <th>路径</th>
                <th>大小</th>
                <th style="width: 80px">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="f in topFiles" :key="f.path">
                <td>{{ f.name }}</td>
                <td class="path mono">{{ f.path }}</td>
                <td>{{ formatBytes(f.size) }}</td>
                <td><button class="btn ghost sm" @click="pickTopFile(f)">删除</button></td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <DirPicker :open="pickerOpen" :initial="scanPath" @close="pickerOpen = false" @select="(p) => { scanPath = p; pickerOpen = false }" />

    <ConfirmDialog
      :open="dialog.open"
      :busy="dialog.busy"
      danger
      title="确认删除"
      :lines="[
        dialog.preview ? `即将删除 ${dialog.preview.count} 项` : '正在统计…',
        dialog.preview ? `总大小：${formatBytes(dialog.preview.totalSize)}` : ''
      ].filter(Boolean)"
      :details="dialog.preview ? dialog.preview.paths : []"
      confirm-text="确认删除"
      @confirm="confirmDelete"
      @cancel="dialog.open = false"
    />
  </div>
</template>
