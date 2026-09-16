const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

export function formatBytes(bytes, digits = 2) {
  const n = Number(bytes) || 0
  if (n <= 0) return '0 B'
  let i = 0
  let v = n
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : digits)} ${UNITS[i]}`
}

/** size + unit -> bytes */
export function toBytes(value, unit) {
  const idx = UNITS.indexOf(String(unit || 'B').toUpperCase())
  if (idx < 0) return 0
  return Math.round(Number(value) * Math.pow(1024, idx))
}

export function formatTime(ms) {
  if (!ms) return '-'
  const d = new Date(ms)
  const p = (v) => String(v).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

export function formatDuration(ms) {
  const total = Math.max(0, Math.floor((ms || 0) / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const p = (v) => String(v).padStart(2, '0')
  return h > 0 ? `${p(h)}:${p(m)}:${p(s)}` : `${p(m)}:${p(s)}`
}

export function formatNumber(n) {
  return Number(n || 0).toLocaleString('en-US')
}

/** "2026-09-15" -> unix ms (local midnight) */
export function dateToMs(dateStr, endOfDay = false) {
  if (!dateStr) return 0
  const d = new Date(`${dateStr}T${endOfDay ? '23:59:59' : '00:00:00'}`)
  return Number.isNaN(d.getTime()) ? 0 : d.getTime()
}

export function joinPath(base, name) {
  if (!base) return name
  const sep = base.endsWith('/') ? '' : '/'
  return `${base}${sep}${name}`
}
