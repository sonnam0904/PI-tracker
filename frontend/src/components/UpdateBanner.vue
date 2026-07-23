<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { CheckUpdate, ApplyUpdate } from '../../wailsjs/go/main/App'

const show = ref(false)
const latest = ref('')
const notes = ref('')
const updating = ref(false)
const error = ref('')

async function check() {
  try {
    const st = await CheckUpdate()
    if (st && st.available) {
      latest.value = st.latest
      notes.value = st.notes || ''
      show.value = true
    }
  } catch (e) {
    // Lỗi mạng/không có release: im lặng, không làm phiền người dùng.
  }
}

async function update() {
  updating.value = true
  error.value = ''
  try {
    // Thành công thì app tự khởi động lại → lời gọi này có thể không trở về.
    await ApplyUpdate()
  } catch (e) {
    error.value = 'Cập nhật thất bại: ' + String(e)
    updating.value = false
  }
}

function dismiss() {
  show.value = false
}

// Kiểm tra lúc mở app, rồi lặp lại mỗi 6 giờ khi app còn chạy.
let timer = null
onMounted(() => {
  check()
  timer = setInterval(check, 6 * 60 * 60 * 1000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <transition name="slide">
    <div v-if="show" class="update-banner">
      <span class="up-ico">⤴</span>
      <div class="up-text">
        <template v-if="!error">
          <b>Đã có phiên bản mới {{ latest }}</b>
          <span class="up-sub">Cập nhật để nhận tính năng và sửa lỗi mới nhất.</span>
        </template>
        <template v-else>
          <b>{{ error }}</b>
          <span class="up-sub">Thử lại hoặc tải thủ công từ GitHub Releases.</span>
        </template>
      </div>
      <button class="up-btn" :disabled="updating" @click="update">
        <span v-if="updating" class="spin"></span>
        {{ updating ? 'Đang cập nhật…' : 'Cập nhật ngay' }}
      </button>
      <button class="up-close" :disabled="updating" title="Để sau" @click="dismiss">✕</button>
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
  color: #fff;
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
