// Quản lý các phiên "ghi nhớ đăng nhập" lưu ở local máy. Một máy có thể ghi
// nhớ NHIỀU tài khoản. Chỉ lưu token đã MÃ HÓA THEO MÁY (opaque) — KHÔNG bao
// giờ lưu username hay mật khẩu ở local.
const LIST_KEY = 'tm_sessions' // JSON: mảng token đã mã hóa
const LEGACY_KEY = 'tm_session_token' // khóa token đơn đời cũ (migrate 1 lần)

function read(key) {
  try {
    return JSON.parse(localStorage.getItem(key))
  } catch {
    return null
  }
}

// getTokens trả về danh sách token đã lưu (mới nhất trước). Tự migrate token
// đơn đời cũ sang danh sách.
export function getTokens() {
  let list = read(LIST_KEY)
  if (!Array.isArray(list)) list = []
  const legacy = localStorage.getItem(LEGACY_KEY)
  if (legacy) {
    if (!list.includes(legacy)) list.unshift(legacy)
    localStorage.removeItem(LEGACY_KEY)
    setTokens(list)
  }
  return list
}

export function setTokens(list) {
  localStorage.setItem(LIST_KEY, JSON.stringify(list))
}

// addToken thêm/đưa token lên đầu danh sách (không trùng lặp chuỗi).
export function addToken(token) {
  if (!token) return
  const list = getTokens().filter(t => t !== token)
  list.unshift(token)
  setTokens(list)
}

export function removeToken(token) {
  setTokens(getTokens().filter(t => t !== token))
}
