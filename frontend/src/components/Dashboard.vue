<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { GetMetrics, ExportReport, ListPeople, RevealInFileManager } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { monthStart, addMonths, ymKey, monthLabel, parseISODate, fmtDM, todayISO } from '../lib/date'

const month = ref(monthStart(new Date()))
const data = ref(null)
const error = ref('')
const people = ref([])
const assigneeId = ref(0) // 0 = toàn team
// "Ngày tính": chỉ đếm task có Done date ≤ ngày này (mặc định hôm nay).
// Chỉnh về cuối tháng để mô phỏng PI chốt sổ với các task kế hoạch.
const asOf = ref(todayISO())

// Ngày tính ở dạng Date (00:00 local) — thay cho new Date() trong hiển thị.
const simNow = computed(() => parseISODate(asOf.value) || new Date())

async function load() {
  error.value = ''
  try {
    data.value = await GetMetrics(ymKey(month.value), assigneeId.value, asOf.value)
  } catch (e) {
    error.value = String(e)
  }
}
// Đồng bộ realtime: client khác sửa dữ liệu workspace → backend bắn
// "tasks:changed" → nạp lại số liệu tại chỗ (giữ nguyên tháng/bộ lọc đang xem).
let stopLiveSync = null
onMounted(async () => {
  try {
    // Bỏ người quan sát khỏi ô chọn nhân sự: họ KHÔNG tính vào chỉ số (xem
    // MetricsService.Compute) nên chọn họ chỉ ra một bảng số 0, và họ cũng
    // không xuất hiện ở bảng so sánh thành viên của TeamMetrics.
    // Ô select này còn truyền assigneeId cho ExportReport, nên lọc ở đây cũng
    // là lọc luôn danh sách người xuất được báo cáo Excel/PDF cá nhân.
    people.value = (await ListPeople()).filter(p => !p.observer)
  } catch (e) {
    error.value = String(e)
  }
  await load()
  stopLiveSync = EventsOn('tasks:changed', load)
})
onUnmounted(() => stopLiveSync && stopLiveSync())

const assigneeName = computed(() => {
  const p = people.value.find(p => p.ID === assigneeId.value)
  return p ? p.Name : null
})

function shift(n) {
  month.value = n === 0 ? monthStart(new Date()) : addMonths(month.value, n)
  load()
}

// ---- Xuất báo cáo ----
const showExport = ref(false)
const exporting = ref(false)
const exportMsg = ref('')
// Đường dẫn file vừa xuất — nút "Mở thư mục" trên toast trỏ đúng vào file này.
const exportPath = ref('')
let exportTimer = null

async function doExport(fmtType) {
  showExport.value = false
  exporting.value = true
  error.value = ''
  try {
    const path = await ExportReport(ymKey(month.value), fmtType, asOf.value, assigneeId.value)
    if (path) {
      exportPath.value = path
      exportMsg.value = `✓ Đã xuất báo cáo: ${path}`
      // Hẹn giờ mới thay hẹn giờ cũ: xuất liên tiếp hai file thì toast của file
      // sau không bị hẹn giờ của file trước tắt sớm.
      clearTimeout(exportTimer)
      exportTimer = setTimeout(() => {
        exportMsg.value = ''
        exportPath.value = ''
      }, 15000)
    }
  } catch (e) {
    error.value = String(e)
  } finally {
    exporting.value = false
  }
}

// Mở trình quản lý tệp tại file vừa xuất. Lỗi (thiếu file manager, file đã bị
// xóa) hiện ở dòng lỗi chung chứ không nuốt im lặng.
async function openExportFolder() {
  try {
    await RevealInFileManager(exportPath.value)
  } catch (e) {
    error.value = String(e)
  }
}

const m = computed(() => data.value?.metrics)
const advice = computed(() => data.value?.advice)
const st = computed(() => data.value?.settings)

const fmt = (v, digits = 2) => (v ?? 0).toFixed(digits)

const piPct = computed(() => {
  if (!m.value || !st.value?.PITarget) return 0
  return Math.min((m.value.PI / st.value.PITarget) * 100, 100)
})
const achieved = computed(() => advice.value?.Achieved)

const TIPS = {
  T: 'Throughput TÍCH LŨY — số task Done cộng dồn trong tháng chia cho độ dài cả tháng (tháng chuẩn = 4 tuần). Đầu tháng thấp, tăng dần khi hoàn thành thêm task. Đo LƯỢNG công việc: càng cao càng tốt, so với baseline team = T_baseline × số người.',
  P: 'Điểm/tháng TÍCH LŨY — tổng điểm size của task Done trong tháng (S=1, M=3, L=6, XL=9) chia cho độ dài cả tháng. Bổ trợ cho Throughput vốn đếm mọi task như nhau: người làm ít task nhưng task lớn vẫn được ghi nhận đúng khối lượng. Bug không tính; KHÔNG tham gia công thức PI. Baseline = điểm baseline/người × số người.',
  CT: 'Cycle Time — số ngày làm trung bình 1 task, tính từ Start date đến Done date, trừ thời gian blocked. Đo TỐC ĐỘ xử lý từng task: càng thấp càng tốt.',
  LT: 'Lead Time — trung bình từ NGÀY TẠO task đến khi Done, gồm cả thời gian nằm chờ trong backlog. Phản ánh khách/PM phải chờ bao lâu từ lúc yêu cầu đến lúc nhận được.',
  WIP: 'Work In Progress — số task đang In Progress hoặc Blocked ngay lúc này. WIP cao kéo dài là dấu hiệu tắc nghẽn hoặc đa nhiệm quá mức: nên hoàn thành bớt trước khi nhận thêm.',
  BUG: 'Bug (Phát sinh) được bóc tách riêng: KHÔNG tính vào Throughput, Cycle Time và PI của task. T_bug = bug Done cộng dồn ÷ độ dài cả tháng; CT_bug = trung bình (Done − Start − blocked) của bug. Tỷ lệ bug/task = bug Done ÷ task Done. "Nguồn gốc" = bug quy về task gốc qua liên kết "Task gốc sinh ra bug" — đếm mọi bug đã liên kết tới task Done tháng này, kể cả bug chưa fix hoặc fix ở tháng khác. Cả hai tỷ lệ càng thấp càng tốt.',
}

const windowText = computed(() => {
  if (!m.value) return ''
  const start = parseISODate(m.value.MonthStart)
  const end = parseISODate(m.value.MonthEnd)
  const last = new Date(end.getFullYear(), end.getMonth(), end.getDate() - 1)
  let s = `Cửa sổ tính: ${fmtDM(start)} – ${fmtDM(last)}/${last.getFullYear()} · ${m.value.DoneCount} task Done tính đến ${fmtDM(simNow.value)}`
  if (simNow.value >= start && simNow.value < end) {
    s += ` · đã qua ${m.value.ElapsedWeeks.toFixed(1)}/${m.value.FullWeeks.toFixed(1)} tuần`
  }
  return s
})

// ---- Khối "Đạt mục tiêu PI?" ----
const lastDayLabel = computed(() => {
  if (!m.value) return ''
  const end = parseISODate(m.value.MonthEnd)
  return fmtDM(new Date(end.getFullYear(), end.getMonth(), end.getDate() - 1))
})

// Thời gian còn lại của tháng, tính từ "Ngày tính".
const remainingWeeks = computed(() => {
  if (!m.value) return 0
  const end = parseISODate(m.value.MonthEnd)
  return Math.max((end - simNow.value) / 86400000 / 7, 0)
})

// Hướng 1: throughput (giữ CT) — dùng cho cả 2 trạng thái:
// chưa đạt → mức cần tăng thêm; đã đạt → biên độ an toàn còn lại.
const optThroughput = computed(() => {
  if (!m.value || !advice.value || advice.value.RequiredThroughput <= 0) return null
  const cur = m.value.Throughput
  const req = advice.value.RequiredThroughput
  const max = Math.max(cur, req)
  const paceOK = remainingWeeks.value >= 0.5 && advice.value.AdditionalInMonth > 0
  return {
    cur, req,
    curPct: (cur / max) * 100,
    reqPct: (req / max) * 100,
    incPct: cur > 0 ? ((req - cur) / cur) * 100 : 0,
    addTasks: advice.value.AdditionalInMonth,
    pace: paceOK ? advice.value.AdditionalInMonth / remainingWeeks.value : null,
    // Biên độ khi đã đạt: có thể giảm bao nhiêu mà vẫn giữ mục tiêu
    slack: cur - req,
    slackPct: req > 0 ? ((cur - req) / req) * 100 : 0,
  }
})

// Hướng 2: cycle time (giữ T) — hiển thị theo ngày/task cho dễ hiểu.
const optCycleTime = computed(() => {
  if (!m.value || !advice.value || advice.value.RequiredCycleTime <= 0) return null
  const cur = m.value.CycleTime
  const req = advice.value.RequiredCycleTime
  const max = Math.max(cur, req)
  return {
    cur, req,
    curPct: (cur / max) * 100,
    reqPct: (req / max) * 100,
    cutDays: cur - req,
    cutPct: cur > 0 ? ((cur - req) / cur) * 100 : 0,
    // Biên độ khi đã đạt: CT được phép tăng thêm bao nhiêu
    slackDays: req - cur,
    slackPct: cur > 0 ? ((req - cur) / cur) * 100 : 0,
  }
})

// Delta so với baseline cho stat card (T cao hơn = tốt, CT thấp hơn = tốt)
const tDelta = computed(() => {
  if (!m.value || !m.value.TeamTBaseline) return null
  const pct = ((m.value.Throughput - m.value.TeamTBaseline) / m.value.TeamTBaseline) * 100
  return { pct, good: pct >= 0 }
})
const ctDelta = computed(() => {
  if (!m.value || !st.value?.CTBaseline || m.value.CycleTime <= 0) return null
  const pct = ((m.value.CycleTime - st.value.CTBaseline) / st.value.CTBaseline) * 100
  return { pct, good: pct <= 0 }
})
const pDelta = computed(() => {
  if (!m.value || !m.value.TeamPointBaseline) return null
  const pct = ((m.value.PointsPerMonth - m.value.TeamPointBaseline) / m.value.TeamPointBaseline) * 100
  return { pct, good: pct >= 0 }
})
// Tiến độ điểm/tháng hướng tới baseline, kẹp 100% để vẽ thanh.
const pProgress = computed(() => {
  if (!m.value || !m.value.TeamPointBaseline) return null
  return Math.min((m.value.PointsPerMonth / m.value.TeamPointBaseline) * 100, 100)
})

// Độ sát của estimate AI so với EFFORT THỰC TẾ nhập tay (chính xác nhất —
// chỉ so trên các task đã nhập effort, estimate lấy đúng các task đó).
const effortAccuracy = computed(() => {
  if (!m.value || !m.value.ActualEffortCount || m.value.EstAIPairedTotal <= 0) return null
  const actual = m.value.ActualEffortTotal
  const est = m.value.EstAIPairedTotal
  const pct = ((actual - est) / est) * 100
  let level, text
  if (Math.abs(pct) <= 20) {
    level = 'ok'
    text = `✓ Estimate AI sát effort thực tế (lệch ${pct >= 0 ? '+' : ''}${pct.toFixed(0)}% trên ${m.value.ActualEffortCount} task đã nhập effort).`
  } else if (pct > 0) {
    level = 'warn'
    text = `⚠ Effort thực tế ${actual.toFixed(1)} ngày cao hơn estimate AI ${est.toFixed(1)} ngày — estimate đang LẠC QUAN ${pct.toFixed(0)}% (×${(actual / est).toFixed(2)}, trên ${m.value.ActualEffortCount} task đã nhập effort). Cần estimate dè dặt hơn.`
  } else {
    level = 'warn'
    text = `⚠ Effort thực tế ${actual.toFixed(1)} ngày thấp hơn estimate AI ${est.toFixed(1)} ngày ${Math.abs(pct).toFixed(0)}% (trên ${m.value.ActualEffortCount} task đã nhập effort) — estimate đang dè dặt quá mức.`
  }
  return { pct, level, text }
})

// Độ sát của estimate AI so với cycle thực (task Done trong tháng)
const estAccuracy = computed(() => {
  if (!m.value || m.value.EstAITotal <= 0 || m.value.ActualCycleTotal <= 0) return null
  const actual = m.value.ActualCycleTotal
  const est = m.value.EstAITotal
  const pct = ((actual - est) / est) * 100 // dương = thực tế LÂU hơn estimate
  let level, text
  if (Math.abs(pct) <= 20) {
    level = 'ok'
    text = `✓ Estimate AI sát thực tế (lệch ${pct >= 0 ? '+' : ''}${pct.toFixed(0)}%).`
  } else if (pct > 0) {
    level = 'warn'
    text = `⚠ Cycle thực ${actual.toFixed(1)} ngày cao hơn estimate AI ${est.toFixed(1)} ngày — estimate đang LẠC QUAN ${pct.toFixed(0)}% (×${(actual / est).toFixed(2)}). Cần estimate dè dặt hơn hoặc kiểm tra lại Start/Done date.`
  } else {
    level = 'warn'
    text = `⚠ Cycle thực ${actual.toFixed(1)} ngày thấp hơn estimate AI ${est.toFixed(1)} ngày ${Math.abs(pct).toFixed(0)}% — estimate đang dè dặt quá mức.`
  }
  return { pct, level, text }
})

// ROI ứng dụng AI: tỉ lệ áp dụng + so tốc độ (cycle time) nhóm dùng AI vs không AI.
const aiRoi = computed(() => {
  if (!m.value || !m.value.DoneCount) return null
  const mm = m.value
  const adoptPct = (mm.AIUsedCount / mm.DoneCount) * 100
  let speed = null
  if (mm.AICycleCount > 0 && mm.NonAICycleCount > 0 && mm.NonAICycleTime > 0) {
    const pct = (1 - mm.AICycleTime / mm.NonAICycleTime) * 100 // dương = AI nhanh hơn
    speed = {
      pct, faster: pct >= 0,
      ai: mm.AICycleTime, non: mm.NonAICycleTime,
      aiN: mm.AICycleCount, nonN: mm.NonAICycleCount,
    }
  }
  return { adoptPct, aiCount: mm.AIUsedCount, nonCount: mm.DoneCount - mm.AIUsedCount, speed }
})
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <div class="page-title">Dashboard {{ assigneeName ? '· ' + assigneeName : '· Toàn team' }}</div>
        <div class="page-sub">{{ windowText }}</div>
      </div>
      <div class="month-nav">
        <select v-model.number="assigneeId" class="person-select" @change="load" title="Xem theo nhân sự">
          <option :value="0">👥 Toàn team</option>
          <option v-for="p in people" :key="p.ID" :value="p.ID">{{ p.Name }}</option>
        </select>
        <label class="asof" title="Chỉ đếm task có Done date ≤ ngày này. Chọn cuối tháng để mô phỏng PI chốt sổ.">
          Ngày tính
          <input v-model="asOf" type="date" @change="load" />
        </label>
        <button class="btn icon" @click="shift(-1)">◀</button>
        <span class="label">{{ monthLabel(month) }}</span>
        <button class="btn icon" @click="shift(1)">▶</button>
        <button class="btn" @click="shift(0)">Tháng này</button>
        <div class="export-wrap" @mouseleave="showExport = false">
          <button class="btn primary" :disabled="exporting" @click="showExport = !showExport">
            {{ exporting ? 'Đang xuất…' : '⇩ Xuất báo cáo' }}
          </button>
          <div v-if="showExport" class="export-menu">
            <button @click="doExport('xlsx')">📊 Excel (.xlsx)</button>
            <button @click="doExport('pdf')">📄 PDF (.pdf)</button>
          </div>
        </div>
      </div>
    </div>

    <div v-if="error" class="err">{{ error }}</div>
    <div v-if="exportMsg" class="toast">
      <span class="toast-text">{{ exportMsg }}</span>
      <button v-if="exportPath" class="toast-action" @click="openExportFolder">📂 Mở thư mục</button>
    </div>

    <template v-if="m">
      <div class="stats-grid">
        <div class="stat">
          <div class="k">Throughput (T)<span class="info" :data-tip="TIPS.T">i</span></div>
          <div class="v-row">
            <span class="v">{{ fmt(m.Throughput) }}</span>
            <span class="unit">task/tháng</span>
            <span v-if="tDelta" class="delta" :class="tDelta.good ? 'good' : 'bad'">
              {{ tDelta.pct >= 0 ? '▲ +' : '▼ ' }}{{ tDelta.pct.toFixed(0) }}% vs baseline
            </span>
          </div>
          <div class="s">tích lũy = {{ m.DoneCount }} task ÷ {{ fmt(m.FullWeeks / 4) }} tháng chuẩn (cả tháng)</div>
          <div class="base">
            <span class="tag">Baseline</span>
            <span class="bv">{{ fmt(m.TeamTBaseline) }}</span>
            <span v-if="assigneeId">task/tháng (1 người)</span>
            <span v-else>= {{ fmt(st.TBaseline) }} task/người × {{ m.TeamSize }} người</span>
          </div>
        </div>
        <div class="stat">
          <div class="k">Điểm/tháng (P)<span class="info" :data-tip="TIPS.P">i</span></div>
          <div class="v-row">
            <span class="v">{{ fmt(m.PointsPerMonth) }}</span>
            <span class="unit">điểm/tháng</span>
            <span v-if="pDelta" class="delta" :class="pDelta.good ? 'good' : 'bad'">
              {{ pDelta.pct >= 0 ? '▲ +' : '▼ ' }}{{ pDelta.pct.toFixed(0) }}% vs baseline
            </span>
          </div>
          <div class="s">tích lũy = {{ fmt(m.DonePoints, 1) }} điểm ({{ m.DoneCount }} task · S=1 M=3 L=6 XL=9) ÷ {{ fmt(m.FullWeeks / 4) }} tháng chuẩn</div>
          <div
            v-if="pProgress !== null"
            class="stat-bar"
            :title="`${fmt(m.PointsPerMonth)} / ${fmt(m.TeamPointBaseline)} điểm/tháng (${pProgress.toFixed(0)}% baseline)`"
          >
            <div :style="{ width: pProgress + '%', background: pDelta?.good ? 'var(--green)' : 'var(--accent)' }"></div>
          </div>
          <div class="base">
            <span class="tag">Baseline</span>
            <span class="bv">{{ fmt(m.TeamPointBaseline) }}</span>
            <span v-if="assigneeId">điểm/tháng (1 người)</span>
            <span v-else>= {{ fmt(st.PointBaseline) }} điểm/người × {{ m.TeamSize }} người</span>
          </div>
        </div>
        <div class="stat">
          <div class="k">Cycle Time (CT)<span class="info" :data-tip="TIPS.CT">i</span></div>
          <div class="v-row">
            <span class="v">{{ fmt(m.CycleTime) }}</span>
            <span class="unit">ngày/task</span>
            <span v-if="ctDelta" class="delta" :class="ctDelta.good ? 'good' : 'bad'">
              {{ ctDelta.pct <= 0 ? '▼ ' : '▲ +' }}{{ ctDelta.pct.toFixed(0) }}% vs baseline
            </span>
          </div>
          <div class="s">trung bình (Done − Start − blocked) của task Done trong tháng</div>
          <div class="base">
            <span class="tag">Baseline</span>
            <span class="bv">{{ fmt(st.CTBaseline) }}</span>
            <span>ngày/task</span>
          </div>
        </div>
        <div class="stat">
          <div class="k">Lead Time (LT)<span class="info" :data-tip="TIPS.LT">i</span></div>
          <div class="v-row">
            <span class="v">{{ fmt(m.LeadTime) }}</span>
            <span class="unit">ngày</span>
          </div>
          <div class="s">từ ngày tạo → Done</div>
        </div>
        <div class="stat">
          <div class="k">WIP<span class="info" :data-tip="TIPS.WIP">i</span></div>
          <div class="v-row">
            <span class="v">{{ m.WIP }}</span>
            <span class="unit">task</span>
          </div>
          <div class="s">đang In Progress / Blocked</div>
        </div>
        <div class="stat">
          <div class="k">🐞 Bug<span class="info" :data-tip="TIPS.BUG">i</span></div>
          <div class="v-row">
            <span class="v">{{ m.BugDoneCount }}</span>
            <span class="unit">bug done</span>
            <span v-if="m.DoneCount > 0" class="delta" :class="m.BugRatio === 0 ? 'good' : 'bad'">
              {{ (m.BugRatio * 100).toFixed(0) }}% bug/task
            </span>
          </div>
          <div class="s">T = {{ fmt(m.BugThroughput) }} bug/tháng · CT = {{ fmt(m.BugCycleTime) }} ngày/bug</div>
          <div v-if="m.DoneCount > 0" class="s" :style="m.OriginBugCount ? 'color: var(--amber)' : ''">
            Nguồn gốc: task Done tháng này sinh {{ m.OriginBugCount }} bug ({{ (m.OriginBugRatio * 100).toFixed(0) }}%/task)
          </div>
          <div class="base"><span class="tag">Lưu ý</span><span>không tính vào T / CT / PI của task</span></div>
        </div>
      </div>

      <div class="card">
        <div class="card-title">Performance Index · PI = min((T / (T_baseline × số người)) × (CT_baseline / CT), capacity)</div>
        <div class="pi-hero">
          <div class="pi-num">
            {{ m.PI.toFixed(3) }}
            <span class="target">/ mục tiêu {{ st.PITarget.toFixed(2) }}</span>
            <span v-if="m.PICapped" class="target"> (chạm trần {{ st.Capacity }})</span>
          </div>
          <div class="pi-bar-wrap">
            <div class="pi-bar">
              <div :style="{ width: piPct + '%', background: achieved ? 'var(--green)' : 'var(--accent)' }"></div>
            </div>
            <div class="pi-meta"><span>0</span><span>mục tiêu {{ st.PITarget.toFixed(2) }}</span></div>
          </div>
          <span class="chip" :class="achieved ? 'ok' : 'bad'">
            {{ achieved ? '✓ Đã đạt' : '✗ Chưa đạt' }}
          </span>
        </div>
      </div>

      <div class="card">
        <div class="card-title">Đạt mục tiêu PI?</div>

        <!-- Chưa có dữ liệu -->
        <div v-if="m.DoneCount === 0 && !achieved" class="advice">
          ⚠️ Chưa có task Done nào trong tháng này nên PI = 0. Hoàn thành task (Start date, Done date, trạng thái Done) để bắt đầu đo.
        </div>

        <template v-else>
          <!-- Dải so sánh chung cho cả 2 trạng thái -->
          <div class="goal-summary">
            <div class="item">
              <div class="k">PI hiện tại</div>
              <div class="v" :style="{ color: achieved ? 'var(--green)' : 'var(--red)' }">{{ m.PI.toFixed(3) }}</div>
            </div>
            <div class="arrow">→</div>
            <div class="item">
              <div class="k">Mục tiêu</div>
              <div class="v">{{ advice.TargetPI.toFixed(2) }}</div>
            </div>
            <div class="item" v-if="achieved">
              <div class="k">Vượt mục tiêu</div>
              <div class="v" style="color: var(--green)">+{{ (m.PI - advice.TargetPI).toFixed(3) }} <span style="font-size: 13px; color: var(--text-faint)">(+{{ (((m.PI - advice.TargetPI) / advice.TargetPI) * 100).toFixed(0) }}%)</span></div>
            </div>
            <div class="item" v-else>
              <div class="k">Còn thiếu</div>
              <div class="v">{{ advice.GapPI.toFixed(3) }} <span style="font-size: 13px; color: var(--text-faint)">({{ ((advice.GapPI / advice.TargetPI) * 100).toFixed(0) }}%)</span></div>
            </div>
          </div>

          <div v-if="achieved" class="goal-achieved" style="margin-bottom: 12px">
            <span style="font-size: 20px">✓</span>
            <div>
              ĐÃ ĐẠT — duy trì nhịp độ hiện tại đến hết tháng. Bên dưới là biên độ an toàn: còn cách ngưỡng "tụt mục tiêu" bao xa.
            </div>
          </div>
          <p v-else class="hint" style="margin-bottom: 12px">
            Chọn một trong hai hướng dưới đây (hoặc kết hợp cả hai — mỗi hướng cải thiện một phần thì mục tiêu càng gần):
          </p>

          <div class="goal-grid">
            <div v-if="optThroughput" class="goal-option">
              <div class="head">{{ achieved ? 'Throughput · biên độ an toàn' : 'Hướng 1 · Hoàn thành nhiều task hơn' }}</div>
              <div class="sub">Giữ tốc độ hiện tại ({{ fmt(m.CycleTime) }} ngày/task){{ achieved ? '' : ', tăng số task Done' }}</div>

              <div class="cmp-row">
                <span class="who">Hiện tại</span>
                <div class="cmp-track"><div :style="{ width: optThroughput.curPct + '%', background: achieved ? 'var(--green)' : 'var(--gray)' }"></div></div>
                <span class="val">{{ fmt(optThroughput.cur) }} <span class="u">task/tháng</span></span>
              </div>
              <div class="cmp-row">
                <span class="who">{{ achieved ? 'Tối thiểu' : 'Cần đạt' }}</span>
                <div class="cmp-track"><div :style="{ width: optThroughput.reqPct + '%', background: achieved ? 'var(--gray)' : 'var(--green)' }"></div></div>
                <span class="val">{{ achieved ? '≥' : '' }} {{ fmt(optThroughput.req) }} <span class="u">task/tháng</span></span>
              </div>

              <div v-if="achieved" class="goal-action ok">
                ✓ Đang cao hơn ngưỡng tối thiểu <b>{{ fmt(optThroughput.slack) }} task/tháng</b> (+{{ optThroughput.slackPct.toFixed(0) }}%)
                <span class="note">Throughput có thể giảm tới mức này mà vẫn giữ được mục tiêu (nếu CT không đổi).</span>
              </div>
              <div v-else class="goal-action">
                → Hoàn thành thêm <b>{{ optThroughput.addTasks }} task Done</b> trước ngày {{ lastDayLabel }}
                (tăng throughput +{{ optThroughput.incPct.toFixed(0) }}%)
                <span v-if="optThroughput.pace" class="note">
                  Tương đương ~{{ optThroughput.pace.toFixed(1) }} task/tuần trong {{ remainingWeeks.toFixed(1) }} tuần còn lại của tháng.
                </span>
                <span v-else class="note">
                  Tháng chỉ còn {{ (remainingWeeks * 7).toFixed(0) }} ngày — nếu không kịp, đặt kế hoạch cho tháng sau theo nhịp {{ fmt(optThroughput.req) }} task/tháng.
                </span>
              </div>
            </div>

            <div v-if="optCycleTime" class="goal-option">
              <div class="head">{{ achieved ? 'Cycle Time · biên độ an toàn' : 'Hướng 2 · Làm nhanh hơn mỗi task' }}</div>
              <div class="sub">Giữ số task hiện tại ({{ fmt(m.Throughput) }} task/tháng){{ achieved ? '' : ', rút ngắn thời gian làm' }}</div>

              <div class="cmp-row">
                <span class="who">Hiện tại</span>
                <div class="cmp-track"><div :style="{ width: optCycleTime.curPct + '%', background: achieved ? 'var(--green)' : 'var(--gray)' }"></div></div>
                <span class="val">{{ fmt(optCycleTime.cur) }} <span class="u">ngày/task</span></span>
              </div>
              <div class="cmp-row">
                <span class="who">{{ achieved ? 'Tối đa' : 'Cần đạt' }}</span>
                <div class="cmp-track"><div :style="{ width: optCycleTime.reqPct + '%', background: achieved ? 'var(--gray)' : 'var(--green)' }"></div></div>
                <span class="val">≤ {{ fmt(optCycleTime.req) }} <span class="u">ngày/task</span></span>
              </div>

              <div v-if="achieved" class="goal-action ok">
                ✓ Còn dư địa <b>{{ fmt(optCycleTime.slackDays, 1) }} ngày/task</b> (+{{ optCycleTime.slackPct.toFixed(0) }}%)
                <span class="note">CT được phép tăng tới mức này mà vẫn giữ được mục tiêu (nếu throughput không đổi).</span>
              </div>
              <div v-else class="goal-action">
                → Rút ngắn trung bình mỗi task <b>{{ fmt(optCycleTime.cutDays, 1) }} ngày</b>
                (giảm {{ optCycleTime.cutPct.toFixed(0) }}%)
                <span class="note">
                  Gợi ý: tận dụng AI cho phần tốn thời gian (viết test, boilerplate, review), chia nhỏ task lớn, xử lý blocker sớm.
                </span>
              </div>
            </div>
          </div>
        </template>
      </div>

      <div class="card">
        <div class="card-title">Estimate vs thực tế (task Done trong tháng)</div>
        <div class="stats-grid" style="margin-bottom: 0">
          <div class="stat" style="border: none; padding: 4px 0">
            <div class="k">Est. báo khách</div>
            <div class="v">{{ fmt(m.EstCustomerTotal, 1) }} <span style="font-size:14px">ngày</span></div>
          </div>
          <div class="stat" style="border: none; padding: 4px 0">
            <div class="k">Est. làm bằng AI</div>
            <div class="v">{{ fmt(m.EstAITotal, 1) }} <span style="font-size:14px">ngày</span></div>
          </div>
          <div class="stat" style="border: none; padding: 4px 0">
            <div class="k">Effort thực tế</div>
            <div class="v-row">
              <span class="v">{{ fmt(m.ActualEffortTotal, 1) }} <span style="font-size:14px">ngày</span></span>
              <span v-if="effortAccuracy" class="delta" :class="effortAccuracy.level === 'ok' ? 'good' : 'bad'">
                {{ effortAccuracy.pct >= 0 ? '▲ +' : '▼ ' }}{{ effortAccuracy.pct.toFixed(0) }}% vs est AI
              </span>
            </div>
            <div class="s">nhập tay · {{ m.ActualEffortCount }}/{{ m.DoneCount }} task Done đã nhập</div>
          </div>
          <div class="stat" style="border: none; padding: 4px 0">
            <div class="k">Cycle thực</div>
            <div class="v-row">
              <span class="v">{{ fmt(m.ActualCycleTotal, 1) }} <span style="font-size:14px">ngày</span></span>
              <span v-if="!effortAccuracy && estAccuracy" class="delta" :class="estAccuracy.level === 'ok' ? 'good' : 'bad'">
                {{ estAccuracy.pct >= 0 ? '▲ +' : '▼ ' }}{{ estAccuracy.pct.toFixed(0) }}% vs est AI
              </span>
            </div>
            <div class="s">tổng (Done − Start − blocked)</div>
          </div>
          <div class="stat" style="border: none; padding: 4px 0">
            <div class="k">Tiết kiệm (est. khách − est. AI)</div>
            <div class="v" :style="{ color: m.SavedDays >= 0 ? 'var(--green)' : 'var(--red)' }">
              {{ fmt(m.SavedDays, 1) }} <span style="font-size:14px">ngày</span>
            </div>
          </div>
        </div>
        <div v-if="effortAccuracy" class="goal-action" :class="{ ok: effortAccuracy.level === 'ok' }" style="margin-top: 12px">
          {{ effortAccuracy.text }}
          <span class="note">So trên effort thực tế nhập tay — chính xác hơn cycle time (cycle gồm cả thời gian chờ).</span>
        </div>
        <div v-else-if="estAccuracy" class="goal-action" :class="{ ok: estAccuracy.level === 'ok' }" style="margin-top: 12px">
          {{ estAccuracy.text }}
          <span class="note">Đang so bằng cycle time (gồm cả thời gian chờ trong ngày làm việc) — nhập "Effort thực tế" khi Done task để so chính xác hơn.</span>
        </div>
        <div v-else class="hint" style="margin-top: 10px">
          Chưa đủ dữ liệu so sánh estimate AI với thực tế (cần task Done có estimate AI &gt; 0, kèm Start/Done date hoặc effort thực tế).
        </div>
      </div>

      <div v-if="aiRoi" class="card">
        <div class="card-title">ROI ứng dụng AI (task Done trong tháng)</div>
        <div class="stats-grid" style="margin-bottom: 0">
          <div class="stat" style="border: none; padding: 4px 0">
            <div class="k">Tỉ lệ áp dụng AI</div>
            <div class="v">{{ fmt(aiRoi.adoptPct, 0) }}<span style="font-size:14px">%</span></div>
            <div class="s">{{ aiRoi.aiCount }} dùng AI / {{ aiRoi.nonCount }} không AI</div>
          </div>
          <div class="stat" v-if="aiRoi.speed" style="border: none; padding: 4px 0">
            <div class="k">Cycle nhóm AI</div>
            <div class="v">{{ fmt(aiRoi.speed.ai, 1) }} <span style="font-size:14px">ngày</span></div>
            <div class="s">{{ aiRoi.speed.aiN }} task</div>
          </div>
          <div class="stat" v-if="aiRoi.speed" style="border: none; padding: 4px 0">
            <div class="k">Cycle nhóm không AI</div>
            <div class="v">{{ fmt(aiRoi.speed.non, 1) }} <span style="font-size:14px">ngày</span></div>
            <div class="s">{{ aiRoi.speed.nonN }} task</div>
          </div>
        </div>
        <div v-if="aiRoi.speed" class="goal-action" :class="{ ok: aiRoi.speed.faster }" style="margin-top: 12px">
          <template v-if="aiRoi.speed.faster">
            ✓ Task dùng AI hoàn thành NHANH hơn {{ fmt(aiRoi.speed.pct, 0) }}% so với không dùng AI
            ({{ fmt(aiRoi.speed.ai, 1) }} vs {{ fmt(aiRoi.speed.non, 1) }} ngày/task).
          </template>
          <template v-else>
            ⚠ Task dùng AI đang CHẬM hơn {{ fmt(-aiRoi.speed.pct, 0) }}% so với không dùng AI
            ({{ fmt(aiRoi.speed.ai, 1) }} vs {{ fmt(aiRoi.speed.non, 1) }} ngày/task) — kiểm tra lại cách ghi nhận.
          </template>
          <span class="note">So cycle time trung bình hai nhóm (chỉ task đủ Start/Done date). Cần cả hai nhóm có task để so.</span>
        </div>
        <div v-else class="hint" style="margin-top: 10px">
          Chưa đủ dữ liệu so tốc độ AI vs không AI — cần cả hai nhóm có task Done kèm Start/Done date.
        </div>
      </div>
    </template>
  </div>
</template>
