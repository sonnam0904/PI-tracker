// Loại task — DB lưu SỐ (models.TaskType phía Go), UI hiển thị nhãn tiếng Việt.
// Giữ đồng bộ với internal/models/task.go.

export const TYPE_PLAN = 1
export const TYPE_BUG = 2
export const TYPE_PLAN_ARISING = 3

export const TYPES = [
  { value: TYPE_PLAN, label: 'Theo plan' },
  { value: TYPE_BUG, label: 'Phát sinh (bug)' },
  { value: TYPE_PLAN_ARISING, label: 'Phát sinh theo plan' },
]

export const TYPE_LABEL = Object.fromEntries(TYPES.map(t => [t.value, t.label]))

export const isBug = t => t.type === TYPE_BUG
