<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { GetSession, Logout, ResumeSession, ListSavedAccounts, ForgetAccount, ListWorkspaces, SelectWorkspace, CreateWorkspace, AppVersion, DBStatus, RetryDB } from '../wailsjs/go/main/App'
import { getTokens, setTokens, removeToken } from './lib/session'
import AuthView from './components/AuthView.vue'
import AccountPicker from './components/AccountPicker.vue'
import NotificationBell from './components/NotificationBell.vue'
import UpdateBanner from './components/UpdateBanner.vue'
import Dashboard from './components/Dashboard.vue'
import GanttView from './components/GanttView.vue'
import TeamView from './components/TeamView.vue'
import SettingsView from './components/SettingsView.vue'

const tab = ref('dashboard')
const tabs = [
  { id: 'dashboard', label: 'Dashboard', ico: '◧' },
  { id: 'tasks', label: 'Tasks', ico: '☰' },
  { id: 'team', label: 'Team', ico: '👥' },
  { id: 'settings', label: 'Cài đặt', ico: '⚙' },
]

const session = ref(null)
const workspaces = ref([])
const curWs = ref(0)
const newWsName = ref('')
const error = ref('')
const viewKey = ref(0) // đổi workspace → remount views để nạp lại dữ liệu
const version = ref('') // phiên bản app đang chạy, hiện ở sidebar
const menuOpen = ref(false) // menu xổ từ nút thoát (Kiểm tra cập nhật / Thoát)
const updateBanner = ref(null) // tham chiếu UpdateBanner để kiểm tra thủ công

// Đa tài khoản: authScreen = '' (đã vào app) | 'picker' (chọn tài khoản) |
// 'login' (đăng nhập/thêm tài khoản). savedAccounts = các tài khoản đã ghi nhớ.
const authScreen = ref('')
const savedAccounts = ref([])

// Hai trạng thái DB TÁCH BIỆT (một nguồn chân lý là DBStatus):
//   - dbError: thất bại lúc KHỞI ĐỘNG (chưa có kết nối) → MÀN CHẶN toàn trang.
//   - dbDown : kết nối đã mở nhưng DB RỚT lúc đang chạy → banner runtime, tự
//     tắt đúng lúc DB khỏe lại. Không đụng tới banner `error` chung.
const dbError = ref('')
const dbRetrying = ref(false)
const dbDown = ref('')

// Watcher sức khỏe DB: setTimeout ĐỆ QUY (lên lịch lần kế chỉ SAU khi ping xong
// → không chồng lấn) và DỪNG khi khỏe. Chỉ bật khi nghi DB rớt (một thao tác
// lỗi). Nếu ping cho thấy DB vẫn khỏe thì thôi ngay (lỗi đó không phải do DB).
let healthTimer = null
async function checkDBHealth() {
  healthTimer = null
  let st = null
  try { st = await DBStatus() } catch { /* coi như chưa rõ → dò tiếp */ }
  if (st && st.configured && !st.ok) {
    dbDown.value = st.error || 'Mất kết nối cơ sở dữ liệu'
    error.value = '' // dbDown thay thế banner lỗi chung cho ca DB rớt
    healthTimer = setTimeout(checkDBHealth, 3000) // còn rớt → dò tiếp
  } else if (!st) {
    healthTimer = setTimeout(checkDBHealth, 3000) // không rõ → thử lại
  } else {
    dbDown.value = '' // khỏe (hoặc chưa configured) → dừng dò
  }
}
function startHealthWatch() {
  if (healthTimer) return // đã đang dò
  healthTimer = setTimeout(checkDBHealth, 0)
}
// Bất kỳ lỗi thao tác nào cũng probe MỘT lần: nếu do DB rớt → bật banner dbDown
// (và dò tới khi hồi phục); nếu DB vẫn khỏe → dừng ngay, giữ banner `error`.
watch(error, v => { if (v) startHealthWatch() })

// Bấm "Thử lại" trên banner dbDown: ping ngay (đánh thức pool) rồi cập nhật.
async function retryDBDown() {
  if (healthTimer) { clearTimeout(healthTimer); healthTimer = null }
  try { await RetryDB() } catch { /* vẫn lỗi → checkDBHealth sẽ phản ánh */ }
  await checkDBHealth()
}

const loggedIn = computed(() => !!session.value?.userId)
// Tài khoản khác (để chuyển nhanh) và token của tài khoản đang mở.
const otherAccounts = computed(() =>
  savedAccounts.value.filter(a => a.userId !== session.value?.userId)
)
const activeToken = computed(() =>
  savedAccounts.value.find(a => a.userId === session.value?.userId)?.token || ''
)

function checkUpdate() {
  menuOpen.value = false
  updateBanner.value?.check(true)
}
function doLogout() {
  menuOpen.value = false
  logout()
}

async function refresh() {
  error.value = ''
  try {
    session.value = await GetSession()
    if (session.value.userId) {
      // ListWorkspaces trả null khi user chưa thuộc workspace nào (nil slice →
      // JSON null); ép về [] để template dùng workspaces.length không nổ.
      workspaces.value = (await ListWorkspaces()) || []
      curWs.value = session.value.workspaceId
    } else {
      workspaces.value = []
    }
  } catch (e) {
    error.value = String(e)
  }
}

// Nạp danh sách tài khoản đã ghi nhớ (giải mã + xác thực ở backend) và prune
// lại token local theo kết quả (bỏ token hỏng/hết hạn/của máy khác).
async function loadSavedAccounts() {
  const tokens = getTokens()
  if (!tokens.length) {
    savedAccounts.value = []
    return
  }
  try {
    const accts = await ListSavedAccounts(tokens)
    savedAccounts.value = accts
    setTokens(accts.map(a => a.token))
  } catch {
    savedAccounts.value = []
  }
}

// Khởi tạo màn xác thực (session + tài khoản đã ghi nhớ). Tách riêng để dùng
// lại sau khi RetryDB kết nối lại thành công.
async function initAuth() {
  session.value = await GetSession()
  await loadSavedAccounts()
  authScreen.value = savedAccounts.value.length ? 'picker' : 'login'
}

onMounted(async () => {
  // Chỉ MÀN CHẶN khi CHƯA có kết nối (thất bại lúc khởi động). Nếu đã có kết
  // nối nhưng đang rớt (configured && !ok) thì vẫn vào app + bật health-watch
  // để hiện banner runtime — không chặn toàn trang bằng kết quả ping.
  try {
    const st = await DBStatus()
    if (st && !st.configured) {
      dbError.value = st.error || 'Không kết nối được cơ sở dữ liệu'
      return
    }
    if (st && !st.ok) startHealthWatch()
  } catch { /* DBStatus không trả về (bản cũ) → cứ tiếp tục như thường */ }
  // Mỗi lần mở app: session trong bộ nhớ backend rỗng → hiện màn chọn tài
  // khoản nếu có tài khoản đã ghi nhớ, ngược lại hiện màn đăng nhập.
  await initAuth()
})

async function retryDB() {
  dbRetrying.value = true
  error.value = ''
  try {
    await RetryDB()
    dbError.value = ''
    await initAuth()
  } catch (e) {
    dbError.value = String(e)
  } finally {
    dbRetrying.value = false
  }
}
onMounted(async () => {
  try {
    version.value = await AppVersion()
  } catch {
    version.value = ''
  }
})

async function onLoggedIn() {
  await loadSavedAccounts()
  await refresh()
  authScreen.value = ''
  viewKey.value++
  tab.value = 'dashboard'
}

// Chọn/chuyển sang một tài khoản đã ghi nhớ (không cần nhập lại mật khẩu).
async function pickAccount(acct) {
  error.value = ''
  try {
    await ResumeSession(acct.token)
    await refresh()
    authScreen.value = ''
    viewKey.value++
    tab.value = 'dashboard'
    menuOpen.value = false
  } catch {
    // Token vô hiệu → quên nó, cập nhật lại danh sách.
    removeToken(acct.token)
    await loadSavedAccounts()
    authScreen.value = savedAccounts.value.length ? 'picker' : 'login'
    error.value = 'Phiên đã hết hạn, vui lòng đăng nhập lại.'
  }
}

// "Quên" một tài khoản khỏi máy (xóa phiên đã ghi nhớ, không đụng phiên hiện tại).
async function forgetAccount(acct) {
  await ForgetAccount(acct.token)
  removeToken(acct.token)
  await loadSavedAccounts()
  if (authScreen.value === 'picker' && !savedAccounts.value.length) authScreen.value = 'login'
}

function startAddAccount() {
  menuOpen.value = false
  authScreen.value = 'login'
}
function cancelAuth() {
  authScreen.value = loggedIn.value ? '' : savedAccounts.value.length ? 'picker' : 'login'
}

async function logout() {
  // Đăng xuất tài khoản hiện tại: xóa phiên đã ghi nhớ (local + DB) rồi về màn
  // chọn tài khoản (nếu còn tài khoản khác) hoặc đăng nhập.
  const token = activeToken.value
  if (token) removeToken(token)
  await Logout(token)
  await loadSavedAccounts()
  session.value = await GetSession()
  authScreen.value = savedAccounts.value.length ? 'picker' : 'login'
}

async function switchWs() {
  try {
    await SelectWorkspace(Number(curWs.value))
    await refresh()
    viewKey.value++
  } catch (e) {
    error.value = String(e)
  }
}

async function createWs() {
  if (!newWsName.value.trim()) return
  try {
    await CreateWorkspace(newWsName.value.trim())
    newWsName.value = ''
    await refresh()
    viewKey.value++
  } catch (e) {
    error.value = String(e)
  }
}

// Click thông báo gắn task (nhắc hạn, mention, reply) → nhảy tới tab Tasks,
// mở đúng task (đổi workspace trước nếu cần); có gắn bình luận thì modal sẽ
// scroll tới và làm nổi bật bình luận đó.
const pendingTaskId = ref(0)
const pendingActivityId = ref(0)
async function openTaskFromNotif(n) {
  error.value = ''
  try {
    if (n.workspaceId && n.workspaceId !== session.value.workspaceId) {
      await SelectWorkspace(Number(n.workspaceId))
      await refresh()
      viewKey.value++
    }
    pendingActivityId.value = Number(n.activityId) || 0
    pendingTaskId.value = Number(n.taskId)
    tab.value = 'tasks'
  } catch (e) {
    error.value = String(e)
  }
}
</script>

<template>
  <!-- Kết nối DB thất bại: banner lỗi giống banner cập nhật + nút Thử lại -->
  <div v-if="dbError" class="db-error-wrap">
    <div class="db-error-card">
      <div class="db-banner">
        <span class="ico">⚠</span>
        <div class="txt">
          <b>Không kết nối được cơ sở dữ liệu</b>
          <span>{{ dbError }}</span>
        </div>
      </div>
      <button class="btn primary db-retry" :disabled="dbRetrying" @click="retryDB">
        <span v-if="dbRetrying" class="db-spin"></span>
        {{ dbRetrying ? 'Đang thử lại…' : 'Thử lại' }}
      </button>
      <p class="hint db-error-hint">Kiểm tra máy chủ DB, mạng/VPN và cấu hình trong tệp <code>.env</code>, rồi bấm Thử lại.</p>
    </div>
  </div>

  <AccountPicker
    v-else-if="authScreen === 'picker'"
    :accounts="savedAccounts"
    @pick="pickAccount"
    @remove="forgetAccount"
    @login="startAddAccount"
  />
  <AuthView
    v-else-if="authScreen === 'login'"
    :can-cancel="loggedIn || savedAccounts.length > 0"
    @logged-in="onLoggedIn"
    @cancel="cancelAuth"
  />

  <div v-else-if="loggedIn" class="layout">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-badge">PI</div>
        <div>
          <div class="brand-name">Task Manager</div>
          <div class="brand-sub">PI Tracker</div>
        </div>
      </div>

      <!-- Workspace -->
      <div class="ws-box">
        <label>Workspace</label>
        <select v-if="workspaces.length" v-model.number="curWs" @change="switchWs">
          <option v-for="w in workspaces" :key="w.id" :value="w.id">{{ w.name }}</option>
        </select>
        <div class="ws-new">
          <input v-model="newWsName" placeholder="Tạo workspace mới…" @keyup.enter="createWs" />
          <button class="btn sm" @click="createWs">+</button>
        </div>
      </div>

      <button
        v-for="t in tabs" :key="t.id"
        class="nav-item" :class="{ active: tab === t.id }"
        @click="tab = t.id"
      >
        <span class="ico">{{ t.ico }}</span> {{ t.label }}
      </button>

      <div style="flex: 1"></div>

      <!-- Phiên bản app đang chạy -->
      <div v-if="version" class="app-version" title="Phiên bản đang chạy">
        {{ version === 'dev' ? 'dev build' : 'v' + version }}
      </div>

      <!-- User + chuông + đăng xuất -->
      <div class="user-box">
        <NotificationBell
          @workspace-joined="refresh(); viewKey++"
          @open-task="openTaskFromNotif"
          @error="e => (error = e)"
        />
        <span class="user-name" :title="session.username">@{{ session.username }}</span>
        <div class="exit-wrap">
          <button class="btn sm icon" title="Tùy chọn" @click="menuOpen = !menuOpen">⎋</button>
          <template v-if="menuOpen">
            <div class="exit-backdrop" @click="menuOpen = false"></div>
            <div class="exit-menu">
              <template v-if="otherAccounts.length">
                <div class="exit-label">Chuyển tài khoản</div>
                <button
                  v-for="a in otherAccounts" :key="a.token"
                  class="exit-item" @click="pickAccount(a)"
                >👤 @{{ a.username }}</button>
                <div class="exit-sep"></div>
              </template>
              <button class="exit-item" @click="startAddAccount">＋ Thêm tài khoản</button>
              <button class="exit-item" @click="checkUpdate">↻ Kiểm tra cập nhật</button>
              <button class="exit-item" @click="doLogout">⎋ Đăng xuất</button>
            </div>
          </template>
        </div>
      </div>
    </aside>

    <main class="main" :key="viewKey">
      <UpdateBanner ref="updateBanner" />
      <!-- DB rớt lúc đang chạy: banner riêng, tự tắt khi DB khỏe trở lại -->
      <div v-if="dbDown" class="db-down-banner">
        <span class="db-spin"></span>
        <span class="txt">{{ dbDown }}</span>
        <button class="db-down-retry" title="Thử lại ngay" @click="retryDBDown">↻ Thử lại</button>
      </div>
      <div v-if="error" class="err app-err">
        <span>{{ error }}</span>
        <button class="err-close" title="Đóng" @click="error = ''">✕</button>
      </div>

      <template v-if="session.workspaceId">
        <Dashboard v-if="tab === 'dashboard'" />
        <GanttView
          v-else-if="tab === 'tasks'"
          :open-task-id="pendingTaskId"
          :open-activity-id="pendingActivityId"
          @task-opened="pendingTaskId = 0; pendingActivityId = 0"
        />
        <TeamView v-else-if="tab === 'team'" />
        <SettingsView v-else />
      </template>

      <div v-else class="empty" style="padding-top: 120px">
        <h3 style="margin-bottom: 8px">Chưa có workspace nào</h3>
        <p class="hint" style="margin-bottom: 18px">
          Tạo workspace đầu tiên để bắt đầu quản lý task, hoặc chờ lời mời từ người khác (kiểm tra chuông 🔔).
        </p>
        <div style="display: inline-flex; gap: 8px">
          <input
            v-model="newWsName" placeholder="Tên workspace"
            style="background: var(--panel); border: 1px solid var(--border); border-radius: 8px; padding: 9px 12px; outline: none"
            @keyup.enter="createWs"
          />
          <button class="btn primary" @click="createWs">Tạo workspace</button>
        </div>
      </div>
    </main>
  </div>
</template>
