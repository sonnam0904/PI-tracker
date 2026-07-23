<script setup>
import { ref, computed } from 'vue'
import { parseISODate, todayISO } from '../lib/date'
import { UNASSIGNED_COLOR } from '../lib/people'
import { TYPE_LABEL, isBug } from '../lib/taskTypes'
import { FIELD_BY_KEY } from '../lib/taskFields'
import { buildGroups } from '../lib/taskFilters'

const props = defineProps({
  tasks: { type: Array, default: () => [] }, // đã được lọc + sắp xếp sẵn ở GanttView
  meta: { type: Object, default: () => ({}) },  // userID → {color, initials}
  names: { type: Object, default: () => ({}) }, // userID → tên
  sorts: { type: Array, default: () => [] },     // để hiện mũi tên trên header
  groups: { type: Array, default: () => [] },    // nhóm bảng (header thu gọn được)
})
const emit = defineEmits(['edit', 'sort'])

const STATUS_COLORS = {
  Todo: 'var(--gray)', 'In Progress': 'var(--accent)',
  Blocked: 'var(--red)', Done: 'var(--green)',
}

// Quá hạn: có hạn chót, chưa Done, hạn đã qua (so sánh chuỗi ISO theo ngày).
const overdue = t => !!t.dueDate && t.status !== 'Done' && t.dueDate < todayISO()
// Done trễ hạn: xong nhưng sau hạn chót.
const doneLate = t => !!t.dueDate && t.status === 'Done' && !!t.doneDate && t.doneDate > t.dueDate

// Cycle thực (ngày) khi có đủ Start + Done.
function cycleDays(t) {
  const s = parseISODate(t.startDate)
  const d = parseISODate(t.doneDate)
  if (!s || !d) return null
  return Math.max((d - s) / 86400000 - (t.blockedDays || 0), 0)
}

// key = cột hiển thị; field = key trong registry (đa số trùng, riêng assignee).
const COLS = [
  { field: 'title', label: 'Tiêu đề' },
  { field: 'assigneeId', label: 'Phụ trách' },
  { field: 'priority', label: 'Ưu tiên' },
  { field: 'status', label: 'Trạng thái' },
  { field: 'type', label: 'Loại' },
  { field: 'size', label: 'Size' },
  { field: 'aiUsed', label: 'AI' },
  { field: 'estimateCustomerDays', label: 'Est KH' },
  { field: 'estimateAiDays', label: 'Est AI' },
  { field: 'cycle', label: 'Cycle' },
  { field: 'startDate', label: 'Start' },
  { field: 'dueDate', label: 'Hạn chót' },
  { field: 'doneDate', label: 'Done' },
]

// Sort chính (cấp 1) để vẽ mũi tên; các cấp sau vẫn áp nhưng không hiện trên header.
const primarySort = computed(() => props.sorts?.[0] || null)

const fmtD = s => (s ? s.split('-').reverse().slice(0, 2).join('/') + '/' + s.slice(0, 4) : '—')
const groupFieldLabel = field => FIELD_BY_KEY[field]?.label || field

// ---- Thu gọn nhóm ----
const collapsed = ref(new Set())
function toggle(path) {
  const next = new Set(collapsed.value)
  next.has(path) ? next.delete(path) : next.add(path)
  collapsed.value = next
}
const isCollapsed = path => collapsed.value.has(path)

// ---- Thu gọn task cha (bug con) ----
// Dùng chung Set `collapsed` với nhóm, tiền tố 'task:' để không đụng path nhóm.
const taskKey = id => 'task:' + id
const toggleTask = id => toggle(taskKey(id))
const isTaskCollapsed = id => isCollapsed(taskKey(id))

// ---- Danh sách dòng để render ----
// Có nhóm → header nhóm xen task (phẳng trong nhóm). Không nhóm → giữ logic
// bug xếp dưới task cha (relatedTaskId).
const renderItems = computed(() => {
  const grouped = buildGroups(props.tasks, props.groups, { names: props.names })
  if (grouped) {
    return grouped
      .filter(it => it.ancestors.every(a => !isCollapsed(a)))
      .map(it => (it.type === 'task' ? { ...it, sub: false } : it))
  }
  // Không nhóm: bug có task gốc trong danh sách → thụt vào dưới cha.
  const ids = new Set(props.tasks.map(t => t.id))
  const children = new Map()
  const roots = []
  for (const t of props.tasks) {
    if (isBug(t) && t.relatedTaskId && ids.has(t.relatedTaskId)) {
      if (!children.has(t.relatedTaskId)) children.set(t.relatedTaskId, [])
      children.get(t.relatedTaskId).push(t)
    } else roots.push(t)
  }
  const out = []
  for (const r of roots) {
    const kids = children.get(r.id) || []
    out.push({ type: 'task', task: r, sub: false, ancestors: [], hasChildren: kids.length > 0, childCount: kids.length })
    if (isTaskCollapsed(r.id)) continue
    for (const b of kids) out.push({ type: 'task', task: b, sub: true, ancestors: [] })
  }
  return out
})
</script>

<template>
  <div>
    <div class="table-wrap">
      <table class="task-table">
        <thead>
          <tr>
            <th
              v-for="c in COLS" :key="c.field"
              :class="{ sorted: primarySort && primarySort.field === c.field }"
              :title="'Bấm để sắp xếp theo ' + c.label"
              @click="emit('sort', c.field)"
            >
              {{ c.label }}
              <span v-if="primarySort && primarySort.field === c.field">{{ primarySort.dir === 'asc' ? '▲' : '▼' }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <template v-for="it in renderItems" :key="it.type === 'group' ? it.path : 't' + it.task.id">
            <!-- Header nhóm -->
            <tr v-if="it.type === 'group'" class="tt-group" @click="toggle(it.path)">
              <td :colspan="COLS.length">
                <span :style="{ paddingLeft: it.depth * 20 + 'px' }">
                  <span class="tt-caret">{{ isCollapsed(it.path) ? '▸' : '▾' }}</span>
                  <span class="tt-gfield">{{ groupFieldLabel(it.field) }}:</span>
                  <b>{{ it.label }}</b>
                  <span class="tt-gcount">{{ it.count }}</span>
                </span>
              </td>
            </tr>

            <!-- Dòng task -->
            <tr v-else :class="{ 'tt-sub': it.sub }" @click="emit('edit', it.task)">
              <td class="tt-title">
                <span v-if="it.sub" class="tt-sub-arrow">└</span>
                <button
                  v-if="it.hasChildren"
                  class="tt-task-caret"
                  :title="isTaskCollapsed(it.task.id) ? 'Mở ' + it.childCount + ' task con' : 'Thu gọn task con'"
                  @click.stop="toggleTask(it.task.id)"
                >{{ isTaskCollapsed(it.task.id) ? '▸' : '▾' }}</button>
                {{ it.task.title }}
                <span v-if="it.hasChildren && isTaskCollapsed(it.task.id)" class="tt-child-count">{{ it.childCount }}</span>
                <span v-if="isBug(it.task)" class="kb-tag bug" style="margin-left: 6px">
                  🐞{{ it.task.severity ? ' ' + it.task.severity : '' }}
                </span>
                <span v-if="it.task.todoTotal" class="kb-tag" style="margin-left: 6px">☑ {{ it.task.todoDone }}/{{ it.task.todoTotal }}</span>
              </td>
              <td>
                <span v-if="it.task.assigneeId && names[it.task.assigneeId]" class="tt-assignee">
                  <span class="avatar" :style="{ background: meta[it.task.assigneeId].color, color: '#fff' }">
                    {{ meta[it.task.assigneeId].initials }}
                  </span>
                  {{ names[it.task.assigneeId] }}
                </span>
                <span v-else class="hint" :style="{ color: UNASSIGNED_COLOR }">Chưa gán</span>
              </td>
              <td><span class="prio" :class="it.task.priority">{{ it.task.priority || '—' }}</span></td>
              <td>
                <span class="tt-status" :style="{ color: STATUS_COLORS[it.task.status] }">
                  ● {{ it.task.status }}
                </span>
              </td>
              <td>{{ TYPE_LABEL[it.task.type] || '—' }}</td>
              <td>{{ it.task.size }}</td>
              <td>{{ it.task.aiUsed ? '✓' : '—' }}</td>
              <td>{{ it.task.estimateCustomerDays || '—' }}</td>
              <td>{{ it.task.estimateAiDays || '—' }}</td>
              <td>{{ cycleDays(it.task) !== null ? cycleDays(it.task).toFixed(1) + 'd' : '—' }}</td>
              <td>{{ fmtD(it.task.startDate) }}</td>
              <td>
                <span :class="{ 'due-overdue': overdue(it.task), 'due-late': doneLate(it.task) }">
                  {{ fmtD(it.task.dueDate) }}<template v-if="overdue(it.task)"> ⏰</template>
                </span>
                <span v-if="doneLate(it.task)" class="kb-tag late" title="Done sau hạn chót">trễ</span>
              </td>
              <td>{{ fmtD(it.task.doneDate) }}</td>
            </tr>
          </template>

          <tr v-if="!tasks.length">
            <td :colspan="COLS.length" class="empty" style="padding: 30px 0">
              Không có task nào khớp bộ lọc.
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
