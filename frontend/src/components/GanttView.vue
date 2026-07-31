<script setup>
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import {
  ListTasksInMonth, ListTaskRefs, GetTask, ListPeople, ListSavedViews, CreateSavedView,
  UpdateSavedView, DeleteSavedView, ListTags,
} from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { monthStart, addMonths, daysInMonth, monthLabel, parseISODate, daysBetween, ymKey } from '../lib/date'
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
const ROW_H = 46

// Cột nhãn kéo rộng được: tiêu đề task hay dài hơn 280px nên bị cắt, mà đọc được
// tiêu đề là việc chính của cột này. Là ref (không phải const) vì mọi chỗ tính
// toạ độ theo nó — kể cả mũi tên phụ thuộc vẽ bằng SVG — phải chạy lại khi kéo.
const LABEL_W_DEFAULT = 280
const LABEL_W_MIN = 180
const LABEL_W_MAX = 720
const LABEL_W_KEY = 'gantt.labelW'

const LABEL_W = ref(clampLabelW(Number(localStorage.getItem(LABEL_W_KEY)) || LABEL_W_DEFAULT))
const resizing = ref(false)

function clampLabelW(v) {
  return Math.min(LABEL_W_MAX, Math.max(LABEL_W_MIN, Math.round(v) || LABEL_W_DEFAULT))
}

// Kéo bằng mousemove trên window, không trên chính cái handle: con trỏ hay chạy
// ra ngoài handle rộng 7px khi kéo nhanh, bắt trên handle sẽ mất giữa đường.
let dragX0 = 0
let dragW0 = 0

function onResizeMove(e) {
  LABEL_W.value = clampLabelW(dragW0 + e.clientX - dragX0)
}

function endResize() {
  resizing.value = false
  document.body.classList.remove('col-resizing')
  window.removeEventListener('mousemove', onResizeMove)
  window.removeEventListener('mouseup', endResize)
  localStorage.setItem(LABEL_W_KEY, String(LABEL_W.value))
}

function startResize(e) {
  // preventDefault chặn bôi đen chữ trong lúc kéo; handle nằm TRONG .g-label vốn
  // có @click mở modal nên phải chặn cả nổi bọt.
  e.preventDefault()
  e.stopPropagation()
  dragX0 = e.clientX
  dragW0 = LABEL_W.value
  resizing.value = true
  document.body.classList.add('col-resizing')
  window.addEventListener('mousemove', onResizeMove)
  window.addEventListener('mouseup', endResize)
}

// Nháy đúp lên vạch = về mặc định, quy ước quen của cột bảng kéo được.
function resetResize() {
  LABEL_W.value = LABEL_W_DEFAULT
  localStorage.setItem(LABEL_W_KEY, String(LABEL_W.value))
}

const view = ref('timeline') // 'timeline' | 'kanban' | 'table'
const month = ref(monthStart(new Date()))
const tasks = ref([]) // task của THÁNG đang xem
// taskRefs: {id, title} của mọi task workspace — picker phụ thuộc / task gốc sinh
// bug trong TaskModal phải chọn được cả task ngoài tháng đang xem.
const taskRefs = ref([])
const people = ref([])
const tags = ref([]) // [{id, name}] từ vựng tag của workspace
const error = ref('')
const editing = ref(undefined) // undefined = đóng, null = thêm mới, object = sửa

const personMeta = computed(() => buildPeopleMeta(people.value))
const personName = computed(() => {
  const map = {}
  for (const p of people.value) map[p.ID] = p.Name
  return map
})

// Backend chỉ trả task của THÁNG ĐANG XEM (không còn tải cả workspace). Bộ lọc
// `rows` bên dưới vẫn giữ nguyên: server trả tập bao, client cắt chính xác.
//
// reqSeq chống chồng response: bấm ◀ ▶ liên tiếp thì các lời gọi có thể về không
// đúng thứ tự, và nếu cứ gán thẳng thì tháng hiển thị sẽ lệch khỏi dữ liệu.
let reqSeq = 0
async function loadTasks() {
  const seq = ++reqSeq
  error.value = ''
  try {
    const list = await ListTasksInMonth(ymKey(month.value))
    if (seq === reqSeq) tasks.value = list
  } catch (e) {
    if (seq === reqSeq) error.value = String(e)
  }
}

async function load() {
  const seq = ++reqSeq
  error.value = ''
  try {
    // Người, saved view, tag và danh sách task rút gọn không phụ thuộc tháng —
    // chỉ nạp ở đây, không nạp lại mỗi lần đổi tháng.
    const [list, p, v, tg, refs] = await Promise.all([
      ListTasksInMonth(ymKey(month.value)), ListPeople(), ListSavedViews(), ListTags(),
      ListTaskRefs(),
    ])
    // Phần không theo tháng thì gán luôn; riêng danh sách task phải nhường cho lời
    // gọi mới hơn, nếu không một load() bắt đầu ở tháng cũ sẽ ghi đè tháng vừa chọn.
    ;[people.value, savedViews.value, tags.value, taskRefs.value] = [p, v, tg, refs]
    if (seq === reqSeq) tasks.value = list
  } catch (e) {
    if (seq === reqSeq) error.value = String(e)
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
async function maybeOpenRequested() {
  if (!props.openTaskId) return
  // Chốt cả hai prop trước await: App xóa pending ngay khi nhận task-opened.
  const id = props.openTaskId
  const actId = props.openActivityId || 0
  // Danh sách chỉ có task của tháng đang xem, nên task được nhắc có thể không nằm
  // trong đó — hỏi thẳng server thay vì báo sai là "không tìm thấy".
  let t = tasks.value.find(x => x.id === id)
  if (!t) {
    try {
      t = await GetTask(id)
    } catch {
      t = null
    }
  }
  if (t) {
    focusActId.value = actId
    editing.value = t
  } else {
    error.value = `Không tìm thấy task #${id} trong workspace hiện tại (có thể đã bị xóa)`
  }
  emitEvents('task-opened')
}

// Đồng bộ realtime khi client khác sửa dữ liệu: nạp lại danh sách task tại chỗ
// (giữ vị trí scroll — load() không đụng scroll). Với saved view: ƯU TIÊN REMOTE
// — nếu định nghĩa view đang mở bị sửa từ xa thì ÁP DỤNG ngay (đồng bộ lại config,
// không hiện cờ "đã đổi"); nếu view bị xóa từ xa thì về tab "Tất cả". Thay đổi
// dữ liệu thuần (không đụng view) thì GIỮ nguyên config để không phá filter đang xem.
async function onRemoteChange() {
  const prevFilters = activeView.value?.filters
  await load()
  if (activeViewId.value === 0) return // tab "Tất cả": không có config đã lưu
  if (!activeView.value) {
    selectView(0) // view đang mở đã bị xóa từ xa
    return
  }
  if (activeView.value.filters !== prevFilters) {
    config.value = parseConfig(activeView.value.filters) // định nghĩa view đổi từ xa → áp dụng
  }
}

// Dùng hàm hủy do EventsOn trả về để chỉ gỡ listener này, tránh xóa nhầm
// listener của TaskModal đang lồng bên trong (cùng nghe "tasks:changed").
let stopLiveSync = null
onMounted(async () => {
  await load()
  maybeOpenRequested()
  await nextTick()
  measureHead()
  stopLiveSync = EventsOn('tasks:changed', onRemoteChange)
})
onUnmounted(() => {
  stopLiveSync && stopLiveSync()
  // Component có thể bị huỷ giữa lúc đang kéo (đổi tab/tháng) — mouseup lúc đó
  // không bao giờ tới, listener trên window sẽ sống mãi nếu không dọn ở đây.
  if (resizing.value) endResize()
})
watch(() => props.openTaskId, maybeOpenRequested)
// Header chỉ tồn tại khi ở Timeline — đo lại khi chuyển về view này.
watch(view, v => { if (v === 'timeline') nextTick(measureHead) })

function shift(n) {
  month.value = n === 0 ? monthStart(new Date()) : addMonths(month.value, n)
}
// Đổi tháng = nạp lại task của tháng đó. Đặt ở watch chứ không nhồi vào shift()
// để mọi đường đổi tháng đều nạp, kể cả sau này thêm cách chọn tháng khác.
watch(month, loadTasks)

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

// barBox: vị trí bar (px, số) trong track — dùng chung cho style bar và toạ độ
// mũi tên phụ thuộc. null khi task chưa có Start date (không vẽ được bar).
function barBox(t) {
  const s = parseISODate(t.startDate)
  if (!s) return null
  const from = Math.max(daysBetween(mStart.value, s), 0)
  const to = Math.min(daysBetween(mStart.value, barEnd(t)), days.value)
  const width = Math.max((to - from) * DAY_W, DAY_W / 2)
  return { left: from * DAY_W, width }
}

function barStyle(t) {
  const b = barBox(t)
  if (!b) return null
  return { left: b.left + 'px', width: b.width + 'px', background: colorOf(t) }
}

// ---- Mũi tên phụ thuộc (finish-to-start): predecessor → task ----
// Đo chiều cao header một lần (CSS quyết định, không cố định) để canh trục Y.
const headEl = ref(null)
const headH = ref(28)
function measureHead() {
  if (headEl.value) headH.value = headEl.value.offsetHeight
}

// SVG phủ toàn bộ .gantt: cao = header + số hàng × ROW_H.
const overlayH = computed(() => headH.value + filteredRows.value.length * ROW_H)

// Mỗi cạnh nối cạnh phải bar của predecessor tới cạnh trái bar của task này;
// chỉ vẽ khi cả hai bar cùng hiển thị (có Start date & lọt bộ lọc tháng).
const depLines = computed(() => {
  const idx = new Map()
  filteredRows.value.forEach((t, i) => idx.set(t.id, i))
  const lines = []
  filteredRows.value.forEach((t, i) => {
    const succ = barBox(t)
    if (!succ) return
    for (const pid of t.dependsOn || []) {
      if (!idx.has(pid)) continue
      const pi = idx.get(pid)
      const pred = barBox(filteredRows.value[pi])
      if (!pred) continue
      lines.push({
        key: `${pid}-${t.id}`,
        x1: LABEL_W.value + pred.left + pred.width,
        y1: headH.value + pi * ROW_H + ROW_H / 2,
        x2: LABEL_W.value + succ.left,
        y2: headH.value + i * ROW_H + ROW_H / 2,
      })
    }
  })
  return lines
})

// Đường cong finish-to-start: bezier ngang mềm, thò ra một đoạn để tách khỏi bar.
function depPath(l) {
  const off = Math.max(14, Math.min(28, Math.abs(l.x2 - l.x1) / 2))
  return `M ${l.x1} ${l.y1} C ${l.x1 + off} ${l.y1}, ${l.x2 - off} ${l.y2}, ${l.x2} ${l.y2}`
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
      :cfg="config" :names="personName" :tags="tags"
      :shown="filteredRows.length" :total="rows.length"
      :groupable="view === 'table'"
    />

    <!-- Timeline -->
    <div v-if="view === 'timeline'" class="gantt-wrap">
      <div class="gantt" :style="{ minWidth: LABEL_W + trackW + 'px' }">
        <div class="gantt-head" ref="headEl">
          <div class="corner" :style="{ width: LABEL_W + 'px' }">
            Task · Nhân sự
            <!-- Vạch kéo lặp lại ở MỌI dòng, không chỉ ở header: người dùng đưa
                 chuột vào mép phải của cột ở bất kỳ đâu cũng phải kéo được. -->
            <span
              class="g-resize" :class="{ on: resizing }"
              title="Kéo để đổi độ rộng cột · nháy đúp để về mặc định"
              @mousedown="startResize" @click.stop @dblclick.stop="resetResize"
            ></span>
          </div>
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
              <span v-if="t.size" class="size-badge" :class="'sz-' + t.size" :title="'Size ' + t.size">{{ t.size }}</span>
              <span v-if="isBug(t)" title="Bug">🐞</span>
              {{ t.title }}
            </span>
            <span class="a">
              <template v-if="t.assigneeId && personName[t.assigneeId]">
                <span class="avatar" :style="{ background: personMeta[t.assigneeId].color, color: '#fff' }">
                  {{ personMeta[t.assigneeId].initials }}
                </span>
                <span class="g-who">{{ personName[t.assigneeId] }}</span>
              </template>
              <template v-else>Chưa gán</template>
              <span v-if="t.aiUsed" title="Task có dùng AI">· AI</span>
              <span class="g-id" :title="'ID task — gõ #' + t.id + ' vào ô tìm kiếm để lọc ra task này'">
                · #{{ t.id }}
              </span>
            </span>
            <span
              class="g-resize" :class="{ on: resizing }"
              title="Kéo để đổi độ rộng cột · nháy đúp để về mặc định"
              @mousedown="startResize" @click.stop @dblclick.stop="resetResize"
            ></span>
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

        <!-- Mũi tên phụ thuộc: phủ toàn track, không chắn click vào bar/label -->
        <svg
          v-if="depLines.length"
          class="g-deps"
          :width="LABEL_W + trackW" :height="overlayH"
          :style="{ width: LABEL_W + trackW + 'px', height: overlayH + 'px' }"
        >
          <defs>
            <marker
              id="dep-arrow" markerWidth="7" markerHeight="7"
              refX="5.5" refY="3" orient="auto" markerUnits="userSpaceOnUse"
            >
              <path d="M0,0 L6,3 L0,6 Z" fill="var(--accent, #6d4aff)" />
            </marker>
          </defs>
          <path
            v-for="l in depLines" :key="l.key"
            :d="depPath(l)"
            class="dep-edge"
            marker-end="url(#dep-arrow)"
          />
        </svg>
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
      :tasks="taskRefs"
      :tags="tags"
      :focus-activity-id="focusActId"
      @close="onSaved"
      @saved="onSaved"
    />
  </div>
</template>
