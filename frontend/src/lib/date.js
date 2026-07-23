// Date helpers — all in local time, dates as "YYYY-MM-DD" strings from backend.

export function monthStart(d) {
  return new Date(d.getFullYear(), d.getMonth(), 1)
}

export function addMonths(d, n) {
  return new Date(d.getFullYear(), d.getMonth() + n, 1)
}

export function daysInMonth(d) {
  return new Date(d.getFullYear(), d.getMonth() + 1, 0).getDate()
}

// "2026-07" key for the backend GetMetrics API
export function ymKey(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

export function monthLabel(d) {
  return `Tháng ${String(d.getMonth() + 1).padStart(2, '0')}/${d.getFullYear()}`
}

export function parseISODate(s) {
  if (!s) return null
  const [y, m, d] = s.slice(0, 10).split('-').map(Number)
  return new Date(y, m - 1, d)
}

export function todayISO() {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function fmtDM(d) {
  return `${String(d.getDate()).padStart(2, '0')}/${String(d.getMonth() + 1).padStart(2, '0')}`
}

export function daysBetween(a, b) {
  return (b - a) / 86400000
}
