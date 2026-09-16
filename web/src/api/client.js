const BASE = '/api/v1'

function qs(params) {
  const sp = new URLSearchParams()
  Object.entries(params || {}).forEach(([k, v]) => {
    if (v === undefined || v === null || v === '') return
    sp.append(k, String(v))
  })
  const s = sp.toString()
  return s ? `?${s}` : ''
}

async function request(path, options = {}) {
  const res = await fetch(BASE + path, {
    method: options.method || 'GET',
    headers: options.body ? { 'Content-Type': 'application/json' } : undefined,
    body: options.body ? JSON.stringify(options.body) : undefined,
    signal: options.signal
  })

  const text = await res.text()
  let data = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = { error: text }
    }
  }

  if (!res.ok) {
    const msg = (data && data.error) || `请求失败 (${res.status})`
    const err = new Error(msg)
    err.status = res.status
    throw err
  }
  return data
}

export const api = {
  systemInfo: () => request('/system/info'),

  listFiles: (params) => request(`/files${qs(params)}`),
  deletePreview: (paths, keepRoots) => request('/files/delete/preview', { method: 'POST', body: { paths, keepRoots } }),
  deleteFiles: (paths, keepRoots, taskId) =>
    request('/files/delete', { method: 'POST', body: { paths, keepRoots, taskId: taskId || '' } }),

  scanCleanup: (payload) => request('/cleanup/scan', { method: 'POST', body: payload }),

  scanDuplicates: (payload) => request('/duplicate/scan', { method: 'POST', body: payload }),
  duplicateGroups: (id, page = 1, pageSize = 50) =>
    request(`/duplicate/${id}/groups${qs({ page, pageSize })}`),

  scanDisk: (payload) => request('/disk/scan', { method: 'POST', body: payload }),
  diskTree: (id, path, depth = 3) => request(`/disk/${id}/tree${qs({ path, depth })}`),
  diskTreemap: (id, path, limit = 300) => request(`/disk/${id}/treemap${qs({ path, limit })}`),

  tasks: () => request('/tasks'),
  task: (id) => request(`/tasks/${id}`),
  taskResults: (id, page = 1, pageSize = 100) => request(`/tasks/${id}/results${qs({ page, pageSize })}`),
  cancelTask: (id) => request(`/tasks/${id}/cancel`, { method: 'POST' }),

  taskEventsUrl: (id) => `${BASE}/tasks/${id}/events`
}
