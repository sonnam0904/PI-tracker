<script setup>
import { ref, onMounted } from 'vue'
import { GetSession, Logout, ListWorkspaces, SelectWorkspace, CreateWorkspace, AppVersion } from '../wailsjs/go/main/App'
import AuthView from './components/AuthView.vue'
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

async function refresh() {
  error.value = ''
  try {
    session.value = await GetSession()
    if (session.value.userId) {
      workspaces.value = await ListWorkspaces()
      curWs.value = session.value.workspaceId
    } else {
      workspaces.value = []
    }
  } catch (e) {
    error.value = String(e)
  }
}
onMounted(refresh)
onMounted(async () => {
  try {
    version.value = await AppVersion()
  } catch {
    version.value = ''
  }
})

async function onLoggedIn() {
  await refresh()
  viewKey.value++
  tab.value = 'dashboard'
}

async function logout() {
  await Logout()
  await refresh()
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
  <AuthView v-if="!session || !session.userId" @logged-in="onLoggedIn" />

  <div v-else class="layout">
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
        <button class="btn sm icon" title="Đăng xuất" @click="logout">⎋</button>
      </div>
    </aside>

    <main class="main" :key="viewKey">
      <UpdateBanner />
      <div v-if="error" class="err">{{ error }}</div>

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
