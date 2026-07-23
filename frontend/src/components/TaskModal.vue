<script setup>
import { ref, reactive, computed, onMounted, nextTick } from 'vue'
import {
  SaveTask, DeleteTask, ListTodos, AddTodo, ToggleTodo, DeleteTodo,
  AddComment, ListActivities, ListStatusChanges,
  SuggestEstimate, AIStatus,
} from '../../wailsjs/go/main/App'
import { todayISO } from '../lib/date'
import { buildPeopleMeta, UNASSIGNED_COLOR } from '../lib/people'
import { TYPES, TYPE_PLAN, TYPE_BUG } from '../lib/taskTypes'

const props = defineProps({
  task: { type: Object, default: null }, // null = thêm mới
  people: { type: Array, default: () => [] },
  tasks: { type: Array, default: () => [] }, // toàn bộ task workspace — picker "task gốc" của bug
  // Bình luận cần scroll tới + làm nổi bật khi mở (từ thông báo mention/reply).
  focusActivityId: { type: Number, default: 0 },
})
const emit = defineEmits(['close', 'saved'])

const editing = computed(() => !!props.task)
const error = ref('')
const confirmDelete = ref(false)

const SIZES = ['S', 'M', 'L', 'XL']
const STATUSES = ['Todo', 'In Progress', 'Blocked', 'Done']
const PRIORITIES = [
  { value: 'P1', label: 'P1 · Khẩn cấp' },
  { value: 'P2', label: 'P2 · Cao' },
  { value: 'P3', label: 'P3 · Trung bình' },
  { value: 'P4', label: 'P4 · Thấp' },
]
const SEVERITIES = ['Critical', 'Major', 'Minor']
const RESOLUTIONS = ['Fixed', "Won't Fix", 'Cannot Reproduce', 'Duplicate']

const form = reactive(props.task
  ? { ...props.task }
  : {
      id: 0,
      title: '',
      description: '',
      type: TYPE_PLAN,
      size: 'M',
      status: 'Todo',
      priority: 'P3',
      assigneeId: 0,
      estimateCustomerDays: 0,
      estimateAiDays: 0,
      actualDays: 0,
      aiUsed: false,
      blocker: '',
      blockedDays: 0,
      createdDate: todayISO(),
      startDate: '',
      dueDate: '',
      doneDate: '',
      reporterId: 0,
      severity: '',
      resolution: '',
      relatedTaskId: 0,
      dependsOn: [],
    })

// Tách bản sao mảng phụ thuộc để sửa trong modal không đụng vào prop gốc.
form.dependsOn = Array.isArray(form.dependsOn) ? [...form.dependsOn] : []

// ---- Gợi ý estimate bằng AI ----
const ai = reactive({ enabled: false, provider: '', model: '' })
const aiBusy = ref(false)
const aiError = ref('')
const aiSuggestion = ref(null) // Suggestion cuối cùng để hiển thị lý do/độ tin cậy
// Checklist AI gợi ý, CHỜ tạo khi lưu task mới (task đang sửa thì thêm ngay).
const aiChecklist = ref([])

onMounted(async () => {
  try {
    const s = await AIStatus()
    ai.enabled = s.enabled
    ai.provider = s.provider
    ai.model = s.model
  } catch { /* không cấu hình AI → giữ nút ẩn */ }
})

const CONFIDENCE_LABEL = { high: 'cao', medium: 'trung bình', low: 'thấp' }

async function suggestEstimate() {
  if (aiBusy.value) return
  aiError.value = ''
  if (!form.title.trim()) {
    aiError.value = 'Nhập tiêu đề task trước khi xin gợi ý'
    return
  }
  aiBusy.value = true
  try {
    const s = await SuggestEstimate({
      ...form,
      type: Number(form.type) || TYPE_PLAN,
    })
    aiSuggestion.value = s
    if (s.description && s.description.trim()) form.description = s.description
    if (s.estimateAiDays > 0) form.estimateAiDays = s.estimateAiDays
    if (s.estimateCustomerDays > 0) form.estimateCustomerDays = s.estimateCustomerDays
    if (SIZES.includes(s.size)) form.size = s.size

    const items = (s.checklist || []).filter(x => x && x.trim())
    if (form.id) {
      // Task đã tồn tại → thêm todo ngay rồi nạp lại danh sách checklist.
      for (const title of items) await AddTodo(form.id, title)
      if (items.length) await loadDetail()
      aiChecklist.value = []
    } else {
      // Task mới chưa có ID → giữ lại, tạo khi lưu (gửi qua initialTodos).
      aiChecklist.value = items
    }
  } catch (e) {
    aiError.value = String(e)
  } finally {
    aiBusy.value = false
  }
}

// Bỏ 1 mục khỏi checklist AI gợi ý (chỉ với task mới, trước khi lưu).
function removeAiTodo(i) {
  aiChecklist.value.splice(i, 1)
}

// ---- Bug tracking ----
const isBug = computed(() => form.type === TYPE_BUG)
// Task khác (không phải chính bug này) để chọn làm task gốc.
const relatable = computed(() => props.tasks.filter(t => t.id !== form.id))

// ---- Phụ thuộc (finish-to-start): task phải xong trước task này ----
// Ứng viên = task khác chưa được chọn (chặn vòng lặp do backend lo).
const depCandidates = computed(() =>
  props.tasks.filter(t => t.id !== form.id && !form.dependsOn.includes(t.id))
)
function depTitle(id) {
  const t = props.tasks.find(x => x.id === id)
  return t ? `#${t.id} · ${t.title}` : `#${id}`
}
function addDep(e) {
  const id = Number(e.target.value)
  if (id && !form.dependsOn.includes(id)) form.dependsOn.push(id)
  e.target.value = ''
}
function removeDep(id) {
  form.dependsOn = form.dependsOn.filter(x => x !== id)
}

const BUG_TEMPLATE = `## Các bước tái hiện
1.

## Kết quả mong đợi


## Kết quả thực tế


## Môi trường
`

function insertBugTemplate() {
  if (form.description.includes('## Các bước tái hiện')) return
  form.description = form.description.trim()
    ? form.description.trimEnd() + '\n\n' + BUG_TEMPLATE
    : BUG_TEMPLATE
}

// ---- Checklist + Bình luận & hoạt động + Lịch sử trạng thái ----
const todos = ref([])
const acts = ref([])
const statusHist = ref([])
const newTodo = ref('')
const newComment = ref('')

const peopleMeta = computed(() => buildPeopleMeta(props.people))
const todoDone = computed(() => todos.value.filter(t => t.done).length)
const todoPct = computed(() => (todos.value.length ? (todoDone.value / todos.value.length) * 100 : 0))

async function loadDetail() {
  if (!editing.value) return
  try {
    ;[todos.value, acts.value, statusHist.value] = await Promise.all([
      ListTodos(form.id), ListActivities(form.id), ListStatusChanges(form.id),
    ])
  } catch (e) {
    error.value = String(e)
  }
}

// ---- Nhảy tới bình luận từ thông báo: scroll + flash nền 5s (kiểu FB) ----

const highlightId = ref(0)
let highlightTimer = null

function focusComment(id) {
  if (!id || !acts.value.some(a => a.id === id)) return
  highlightId.value = id
  nextTick(() => {
    document.querySelector(`[data-act-id="${id}"]`)
      ?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  })
  clearTimeout(highlightTimer)
  highlightTimer = setTimeout(() => (highlightId.value = 0), 5000)
}

// Timeline trạng thái: mỗi bước kèm thời gian đã nằm ở trạng thái đó
// (đến bước kế tiếp, hoặc đến hiện tại với bước cuối khi chưa Done).
const statusSteps = computed(() =>
  statusHist.value.map((c, i) => {
    const from = new Date(c.createdAt)
    const to = i + 1 < statusHist.value.length ? new Date(statusHist.value[i + 1].createdAt) : new Date()
    const open = i + 1 >= statusHist.value.length // bước hiện tại
    const days = (to - from) / 86400000
    return {
      ...c,
      // Done là trạng thái kết thúc — không đếm "đang ở Done bao lâu".
      durText: open && c.toStatus === 'Done' ? '' : `${open ? 'đang ' : ''}ở ${c.toStatus} ${days.toFixed(1)} ngày`,
    }
  }).reverse() // mới nhất trước, khớp feed hoạt động
)

const statusClass = s => (s || '').replace(/\s+/g, '')
onMounted(async () => {
  await loadDetail()
  if (props.focusActivityId) focusComment(props.focusActivityId)
})

async function run(fn) {
  error.value = ''
  try {
    await fn()
    await loadDetail()
  } catch (e) {
    error.value = String(e)
  }
}

const addTodoItem = () => {
  if (!newTodo.value.trim()) return
  run(async () => { await AddTodo(form.id, newTodo.value); newTodo.value = '' })
}
const toggleTodoItem = item => run(() => ToggleTodo(item.id, !item.done))
const delTodoItem = item => run(() => DeleteTodo(item.id))
const sendComment = () => {
  if (!newComment.value.trim()) return
  run(async () => {
    await AddComment(form.id, newComment.value, replyTo.value?.id || 0)
    newComment.value = ''
    replyTo.value = null
  })
}

// ---- Reply bình luận (thread 1 cấp kiểu Facebook) ----

const replyTo = ref(null) // { id, actorName } — comment đang được trả lời

function startReply(a) {
  replyTo.value = { id: a.id, actorName: a.actorName }
  if (!newComment.value.trim()) newComment.value = `@${a.actorName} `
  commentInput.value?.focus()
}

// Gom feed thành thread: hoạt động thường + comment gốc theo thứ tự mới nhất
// trước; reply xếp dưới comment gốc của nó, cũ trước (đọc như hội thoại).
const feed = computed(() => {
  const roots = []
  const byId = {}
  for (const a of acts.value) {
    if (!a.parentId) {
      byId[a.id] = { ...a, replies: [] }
      roots.push(byId[a.id])
    }
  }
  for (const a of acts.value) {
    if (a.parentId && byId[a.parentId]) byId[a.parentId].replies.push(a)
  }
  for (const r of roots) r.replies.reverse() // acts mới→cũ, reply đọc cũ→mới
  return roots
})

// ---- Mention @username: autosuggest khi gõ + tô nền khi hiển thị ----

const commentInput = ref(null)
// Trạng thái gợi ý: đang mở?, chuỗi sau @, vị trí @ trong input, mục đang chọn.
const mention = reactive({ open: false, query: '', start: -1, idx: 0 })

const mentionCandidates = computed(() => {
  if (!mention.open) return []
  const q = mention.query.toLowerCase()
  return props.people.filter(p => p.Name.toLowerCase().startsWith(q)).slice(0, 6)
})

// Ký tự thuộc "từ" của username (chữ/số/gạch dưới, có dấu tiếng Việt).
const WORD = /[\p{L}\p{N}_]/u

function onCommentInput(e) {
  const el = e.target
  const upto = el.value.slice(0, el.selectionStart)
  const m = upto.match(/@([\p{L}\p{N}_.-]*)$/u)
  // Chỉ gợi ý khi @ đứng đầu hoặc sau ký tự không thuộc từ (tránh email a@b).
  const at = m ? upto.length - m[1].length - 1 : -1
  if (m && (at === 0 || !WORD.test(upto[at - 1]))) {
    mention.open = true
    mention.query = m[1]
    mention.start = at
    mention.idx = 0
  } else {
    mention.open = false
  }
}

function pickMention(p) {
  const before = newComment.value.slice(0, mention.start)
  const after = newComment.value.slice(mention.start + 1 + mention.query.length)
  newComment.value = `${before}@${p.Name} ${after.trimStart()}`
  mention.open = false
  commentInput.value?.focus()
}

// Đóng dropdown khi rời ô nhập; trễ một nhịp để mousedown trên gợi ý kịp chạy.
function onCommentBlur() {
  setTimeout(() => (mention.open = false), 150)
}

function onCommentKeydown(e) {
  const list = mentionCandidates.value
  if (mention.open && list.length) {
    if (e.key === 'ArrowDown') { e.preventDefault(); mention.idx = (mention.idx + 1) % list.length; return }
    if (e.key === 'ArrowUp') { e.preventDefault(); mention.idx = (mention.idx - 1 + list.length) % list.length; return }
    if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); pickMention(list[mention.idx]); return }
    if (e.key === 'Escape') { mention.open = false; return }
  }
  if (e.key === 'Enter') sendComment()
}

// Tách comment thành các đoạn thường / mention (khớp đúng username thành
// viên, dài trước ngắn sau để @sonnn không bị khớp nhầm thành @son).
const memberNames = computed(() =>
  props.people.map(p => p.Name).filter(Boolean).sort((a, b) => b.length - a.length)
)

function commentParts(text) {
  const parts = []
  let i = 0
  while (i < text.length) {
    const at = text.indexOf('@', i)
    if (at < 0) break
    let hit = null
    if (at === 0 || !WORD.test(text[at - 1])) {
      for (const n of memberNames.value) {
        if (!text.startsWith(n, at + 1)) continue
        const next = text[at + 1 + n.length]
        if (!next || !WORD.test(next)) { hit = n; break }
      }
    }
    if (hit) {
      if (at > i) parts.push({ t: text.slice(i, at) })
      parts.push({ t: '@' + hit, m: true })
      i = at + 1 + hit.length
    } else {
      parts.push({ t: text.slice(i, at + 1) })
      i = at + 1
    }
  }
  if (i < text.length) parts.push({ t: text.slice(i) })
  return parts
}

const KIND_LABEL = {
  create: 'đã tạo task',
  update: 'đã cập nhật',
  todo: '· checklist',
  comment: 'đã bình luận',
}

function fmtDT(iso) {
  const d = new Date(iso)
  const p = n => String(n).padStart(2, '0')
  return `${p(d.getDate())}/${p(d.getMonth() + 1)} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function actorMeta(name) {
  const p = props.people.find(p => p.Name === name)
  if (p && peopleMeta.value[p.ID]) return peopleMeta.value[p.ID]
  return { color: UNASSIGNED_COLOR, initials: (name || '?').slice(0, 2).toUpperCase() }
}

// ---- Lưu / Xóa ----
async function save() {
  error.value = ''
  try {
    await SaveTask({
      ...form,
      assigneeId: Number(form.assigneeId) || 0,
      estimateCustomerDays: Number(form.estimateCustomerDays) || 0,
      estimateAiDays: Number(form.estimateAiDays) || 0,
      actualDays: Number(form.actualDays) || 0,
      blockedDays: Number(form.blockedDays) || 0,
      reporterId: Number(form.reporterId) || 0,
      relatedTaskId: Number(form.relatedTaskId) || 0,
      dependsOn: (form.dependsOn || []).map(Number),
      // Checklist AI gợi ý chỉ áp khi tạo mới; task đang sửa đã thêm todo trực tiếp.
      initialTodos: form.id ? [] : aiChecklist.value,
    })
    emit('saved')
  } catch (e) {
    error.value = String(e)
  }
}

async function doDelete() {
  if (!confirmDelete.value) {
    confirmDelete.value = true
    return
  }
  error.value = ''
  try {
    await DeleteTask(form.id)
    emit('saved')
  } catch (e) {
    error.value = String(e)
  }
}
</script>

<template>
  <div class="modal-overlay" @mousedown.self="emit('close')">
    <div class="modal" :class="{ wide: editing }">
      <div class="modal-head">
        <h3>{{ editing ? 'Sửa task' : 'Thêm task' }}</h3>
        <div class="actions">
          <button
            v-if="editing"
            class="btn sm"
            :class="confirmDelete ? 'danger' : 'ghost-danger'"
            @click="doDelete"
            @mouseleave="confirmDelete = false"
          >
            {{ confirmDelete ? 'Bấm lần nữa để xóa' : 'Xóa task' }}
          </button>
          <button class="btn sm icon" @click="emit('close')">✕</button>
        </div>
      </div>

      <div class="modal-body">
        <div v-if="error" class="err">{{ error }}</div>

        <div :class="{ 'detail-grid': editing }">
          <!-- Cột trái: form -->
          <div class="form-grid">
            <div class="field full">
              <label>Tiêu đề *</label>
              <input v-model="form.title" placeholder="Tên task" autofocus />
            </div>
            <div class="field full">
              <label>
                Mô tả
                <button
                  v-if="isBug" class="btn sm" style="margin-left: 8px"
                  title="Chèn khung Các bước tái hiện / Kết quả mong đợi / Thực tế / Môi trường"
                  @click="insertBugTemplate"
                >🐞 Chèn template bug</button>
              </label>
              <textarea v-model="form.description" placeholder="Chi tiết công việc…"></textarea>
            </div>

            <div class="field">
              <label>Nhân sự phụ trách</label>
              <select v-model.number="form.assigneeId">
                <option :value="0">(chưa gán)</option>
                <option v-for="p in people" :key="p.ID" :value="p.ID">{{ p.Name }}</option>
              </select>
              <span v-if="people.length === 0" class="hint">Thêm nhân sự trong tab Cài đặt</span>
            </div>
            <div class="field">
              <label>Trạng thái</label>
              <select v-model="form.status">
                <option v-for="s in STATUSES" :key="s">{{ s }}</option>
              </select>
            </div>

            <div class="field">
              <label>Loại task</label>
              <select v-model.number="form.type">
                <option v-for="t in TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
              </select>
            </div>
            <div class="field">
              <label>Size</label>
              <select v-model="form.size">
                <option v-for="s in SIZES" :key="s">{{ s }}</option>
              </select>
            </div>

            <div class="field">
              <label>Ưu tiên</label>
              <select v-model="form.priority">
                <option v-for="p in PRIORITIES" :key="p.value" :value="p.value">{{ p.label }}</option>
              </select>
            </div>
            <div class="field">
              <label>Hạn chót (deadline)</label>
              <div class="date-wrap">
                <input v-model="form.dueDate" type="date" />
                <button v-if="form.dueDate" class="btn icon clear" title="Xóa hạn chót" @click="form.dueDate = ''">✕</button>
              </div>
            </div>

            <!-- Khu vực bug: chỉ hiện khi Loại = Phát sinh (bug) -->
            <template v-if="isBug">
              <div class="field full bug-sec-head">🐞 Thông tin bug</div>
              <div class="field">
                <label>Người báo (phát hiện bug)</label>
                <select v-model.number="form.reporterId">
                  <option :value="0">(mặc định: bạn)</option>
                  <option v-for="p in people" :key="p.ID" :value="p.ID">{{ p.Name }}</option>
                </select>
              </div>
              <div class="field">
                <label>Mức độ nghiêm trọng</label>
                <select v-model="form.severity">
                  <option value="">(chưa phân loại)</option>
                  <option v-for="s in SEVERITIES" :key="s" :value="s">{{ s }}</option>
                </select>
              </div>
              <div class="field">
                <label>Task gốc sinh ra bug</label>
                <select v-model.number="form.relatedTaskId">
                  <option :value="0">(không liên kết)</option>
                  <option v-for="t in relatable" :key="t.id" :value="t.id">#{{ t.id }} · {{ t.title }}</option>
                </select>
              </div>
              <div class="field">
                <label>Cách đóng bug</label>
                <select v-model="form.resolution">
                  <option value="">(chưa kết luận)</option>
                  <option v-for="r in RESOLUTIONS" :key="r" :value="r">{{ r }}</option>
                </select>
                <span v-if="form.status === 'Done' && !form.resolution" class="hint">Bug Done nên ghi rõ cách đóng</span>
              </div>
            </template>

            <div v-if="ai.enabled" class="field full ai-suggest">
              <div class="ai-bar">
                <button type="button" class="btn ai" :disabled="aiBusy" @click="suggestEstimate">
                  <span v-if="aiBusy">⏳ Đang phân tích…</span>
                  <span v-else>✨ Phân tích bằng AI (mô tả chi tiết + estimate)</span>
                </button>
                <span class="hint ai-model">{{ ai.provider }} · {{ ai.model }}</span>
              </div>
              <div v-if="aiError" class="hint ai-err">{{ aiError }}</div>
              <div v-if="aiSuggestion" class="ai-result">
                <div class="ai-line">
                  Đề xuất: <b>{{ aiSuggestion.estimateAiDays }}</b> ngày (AI) ·
                  khách <b>{{ aiSuggestion.estimateCustomerDays }}</b> ·
                  size <b>{{ aiSuggestion.size }}</b>
                  <span v-if="aiSuggestion.confidence">— độ tin cậy {{ CONFIDENCE_LABEL[aiSuggestion.confidence] || aiSuggestion.confidence }}</span>
                </div>
                <div v-if="aiSuggestion.description" class="ai-line ai-desc-note">✓ Đã viết mô tả chi tiết vào ô Mô tả phía trên (kiểm tra & sửa nếu cần).</div>
                <div v-if="form.id && aiChecklist.length === 0 && (aiSuggestion.checklist || []).length" class="ai-line ai-desc-note">
                  ✓ Đã thêm {{ (aiSuggestion.checklist || []).length }} việc vào checklist.
                </div>
                <div v-if="!form.id && aiChecklist.length" class="ai-checklist">
                  <div class="ai-ck-head">Checklist sẽ tạo khi lưu ({{ aiChecklist.length }}):</div>
                  <div v-for="(item, i) in aiChecklist" :key="i" class="ai-ck-row">
                    <span>☐ {{ item }}</span>
                    <button type="button" class="ai-ck-del" title="Bỏ mục này" @click="removeAiTodo(i)">✕</button>
                  </div>
                </div>
                <div v-if="aiSuggestion.rationale" class="ai-why">{{ aiSuggestion.rationale }}</div>
              </div>
            </div>

            <div class="field">
              <label>Estimate báo khách (ngày)</label>
              <input v-model="form.estimateCustomerDays" type="number" min="0" step="0.5" />
            </div>
            <div class="field">
              <label>Estimate làm bằng AI (ngày)</label>
              <input v-model="form.estimateAiDays" type="number" min="0" step="0.5" />
            </div>

            <div class="field">
              <label>Effort thực tế (ngày công)</label>
              <input v-model="form.actualDays" type="number" min="0" step="0.25" />
              <span v-if="form.status === 'Done' && !Number(form.actualDays)" class="hint">
                Nhập effort thực tế khi Done để đo độ chính xác estimate
              </span>
            </div>
            <div class="field">
              <label>Ngày tạo task</label>
              <input v-model="form.createdDate" type="date" />
            </div>

            <div class="field">
              <label class="checkbox" style="margin-top: 4px">
                <input v-model="form.aiUsed" type="checkbox" /> Task có dùng AI
              </label>
            </div>
            <div class="field"></div>

            <div class="field">
              <label>Start date (bắt đầu code)</label>
              <div class="date-wrap">
                <input v-model="form.startDate" type="date" />
                <button v-if="form.startDate" class="btn icon clear" title="Xóa ngày" @click="form.startDate = ''">✕</button>
              </div>
            </div>
            <div class="field">
              <label>Done date (merge / deploy)</label>
              <div class="date-wrap">
                <input v-model="form.doneDate" type="date" />
                <button v-if="form.doneDate" class="btn icon clear" title="Xóa ngày" @click="form.doneDate = ''">✕</button>
              </div>
            </div>

            <div class="field">
              <label>Blocker</label>
              <input v-model="form.blocker" placeholder="Mô tả blocker nếu có" />
            </div>
            <div class="field">
              <label>Thời gian blocked (ngày)</label>
              <input v-model="form.blockedDays" type="number" min="0" step="0.5" />
            </div>

            <div class="field full">
              <label>Phụ thuộc — task phải xong trước (finish-to-start)</label>
              <div v-if="form.dependsOn.length" class="dep-chips">
                <span v-for="id in form.dependsOn" :key="id" class="dep-chip">
                  {{ depTitle(id) }}
                  <button type="button" class="dep-del" title="Bỏ phụ thuộc" @click="removeDep(id)">✕</button>
                </span>
              </div>
              <select :disabled="!depCandidates.length" @change="addDep">
                <option value="">+ Thêm task phải xong trước…</option>
                <option v-for="t in depCandidates" :key="t.id" :value="t.id">#{{ t.id }} · {{ t.title }}</option>
              </select>
              <span class="hint">Timeline sẽ vẽ mũi tên từ mỗi task phải xong trước → task này.</span>
            </div>
          </div>

          <!-- Cột phải: checklist + bình luận & hoạt động (chỉ khi sửa) -->
          <div v-if="editing" class="detail-side">
            <div class="side-sec">
              <div class="side-title">
                Checklist
                <span v-if="todos.length">{{ todoDone }}/{{ todos.length }}</span>
              </div>
              <div v-if="todos.length" class="pi-bar" style="height: 7px; margin-bottom: 10px">
                <div :style="{ width: todoPct + '%', background: 'var(--green)' }"></div>
              </div>
              <div v-for="item in todos" :key="item.id" class="todo-row">
                <label class="checkbox">
                  <input type="checkbox" :checked="item.done" @change="toggleTodoItem(item)" />
                  <span :class="{ 'todo-done': item.done }">{{ item.title }}</span>
                </label>
                <button class="btn sm icon clear" title="Xóa" @click="delTodoItem(item)">✕</button>
              </div>
              <div class="todo-add">
                <input v-model="newTodo" placeholder="Thêm việc cần làm…" @keyup.enter="addTodoItem" />
                <button class="btn sm" @click="addTodoItem">Thêm</button>
              </div>
            </div>

            <div class="side-sec" v-if="statusSteps.length">
              <div class="side-title">Lịch sử trạng thái</div>
              <div class="st-timeline">
                <div v-for="c in statusSteps" :key="c.id" class="st-step">
                  <template v-if="c.fromStatus">
                    <span class="st-chip" :class="statusClass(c.fromStatus)">{{ c.fromStatus }}</span>
                    <span>→</span>
                  </template>
                  <span class="st-chip" :class="statusClass(c.toStatus)">{{ c.toStatus }}</span>
                  <span class="st-meta">{{ c.actorName }} · {{ fmtDT(c.createdAt) }}</span>
                  <span v-if="c.durText" class="st-dur">{{ c.durText }}</span>
                </div>
              </div>
            </div>

            <div class="side-sec">
              <div class="side-title">Bình luận &amp; hoạt động</div>
              <div v-if="replyTo" class="reply-chip">
                ↩ Đang trả lời <b>{{ replyTo.actorName }}</b>
                <button class="btn sm icon clear" title="Hủy trả lời" @click="replyTo = null">✕</button>
              </div>
              <div class="todo-add mention-wrap" style="margin-top: 0">
                <input
                  ref="commentInput"
                  v-model="newComment"
                  placeholder="Viết bình luận… (@tên để nhắc thành viên)"
                  @input="onCommentInput"
                  @keydown="onCommentKeydown"
                  @blur="onCommentBlur"
                />
                <button class="btn sm primary" @click="sendComment">Gửi</button>
                <div v-if="mention.open && mentionCandidates.length" class="mention-pop">
                  <button
                    v-for="(p, i) in mentionCandidates" :key="p.ID"
                    class="mention-item" :class="{ active: i === mention.idx }"
                    @mousedown.prevent="pickMention(p)"
                    @mousemove="mention.idx = i"
                  >
                    <span class="avatar" :style="{ background: peopleMeta[p.ID]?.color, color: '#fff' }">
                      {{ peopleMeta[p.ID]?.initials }}
                    </span>
                    @{{ p.Name }}
                  </button>
                </div>
              </div>
              <div class="act-feed">
                <div
                  v-for="a in feed" :key="a.id"
                  class="act" :class="{ 'act-flash': a.id === highlightId }"
                  :data-act-id="a.id"
                >
                  <span class="avatar" :style="{ background: actorMeta(a.actorName).color, color: '#fff' }">
                    {{ actorMeta(a.actorName).initials }}
                  </span>
                  <div class="act-main">
                    <div class="act-head">
                      <b>{{ a.actorName }}</b> {{ KIND_LABEL[a.kind] || '' }} · {{ fmtDT(a.createdAt) }}
                    </div>
                    <div v-if="a.kind === 'comment'" class="act-comment"><template
                      v-for="(p, i) in commentParts(a.content)" :key="i"
                    ><span v-if="p.m" class="mention">{{ p.t }}</span><template v-else>{{ p.t }}</template></template></div>
                    <div v-else-if="a.kind !== 'create'" class="act-change">{{ a.content }}</div>
                    <button v-if="a.kind === 'comment'" class="act-reply-btn" @click="startReply(a)">↩ Trả lời</button>

                    <!-- Reply của comment này, thụt vào, cũ trước -->
                    <div v-if="a.replies.length" class="act-replies">
                      <div
                        v-for="r in a.replies" :key="r.id"
                        class="act" :class="{ 'act-flash': r.id === highlightId }"
                        :data-act-id="r.id"
                      >
                        <span class="avatar" :style="{ background: actorMeta(r.actorName).color, color: '#fff' }">
                          {{ actorMeta(r.actorName).initials }}
                        </span>
                        <div class="act-main">
                          <div class="act-head"><b>{{ r.actorName }}</b> đã trả lời · {{ fmtDT(r.createdAt) }}</div>
                          <div class="act-comment"><template
                            v-for="(p, i) in commentParts(r.content)" :key="i"
                          ><span v-if="p.m" class="mention">{{ p.t }}</span><template v-else>{{ p.t }}</template></template></div>
                          <button class="act-reply-btn" @click="startReply(r)">↩ Trả lời</button>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
                <div v-if="!acts.length" class="hint">Chưa có hoạt động nào.</div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div class="modal-foot">
        <button class="btn" @click="emit('close')">Hủy</button>
        <button class="btn primary" @click="save">Lưu</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Grid item trong .form-grid: min-width:0 để nội dung dài không phá cột. */
.ai-suggest { min-width: 0; }
.ai-bar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.btn.ai {
  background: linear-gradient(90deg, #6d4aff, #9a6bff);
  color: #fff;
  border: none;
  white-space: nowrap;
}
.btn.ai:disabled { opacity: 0.6; cursor: default; }
.ai-model { opacity: 0.7; }
.ai-err { color: #ff6b6b; margin-top: 6px; }
.ai-result {
  margin-top: 8px;
  background: rgba(109, 74, 255, 0.1);
  border: 1px solid rgba(109, 74, 255, 0.35);
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 13px;
  overflow-wrap: anywhere; /* link/chuỗi dài tự xuống dòng, không tràn modal */
}
.ai-line + .ai-line { margin-top: 4px; }
.ai-desc-note { color: var(--green, #4caf50); }
.ai-why { margin-top: 4px; opacity: 0.85; font-style: italic; }
.ai-checklist { margin-top: 8px; }
.ai-ck-head { font-weight: 600; margin-bottom: 4px; }
.ai-ck-row {
  display: flex; align-items: center; justify-content: space-between;
  gap: 8px; padding: 2px 0;
}
.ai-ck-del {
  background: none; border: none; color: var(--text-dim, #888);
  cursor: pointer; font-size: 12px; line-height: 1; padding: 2px 4px;
}
.ai-ck-del:hover { color: #ff6b6b; }

/* Phụ thuộc: chip task đã chọn + nút bỏ */
.dep-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 6px; }
.dep-chip {
  display: inline-flex; align-items: center; gap: 6px;
  max-width: 100%;
  background: var(--accent-soft, rgba(109, 74, 255, 0.14));
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: 2px 6px 2px 10px;
  font-size: 12px;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.dep-del {
  background: none; border: none; cursor: pointer;
  color: var(--text-dim, #888); font-size: 12px; line-height: 1; padding: 2px;
}
.dep-del:hover { color: #ff6b6b; }
</style>
