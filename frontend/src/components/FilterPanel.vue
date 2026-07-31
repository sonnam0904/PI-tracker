<script setup>
// Popover lọc động kiểu Lark: "Khớp [tất cả|bất kỳ] điều kiện" + danh sách
// điều kiện (field · toán tử · giá trị). Ghi trực tiếp vào cfg do GanttView sở hữu.
import { ref } from 'vue'
import { filterFields, opsFor, opDef, FIELD_BY_KEY } from '../lib/taskFields'
import { newCondition } from '../lib/taskFilters'

const props = defineProps({
  cfg: { type: Object, required: true },
  names: { type: Object, default: () => ({}) }, // userID → tên
  // Tag của workspace. Options của field 'tags' đến từ dữ liệu lúc chạy, không
  // nằm trong registry — cùng lý do như 'person' nhận `names`.
  tags: { type: Array, default: () => [] },
})

const open = ref(false)

function add() {
  props.cfg.conditions.push(newCondition())
}
function remove(i) {
  props.cfg.conditions.splice(i, 1)
}
function clearAll() {
  props.cfg.conditions.splice(0)
}

// Đổi field → reset toán tử về cái đầu hợp lệ và xóa giá trị (type có thể khác).
function onFieldChange(c) {
  c.op = opsFor(c.field)[0].value
  c.value = ''
}

const fieldType = c => FIELD_BY_KEY[c.field]?.type
const fieldOptions = c => FIELD_BY_KEY[c.field]?.options || []
const needsInput = c => !opDef(c.field, c.op)?.noInput
</script>

<template>
  <div class="vp-wrap">
    <button class="vp-trigger" :class="{ active: open || cfg.conditions.length }" @click="open = !open">
      <span class="vp-ico">⧩</span> Lọc
      <span v-if="cfg.conditions.length" class="vp-badge">{{ cfg.conditions.length }}</span>
    </button>

    <template v-if="open">
      <div class="vp-backdrop" @click="open = false"></div>
      <div class="vp-pop" style="width: 540px">
        <div class="vp-title">Lọc theo điều kiện</div>

        <div v-if="cfg.conditions.length" class="vp-match">
          Khớp
          <select v-model="cfg.match" class="vp-sel narrow">
            <option value="all">tất cả</option>
            <option value="any">bất kỳ</option>
          </select>
          điều kiện dưới đây
        </div>

        <div v-for="(c, i) in cfg.conditions" :key="i" class="cond-row">
          <select v-model="c.field" class="vp-sel" @change="onFieldChange(c)">
            <option v-for="f in filterFields" :key="f.key" :value="f.key">{{ f.label }}</option>
          </select>
          <select v-model="c.op" class="vp-sel narrow">
            <option v-for="o in opsFor(c.field)" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>

          <!-- Ô giá trị đổi theo type; toán tử trống/không-trống thì ẩn -->
          <span v-if="!needsInput(c)" class="cond-noinput">—</span>
          <template v-else>
            <select v-if="fieldType(c) === 'select'" v-model="c.value" class="vp-sel">
              <option value="">Chọn…</option>
              <option v-for="o in fieldOptions(c)" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
            <select v-else-if="fieldType(c) === 'person'" v-model.number="c.value" class="vp-sel">
              <option value="">Chọn…</option>
              <option :value="0">Chưa gán</option>
              <option v-for="(name, id) in names" :key="id" :value="Number(id)">{{ name }}</option>
            </select>
            <select v-else-if="fieldType(c) === 'bool'" v-model="c.value" class="vp-sel">
              <option value="">Chọn…</option>
              <option value="yes">Có</option>
              <option value="no">Không</option>
            </select>
            <select v-else-if="fieldType(c) === 'tags'" v-model="c.value" class="vp-sel">
              <option value="">Chọn tag…</option>
              <option v-for="tg in tags" :key="tg.id" :value="tg.name">{{ tg.name }}</option>
            </select>
            <select v-else-if="fieldType(c) === 'duestate'" v-model="c.value" class="vp-sel">
              <option value="">Chọn…</option>
              <option v-for="o in fieldOptions(c)" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
            <input v-else-if="fieldType(c) === 'date'" v-model="c.value" type="date" class="vp-inp" />
            <input v-else-if="fieldType(c) === 'number'" v-model.number="c.value" type="number" class="vp-inp" placeholder="Giá trị…" />
            <input v-else v-model="c.value" class="vp-inp" placeholder="Nhập giá trị…" />
          </template>

          <button class="vp-x" title="Xóa điều kiện" @click="remove(i)">✕</button>
        </div>

        <div v-if="!cfg.conditions.length" class="vp-empty">Chưa có điều kiện — thêm để lọc task.</div>

        <div class="vp-foot">
          <button class="vp-add" @click="add">＋ Thêm điều kiện</button>
          <button v-if="cfg.conditions.length" class="vp-clear" @click="clearAll">Xóa tất cả</button>
        </div>
      </div>
    </template>
  </div>
</template>
