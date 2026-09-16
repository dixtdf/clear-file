<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api/client'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { toast } from '../composables/useToast'
import { deleteInBatches, localPreview, sortForDelete, PREVIEW_LIMIT, SELECT_ALL_LIMIT, SELECT_PAGE_SIZE } from '../utils/bulk'
import { formatBytes, formatNumber, formatTime, toBytes, dateToMs } from '../utils/format'

const loading = ref(false)
const error = ref('')

const path = ref('/mnt')
const entries = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(200)
const parent = ref('')
const crumbs = ref([])
const summary = ref({ fileCount: 0, dirCount: 0, totalSize: 0 })

const filters = reactive({
  name: '',
  ext: '',
  minValue: '',
  minUnit: 'KB',
  maxValue: '',
  maxUnit: 'MB',
  from: '',
  to: ''
})

const selection = reactive({}) // path -> entry
const sort = ref('name')
const order = ref('asc')

const selectedCount = ref(0)
const selectedSize = ref(0)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))

function selectItem(e) {
  if (selection[e.path]) return false
  selection[e.path] = e
  selectedCount.value++
  selectedSize.value += e.size || 0
  return true
}

function unselectItem(e) {
  if (!selection[e.path]) return false
  delete selection[e.path]
  selectedCount.value--
  selectedSize.value -= e.size || 0
  return true
}

function selectedList() {
  return Object.values(selection)
}

const dialog = reactive({ open: false, busy: false, preview: null, mode: 'selected', paths: [], progress: '' })

async function load(targetPath = path.value, targetPage = page.value) {
  loading.value = true
  error.value = ''
  try {
    const params = {
      path: targetPath,
      page: targetPage,
      pageSize: pageSize.value,
      sort: sort.value,
      order: order.value,
      name: filters.name || undefined,
      ext: filters.ext || undefined,
      mtimeFrom: dateToMs(filters.from) || undefined,
      mtimeTo: dateToMs(filters.to, true) || undefined
    }
    if (filters.minValue !== '') params.minSize = toBytes(filters.minValue, filters.minUnit)
    if (filters.maxValue !== '') params.maxSize = toBytes(filters.maxValue, filters.maxUnit)

    const res = await api.listFiles(params)
    path.value = res.path
    entries.value = res.entries || []
    total.value = res.total
    page.value = res.page
    parent.value = res.parent
    crumbs.value = res.breadcrumbs || []
    summary.value = res.summary || { fileCount: 0, dirCount: 0, totalSize: 0 }
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function openDir(entry) {
  if (entry.isDir) {
    clearSelection()
    load(entry.path, 1)
  }
}

function goUp() {
  if (parent.value) {
    clearSelection()
    load(parent.value, 1)
  }
}

function applyFilters() {
  clearSelection()
  load(path.value, 1)
}

function resetFilters() {
  filters.name = ''
  filters.ext = ''
  filters.minValue = ''
  filters.maxValue = ''
  filters.from = ''
  filters.to = ''
  applyFilters()
}

function setSort(key) {
  if (sort.value === key) {
    order.value = order.value === 'asc' ? 'desc' : 'asc'
  } else {
    sort.value = key
    order.value = 'asc'
  }
  load()
}

function toggle(entry) {
  if (!unselectItem(entry)) selectItem(entry)
}

function isSelected(entry) {
  return !!selection[entry.path]
}

function togglePage() {
  const all = entries.value.length > 0 && entries.value.every(isSelected)
  entries.value.forEach((e) => {
    if (all) unselectItem(e)
    else selectItem(e)
  })
}

function invertPage() {
  entries.value.forEach((e) => {
    if (!unselectItem(e)) selectItem(e)
  })
}

function clearSelection() {
  Object.keys(selection).forEach((k) => delete selection[k])
  selectedCount.value = 0
  selectedSize.value = 0
}

/** Selects every entry matching the current filter, across all pages. */
async function selectAllFiltered() {
  const size = SELECT_PAGE_SIZE
  let p = 1
  let added = 0
  try {
    while (added < SELECT_ALL_LIMIT) {
      const params = {
        path: path.value,
        page: p,
        pageSize: size,
        sort: sort.value,
        order: order.value,
        name: filters.name || undefined,
        ext: filters.ext || undefined,
        mtimeFrom: dateToMs(filters.from) || undefined,
        mtimeTo: dateToMs(filters.to, true) || undefined
      }
      if (filters.minValue !== '') params.minSize = toBytes(filters.minValue, filters.minUnit)
      if (filters.maxValue !== '') params.maxSize = toBytes(filters.maxValue, filters.maxUnit)

      const res = await api.listFiles(params)
      const list = res.entries || []
      list.forEach((e) => {
        if (selectItem(e)) added++
      })
      if (p * size >= res.total || list.length < size) break
      p++
    }
    if (added >= SELECT_ALL_LIMIT) toast(`为避免误操作，单次最多选择 ${SELECT_ALL_LIMIT} 项`, 'warn')
  } catch (e) {
    toast(e.message, 'error')
  }
}

async function askDelete(list) {
  if (!list.length) return
  // Deepest paths first, so nested selections delete cleanly.
  const paths = sortForDelete(list)
  dialog.mode = 'selected'
  dialog.paths = paths
  dialog.progress = ''
  dialog.busy = true
  dialog.open = true
  try {
    // A server preview echoes every path back; for huge selections we just
    // summarize locally so the dialog opens instantly.
    dialog.preview =
      paths.length <= PREVIEW_LIMIT ? await api.deletePreview(paths, []) : localPreview(list)
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
    const res = await deleteInBatches(dialog.paths, [], '', (done, total) => {
      dialog.progress = `正在删除 ${formatNumber(done)} / ${formatNumber(total)}`
    })
    toast(`已删除 ${res.count} 项，释放 ${formatBytes(res.freedBytes)}`, 'ok')
    if (res.failed && res.failed.length) {
      toast(`${res.failed.length} 项删除失败：${res.failed[0].error}`, 'error')
    }
    dialog.open = false
    clearSelection()
    load()
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    dialog.busy = false
    dialog.progress = ''
  }
}

onMounted(() => load())
</script>

<template>
  <div>
    <div class="card">
      <div class="card-head">
        <button class="btn" :disabled="!parent" @click="goUp">↑ 上级</button>
        <button class="btn" @click="load()">刷新</button>
        <div class="breadcrumb grow">
          <template v-for="(c, i) in crumbs" :key="c.path">
            <span v-if="i > 0" class="sep">/</span>
            <span class="crumb" @click="load(c.path, 1)">{{ c.name }}</span>
          </template>
        </div>
        <span class="muted small">
          当前目录 {{ formatNumber(summary.dirCount) }} 个文件夹 · {{ formatNumber(summary.fileCount) }} 个文件 ·
          {{ formatBytes(summary.totalSize) }}
        </span>
      </div>

      <div class="toolbar">
        <div class="field">
          <label>文件名</label>
          <input v-model="filters.name" class="input" style="width: 170px" placeholder="abc" @keyup.enter="applyFilters" />
        </div>
        <div class="field">
          <label>扩展名（逗号分隔）</label>
          <input v-model="filters.ext" class="input" style="width: 180px" placeholder=".log,.tmp" @keyup.enter="applyFilters" />
        </div>
        <div class="field">
          <label>大小下限</label>
          <div class="row">
            <input v-model="filters.minValue" class="input" style="width: 90px" placeholder="1" />
            <select v-model="filters.minUnit" class="select">
              <option>B</option><option>KB</option><option>MB</option><option>GB</option>
            </select>
          </div>
        </div>
        <div class="field">
          <label>大小上限</label>
          <div class="row">
            <input v-model="filters.maxValue" class="input" style="width: 90px" placeholder="100" />
            <select v-model="filters.maxUnit" class="select">
              <option>B</option><option>KB</option><option>MB</option><option>GB</option>
            </select>
          </div>
        </div>
        <div class="field">
          <label>修改时间从</label>
          <input v-model="filters.from" type="date" class="input" />
        </div>
        <div class="field">
          <label>到</label>
          <input v-model="filters.to" type="date" class="input" />
        </div>
        <button class="btn primary" @click="applyFilters">筛选</button>
        <button class="btn" @click="resetFilters">重置</button>
        <div class="grow"></div>
        <div class="field">
          <label>每页</label>
          <select v-model.number="pageSize" class="select" @change="load(path, 1)">
            <option :value="100">100</option>
            <option :value="200">200</option>
            <option :value="500">500</option>
          </select>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="card-head">
        <button class="btn sm" @click="togglePage">全选当前页</button>
        <button class="btn sm" @click="selectAllFiltered">全选筛选结果</button>
        <button class="btn sm" @click="invertPage">反选本页</button>
        <button class="btn sm" @click="clearSelection">取消选择</button>
        <div class="grow"></div>
        <span v-if="selectedCount" class="chip primary">
          已选择 {{ formatNumber(selectedCount) }} 项 · 预计释放 {{ formatBytes(selectedSize) }}
        </span>
        <button class="btn danger sm" :disabled="!selectedCount" @click="askDelete(selectedList())">
          删除选中
        </button>
      </div>

      <div v-if="error" class="empty">{{ error }}</div>
      <div v-else-if="loading && !entries.length" class="empty">加载中…</div>
      <div v-else-if="!entries.length" class="empty">没有匹配的文件</div>

      <div v-else class="table-wrap">
        <table class="data">
          <thead>
            <tr>
              <th style="width: 34px">
                <input type="checkbox" :checked="entries.length > 0 && entries.every(isSelected)" @change="togglePage" />
              </th>
              <th @click="setSort('name')" style="cursor: pointer">
                名称 <span v-if="sort === 'name'" class="muted">{{ order === 'asc' ? '▲' : '▼' }}</span>
              </th>
              <th @click="setSort('type')" style="cursor: pointer">类型</th>
              <th @click="setSort('size')" style="cursor: pointer" class="num">
                大小 <span v-if="sort === 'size'" class="muted">{{ order === 'asc' ? '▲' : '▼' }}</span>
              </th>
              <th @click="setSort('mtime')" style="cursor: pointer">
                修改时间 <span v-if="sort === 'mtime'" class="muted">{{ order === 'asc' ? '▲' : '▼' }}</span>
              </th>
              <th style="width: 90px">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="e in entries" :key="e.path">
              <td><input type="checkbox" :checked="isSelected(e)" @change="toggle(e)" /></td>
              <td class="name" @click="openDir(e)">
                <span>{{ e.isDir ? '📁' : e.isSymlink ? '🔗' : '📄' }}</span>
                {{ e.name }}
              </td>
              <td class="muted">{{ e.isDir ? '文件夹' : e.ext ? e.ext.slice(1) : '文件' }}</td>
              <td>{{ e.isDir ? '-' : formatBytes(e.size) }}</td>
              <td class="muted">{{ formatTime(e.mtime) }}</td>
              <td>
                <button class="btn ghost sm" @click="askDelete([e])">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="pager">
        <button class="btn sm" :disabled="page <= 1" @click="load(path, page - 1)">上一页</button>
        <span class="muted small">{{ page }} / {{ pageCount }} · 共 {{ formatNumber(total) }} 项</span>
        <button class="btn sm" :disabled="page >= pageCount" @click="load(path, page + 1)">下一页</button>
        <div class="grow"></div>
        <span class="muted small mono">{{ path }}</span>
      </div>
    </div>

    <ConfirmDialog
      :open="dialog.open"
      :busy="dialog.busy"
      :progress="dialog.progress"
      danger
      title="确认删除"
      :lines="[
        dialog.preview ? `即将删除 ${dialog.preview.count} 项` : '正在统计…',
        dialog.preview ? `总大小：${formatBytes(dialog.preview.totalSize)}${dialog.preview.approximate ? '（本地估算）' : ''}` : '',
        dialog.preview && dialog.preview.skipped.length ? `已跳过 ${dialog.preview.skipped.length} 项受保护路径` : ''
      ].filter(Boolean)"
      :details="dialog.preview ? dialog.preview.paths : []"
      confirm-text="确认删除"
      @confirm="confirmDelete"
      @cancel="dialog.open = false"
    />
  </div>
</template>
