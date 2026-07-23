<script setup>
// Popover Sắp xếp / Nhóm kiểu Lark (dùng chung): danh sách field + nút đảo
// chiều (mũi tên đôi). Ghi trực tiếp vào mảng `items` do GanttView sở hữu.
import { ref, computed } from 'vue'
import { dirLabels } from '../lib/taskFields'
import { newOrderItem } from '../lib/taskFilters'

const props = defineProps({
  items: { type: Array, required: true },     // cfg.sorts hoặc cfg.groups
  fields: { type: Array, required: true },     // sortFields | groupFields
  label: { type: String, default: 'Sắp xếp' },
  icon: { type: String, default: '⇅' },
  title: { type: String, default: 'Sắp xếp theo' },
})

const open = ref(false)
const usedKeys = computed(() => props.items.map(o => o.field))
const remaining = computed(() => props.fields.filter(f => !usedKeys.value.includes(f.key)))

function add() {
  if (!remaining.value.length) return
  props.items.push(newOrderItem(props.fields, usedKeys.value))
}
function remove(i) {
  props.items.splice(i, 1)
}
function clearAll() {
  props.items.splice(0)
}
function setDir(o, dir) {
  o.dir = dir
}
// Đổi field trên một dòng: nếu trùng field đã dùng ở dòng khác thì bỏ qua.
function onFieldChange(o, i) {
  if (usedKeys.value.filter((k, idx) => idx !== i).includes(o.field)) {
    o.field = remaining.value[0]?.key || o.field
  }
}
</script>

<template>
  <div class="vp-wrap">
    <button class="vp-trigger" :class="{ active: open || items.length }" @click="open = !open">
      <span class="vp-ico">{{ icon }}</span> {{ label }}
      <span v-if="items.length" class="vp-badge">{{ items.length }}</span>
    </button>

    <template v-if="open">
      <div class="vp-backdrop" @click="open = false"></div>
      <div class="vp-pop" style="width: 460px">
        <div class="vp-title">{{ title }}</div>

        <div v-for="(o, i) in items" :key="i" class="cond-row">
          <select v-model="o.field" class="vp-sel" @change="onFieldChange(o, i)">
            <option v-for="f in fields" :key="f.key" :value="f.key">{{ f.label }}</option>
          </select>
          <div class="dir-toggle">
            <button :class="{ active: o.dir === 'asc' }" @click="setDir(o, 'asc')">{{ dirLabels(o.field).asc }}</button>
            <button :class="{ active: o.dir === 'desc' }" @click="setDir(o, 'desc')">{{ dirLabels(o.field).desc }}</button>
          </div>
          <button class="vp-x" title="Xóa" @click="remove(i)">✕</button>
        </div>

        <div v-if="!items.length" class="vp-empty">Chưa chọn field nào.</div>

        <div class="vp-foot">
          <button class="vp-add" :disabled="!remaining.length" @click="add">
            ＋ Thêm field<template v-if="!remaining.length"> (hết field)</template>
          </button>
          <button v-if="items.length" class="vp-clear" @click="clearAll">Xóa tất cả</button>
        </div>
      </div>
    </template>
  </div>
</template>
