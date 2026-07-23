<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import {
  ListNotifications, UnreadNotifications, MarkNotificationsRead, RespondInvitation,
} from '../../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'

const emit = defineEmits(['workspace-joined', 'open-task', 'error'])

const open = ref(false)
const unread = ref(0)
const items = ref([])
let timer = null

async function poll() {
  try {
    unread.value = await UnreadNotifications()
  } catch {
    /* chưa đăng nhập / lỗi tạm thời — bỏ qua */
  }
}

async function toggle() {
  open.value = !open.value
  if (!open.value) return
  try {
    items.value = await ListNotifications()
    await MarkNotificationsRead() // lời mời pending vẫn giữ unread
    await poll()
  } catch (e) {
    emit('error', String(e))
  }
}

async function respond(inv, accept) {
  try {
    await RespondInvitation(inv, accept)
    items.value = await ListNotifications()
    await poll()
    if (accept) emit('workspace-joined')
  } catch (e) {
    emit('error', String(e))
  }
}

// Thông báo có gắn task (nhắc hạn chót) → click là nhảy thẳng tới task đó.
function openTask(n) {
  if (!n.taskId) return
  open.value = false
  emit('open-task', n)
}

function fmtDT(iso) {
  const d = new Date(iso)
  const p = n => String(n).padStart(2, '0')
  return `${p(d.getDate())}/${p(d.getMonth() + 1)} ${p(d.getHours())}:${p(d.getMinutes())}`
}

async function onNew() {
  // Backend vừa phát hiện thông báo mới (đã đẩy notification HĐH):
  // cập nhật badge ngay; panel đang mở thì nạp lại danh sách luôn.
  await poll()
  if (open.value) {
    try {
      items.value = await ListNotifications()
    } catch { /* bỏ qua lỗi tạm thời */ }
  }
}

onMounted(() => {
  poll()
  timer = setInterval(poll, 30000)
  EventsOn('notifications:new', onNew)
})
onUnmounted(() => {
  clearInterval(timer)
  EventsOff('notifications:new')
})
</script>

<template>
  <div class="bell-wrap">
    <button class="bell-btn" title="Thông báo" @click="toggle">
      🔔
      <span v-if="unread > 0" class="bell-badge">{{ unread > 9 ? '9+' : unread }}</span>
    </button>

    <div v-if="open" class="bell-panel">
      <div class="bell-head">
        Thông báo
        <button class="btn sm icon" @click="open = false">✕</button>
      </div>
      <div class="bell-list">
        <div
          v-for="n in items" :key="n.id"
          class="bell-item" :class="{ unread: !n.read, clickable: !!n.taskId }"
          :title="n.taskId ? 'Mở task này' : ''"
          @click="openTask(n)"
        >
          <div class="bell-content">{{ n.content }}</div>
          <div class="bell-time">{{ fmtDT(n.createdAt) }}<template v-if="n.taskId"> · bấm để mở task ↗</template></div>
          <div v-if="n.kind === 'invite' && n.invitationStatus === 'pending'" class="bell-actions">
            <button class="btn sm primary" @click="respond(n.invitationId, true)">Chấp nhận</button>
            <button class="btn sm" @click="respond(n.invitationId, false)">Từ chối</button>
          </div>
          <div v-else-if="n.kind === 'invite'" class="bell-time">
            → đã {{ n.invitationStatus === 'accepted' ? 'chấp nhận' : 'từ chối' }}
          </div>
        </div>
        <div v-if="!items.length" class="hint" style="text-align: center; padding: 18px 0">
          Chưa có thông báo nào.
        </div>
      </div>
    </div>
  </div>
</template>
