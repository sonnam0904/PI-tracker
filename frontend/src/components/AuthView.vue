<script setup>
import { ref } from 'vue'
import { Login, Register } from '../../wailsjs/go/main/App'

const emit = defineEmits(['logged-in'])

const mode = ref('login') // 'login' | 'register'
const username = ref('')
const password = ref('')
const confirm = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  error.value = ''
  if (!username.value.trim() || !password.value) {
    error.value = 'Nhập username và mật khẩu'
    return
  }
  if (mode.value === 'register' && password.value !== confirm.value) {
    error.value = 'Mật khẩu nhập lại không khớp'
    return
  }
  busy.value = true
  try {
    const fn = mode.value === 'login' ? Login : Register
    const session = await fn(username.value, password.value)
    emit('logged-in', session)
  } catch (e) {
    error.value = String(e)
  } finally {
    busy.value = false
  }
}

function switchMode(m) {
  mode.value = m
  error.value = ''
}
</script>

<template>
  <div class="auth-wrap">
    <div class="auth-card">
      <div class="brand" style="border: none; margin: 0 0 6px; padding: 0; justify-content: center">
        <div class="brand-badge">PI</div>
        <div>
          <div class="brand-name">PI Tracker</div>
          <div class="brand-sub">PI Tracker</div>
        </div>
      </div>

      <div class="view-toggle" style="align-self: center">
        <button :class="{ active: mode === 'login' }" @click="switchMode('login')">Đăng nhập</button>
        <button :class="{ active: mode === 'register' }" @click="switchMode('register')">Đăng ký</button>
      </div>

      <div v-if="error" class="err" style="margin: 0">{{ error }}</div>

      <div class="field">
        <label>Username</label>
        <input v-model="username" autocomplete="username" placeholder="Username" @keyup.enter="submit" />
      </div>
      <div class="field">
        <label>Mật khẩu</label>
        <input v-model="password" type="password" placeholder="Password" autocomplete="current-password" @keyup.enter="submit" />
      </div>
      <div v-if="mode === 'register'" class="field">
        <label>Nhập lại mật khẩu</label>
        <input v-model="confirm" type="password" @keyup.enter="submit" />
      </div>

      <button class="btn primary" style="justify-content: center" :disabled="busy" @click="submit">
        {{ busy ? 'Đang xử lý…' : mode === 'login' ? 'Đăng nhập' : 'Tạo tài khoản' }}
      </button>
    </div>
  </div>
</template>
