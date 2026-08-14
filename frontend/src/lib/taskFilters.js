// Cấu hình view của trang Tasks: tìm nhanh + điều kiện lọc động + sắp xếp +
// nhóm (kiểu Lark). Toàn bộ được serialize thành JSON và lưu ở backend
// (models.SavedView.Filters) — backend chỉ lưu hộ, không đọc nội dung, nên
// mọi thay đổi shape chỉ cần sửa ở đây.
import {
  FIELD_BY_KEY, filterFields, sortFields, groupFields, opsFor,
  matchesCondition, sortValue, sortBlank, groupBuckets,
} from './taskFields'

export function emptyConfig() {
  return {
    q: '',            // tìm nhanh theo tiêu đề / mô tả
    match: 'all',     // 'all' (AND) | 'any' (OR)
    conditions: [],   // [{ field, op, value }]
    sorts: [],        // [{ field, dir: 'asc' | 'desc' }]
    groups: [],       // [{ field, dir: 'asc' | 'desc' }] — chỉ áp cho bảng
  }
}

export function newCondition() {
  const f = filterFields[0]
  return { field: f.key, op: opsFor(f.key)[0].value, value: '' }
}

export function newOrderItem(fields, usedKeys) {
  const f = fields.find(x => !usedKeys.includes(x.key)) || fields[0]
  return { field: f.key, dir: 'asc' }
}

// JSON đã lưu → config đầy đủ. Nhận cả format mới lẫn format cũ (bộ lọc cố
// định trước đây) để view lưu từ phiên bản cũ vẫn dùng được.
export function parseConfig(json) {
  let raw = {}
  try { raw = JSON.parse(json || '{}') || {} } catch { /* config hỏng → rỗng */ }
  if (Array.isArray(raw.conditions) || Array.isArray(raw.sorts) || Array.isArray(raw.groups)) {
    return normalizeConfig(raw)
  }
  return legacyToConfig(raw)
}

function normalizeConfig(raw) {
  const cfg = emptyConfig()
  cfg.q = typeof raw.q === 'string' ? raw.q : ''
  cfg.match = raw.match === 'any' ? 'any' : 'all'
  cfg.conditions = (raw.conditions || [])
    .filter(c => c && FIELD_BY_KEY[c.field])
    .map(c => ({ field: c.field, op: c.op, value: c.value ?? '' }))
  cfg.sorts = normOrder(raw.sorts, sortFields)
  cfg.groups = normOrder(raw.groups, groupFields)
  return cfg
}

function normOrder(arr, allowed) {
  const ok = new Set(allowed.map(f => f.key))
  return (arr || [])
    .filter(o => o && ok.has(o.field))
    .map(o => ({ field: o.field, dir: o.dir === 'desc' ? 'desc' : 'asc' }))
}

// Bộ lọc cố định đời cũ {q,assignee,status,type,size,ai,priority,due} → điều kiện.
function legacyToConfig(raw) {
  const cfg = emptyConfig()
  if (typeof raw.q === 'string') cfg.q = raw.q
  const add = (field, op, value) => cfg.conditions.push({ field, op, value })
  if (raw.assignee !== undefined && raw.assignee !== -1) add('assigneeId', 'is', raw.assignee)
  if (raw.status) add('status', 'is', raw.status)
  if (raw.type !== undefined && raw.type !== '') add('type', 'is', raw.type)
  if (raw.size) add('size', 'is', raw.size)
  if (raw.priority) add('priority', 'is', raw.priority)
  if (raw.ai === 'yes' || raw.ai === 'no') add('aiUsed', 'is', raw.ai)
  if (raw.due) add('dueState', 'is', raw.due)
  return cfg
}

export function isEmptyConfig(cfg) {
  return !cfg.q && !cfg.conditions.length && !cfg.sorts.length && !cfg.groups.length
}

// Hai config giống nhau (để phát hiện tab view đã bị chỉnh). Cả hai đều do
// emptyConfig/parseConfig sinh ra nên thứ tự key ổn định → so bằng JSON được.
export function sameConfig(a, b) {
  return JSON.stringify(a) === JSON.stringify(b)
}

// ID_QUERY — ô tìm nhanh đang tra ĐÚNG MỘT id: "#12" (cho phép khoảng trắng sau
// dấu #). Chỉ toàn chữ số mới tính; "#12 lỗi" hay "#" trơ là tìm text bình thường.
const ID_QUERY = /^#\s*(\d+)$/

export function matchesConfig(t, cfg) {
  const needle = (cfg.q || '').trim().toLowerCase()
  // Tìm nhanh quét cả tên tag — gõ tên tag là ra ngay các task thuộc tag đó.
  if (needle) {
    const idOnly = needle.match(ID_QUERY)
    if (idOnly) {
      // "#12" là tra CHÍNH XÁC task id 12, không phải "id có chứa 12": gõ "#1"
      // mà ra cả #12, #31, #201 thì số đếm "n/81 task" vô nghĩa và vẫn phải tự
      // soi từng dòng. Đây là bộ lọc nên phải trả đúng cái được hỏi — khác
      // combobox chọn task phụ thuộc (TaskModal), nơi thu hẹp dần theo từng ký
      // tự là đúng vì người dùng NHÌN danh sách gợi ý rồi bấm chọn.
      if (Number(idOnly[1]) !== Number(t.id)) return false
    } else {
      // Nhánh text vẫn giữ "#<id>" trong đống rơm: gõ số trơ ("12") hoặc dán cả
      // câu có "#12" vẫn tìm được như trước — "#" là dạng tra chính xác, không
      // phải cách duy nhất tìm theo id.
      const hay = [`#${t.id}`, t.title || '', t.description || '', ...(t.tags || [])]
        .join(' ').toLowerCase()
      if (!hay.includes(needle)) return false
    }
  }
  const conds = (cfg.conditions || []).filter(c => c.field && c.op)
  if (!conds.length) return true
  const res = conds.map(c => matchesCondition(t, c))
  return cfg.match === 'any' ? res.some(Boolean) : res.every(Boolean)
}

// Sắp xếp nhiều cấp; ô trống luôn xuống cuối bất kể chiều. Không có sort nào
// active → trả về mảng gốc (giữ nguyên thứ tự do caller đưa vào).
export function sortTasks(tasks, sorts, ctx) {
  const active = (sorts || []).filter(s => s.field && FIELD_BY_KEY[s.field])
  if (!active.length) return tasks
  return [...tasks]
    .map((t, i) => [t, i])
    .sort(([a, ia], [b, ib]) => {
      for (const s of active) {
        const r = cmpBy(a, b, s, ctx)
        if (r) return r
      }
      return ia - ib // ổn định
    })
    .map(([t]) => t)
}

function cmpBy(a, b, s, ctx) {
  const va = sortValue(a, s.field, ctx)
  const vb = sortValue(b, s.field, ctx)
  const ba = sortBlank(va), bb = sortBlank(vb)
  if (ba && bb) return 0
  if (ba) return 1
  if (bb) return -1
  const r = va < vb ? -1 : va > vb ? 1 : 0
  return s.dir === 'desc' ? -r : r
}

// Xây danh sách phẳng xen kẽ header nhóm và task cho bảng (nhóm nhiều cấp).
// Trả null nếu không có nhóm nào. Mỗi phần tử:
//   { type:'group', depth, label, count, path, field, ancestors }
//   { type:'task', task, ancestors }
// ancestors = các path nhóm cha (để thu gọn: ẩn khi một tổ tiên bị gập).
export function buildGroups(tasks, groups, ctx) {
  const active = (groups || []).filter(g => g.field && FIELD_BY_KEY[g.field])
  if (!active.length) return null
  const out = []
  const rec = (list, depth, ancestors) => {
    const g = active[depth]
    const buckets = new Map()
    for (const t of list) {
      // groupBuckets trả về NHIỀU nhóm cho field nhiều giá trị (tags): task gắn
      // n tag được đếm vào cả n nhóm, nên tổng count các nhóm > số task.
      for (const b of groupBuckets(t, g.field, ctx)) {
        if (!buckets.has(b.key)) {
          buckets.set(b.key, { key: b.key, label: b.label, sortVal: b.sortVal, tasks: [] })
        }
        buckets.get(b.key).tasks.push(t)
      }
    }
    const arr = [...buckets.values()].sort((a, b) => groupCmp(a, b, g.dir))
    for (const b of arr) {
      const path = ancestors.join('>') + '>' + g.field + ':' + b.key
      out.push({ type: 'group', depth, label: b.label, count: b.tasks.length, path, field: g.field, ancestors: [...ancestors] })
      const childAnc = [...ancestors, path]
      if (depth + 1 < active.length) rec(b.tasks, depth + 1, childAnc)
      else for (const t of b.tasks) out.push({ type: 'task', task: t, ancestors: childAnc })
    }
  }
  rec(tasks, 0, [])
  return out
}

function groupCmp(a, b, dir) {
  const ba = a.key === '∅', bb = b.key === '∅'
  if (ba && bb) return 0
  if (ba) return 1
  if (bb) return -1
  const r = a.sortVal < b.sortVal ? -1 : a.sortVal > b.sortVal ? 1 : 0
  return dir === 'desc' ? -r : r
}
