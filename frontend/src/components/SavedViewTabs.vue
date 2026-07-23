<script setup>
// Dãy tab view kiểu Lark trên trang Tasks: "Tất cả" + các bộ lọc user đã lưu.
// Chỉ quản lý tương tác tab (chọn / thêm / đổi tên / xóa / lưu thay đổi);
// dữ liệu view và bộ lọc hiện tại do GanttView sở hữu.
import { ref, nextTick } from 'vue'

const props = defineProps({
  views: { type: Array, default: () => [] }, // models.SavedView[]
  activeId: { type: Number, default: 0 },    // 0 = "Tất cả"
  dirty: { type: Boolean, default: false },  // bộ lọc hiện tại ≠ bộ lọc đã lưu của tab active
})
const emit = defineEmits(['select', 'create', 'update-filters', 'rename', 'remove'])

// Thêm view mới: "+ Lưu view" biến thành ô nhập tên.
const adding = ref(false)
const newName = ref('')
const addInput = ref(null)

async function startAdd() {
  adding.value = true
  newName.value = ''
  await nextTick()
  addInput.value?.focus()
}
function submitAdd() {
  if (!adding.value) return // Enter đã xử lý xong thì blur theo sau không bắn lại
  adding.value = false
  const name = newName.value.trim()
  if (name) emit('create', name)
}

// Đổi tên inline trên tab đang active. Ô input nằm trong v-for nên dùng
// function ref (ref thường trong v-for thành mảng, không .focus() được).
const renamingId = ref(0)
const renameText = ref('')
let renameEl = null

async function startRename(v) {
  renamingId.value = v.id
  renameText.value = v.name
  await nextTick()
  renameEl?.focus()
}
function submitRename(v) {
  if (renamingId.value !== v.id) return // Enter đã xử lý xong thì blur theo sau không bắn lại
  renamingId.value = 0
  const name = renameText.value.trim()
  if (name && name !== v.name) emit('rename', v, name)
}

// Xóa có xác nhận hiện rõ trên tab. Việc lưu KHÔNG cần nút đĩa: view vừa đổi
// là hiện ngay prompt "Lưu view thay đổi? [Lưu] [Hủy]" (dirty do cha tính).
const confirmRemoveId = ref(0)
function askRemove(v) {
  confirmRemoveId.value = v.id
}
function doRemove(v) {
  confirmRemoveId.value = 0
  emit('remove', v)
}
// Hủy thay đổi = nạp lại đúng bản đã lưu (chọn lại chính view đó → cha reset config).
function discard(v) {
  emit('select', v.id)
}

function select(id) {
  confirmRemoveId.value = 0
  renamingId.value = 0
  emit('select', id)
}
</script>

<template>
  <div class="view-tabs">
    <button class="vt-tab" :class="{ active: activeId === 0 }" @click="select(0)">Tất cả</button>

    <div
      v-for="v in views" :key="v.id"
      class="vt-tab" :class="{ active: activeId === v.id }"
      @click="activeId !== v.id && select(v.id)"
    >
      <input
        v-if="renamingId === v.id"
        :ref="el => (renameEl = el)"
        v-model="renameText"
        class="vt-input"
        @keyup.enter="submitRename(v)"
        @keyup.esc="renamingId = 0"
        @blur="submitRename(v)"
        @click.stop
      />
      <template v-else>
        <span class="vt-name" :title="v.name" @dblclick="activeId === v.id && startRename(v)">{{ v.name }}</span>

        <!-- Xác nhận xóa (ưu tiên cao nhất) -->
        <span v-if="confirmRemoveId === v.id" class="vt-confirm">
          <span class="vt-confirm-q">Xóa?</span>
          <button class="vt-cbtn danger" title="Xóa view này" @click.stop="doRemove(v)">Xóa</button>
          <button class="vt-cbtn" title="Giữ lại" @click.stop="confirmRemoveId = 0">Hủy</button>
        </span>

        <!-- View đang mở & có thay đổi: hỏi lưu ngay (không cần nút đĩa) -->
        <span v-else-if="activeId === v.id && dirty" class="vt-confirm">
          <span class="vt-confirm-q">Lưu view thay đổi?</span>
          <button class="vt-cbtn save" title="Ghi đè thay đổi vào view này" @click.stop="emit('update-filters', v)">Lưu</button>
          <button class="vt-cbtn" title="Hoàn tác, về bản đã lưu" @click.stop="discard(v)">Hủy</button>
        </span>

        <!-- Bình thường: đổi tên / xóa -->
        <span v-else-if="activeId === v.id" class="vt-actions">
          <button class="vt-btn" title="Đổi tên view" @click.stop="startRename(v)">✎</button>
          <button class="vt-btn" title="Xóa view" @click.stop="askRemove(v)">✕</button>
        </span>
      </template>
    </div>

    <div v-if="adding" class="vt-tab vt-new">
      <input
        ref="addInput"
        v-model="newName"
        class="vt-input"
        placeholder="Tên view…"
        @keyup.enter="submitAdd"
        @keyup.esc="adding = false"
        @blur="adding = false"
      />
    </div>
    <button v-else class="vt-add" title="Lưu bộ lọc hiện tại thành view mới" @click="startAdd">＋ Lưu view</button>
  </div>
</template>
