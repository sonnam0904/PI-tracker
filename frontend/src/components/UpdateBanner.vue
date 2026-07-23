<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { CheckUpdate, ApplyUpdate } from '../../wailsjs/go/main/App'

// mode: '' ẩn | 'update' có bản mới | 'uptodate' đã mới nhất | 'error'
const mode = ref('')
const latest = ref('')
const current = ref('')
const notes = ref('')
const updating = ref(false)
const errorMsg = ref('')
let hideTimer = null

// manual = true khi người dùng tự bấm "Kiểm tra cập nhật" → cần phản hồi cả khi
// đã là bản mới nhất hoặc lỗi. Tự động (nền) thì chỉ hiện khi có bản mới.
async function check(manual = false) {
  clearTimeout(hideTimer)
  try {
    const st = await CheckUpdate()
    current.value = st?.current || ''
    latest.value = st?.latest || ''
    notes.value = st?.notes || ''
    if (st && st.available) {
      mode.value = 'update'
    } else if (manual) {
      mode.value = 'uptodate'
      hideTimer = setTimeout(() => { if (mode.value === 'uptodate') mode.value = '' }, 4000)
    }
  } catch (e) {
    if (manual) {
      errorMsg.value = 'Không kiểm tra được cập nhật: ' + String(e)
      mode.value = 'error'
      hideTimer = setTimeout(() => { if (mode.value === 'error') mode.value = '' }, 5000)
    }
    // Tự động: im lặng khi lỗi mạng/không có release.
  }
}

async function update() {
  updating.value = true
  errorMsg.value = ''
  try {
    // Thành công thì app tự khởi động lại → lời gọi này có thể không trở về.
    await ApplyUpdate()
  } catch (e) {
    errorMsg.value = 'Cập nhật thất bại: ' + String(e)
    mode.value = 'error'
    updating.value = false
  }
}

function dismiss() {
  mode.value = ''
}

// Kiểm tra lúc mở app, rồi lặp lại mỗi 1 giờ khi app còn chạy.
let timer = null
onMounted(() => {
  check()
  timer = setInterval(() => check(), 60 * 60 * 1000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
  clearTimeout(hideTimer)
})

// Cho App.vue gọi khi người dùng bấm "Kiểm tra cập nhật" trong menu.
defineExpose({ check })
</script>

<template>
  <transition name="slide">
    <!-- Có bản mới -->
    <div v-if="mode === 'update'" class="update-banner">
      <span class="up-ico">⤴</span>
      <div class="up-text">
        <b>Đã có phiên bản mới v{{ latest }}</b>
        <span class="up-sub">Cập nhật để nhận tính năng và sửa lỗi mới nhất.</span>
      </div>
      <button class="up-btn" :disabled="updating" @click="update">
        <span v-if="updating" class="spin"></span>
        {{ updating ? 'Đang cập nhật…' : 'Cập nhật ngay' }}
      </button>
      <button class="up-close" :disabled="updating" title="Để sau" @click="dismiss">✕</button>
    </div>

    <!-- Đã là bản mới nhất (chỉ hiện khi bấm kiểm tra thủ công) -->
    <div v-else-if="mode === 'uptodate'" class="update-banner neutral">
      <span class="up-ico">✓</span>
      <div class="up-text">
        <b>Bạn đang dùng bản mới nhất</b>
        <span class="up-sub">Phiên bản hiện tại: v{{ current }}</span>
      </div>
      <button class="up-close" title="Đóng" @click="dismiss">✕</button>
    </div>

    <!-- Lỗi -->
    <div v-else-if="mode === 'error'" class="update-banner danger">
      <span class="up-ico">⚠</span>
      <div class="up-text">
        <b>{{ errorMsg }}</b>
        <span class="up-sub">Thử lại hoặc tải thủ công từ GitHub Releases.</span>
      </div>
      <button class="up-close" title="Đóng" @click="dismiss">✕</button>
    </div>
  </transition>
</template>

<style scoped>
.update-banner {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  margin: 0 0 14px;
  border-radius: 10px;
  background: linear-gradient(90deg, #2563eb, #3b82f6);
  color: #fff;
  box-shadow: 0 6px 20px rgba(37, 99, 235, 0.35);
}
.update-banner.neutral {
  background: var(--panel-2, #1b2230);
  color: var(--text, #e6edf3);
  border: 1px solid var(--border, #30363d);
  box-shadow: none;
}
.update-banner.danger {
  background: linear-gradient(90deg, #b91c1c, #dc2626);
  box-shadow: 0 6px 20px rgba(220, 38, 38, 0.3);
}
.up-ico {
  font-size: 20px;
  line-height: 1;
}
.up-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}
.up-text b {
  font-weight: 600;
}
.up-sub {
  font-size: 12px;
  opacity: 0.85;
}
.up-btn {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  background: #fff;
  color: #1d4ed8;
  border: none;
  border-radius: 8px;
  padding: 8px 16px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
}
.up-btn:disabled {
  opacity: 0.75;
  cursor: default;
}
.up-close {
  background: transparent;
  border: none;
  color: currentColor;
  opacity: 0.8;
  cursor: pointer;
  font-size: 14px;
  padding: 4px 6px;
}
.up-close:disabled {
  opacity: 0.4;
  cursor: default;
}
.spin {
  width: 13px;
  height: 13px;
  border: 2px solid rgba(29, 78, 216, 0.35);
  border-top-color: #1d4ed8;
  border-radius: 50%;
  animation: up-spin 0.7s linear infinite;
}
@keyframes up-spin {
  to { transform: rotate(360deg); }
}
.slide-enter-active,
.slide-leave-active {
  transition: all 0.25s ease;
}
.slide-enter-from,
.slide-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
