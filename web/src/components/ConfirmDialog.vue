<script setup>
import { computed } from 'vue'

const props = defineProps({
  open: Boolean,
  title: { type: String, default: '确认操作' },
  lines: { type: Array, default: () => [] },
  details: { type: Array, default: () => [] },
  confirmText: { type: String, default: '确认' },
  cancelText: { type: String, default: '取消' },
  danger: Boolean,
  busy: Boolean,
  wide: Boolean,
  /** How many detail lines to render; the rest is summarized. */
  maxDetails: { type: Number, default: 20 },
  /** Live progress text, e.g. "正在删除 500 / 12000". */
  progress: { type: String, default: '' }
})
const emit = defineEmits(['confirm', 'cancel'])

const visible = computed(() => props.details.slice(0, props.maxDetails))
const hidden = computed(() => Math.max(0, props.details.length - props.maxDetails))
</script>

<template>
  <div v-if="open" class="modal-mask" @click.self="emit('cancel')">
    <div class="modal" :class="{ wide }">
      <div class="modal-head">{{ title }}</div>
      <div class="modal-body">
        <p v-for="(line, i) in lines" :key="i" style="margin: 0 0 8px">{{ line }}</p>
        <div v-if="progress" class="chip primary" style="margin-top: 4px">{{ progress }}</div>
        <div v-if="details.length" class="path-list mono">
          <div v-for="(d, i) in visible" :key="i">{{ d }}</div>
          <div v-if="hidden" class="muted">… 其余 {{ hidden }} 项已省略</div>
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn" :disabled="busy" @click="emit('cancel')">{{ cancelText }}</button>
        <button class="btn" :class="danger ? 'danger' : 'primary'" :disabled="busy" @click="emit('confirm')">
          {{ busy ? '处理中…' : confirmText }}
        </button>
      </div>
    </div>
  </div>
</template>
