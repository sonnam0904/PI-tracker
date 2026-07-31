<script setup>
// Thanh công cụ view kiểu Lark: tìm nhanh + Lọc / Sắp xếp / Nhóm (mỗi cái là
// một popover), kèm số task đang hiện và nút xóa toàn bộ cấu hình.
import { computed } from 'vue'
import { sortFields, groupFields } from '../lib/taskFields'
import { emptyConfig, isEmptyConfig } from '../lib/taskFilters'
import FilterPanel from './FilterPanel.vue'
import OrderPanel from './OrderPanel.vue'

const props = defineProps({
  cfg: { type: Object, required: true },
  names: { type: Object, default: () => ({}) },
  tags: { type: Array, default: () => [] }, // [{id, name}] cho ô chọn giá trị tag
  shown: { type: Number, default: 0 },
  total: { type: Number, default: 0 },
  groupable: { type: Boolean, default: true }, // nhóm chỉ có nghĩa ở bảng
})

const active = computed(() => !isEmptyConfig(props.cfg))

function clearAll() {
  Object.assign(props.cfg, emptyConfig())
}
</script>

<template>
  <div class="view-toolbar">
    <input v-model="cfg.q" class="filter-search" placeholder="🔍 Tìm theo tiêu đề / mô tả…" />

    <FilterPanel :cfg="cfg" :names="names" :tags="tags" />
    <OrderPanel :items="cfg.sorts" :fields="sortFields" label="Sắp xếp" icon="⇅" title="Sắp xếp theo" />
    <OrderPanel
      v-if="groupable"
      :items="cfg.groups" :fields="groupFields" label="Nhóm" icon="⊞" title="Nhóm bảng theo"
    />

    <button v-if="active" class="btn sm" @click="clearAll">✕ Xóa lọc</button>
    <span class="hint" style="margin-left: auto">{{ shown }}/{{ total }} task</span>
  </div>
</template>
