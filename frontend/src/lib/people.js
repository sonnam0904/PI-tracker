// Màu & avatar cho nhân sự — dùng chung cho Gantt, Kanban, Settings.

export const UNASSIGNED_COLOR = '#8b949e'

// Palette dễ phân biệt, đọc được với chữ trắng; tránh trùng đỏ (Blocked)
// và xám (chưa gán).
const PALETTE = [
  '#2f81f7', // xanh dương
  '#a371f7', // tím
  '#d29922', // vàng đất
  '#39c5cf', // xanh ngọc
  '#f778ba', // hồng
  '#57ab5a', // xanh lá đậm
  '#f0883e', // cam
  '#6c7bf5', // chàm
  '#0aa2c0', // cyan đậm
  '#bf8700', // nâu vàng
  '#d2a8ff', // tím nhạt
  '#2da44e', // lá
]

// buildPeopleMeta trả về map ID → { color, initials } với initials LUÔN
// 2 ký tự và không trùng nhau giữa các nhân sự (thứ tự danh sách ổn định
// vì backend sort theo tên).
export function buildPeopleMeta(people) {
  const used = new Set()
  const meta = {}
  people.forEach((p, i) => {
    meta[p.ID] = {
      color: PALETTE[i % PALETTE.length],
      initials: uniqueInitials(p.Name || '?', used),
    }
  })
  return meta
}

function uniqueInitials(name, used) {
  const words = name.trim().split(/\s+/).filter(Boolean)
  const w = words[0] || '??'
  const candidates = []
  // Ưu tiên: chữ đầu của từ đầu + từ cuối ("Nguyễn Văn An" → NA)
  if (words.length >= 2) candidates.push(words[0][0] + words[words.length - 1][0])
  // Tên 1 từ: 2 ký tự đầu, rồi ký tự đầu + các ký tự sau ("sơn" → SƠ, SN)
  for (let i = 1; i < w.length; i++) candidates.push(w[0] + w[i])
  // 2 từ đầu ("Nguyễn Văn An" → NV)
  if (words.length >= 3) candidates.push(words[0][0] + words[1][0])
  // Cuối cùng: chữ đầu + số
  for (let d = 2; d <= 9; d++) candidates.push(w[0] + d)

  for (const c of candidates) {
    const u = c.toUpperCase()
    if (u.length === 2 && !used.has(u)) {
      used.add(u)
      return u
    }
  }
  return '??'
}
