import { ref, onUnmounted } from 'vue'
import { api } from '../api/client'

/**
 * Subscribes to a background task over SSE.
 * Returns reactive task state plus start/stop helpers.
 */
export function useTaskStream() {
  const task = ref(null)
  const connected = ref(false)
  let source = null

  function close() {
    if (source) {
      source.close()
      source = null
    }
    connected.value = false
  }

  function watch(taskId, { onDone } = {}) {
    close()
    if (!taskId) return
    source = new EventSource(api.taskEventsUrl(taskId))
    connected.value = true

    const apply = (raw) => {
      try {
        task.value = JSON.parse(raw)
      } catch {
        /* ignore malformed frame */
      }
    }

    source.addEventListener('snapshot', (e) => apply(e.data))
    source.addEventListener('progress', (e) => apply(e.data))
    source.addEventListener('done', (e) => {
      apply(e.data)
      close()
      if (onDone) onDone(task.value)
    })
    source.addEventListener('failed', (e) => {
      apply(e.data)
    })
    // EventSource fires a plain `error` event on transport problems; the
    // stream may still recover by itself, so only mark it disconnected.
    source.addEventListener('error', () => {
      connected.value = false
    })
  }

  onUnmounted(close)

  return { task, connected, watch, close }
}

/** Polls the task registry - used by the global running-task bar. */
export function useTaskPoller(intervalMs = 2500) {
  const tasks = ref([])
  let timer = null

  async function refresh() {
    try {
      const res = await api.tasks()
      tasks.value = res.tasks || []
    } catch {
      /* keep the previous snapshot */
    }
  }

  function start() {
    if (timer) return
    refresh()
    timer = setInterval(refresh, intervalMs)
  }

  function stop() {
    if (timer) {
      clearInterval(timer)
      timer = null
    }
  }

  onUnmounted(stop)

  return { tasks, refresh, start, stop }
}
