import { ref } from 'vue'

const items = ref([])
let seq = 0

export function toast(message, type = 'info', timeout = 4200) {
  const id = ++seq
  items.value = [...items.value, { id, message, type }]
  if (timeout > 0) {
    setTimeout(() => dismiss(id), timeout)
  }
}

export function dismiss(id) {
  items.value = items.value.filter((t) => t.id !== id)
}

export function useToasts() {
  return { items, dismiss }
}
