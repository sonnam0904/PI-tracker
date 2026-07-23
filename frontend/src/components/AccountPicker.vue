<script setup>
defineProps({
  accounts: { type: Array, default: () => [] }, // [{ token, userId, username }]
})
const emit = defineEmits(['pick', 'remove', 'login'])

// Màu avatar suy ra ổn định từ username (không phụ thuộc backend).
const PALETTE = ['#2f81f7', '#a371f7', '#d29922', '#39c5cf', '#f778ba', '#57ab5a', '#f0883e', '#6c7bf5', '#0aa2c0']
function color(name) {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0
  return PALETTE[h % PALETTE.length]
}
function initials(name) {
  return (name || '?').slice(0, 2).toUpperCase()
}
</script>

<template>
  <div class="auth-wrap">
    <div class="auth-card">
      <div class="brand" style="border: none; margin: 0 0 6px; padding: 0; justify-content: center">
        <div class="brand-badge">PI</div>
        <div>
          <div class="brand-name">PI Tracker</div>
          <div class="brand-sub">Chọn tài khoản</div>
        </div>
      </div>

      <div class="acct-list">
        <div v-for="a in accounts" :key="a.token" class="acct-row" @click="emit('pick', a)">
          <span class="avatar" :style="{ background: color(a.username), color: '#fff' }">{{ initials(a.username) }}</span>
          <span class="acct-name">@{{ a.username }}</span>
          <button
            class="acct-remove" title="Quên tài khoản này trên máy"
            @click.stop="emit('remove', a)"
          >×</button>
        </div>
      </div>

      <button class="btn primary" style="justify-content: center" @click="emit('login')">
        ＋ Đăng nhập tài khoản khác
      </button>
    </div>
  </div>
</template>
