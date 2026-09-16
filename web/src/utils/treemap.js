/**
 * Squarified treemap layout (Bruls, Huizing & van Wijk).
 * Input items need a numeric `value`; the result is a list of
 * { item, x, y, w, h } rectangles that tile the given box exactly.
 */
export function layoutTreemap(items, width, height) {
  const list = (items || []).filter((i) => Number(i.value) > 0)
  const total = list.reduce((s, i) => s + Number(i.value), 0)
  if (!total || width <= 0 || height <= 0) return []

  const nodes = list.map((item) => ({ item, area: (Number(item.value) / total) * width * height }))
  const out = []

  let x = 0
  let y = 0
  let w = width
  let h = height
  let i = 0

  while (i < nodes.length) {
    const shorter = Math.min(w, h)
    const row = [nodes[i]]
    let rowArea = nodes[i].area
    i++

    while (i < nodes.length) {
      const nextArea = rowArea + nodes[i].area
      if (worst(row, rowArea, shorter) >= worst([...row, nodes[i]], nextArea, shorter)) {
        row.push(nodes[i])
        rowArea = nextArea
        i++
      } else {
        break
      }
    }

    const horizontal = w >= h
    const thickness = rowArea / (horizontal ? h : w)
    let offset = 0

    for (const n of row) {
      const len = thickness > 0 ? n.area / thickness : 0
      if (horizontal) {
        out.push({ item: n.item, x, y: y + offset, w: thickness, h: len })
      } else {
        out.push({ item: n.item, x: x + offset, y, w: len, h: thickness })
      }
      offset += len
    }

    if (horizontal) {
      x += thickness
      w -= thickness
    } else {
      y += thickness
      h -= thickness
    }
  }

  return out
}

function worst(row, area, shorter) {
  const areas = row.map((n) => n.area)
  const max = Math.max(...areas)
  const min = Math.min(...areas)
  if (min <= 0) return Number.POSITIVE_INFINITY
  const s2 = area * area
  const w2 = shorter * shorter
  return Math.max((w2 * max) / s2, s2 / (w2 * min))
}

const PALETTE = [
  '#4f7cff', '#22c55e', '#f59e0b', '#ef4444', '#a855f7',
  '#06b6d4', '#ec4899', '#84cc16', '#f97316', '#14b8a6',
  '#6366f1', '#eab308', '#0ea5e9', '#d946ef', '#10b981'
]

export function colorFor(index) {
  return PALETTE[index % PALETTE.length]
}
