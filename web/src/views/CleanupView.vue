<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api/client'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import DirPicker from '../components/DirPicker.vue'
import TaskProgress from '../components/TaskProgress.vue'
import { useTaskStream } from '../composables/useTask'
import { toast } from '../composables/useToast'
import { deleteInBatches, localPreview, sortForDelete, PREVIEW_LIMIT, SELECT_ALL_LIMIT, SELECT_PAGE_SIZE } from '../utils/bulk'
import { formatBytes, formatNumber, formatTime, toBytes } from '../utils/format'

const tabs = [
  { key: 'empty_file', label: '空文件', hint: 'size = 0 的文件' },
  { key: 'empty_dir', label: '空目录', hint: '不含任何文件或目录，支持递归向上清理' },
  { key: 'small_file', label: '小文件', hint: '按大小阈值筛选' }
]

const mode = ref('empty_file')
const scanRoot = ref('')
const recursive = ref(true)
const includeEmpty = ref(true)
const smallValue = ref('500')
const smallUnit = ref('KB')
const pickerOpen = ref(false)

onMounted(async () => {
  try {
    const root = (await api.systemInfo()).root
    if (!scanRoot.value) scanRoot.value = root
  } catch { /* scan API reports unavailable server */ }
})

const { task, watch: watchTask } = useTaskStream()
const results = ref([])
const total = ref(0)
const totalSize = ref(0)
const page = ref(1)
const pageSize = ref(100)
const truncated = ref(false)
const busy = ref(false)

const selection = reactive({})
const selectedCount = ref(0)
const selectedSize = ref(0)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))

function selectItem(it) {
  if (selection[it.path]) return false
  selection[it.path] = it
  selectedCount.value++
  selectedSize.value += it.size || 0
  return true
}

function unselectItem(it) {
  if (!selection[it.path]) return false
  delete selection[it.path]
  selectedCount.value--
  selectedSize.value -= it.size || 0
  return true
}

const dialog = reactive({ open: false, busy: false, preview: null, paths: [], progress: '' })

async function startScan() {
  busy.value = true
  results.value = []
  total.value = 0
  clearSelection()
  try {
    const payload = {
      mode: mode.value,
      path: scanRoot.value,
      recursive: recursive.value
    }
    if (mode.value === 'small_file') {
      payload.maxSizeBytes = toBytes(smallValue.value, smallUnit.value)
      payload.includeEmpty = includeEmpty.value
    }
    const res = await api.scanCleanup(payload)
    watchTask(res.taskId, { onDone: () => loadResults(res.taskId, 1) })
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    busy.value = false
  }
}

async function loadResults(taskId, targetPage = page.value) {
  try {
    const res = await api.taskResults(taskId, targetPage, pageSize.value)
    results.value = res.items || []
    total.value = res.total
    totalSize.value = res.totalSize
    truncated.value = res.truncated
    page.value = res.page
  } catch (e) {
    toast(e.message, 'error')
  }
}

function toggle(item) {
  if (!unselectItem(item)) selectItem(item)
}

function isSelected(item) {
  return !!selection[item.path]
}

function togglePage() {
  const all = results.value.length > 0 && results.value.every(isSelected)
  results.value.forEach((it) => {
    if (all) unselectItem(it)
    else selectItem(it)
  })
}

function invertPage() {
  results.value.forEach((it) => {
    if (!unselectItem(it)) selectItem(it)
  })
}

async function selectAllResults() {
  if (!task.value) return
  const id = task.value.id
  const size = SELECT_PAGE_SIZE
  let p = 1
  let added = 0
  try {
    while (added < SELECT_ALL_LIMIT) {
      const res = await api.taskResults(id, p, size)
      const items = res.items || []
      items.forEach((it) => {
        if (selectItem(it)) added++
      })
      if (p * size >= res.total || items.length < size) break
      p++
    }
    if (added >= SELECT_ALL_LIMIT) toast(`单次最多选择 ${SELECT_ALL_LIMIT} 项`, 'warn')
  } catch (e) {
    toast(e.message, 'error')
  }
}

function clearSelection() {
  Object.keys(selection).forEach((k) => delete selection[k])
  selectedCount.value = 0
  selectedSize.value = 0
}

function selectedList() {
  return Object.values(selection)
}

async function askDelete(list) {
  if (!list.length) return
  // Deepest paths first: deleting /a/b/c before /a/b keeps recursive empty
  // directory cleanups working in one pass.
  const paths = sortForDelete(list)
  dialog.paths = paths
  dialog.progress = ''
  dialog.busy = true
  dialog.open = true
  try {
    // Empty-file scans can produce a huge selection; summarize it locally
    // instead of asking the server to echo every path back.
    dialog.preview =
      paths.length <= PREVIEW_LIMIT ? await api.deletePreview(paths, [scanRoot.value]) : localPreview(list)
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
    const taskId = task.value ? task.value.id : ''
    const res = await deleteInBatches(dialog.paths, [scanRoot.value], taskId, (done, total) => {
      dialog.progress = `正在删除 ${formatNumber(done)} / ${formatNumber(total)}`
    })
    toast(`已删除 ${res.count} 项，释放 ${formatBytes(res.freedBytes)}`, 'ok')
    if (res.failed && res.failed.length) {
      toast(`${res.failed.length} 项删除失败`, 'error')
    }
    dialog.open = false
    clearSelection()
    // Results are pruned server-side, so this refresh shows what is left.
    if (task.value) await loadResults(task.value.id, 1)
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    dialog.busy = false
    dialog.progress = ''
  }
}

async function cancelScan() {
  if (task.value) await api.cancelTask(task.value.id)
}
</script>

<template>
  <div>
    <div class="card">
      <div class="card-head">
        <div class="tabs">
          <div
            v-for="t in tabs"
            :key="t.key"
            class="tab"
            :class="{ active: mode === t.key }"
            @click="mode = t.key"
          >
            {{ t.label }}
          </div>
        </div>
        <span class="muted small">{{ tabs.find((t) => t.key === mode).hint }}</span>
      </div>

      <div class="toolbar">
        <div class="field">
          <label>扫描目录</label>
          <div class="row">
            <input v-model="scanRoot" class="input mono" style="width: 320px" />
            <button class="btn" @click="pickerOpen = true">浏览</button>
          </div>
        </div>

        <div class="field">
          <label>扫描范围</label>
          <label class="row" style="gap: 6px">
            <input v-model="recursive" type="checkbox" />
            <span>递归子目录</span>
          </label>
        </div>

        <template v-if="mode === 'small_file'">
          <div class="field">
            <label>小于</label>
            <div class="row">
              <input v-model="smallValue" class="input" style="width: 90px" />
              <select v-model="smallUnit" class="select">
                <option>B</option><option>KB</option><option>MB</option><option>GB</option>
              </select>
            </div>
          </div>
          <div class="field">
            <label>快捷阈值</label>
            <div class="row">
              <button v-for="v in [['1','KB'],['10','KB'],['100','KB'],['1','MB']]" :key="v.join()" class="btn sm"
                @click="smallValue = v[0]; smallUnit = v[1]">
                {{ v[0] }} {{ v[1] }}
              </button>
            </div>
          </div>
          <div class="field">
            <label>包含 0 字节文件</label>
            <input v-model="includeEmpty" type="checkbox" />
          </div>
        </template>

        <div class="grow"></div>
        <button class="btn primary" :disabled="busy" @click="startScan">开始扫描</button>
      </div>
    </div>

    <TaskProgress :task="task" @cancel="cancelScan" />

    <div v-if="task && (task.status === 'completed' || results.length)" class="card">
      <div class="card-head">
        <span class="card-title">扫描结果</span>
        <span class="chip">共 {{ formatNumber(total) }} 项</span>
        <span v-if="totalSize" class="chip">合计 {{ formatBytes(totalSize) }}</span>
        <span v-if="truncated" class="chip warn">结果超出上限，已截断</span>
        <div class="grow"></div>
        <button class="btn sm" @click="togglePage">全选本页</button>
        <button class="btn sm" @click="selectAllResults">全选全部结果</button>
        <button class="btn sm" @click="invertPage">反选本页</button>
        <button class="btn sm" @click="clearSelection">取消选择</button>
        <button class="btn danger sm" :disabled="!selectedCount" @click="askDelete(selectedList())">
          删除选中（{{ formatNumber(selectedCount) }}）
        </button>
      </div>

      <div class="card-body" v-if="selectedCount">
        <span class="chip primary">已选择 {{ formatNumber(selectedCount) }} 项 · 预计释放 {{ formatBytes(selectedSize) }}</span>
      </div>

      <div v-if="!results.length" class="empty">没有发现符合条件的项目</div>
      <div v-else class="table-wrap">
        <table class="data">
          <thead>
            <tr>
              <th style="width: 34px"><input type="checkbox" @change="togglePage" /></th>
              <th>名称</th>
              <th>路径</th>
              <th>大小</th>
              <th>修改时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="it in results" :key="it.path">
              <td><input type="checkbox" :checked="isSelected(it)" @change="toggle(it)" /></td>
              <td>{{ it.isDir ? '📁' : '📄' }} {{ it.name }}</td>
              <td class="path mono">{{ it.path }}</td>
              <td>{{ it.isDir ? '-' : formatBytes(it.size) }}</td>
              <td class="muted">{{ formatTime(it.mtime) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="pager">
        <button class="btn sm" :disabled="page <= 1" @click="loadResults(task.id, page - 1)">上一页</button>
        <span class="muted small">{{ page }} / {{ pageCount }}</span>
        <button class="btn sm" :disabled="page >= pageCount" @click="loadResults(task.id, page + 1)">下一页</button>
      </div>
    </div>

    <DirPicker :open="pickerOpen" :initial="scanRoot" @close="pickerOpen = false" @select="(p) => { scanRoot = p; pickerOpen = false }" />

    <ConfirmDialog
      :open="dialog.open"
      :busy="dialog.busy"
      :progress="dialog.progress"
      danger
      title="确认删除"
      :lines="[
        dialog.preview ? `即将删除 ${dialog.preview.count} 项` : '正在统计…',
        dialog.preview ? `总大小：${formatBytes(dialog.preview.totalSize)}${dialog.preview.approximate ? '（本地估算）' : ''}` : ''
      ].filter(Boolean)"
      :details="dialog.preview ? dialog.preview.paths : []"
      confirm-text="确认删除"
      @confirm="confirmDelete"
      @cancel="dialog.open = false"
    />
  </div>
</template>
