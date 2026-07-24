<script setup>
import { ref, computed, onMounted } from 'vue'
import { GetSettings, SaveSettings, ListPeople, InviteMember, GetSession, SetMemberLock, SetMemberObserver } from '../../wailsjs/go/main/App'
import { buildPeopleMeta } from '../lib/people'

const st = ref(null)
const people = ref([])
const session = ref(null)
const inviteName = ref('')
const inviteMsg = ref('')
const error = ref('')
const saved = ref(false)

const peopleMeta = computed(() => buildPeopleMeta(people.value))
// Số người THỰC SỰ tính vào chỉ số (bỏ observer) — dùng cho ghi chú công thức PI.
const countedPeople = computed(() => people.value.filter(p => !p.observer).length)

async function load() {
  error.value = ''
  try {
    session.value = await GetSession()
    st.value = await GetSettings()
    people.value = await ListPeople()
  } catch (e) {
    error.value = String(e)
  }
}
onMounted(load)

async function save() {
  error.value = ''
  saved.value = false
  try {
    await SaveSettings({
      ...st.value,
      TBaseline: Number(st.value.TBaseline),
      CTBaseline: Number(st.value.CTBaseline),
      PointBaseline: Number(st.value.PointBaseline),
      PITarget: Number(st.value.PITarget),
      Capacity: Number(st.value.Capacity),
    })
    saved.value = true
    setTimeout(() => (saved.value = false), 2500)
  } catch (e) {
    error.value = String(e)
  }
}

const isOwner = computed(() => session.value?.role === 'owner')

// Khóa/mở khóa thành viên (chỉ owner thấy nút): họ sẽ nhận notification.
async function toggleLock(p) {
  error.value = ''
  try {
    await SetMemberLock(p.ID, !p.locked)
    people.value = await ListPeople()
  } catch (e) {
    error.value = String(e)
  }
}

// Bật/tắt "chỉ quan sát": observer không tính vào baseline/chỉ số team.
async function toggleObserver(p) {
  error.value = ''
  try {
    await SetMemberObserver(p.ID, !p.observer)
    people.value = await ListPeople()
  } catch (e) {
    error.value = String(e)
  }
}

async function invite() {
  error.value = ''
  inviteMsg.value = ''
  if (!inviteName.value.trim()) return
  try {
    await InviteMember(inviteName.value.trim())
    inviteMsg.value = `✓ Đã gửi lời mời tới @${inviteName.value.trim()}`
    inviteName.value = ''
    setTimeout(() => (inviteMsg.value = ''), 6000)
  } catch (e) {
    error.value = String(e)
  }
}
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <div class="page-title">Cài đặt · {{ session?.workspaceName }}</div>
        <div class="page-sub">Baseline, mục tiêu PI và thành viên workspace</div>
      </div>
    </div>

    <div v-if="error" class="err">{{ error }}</div>

    <div class="card" v-if="st">
      <div class="card-title">Chỉ số &amp; Baseline (đơn vị THÁNG — 1 tháng chuẩn = 4 tuần)</div>
      <div class="form-grid">
        <div class="field">
          <label>T_baseline (task/tháng/người)</label>
          <input v-model="st.TBaseline" type="number" step="0.000000001" min="0" />
        </div>
        <div class="field">
          <label>CT_baseline (ngày/task)</label>
          <input v-model="st.CTBaseline" type="number" step="0.000000001" min="0" />
        </div>
        <div class="field">
          <label title="Điểm theo size task: S=1, M=3, L=6, XL=9. Không tham gia công thức PI.">
            Baseline Điểm/tháng (điểm/người)
          </label>
          <input v-model="st.PointBaseline" type="number" step="1" min="0" />
        </div>
        <div class="field">
          <label>Mục tiêu PI</label>
          <input v-model="st.PITarget" type="number" step="0.05" min="0" />
        </div>
        <div class="field">
          <label>Capacity — trần PI</label>
          <input v-model="st.Capacity" type="number" step="0.5" min="0" />
        </div>
        <div class="field full" style="align-items: flex-end">
          <button class="btn primary" @click="save">
            {{ saved ? '✓ Đã lưu' : 'Lưu cài đặt' }}
          </button>
        </div>
      </div>
      <p class="hint" style="margin-top: 14px">
        PI = min((T / (T_baseline × số người)) × (CT_baseline / CT), capacity) —
        throughput tăng hoặc cycle time giảm đều làm PI tăng.
        <b>Số người trong team = số thành viên được tính chỉ số (hiện tại: {{ countedPeople || 1 }})</b> —
        người "👁 quan sát" không được tính.
        Baseline mặc định: T = 4.454810496 task/tháng/người, CT = 6.560209424 ngày/task.
        Chỉ số tính theo tháng dương lịch (mùng 1 → hết tháng).
        <br />
        <b>Điểm/tháng</b> là chỉ số bổ trợ (không tham gia công thức PI): tổng điểm size của task Done
        trong tháng — S=1, M=3, L=6, XL=9 điểm; bug không tính. Baseline mặc định 24 điểm/người ≈ 4 task L/tháng.
      </p>
    </div>

    <div class="card">
      <div class="card-title">Thành viên workspace — người phụ trách task</div>

      <div class="people-add">
        <input
          v-model="inviteName"
          placeholder="Username muốn mời (họ sẽ nhận thông báo trong app)"
          style="background: var(--bg); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 8px 11px; outline: none"
          @keyup.enter="invite"
        />
        <button class="btn primary" @click="invite">Mời</button>
      </div>
      <div v-if="inviteMsg" class="goal-action ok" style="margin-top: 10px">{{ inviteMsg }}</div>

      <div v-for="p in people" :key="p.ID" class="people-row" :class="{ 'member-locked': p.locked }">
        <span class="name">
          <span class="avatar" :style="{ background: peopleMeta[p.ID].color, color: '#fff' }">
            {{ peopleMeta[p.ID].initials }}
          </span>
          {{ p.Name }}
          <span v-if="p.Name === session?.username" class="hint">(bạn)</span>
          <span v-if="p.locked" class="kb-tag overdue">🔒 đã khóa</span>
          <span v-if="p.observer" class="kb-tag" title="Không tính vào chỉ số team — chỉ quan sát/quản lý">👁 quan sát</span>
        </span>
        <span style="display: inline-flex; align-items: center; gap: 8px">
          <button
            v-if="isOwner"
            class="btn sm" :class="p.observer ? '' : 'ghost'"
            :title="p.observer ? 'Tính lại người này vào chỉ số team' : 'Đặt làm người quan sát/quản lý — không tính vào chỉ số team'"
            @click="toggleObserver(p)"
          >
            {{ p.observer ? '📊 Tính chỉ số' : '👁 Chỉ quan sát' }}
          </button>
          <button
            v-if="isOwner && p.role !== 'owner'"
            class="btn sm" :class="p.locked ? '' : 'ghost-danger'"
            :title="p.locked ? 'Cho phép thao tác trở lại trong workspace' : 'Chặn mọi thao tác của thành viên này trong workspace'"
            @click="toggleLock(p)"
          >
            {{ p.locked ? '🔓 Mở khóa' : '🔒 Khóa' }}
          </button>
          <span class="kb-tag">{{ p.role === 'owner' ? '👑 owner' : 'member' }}</span>
        </span>
      </div>
    </div>
  </div>
</template>
