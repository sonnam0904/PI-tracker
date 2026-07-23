<script setup>
import { ref, computed } from 'vue'
import { SaveTask } from '../../wailsjs/go/main/App'
import { todayISO } from '../lib/date'
import { UNASSIGNED_COLOR } from '../lib/people'
import { isBug } from '../lib/taskTypes'

const props = defineProps({
  tasks: { type: Array, default: () => [] },
  meta: { type: Object, default: () => ({}) },  // ID → {color, initials}
  names: { type: Object, default: () => ({}) }, // ID → tên
})
const emit = defineEmits(['edit', 'changed', 'error'])

const COLS = ['Todo', 'In Progress', 'Blocked', 'Done']
const COL_COLORS = {
  Todo: 'var(--gray)',
  'In Progress': 'var(--accent)',
  Blocked: 'var(--red)',
  Done: 'var(--green)',
}

const dragging = ref(null)
const overCol = ref('')
// Chặn click mở modal ngay sau khi thả kéo (WebKit vẫn bắn click sau drop).
let suppressClick = false

function onDragStart(e, t) {
  dragging.value = t
  // WebKitGTK cần setData thì thao tác kéo mới thực sự khởi động —
  // thiếu dòng này drag bị hủy ngầm.
  e.dataTransfer.setData('text/plain', String(t.id))
  e.dataTransfer.effectAllowed = 'move'
}

function onDragEnd() {
  dragging.value = null
  suppressClick = true
  setTimeout(() => (suppressClick = false), 200)
}

function onDragOver(e, c) {
  e.dataTransfer.dropEffect = 'move'
  overCol.value = c
}

function onCardClick(t) {
  if (suppressClick) return
  emit('edit', t)
}

const byCol = computed(() => {
  const g = {}
  for (const c of COLS) g[c] = []
  for (const t of props.tasks) (g[t.status] || g.Todo).push(t)
  // Done mới nhất lên đầu; các cột khác theo ngày tạo mới nhất.
  g.Done.sort((a, b) => (b.doneDate || '').localeCompare(a.doneDate || ''))
  for (const c of ['Todo', 'In Progress', 'Blocked']) {
    g[c].sort((a, b) => (b.createdDate || '').localeCompare(a.createdDate || ''))
  }
  return g
})

function personColor(t) {
  return t.assigneeId && props.meta[t.assigneeId] ? props.meta[t.assigneeId].color : UNASSIGNED_COLOR
}

// Quá hạn: có hạn chót, chưa Done, hạn đã qua.
const overdue = t => !!t.dueDate && t.status !== 'Done' && t.dueDate < todayISO()

async function drop(status) {
  overCol.value = ''
  const t = dragging.value
  dragging.value = null
  if (!t || t.status === status) return
  const dto = { ...t, status }
  // Rời cột Done thì bỏ Done date (backend từ chối trạng thái ≠ Done kèm Done date);
  // kéo vào Done mà chưa có Done date thì backend tự điền hôm nay.
  if (status !== 'Done') dto.doneDate = ''
  try {
    await SaveTask(dto)
    emit('changed')
  } catch (e) {
    emit('error', String(e))
  }
}
</script>

<template>
  <div class="kanban">
    <div
      v-for="c in COLS" :key="c"
      class="kb-col" :class="{ over: overCol === c }"
      @dragover.prevent="onDragOver($event, c)"
      @dragleave="overCol = overCol === c ? '' : overCol"
      @drop.prevent="drop(c)"
    >
      <div class="kb-head">
        <span class="dot" :style="{ background: COL_COLORS[c] }"></span>
        {{ c }}
        <span class="kb-count">{{ byCol[c].length }}</span>
      </div>

      <div
        v-for="t in byCol[c]" :key="t.id"
        class="kb-card" :class="{ dragging: dragging === t }"
        :style="{ borderLeft: '3px solid ' + personColor(t) }"
        draggable="true"
        @dragstart="onDragStart($event, t)"
        @dragend="onDragEnd"
        @click="onCardClick(t)"
      >
        <div class="kb-title">
          <span v-if="t.priority && t.priority !== 'P3'" class="prio" :class="t.priority">{{ t.priority }}</span>
          {{ t.title }}
        </div>
        <div class="kb-meta">
          <template v-if="t.assigneeId && names[t.assigneeId]">
            <span class="avatar" :style="{ background: meta[t.assigneeId].color, color: '#fff' }">
              {{ meta[t.assigneeId].initials }}
            </span>
            {{ names[t.assigneeId] }}
          </template>
          <span v-else>Chưa gán</span>
          <span class="kb-tag">{{ t.size }}</span>
          <span v-if="isBug(t)" class="kb-tag bug">🐞{{ t.severity ? ' ' + t.severity : '' }}</span>
          <span v-if="t.todoTotal" class="kb-tag" :style="t.todoDone === t.todoTotal ? 'color: var(--green)' : ''">
            ☑ {{ t.todoDone }}/{{ t.todoTotal }}
          </span>
          <span v-if="t.aiUsed" class="kb-tag">AI</span>
          <span v-if="overdue(t)" class="kb-tag overdue" :title="'Hạn chót ' + t.dueDate">⏰ {{ t.dueDate }}</span>
          <span v-else-if="t.dueDate && t.status !== 'Done'" class="kb-tag">⏱ {{ t.dueDate }}</span>
          <span v-if="t.doneDate" class="kb-tag">✓ {{ t.doneDate }}</span>
          <span v-else-if="t.startDate" class="kb-tag">▶ {{ t.startDate }}</span>
        </div>
      </div>

      <div v-if="!byCol[c].length" class="kb-empty">Kéo task vào đây</div>
    </div>
  </div>
</template>
