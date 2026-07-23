<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import {
  ListTasks, ListPeople, ListSavedViews, CreateSavedView, UpdateSavedView, DeleteSavedView,
} from '../../wailsjs/go/main/App'
import { monthStart, addMonths, daysInMonth, monthLabel, parseISODate, daysBetween } from '../lib/date'
import { buildPeopleMeta, UNASSIGNED_COLOR } from '../lib/people'
import { isBug } from '../lib/taskTypes'
import { emptyConfig, parseConfig, matchesConfig, sameConfig, sortTasks } from '../lib/taskFilters'
import TaskModal from './TaskModal.vue'
import KanbanBoard from './KanbanBoard.vue'
import TaskTable from './TaskTable.vue'
import ViewToolbar from './ViewToolbar.vue'
import SavedViewTabs from './SavedViewTabs.vue'

// openTaskId: task cần mở modal ngay (nhảy từ thông báo); openActivityId:
// bình luận cần scroll tới + làm nổi bật trong modal (mention/reply).
const props = defineProps({
  openTaskId: { type: Number, default: 0 },
  openActivityId: { type: Number, default: 0 },
})
const emitEvents = defineEmits(['task-opened'])

const DAY_W = 34
const LABEL_W = 280
const ROW_H = 46

const view = ref('timeline') // 'timeline' | 'kanban' | 'table'
const month = ref(monthStart(new Date()))
const tasks = ref([])
const people = ref([])
const error = ref('')
const editing = ref(undefined) // undefined = đóng, null = thêm mới, object = sửa

const personMeta = computed(() => buildPeopleMeta(people.value))
const personName = computed(() => {
  const map = {}
  for (const p of people.value) map[p.ID] = p.Name
  return map
})

async function load() {
  error.value = ''
  try {
    ;[tasks.value, people.value, savedViews.value] = await Promise.all([
      ListTasks(), ListPeople(), ListSavedViews(),
    ])
  } catch (e) {
    error.value = String(e)
  }
}

// ---- Saved views: cấu hình lọc/sắp xếp/nhóm lưu thành tab kiểu Lark ----
const savedViews = ref([])
const activeViewId = ref(0) // 0 = tab "Tất cả"
const config = ref(emptyConfig())

const activeView = computed(() => savedViews.value.find(v => v.id === activeViewId.value))
// Cấu hình hiện tại đã lệch khỏi bản lưu của tab → hiện chấm + nút 💾 trên tab.
const viewDirty = computed(() =>
  activeView.value ? !sameConfig(config.value, parseConfig(activeView.value.filters)) : false)

function selectView(id) {
  activeViewId.value = id
  config.value = activeView.value ? parseConfig(activeView.value.filters) : emptyConfig()
}

async function createView(name) {
  try {
    const v = await CreateSavedView(name, JSON.stringify(config.value))
    savedViews.value = await ListSavedViews()
    activeViewId.value = v.id
  } catch (e) {
    error.value = String(e)
  }
}

async function saveActiveView(v) {
  try {
    await UpdateSavedView(v.id, v.name, JSON.stringify(config.value))
    savedViews.value = await ListSavedViews()
  } catch (e) {
    error.value = String(e)
  }
}

async function renameView(v, name) {
  try {
    await UpdateSavedView(v.id, name, v.filters)
    savedViews.value = await ListSavedViews()
  } catch (e) {
    error.value = String(e)
  }
}

async function removeView(v) {
  try {
    await DeleteSavedView(v.id)
    savedViews.value = await ListSavedViews()
    if (activeViewId.value === v.id) selectView(0)
  } catch (e) {
    error.value = String(e)
  }
}

// Bình luận cần làm nổi bật — chốt lại trước khi emit task-opened (App sẽ
// xóa pending ngay sau đó, prop về 0 trong khi modal vẫn cần giá trị này).
const focusActId = ref(0)

// Mở modal của task được yêu cầu từ ngoài (click thông báo).
function maybeOpenRequested() {
  if (!props.openTaskId) return
  const t = tasks.value.find(x => x.id === props.openTaskId)
  if (t) {
    focusActId.value = props.openActivityId || 0
    editing.value = t
  } else {
    error.value = `Không tìm thấy task #${props.openTaskId} trong workspace hiện tại (có thể đã bị xóa)`
  }
  emitEvents('task-opened')
}

onMounted(async () => {
  await load()
  maybeOpenRequested()
})
watch(() => props.openTaskId, maybeOpenRequested)

function shift(n) {
  month.value = n === 0 ? monthStart(new Date()) : addMonths(month.value, n)
}

const days = computed(() => daysInMonth(month.value))
const mStart = computed(() => month.value)
const mEnd = computed(() => addMonths(month.value, 1))
const trackW = computed(() => days.value * DAY_W)

function colorOf(t) {
  return t.assigneeId && personMeta.value[t.assigneeId]
    ? personMeta.value[t.assigneeId].color
    : UNASSIGNED_COLOR
}

// Done date nếu có; chưa xong thì Start + estimate AI (tối thiểu 1 ngày).
function barEnd(t) {
  const done = parseISODate(t.doneDate)
  if (done) return done
  const start = parseISODate(t.startDate)
  if (!start) return null
  const days = Math.max(t.estimateAiDays || 0, 1)
  return new Date(start.getTime() + days * 86400000)
}

const rows = computed(() => {
  const list = tasks.value.filter(t => {
    const s = parseISODate(t.startDate)
    if (!s) return true // chưa có start date: vẫn hiện để xử lý
    return barEnd(t) > mStart.value && s < mEnd.value
  })
  return list.sort((a, b) => {
    const sa = parseISODate(a.startDate), sb = parseISODate(b.startDate)
    if (!sa && !sb) return b.createdDate.localeCompare(a.createdDate)
    if (!sa) return 1
    if (!sb) return -1
    return sa - sb
  })
})

// Cấu hình view (lọc + sắp xếp) áp chung cho cả Timeline / Kanban / Danh sách,
// chồng lên bộ lọc tháng (rows). Nhóm chỉ áp ở bảng (TaskTable tự xử lý).
const filteredRows = computed(() => {
  const kept = rows.value.filter(t => matchesConfig(t, config.value))
  return sortTasks(kept, config.value.sorts, { names: personName.value })
})

// Bấm header bảng: đặt sort đơn theo cột đó; bấm lại cột đang sort thì đảo chiều.
function onHeaderSort(field) {
  const s = config.value.sorts
  if (s.length === 1 && s[0].field === field) {
    s[0].dir = s[0].dir === 'asc' ? 'desc' : 'asc'
  } else {
    config.value.sorts = [{ field, dir: 'asc' }]
  }
}

function barStyle(t) {
  const s = parseISODate(t.startDate)
  if (!s) return null
  let from = Math.max(daysBetween(mStart.value, s), 0)
  let to = Math.min(daysBetween(mStart.value, barEnd(t)), days.value)
  const width = Math.max((to - from) * DAY_W, DAY_W / 2)
  return { left: from * DAY_W + 'px', width: width + 'px', background: colorOf(t) }
}

function dayMeta(i) {
  const d = new Date(month.value.getFullYear(), month.value.getMonth(), i + 1)
  const wd = d.getDay()
  const now = new Date()
  return {
    weekend: wd === 0 || wd === 6,
    today: d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate(),
  }
}

const todayLeft = computed(() => {
  const now = new Date()
  if (now < mStart.value || now >= mEnd.value) return null
  return daysBetween(mStart.value, now) * DAY_W + 'px'
})

function onSaved() {
  editing.value = undefined
  focusActId.value = 0
  load()
}
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <div class="page-title">Tasks</div>
        <div class="legend" style="margin-top: 6px">
          <span v-for="p in people" :key="p.ID">
            <span class="dot" :style="{ background: personMeta[p.ID].color }"></span>{{ p.Name }}
          </span>
          <span><span class="dot" :style="{ background: UNASSIGNED_COLOR }"></span>Chưa gán</span>
          <span class="hint" v-if="view === 'timeline'">· nét đứt = dự kiến theo estimate AI</span>
        </div>
      </div>
      <div class="month-nav">
        <div class="view-toggle">
          <button :class="{ active: view === 'timeline' }" @click="view = 'timeline'">📅 Timeline</button>
          <button :class="{ active: view === 'kanban' }" @click="view = 'kanban'">▦ Kanban</button>
          <button :class="{ active: view === 'table' }" @click="view = 'table'">☰ Danh sách</button>
        </div>
        <button class="btn icon" @click="shift(-1)">◀</button>
        <span class="label">{{ monthLabel(month) }}</span>
        <button class="btn icon" @click="shift(1)">▶</button>
        <button class="btn" @click="shift(0)">Tháng này</button>
        <button class="btn primary" @click="editing = null">+ Thêm task</button>
      </div>
    </div>

    <div v-if="error" class="err">{{ error }}</div>

    <!-- Tab view kiểu Lark: Tất cả + các bộ lọc đã lưu của user -->
    <SavedViewTabs
      :views="savedViews" :active-id="activeViewId" :dirty="viewDirty"
      @select="selectView" @create="createView" @update-filters="saveActiveView"
      @rename="renameView" @remove="removeView"
    />

    <!-- Thanh công cụ: tìm nhanh + Lọc / Sắp xếp / Nhóm động (kiểu Lark) -->
    <ViewToolbar
      :cfg="config" :names="personName"
      :shown="filteredRows.length" :total="rows.length"
      :groupable="view === 'table'"
    />

    <!-- Timeline -->
    <div v-if="view === 'timeline'" class="gantt-wrap">
      <div class="gantt" :style="{ minWidth: LABEL_W + trackW + 'px' }">
        <div class="gantt-head">
          <div class="corner" :style="{ width: LABEL_W + 'px' }">Task · Nhân sự</div>
          <div
            v-for="i in days" :key="i"
            class="g-day"
            :class="{ weekend: dayMeta(i - 1).weekend, today: dayMeta(i - 1).today }"
            :style="{ width: DAY_W + 'px' }"
          >{{ i }}</div>
        </div>

        <div v-if="filteredRows.length === 0" class="empty">
          <template v-if="rows.length">Không có task nào khớp bộ lọc.</template>
          <template v-else>Chưa có task nào trong tháng này — bấm <b>+ Thêm task</b>.</template>
        </div>

        <div v-for="t in filteredRows" :key="t.id" class="g-row" :style="{ height: ROW_H + 'px' }">
          <div class="g-label" :style="{ width: LABEL_W + 'px' }" @click="editing = t" :title="t.title">
            <span class="t">
              <span v-if="t.priority && t.priority !== 'P3'" class="prio" :class="t.priority">{{ t.priority }}</span>
              <span v-if="isBug(t)" title="Bug">🐞</span>
              {{ t.title }}
            </span>
            <span class="a">
              <template v-if="t.assigneeId && personName[t.assigneeId]">
                <span class="avatar" :style="{ background: personMeta[t.assigneeId].color, color: '#fff' }">
                  {{ personMeta[t.assigneeId].initials }}
                </span>
                {{ personName[t.assigneeId] }}
              </template>
              <template v-else>Chưa gán</template>
              <span v-if="t.aiUsed" title="Task có dùng AI">· AI</span>
            </span>
          </div>
          <div class="g-track" :style="{ width: trackW + 'px' }">
            <div
              v-for="i in days" :key="i"
              class="g-cell"
              :class="{ weekend: dayMeta(i - 1).weekend }"
              :style="{ left: (i - 1) * DAY_W + 'px', width: DAY_W + 'px' }"
            ></div>
            <div v-if="todayLeft !== null" class="g-today-line" :style="{ left: todayLeft }"></div>
            <div
              v-if="barStyle(t)"
              class="g-bar"
              :class="{ ghost: !t.doneDate, blocked: t.status === 'Blocked', bug: isBug(t) }"
              :style="barStyle(t)"
              :title="`${t.title} (${t.status}${isBug(t) ? ' · Bug' : ''})`"
              @click="editing = t"
            >{{ isBug(t) ? '🐞 ' : '' }}{{ t.title }}</div>
            <span v-else class="g-nostart">(chưa có Start date)</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Kanban: dùng chung bộ lọc tháng + tab/thanh lọc với Timeline -->
    <KanbanBoard
      v-else-if="view === 'kanban'"
      :tasks="filteredRows"
      :meta="personMeta"
      :names="personName"
      @edit="t => (editing = t)"
      @changed="load"
      @error="e => (error = e)"
    />

    <!-- Danh sách: bảng nhận task đã lọc+sắp xếp; tự nhóm theo config.groups -->
    <TaskTable
      v-else
      :tasks="filteredRows"
      :meta="personMeta"
      :names="personName"
      :sorts="config.sorts"
      :groups="config.groups"
      @edit="t => (editing = t)"
      @sort="onHeaderSort"
    />

    <TaskModal
      v-if="editing !== undefined"
      :task="editing"
      :people="people"
      :tasks="tasks"
      :focus-activity-id="focusActId"
      @close="onSaved"
      @saved="onSaved"
    />
  </div>
</template>
