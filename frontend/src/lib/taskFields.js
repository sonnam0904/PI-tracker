// Registry field của task cho bộ lọc / sắp xếp / nhóm động (kiểu Lark).
// Mỗi field có `type` quyết định danh sách toán tử, ô nhập giá trị, cách so
// khớp / sắp xếp / gán nhãn nhóm. Thêm/bớt cột lọc chỉ cần sửa mảng FIELDS.
import { TYPES, TYPE_LABEL } from './taskTypes'
import { parseISODate, todayISO } from './date'

export const STATUSES = ['Todo', 'In Progress', 'Blocked', 'Done']
export const SIZES = ['S', 'M', 'L', 'XL']
export const PRIORITIES = ['P1', 'P2', 'P3', 'P4']

const selOpts = arr => arr.map(v => ({ value: v, label: v }))

// filter/sort/group = false → ẩn field khỏi picker tương ứng (mặc định hiện).
// numeric = true → giá trị select là SỐ (field type lưu mã số TaskType).
// computed = true → giá trị suy ra (cycle), không đọc thẳng từ task.
export const FIELDS = [
  { key: 'title', label: 'Tiêu đề', type: 'text', group: false },
  { key: 'assigneeId', label: 'Phụ trách', type: 'person' },
  { key: 'status', label: 'Trạng thái', type: 'select', options: selOpts(STATUSES) },
  { key: 'type', label: 'Loại', type: 'select', numeric: true, options: TYPES.map(t => ({ value: t.value, label: t.label })) },
  { key: 'size', label: 'Size', type: 'select', options: selOpts(SIZES) },
  { key: 'priority', label: 'Ưu tiên', type: 'select', options: selOpts(PRIORITIES) },
  { key: 'aiUsed', label: 'Dùng AI', type: 'bool' },
  { key: 'dueState', label: 'Tình trạng hạn', type: 'duestate', sort: false,
    options: [
      { value: 'overdue', label: '⏰ Quá hạn' },
      { value: 'ontrack', label: 'Còn hạn' },
      { value: 'none', label: 'Không đặt hạn' },
    ] },
  { key: 'estimateCustomerDays', label: 'Est khách (ngày)', type: 'number' },
  { key: 'estimateAiDays', label: 'Est AI (ngày)', type: 'number' },
  { key: 'blockedDays', label: 'Ngày blocked', type: 'number' },
  { key: 'cycle', label: 'Cycle (ngày)', type: 'number', filter: false, group: false, computed: true },
  { key: 'startDate', label: 'Start date', type: 'date' },
  { key: 'dueDate', label: 'Hạn chót', type: 'date' },
  { key: 'doneDate', label: 'Done date', type: 'date' },
  { key: 'createdDate', label: 'Ngày tạo', type: 'date' },
]

export const FIELD_BY_KEY = Object.fromEntries(FIELDS.map(f => [f.key, f]))
export const filterFields = FIELDS.filter(f => f.filter !== false)
export const sortFields = FIELDS.filter(f => f.sort !== false)
export const groupFields = FIELDS.filter(f => f.group !== false)

// Toán tử theo type. noInput = toán tử không cần ô giá trị (trống/không trống).
const OPS = {
  text: [
    { value: 'contains', label: 'chứa' },
    { value: 'notContains', label: 'không chứa' },
    { value: 'is', label: 'là' },
    { value: 'isNot', label: 'không là' },
    { value: 'isEmpty', label: 'trống', noInput: true },
    { value: 'isNotEmpty', label: 'không trống', noInput: true },
  ],
  select: [
    { value: 'is', label: 'là' },
    { value: 'isNot', label: 'không là' },
    { value: 'isEmpty', label: 'trống', noInput: true },
    { value: 'isNotEmpty', label: 'không trống', noInput: true },
  ],
  person: [
    { value: 'is', label: 'là' },
    { value: 'isNot', label: 'không là' },
    { value: 'isEmpty', label: 'chưa gán', noInput: true },
    { value: 'isNotEmpty', label: 'đã gán', noInput: true },
  ],
  bool: [{ value: 'is', label: 'là' }],
  duestate: [{ value: 'is', label: 'là' }],
  number: [
    { value: 'eq', label: '=' },
    { value: 'ne', label: '≠' },
    { value: 'gt', label: '>' },
    { value: 'lt', label: '<' },
    { value: 'gte', label: '≥' },
    { value: 'lte', label: '≤' },
    { value: 'isEmpty', label: 'trống', noInput: true },
    { value: 'isNotEmpty', label: 'không trống', noInput: true },
  ],
  date: [
    { value: 'is', label: 'đúng ngày' },
    { value: 'before', label: 'trước' },
    { value: 'after', label: 'sau' },
    { value: 'onOrBefore', label: '≤ ngày' },
    { value: 'onOrAfter', label: '≥ ngày' },
    { value: 'isEmpty', label: 'trống', noInput: true },
    { value: 'isNotEmpty', label: 'không trống', noInput: true },
  ],
}

export function opsFor(fieldKey) {
  const f = FIELD_BY_KEY[fieldKey]
  return f ? OPS[f.type] || [] : []
}

export function opDef(fieldKey, opValue) {
  return opsFor(fieldKey).find(o => o.value === opValue) || null
}

// Nhãn hướng sắp xếp/nhóm theo type (Lark hiển thị mũi tên đôi).
export function dirLabels(fieldKey) {
  const f = FIELD_BY_KEY[fieldKey]
  switch (f?.type) {
    case 'number': return { asc: 'Nhỏ → Lớn', desc: 'Lớn → Nhỏ' }
    case 'date': return { asc: 'Cũ → Mới', desc: 'Mới → Cũ' }
    default: return { asc: 'A → Z', desc: 'Z → A' }
  }
}

// ---- Truy xuất & so khớp giá trị ----

const overdue = t => !!t.dueDate && t.status !== 'Done' && t.dueDate < todayISO()

function cycleDays(t) {
  const s = parseISODate(t.startDate)
  const d = parseISODate(t.doneDate)
  if (!s || !d) return null
  return Math.max((d - s) / 86400000 - (t.blockedDays || 0), 0)
}

export function rawValue(t, key) {
  switch (key) {
    case 'assigneeId': return t.assigneeId || 0
    case 'cycle': return cycleDays(t)
    default: return t[key]
  }
}

function isBlank(v, type) {
  if (v === undefined || v === null || v === '') return true
  if (type === 'person' && !v) return true
  return false
}

// Một điều kiện có khớp task không. Điều kiện chưa chọn đủ (thiếu giá trị mà
// toán tử cần) coi như không lọc → trả về true, để bộ lọc dở dang không ẩn hết.
export function matchesCondition(t, c) {
  const f = FIELD_BY_KEY[c.field]
  if (!f || !c.op) return true
  const type = f.type
  const v = rawValue(t, c.field)

  if (c.op === 'isEmpty') return isBlank(v, type)
  if (c.op === 'isNotEmpty') return !isBlank(v, type)

  if (type === 'duestate') {
    if (!c.value) return true
    if (c.value === 'overdue') return overdue(t)
    if (c.value === 'ontrack') return !!t.dueDate && !overdue(t) && t.status !== 'Done'
    if (c.value === 'none') return !t.dueDate
    return true
  }
  if (type === 'bool') {
    if (!c.value) return true
    return (!!v) === (c.value === 'yes')
  }

  const cv = c.value
  if (cv === '' || cv === null || cv === undefined) return true // chưa nhập → bỏ qua

  switch (type) {
    case 'text': {
      const s = String(v ?? '').toLowerCase(), q = String(cv).toLowerCase()
      if (c.op === 'contains') return s.includes(q)
      if (c.op === 'notContains') return !s.includes(q)
      if (c.op === 'is') return s === q
      if (c.op === 'isNot') return s !== q
      return true
    }
    case 'select': {
      const val = f.numeric ? Number(v) : v
      const target = f.numeric ? Number(cv) : cv
      if (c.op === 'is') return val === target
      if (c.op === 'isNot') return val !== target
      return true
    }
    case 'person': {
      const val = v || 0, target = Number(cv)
      if (c.op === 'is') return val === target
      if (c.op === 'isNot') return val !== target
      return true
    }
    case 'number': {
      if (isBlank(v, type)) return false
      const val = Number(v), target = Number(cv)
      switch (c.op) {
        case 'eq': return val === target
        case 'ne': return val !== target
        case 'gt': return val > target
        case 'lt': return val < target
        case 'gte': return val >= target
        case 'lte': return val <= target
      }
      return true
    }
    case 'date': {
      if (isBlank(v, type)) return false
      const val = String(v).slice(0, 10), target = String(cv).slice(0, 10)
      switch (c.op) {
        case 'is': return val === target
        case 'before': return val < target
        case 'after': return val > target
        case 'onOrBefore': return val <= target
        case 'onOrAfter': return val >= target
      }
      return true
    }
  }
  return true
}

// Giá trị so sánh khi sắp/nhóm. Trả '' hoặc null cho ô trống (được đẩy xuống cuối).
export function sortValue(t, key, ctx) {
  const f = FIELD_BY_KEY[key]
  const v = rawValue(t, key)
  if (!f) return ''
  switch (f.type) {
    case 'person': return (ctx?.names?.[v] || '').toLowerCase()
    case 'number': return typeof v === 'number' ? v : null
    case 'date': return v ? String(v).slice(0, 10) : ''
    case 'bool': return v ? 1 : 0
    case 'select': return f.numeric ? (Number(v) || 0) : (v || '')
    default: return String(v ?? '').toLowerCase()
  }
}

export function sortBlank(sv) {
  return sv === '' || sv === null || sv === undefined
}

// Khóa gộp nhóm (task cùng khóa vào một nhóm). '∅' = ô trống.
export function groupKey(t, key) {
  const f = FIELD_BY_KEY[key]
  const v = rawValue(t, key)
  if (isBlank(v, f?.type)) return '∅'
  if (f.type === 'duestate') return overdue(t) ? 'overdue' : (!t.dueDate ? 'none' : 'ontrack')
  if (f.type === 'date') return String(v).slice(0, 10)
  return String(v)
}

export function groupLabel(t, key, ctx) {
  const f = FIELD_BY_KEY[key]
  const v = rawValue(t, key)
  if (isBlank(v, f?.type)) return '(Trống)'
  switch (f.type) {
    case 'person': return ctx?.names?.[v] || `#${v}`
    case 'bool': return v ? 'Có dùng AI' : 'Không dùng AI'
    case 'duestate': return overdue(t) ? '⏰ Quá hạn' : (!t.dueDate ? 'Không đặt hạn' : 'Còn hạn')
    case 'select': return f.numeric ? (TYPE_LABEL[v] ?? String(v)) : String(v)
    case 'date': return String(v).slice(0, 10)
    default: return String(v)
  }
}
