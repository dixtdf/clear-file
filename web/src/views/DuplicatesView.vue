<script setup>
import { computed, reactive, ref } from 'vue'
import { api } from '../api/client'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import DirPicker from '../components/DirPicker.vue'
import TaskProgress from '../components/TaskProgress.vue'
import { useTaskStream } from '../composables/useTask'
import { toast } from '../composables/useToast'
import { deleteInBatches, localPreview, PREVIEW_LIMIT } from '../utils/bulk'
import { formatBytes, formatNumber } from '../utils/format'

const modes = [
  { key: 'size', label: '仅大小一致', hint: '最快，准确度低，只作为候选' },
  { key: 'fast', label: '快速校验', hint: '大小 + 头/中/尾 三段 Hash，准确度非常高（推荐）' },
  { key: 'full', label: '完整校验', hint: '整文件 Hash，准确度最高，速度较慢' }
]

/** Groups fetched per page while walking the whole result. */
const GROUP_PAGE_SIZE = 200
/** Safety bound for "apply to every group" actions. */
const GROUP_SCAN_LIMIT = 20000
/** How many files of one group are rendered before the list collapses. */
const FILE_RENDER_LIMIT = 200

const dirs = ref(['/mnt'])
const mode = ref('fast')
const minValue = ref('1')
const minUnit = ref('KB')
const pickerOpen = ref(false)

const { task, watch: watchTask } = useTaskStream()
const groups = ref([])
const stats = ref({ total: 0, totalFiles: 0, totalSize: 0, reclaimable: 0, candidates: 0, truncated: false })
const page = ref(1)
const pageSize = ref(20)
const pageCount = computed(() => Math.max(1, Math.ceil(stats.value.total / pageSize.value)))

// Selection is keyed by path so it survives paging; the counters are kept
// incrementally because a group sweep can tick tens of thousands of files and
// recomputing them from scratch on every render would be quadratic.
const selection = reactive({})
const selectedCount = ref(0)
const selectedSize = ref(0)
const expanded = reactive({}) // groupId -> true (show every file of that group)
const preferredDir = ref('/mnt/movie')
const sweeping = ref(false)

const dialog = reactive({ open: false, busy: false, preview: null, paths: [], progress: '' })

function addDir(p) {
  if (!dirs.value.includes(p)) dirs.value.push(p)
}

function removeDir(p) {
  dirs.value = dirs.value.filter((d) => d !== p)
}

function selectFile(f) {
  if (selection[f.path]) return false
  selection[f.path] = f
  selectedCount.value++
  selectedSize.value += f.size || 0
  return true
}

function unselectFile(f) {
  if (!selection[f.path]) return false
  delete selection[f.path]
  selectedCount.value--
  selectedSize.value -= f.size || 0
  return true
}

function toggle(f) {
  if (!unselectFile(f)) selectFile(f)
}

function isSelected(f) {
  return !!selection[f.path]
}

function clearSelection() {
  Object.keys(selection).forEach((k) => delete selection[k])
  selectedCount.value = 0
  selectedSize.value = 0
}

function selectedFiles() {
  return Object.values(selection)
}

function visibleFiles(g) {
  return expanded[g.id] ? g.files : g.files.slice(0, FILE_RENDER_LIMIT)
}

function hiddenFileCount(g) {
  return Math.max(0, g.files.length - FILE_RENDER_LIMIT)
}

/** Walks every page of the current duplicate result. */
async function eachGroup(fn) {
  const id = task.value && task.value.id
  if (!id) {
    toast('请先执行扫描', 'warn')
    return 0
  }
  let seen = 0
  let p = 1
  while (seen < GROUP_SCAN_LIMIT) {
    const res = await api.duplicateGroups(id, p, GROUP_PAGE_SIZE)
    const list = res.groups || []
    for (const g of list) fn(g)
    seen += list.length
    if (list.length < GROUP_PAGE_SIZE || p * GROUP_PAGE_SIZE >= res.total) break
    p++
  }
  return seen
}

/** Keeps the first file of every group on every page, ticks the rest. */
async function keepFirstPerGroup() {
  sweeping.value = true
  let ticked = 0
  try {
    const groupsSeen = await eachGroup((g) => {
      g.files.forEach((f, i) => {
        if (i === 0) unselectFile(f)
        else if (selectFile(f)) ticked++
      })
    })
    toast(`已处理 ${formatNumber(groupsSeen)} 组，勾选 ${formatNumber(ticked)} 个待删文件`, 'ok')
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    sweeping.value = false
  }
}

/** Keeps everything inside preferredDir across all pages, ticks the rest. */
async function keepPreferredDir() {
  const prefix = preferredDir.value.replace(/\/+$/, '')
  if (!prefix) {
    toast('请先填写优先保留目录', 'warn')
    return
  }
  sweeping.value = true
  let ticked = 0
  try {
    const groupsSeen = await eachGroup((g) => {
      g.files.forEach((f) => {
        if (f.path === prefix || f.path.startsWith(prefix + '/')) unselectFile(f)
        else if (selectFile(f)) ticked++
      })
    })
    toast(`已处理 ${formatNumber(groupsSeen)} 组，勾选 ${formatNumber(ticked)} 个不在 ${prefix} 中的文件`, 'ok')
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    sweeping.value = false
  }
}

/** Per-group shortcut on the group header. */
function keepFirstOfGroup(g) {
  g.files.forEach((f, i) => {
    if (i === 0) unselectFile(f)
    else selectFile(f)
  })
}

async function startScan() {
  if (!dirs.value.length) {
    toast('请先添加扫描目录', 'warn')
    return
  }
  groups.value = []
  clearSelection()
  try {
    const res = await api.scanDuplicates({
      paths: dirs.value,
      mode: mode.value,
      minSizeBytes: toBytesSafe(minValue.value, minUnit.value)
    })
    watchTask(res.taskId, { onDone: () => loadGroups(res.taskId, 1) })
  } catch (e) {
    toast(e.message, 'error')
  }
}

function toBytesSafe(v, u) {
  const n = Number(v)
  if (!n || n <= 0) return 0
  const idx = ['B', 'KB', 'MB', 'GB'].indexOf(u)
  return Math.round(n * Math.pow(1024, idx))
}

async function loadGroups(taskId, targetPage = page.value) {
  try {
    const res = await api.duplicateGroups(taskId, targetPage, pageSize.value)
    groups.value = res.groups || []
    stats.value = {
      total: res.total,
      totalFiles: res.totalFiles,
      totalSize: res.totalSize,
      reclaimable: res.reclaimable,
      candidates: res.candidates,
      truncated: res.truncated
    }
    page.value = res.page
  } catch (e) {
    toast(e.message, 'error')
  }
}

async function askDelete(list) {
  if (!list.length) return
  const paths = list.map((e) => e.path)
  dialog.paths = paths
  dialog.progress = ''
  dialog.busy = true
  dialog.open = true
  try {
    // Big selections are summarized locally: the server preview echoes paths
    // back and the dialog only ever shows a short excerpt anyway.
    dialog.preview =
      paths.length <= PREVIEW_LIMIT ? await api.deletePreview(paths, dirs.value) : localPreview(list)
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
    const res = await deleteInBatches(dialog.paths, dirs.value, taskId, (done, total) => {
      dialog.progress = `正在删除 ${formatNumber(done)} / ${formatNumber(total)}`
    })
    toast(`已删除 ${res.count} 项，释放 ${formatBytes(res.freedBytes)}`, 'ok')
    if (res.failed && res.failed.length) toast(`${res.failed.length} 项删除失败`, 'error')
    dialog.open = false
    clearSelection()
    // Groups are pruned server-side; reload so the list matches disk.
    if (task.value) await loadGroups(task.value.id, 1)
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
        <span class="card-title">扫描目录</span>
        <div class="grow"></div>
        <button class="btn sm" @click="pickerOpen = true">添加目录</button>
      </div>
      <div class="card-body">
        <div class="pill-list">
          <span v-for="d in dirs" :key="d" class="pill mono">
            {{ d }}
            <span class="x" @click="removeDir(d)">✕</span>
          </span>
          <span v-if="!dirs.length" class="muted small">请添加至少一个目录</span>
        </div>
      </div>
      <div class="toolbar">
        <div class="field">
          <label>校验模式</label>
          <div class="row">
            <label v-for="m in modes" :key="m.key" class="row" style="gap: 6px">
              <input v-model="mode" type="radio" :value="m.key" />
              <span>{{ m.label }}</span>
            </label>
          </div>
        </div>
        <div class="field">
          <label>忽略小于</label>
          <div class="row">
            <input v-model="minValue" class="input" style="width: 90px" />
            <select v-model="minUnit" class="select">
              <option>B</option><option>KB</option><option>MB</option><option>GB</option>
            </select>
          </div>
        </div>
        <div class="grow"></div>
        <button class="btn primary" :disabled="sweeping" @click="startScan">开始扫描</button>
      </div>
      <div class="card-body" style="border-top: 1px solid var(--border)">
        <span class="muted small">{{ modes.find((m) => m.key === mode).hint }}</span>
      </div>
    </div>

    <TaskProgress :task="task" @cancel="cancelScan" />

    <div v-if="task && (task.status === 'completed' || groups.length)" class="card">
      <div class="card-head">
        <span class="card-title">重复文件组</span>
        <span class="chip">重复组 {{ formatNumber(stats.total) }}</span>
        <span class="chip">文件 {{ formatNumber(stats.totalFiles) }}</span>
        <span class="chip">占用 {{ formatBytes(stats.totalSize) }}</span>
        <span class="chip ok">可释放 {{ formatBytes(stats.reclaimable) }}</span>
        <span v-if="stats.truncated" class="chip warn">结果已截断</span>
      </div>

      <div class="toolbar">
        <button class="btn sm" :disabled="sweeping || !groups.length" @click="keepFirstPerGroup">
          {{ sweeping ? '处理中…' : '每组保留第一个（全部页）' }}
        </button>
        <div class="row">
          <input v-model="preferredDir" class="input mono sm" style="width: 240px" placeholder="优先保留目录" />
          <button class="btn sm" :disabled="sweeping || !groups.length" @click="keepPreferredDir">
            保留该目录并勾选其他（全部页）
          </button>
        </div>
        <div class="grow"></div>
        <span v-if="selectedCount" class="chip primary">
          已选择 {{ formatNumber(selectedCount) }} 项 · 预计释放 {{ formatBytes(selectedSize) }}
        </span>
        <button class="btn sm" :disabled="!selectedCount" @click="clearSelection">取消选择</button>
        <button class="btn danger sm" :disabled="!selectedCount" @click="askDelete(selectedFiles())">
          删除选中
        </button>
      </div>

      <div class="card-body" v-if="!groups.length">
        <div class="empty">没有发现重复文件</div>
      </div>

      <div class="card-body" v-else>
        <div v-for="g in groups" :key="g.id" class="group">
          <div class="group-head">
            <span class="chip">重复组 #{{ g.id }}</span>
            <span class="muted small">单文件 {{ formatBytes(g.size) }} × {{ g.count }} 个</span>
            <span class="chip warn">可释放 {{ formatBytes(g.reclaimable) }}</span>
            <span class="chip">{{ g.verified === 'full' ? '完整校验' : g.verified === 'fast' ? '快速校验' : '仅大小' }}</span>
            <div class="grow"></div>
            <button class="btn sm" @click="keepFirstOfGroup(g)">保留第一个</button>
          </div>
          <div v-for="f in visibleFiles(g)" :key="f.path" class="group-file">
            <input type="checkbox" :checked="isSelected(f)" @change="toggle(f)" />
            <span class="mono grow path-cell">{{ f.path }}</span>
            <span class="muted small">{{ formatBytes(f.size) }}</span>
          </div>
          <div v-if="hiddenFileCount(g) && !expanded[g.id]" class="group-file muted small">
            仅显示前 {{ FILE_RENDER_LIMIT }} 个文件，还有 {{ formatNumber(hiddenFileCount(g)) }} 个未显示
            <button class="btn ghost sm" @click="expanded[g.id] = true">展开全部</button>
          </div>
          <div v-else-if="expanded[g.id] && hiddenFileCount(g)" class="group-file muted small">
            <button class="btn ghost sm" @click="expanded[g.id] = false">收起</button>
          </div>
        </div>
      </div>

      <div class="pager">
        <button class="btn sm" :disabled="page <= 1" @click="loadGroups(task.id, page - 1)">上一页</button>
        <span class="muted small">{{ page }} / {{ pageCount }}</span>
        <button class="btn sm" :disabled="page >= pageCount" @click="loadGroups(task.id, page + 1)">下一页</button>
        <div class="grow"></div>
        <span class="muted small">“每组保留第一个 / 保留该目录”会对全部 {{ formatNumber(stats.total) }} 组生效</span>
      </div>
    </div>

    <DirPicker :open="pickerOpen" @close="pickerOpen = false" @select="(p) => { addDir(p); pickerOpen = false }" />

    <ConfirmDialog
      :open="dialog.open"
      :busy="dialog.busy"
      :progress="dialog.progress"
      danger
      title="确认删除重复文件"
      :lines="[
        dialog.preview ? `即将删除 ${formatNumber(dialog.preview.count)} 项` : '正在统计…',
        dialog.preview ? `总大小：${formatBytes(dialog.preview.totalSize)}${dialog.preview.approximate ? '（本地估算）' : ''}` : '',
        '每个重复组请确保至少保留一份。'
      ].filter(Boolean)"
      :details="dialog.preview ? dialog.preview.paths : []"
      confirm-text="确认删除"
      @confirm="confirmDelete"
      @cancel="dialog.open = false"
    />
  </div>
</template>
