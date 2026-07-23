<script setup>
import { ref, computed, onMounted } from 'vue'
import { GetTeamMetrics, GetTeamTrend } from '../../wailsjs/go/main/App'
import { monthStart, addMonths, ymKey, monthLabel, todayISO } from '../lib/date'
import { buildPeopleMeta } from '../lib/people'

// Màu đường "Toàn team": trung tính, khác mọi màu nhân sự trong palette.
const TEAM_COLOR = '#e6edf3'

const month = ref(monthStart(new Date()))
const asOf = ref(todayISO())
const trendMonths = ref(6)
const data = ref(null) // TeamMetricsResult
const trend = ref(null) // TeamTrendResult
const error = ref('')

async function load() {
  error.value = ''
  try {
    ;[data.value, trend.value] = await Promise.all([
      GetTeamMetrics(ymKey(month.value), asOf.value),
      GetTeamTrend(ymKey(month.value), trendMonths.value, asOf.value),
    ])
  } catch (e) {
    error.value = String(e)
  }
}
onMounted(load)

function shift(n) {
  month.value = n === 0 ? monthStart(new Date()) : addMonths(month.value, n)
  load()
}

const st = computed(() => data.value?.settings)

// ---- Bảng so sánh tháng ----

// Meta màu/initials tính từ danh sách thành viên (backend đã sort theo tên,
// cùng thứ tự với ListPeople nên màu khớp Gantt/Kanban).
const peopleMeta = computed(() => {
  if (!data.value) return {}
  return buildPeopleMeta(data.value.members.map(m => ({ ID: m.assigneeId, Name: m.name })))
})

const fmt = (v, digits = 2) => (v ?? 0).toFixed(digits)

// Một dòng bảng: gộp team (assigneeId = 0) và từng thành viên về cùng shape.
const rows = computed(() => {
  if (!data.value) return []
  const mk = (id, name, m) => {
    const tBase = id === 0 ? m.TeamTBaseline : st.value.TBaseline
    return {
      id, name, m,
      isTeam: id === 0,
      tDeltaPct: tBase > 0 ? ((m.Throughput - tBase) / tBase) * 100 : null,
      ctDeltaPct: st.value.CTBaseline > 0 && m.CycleTime > 0
        ? ((m.CycleTime - st.value.CTBaseline) / st.value.CTBaseline) * 100
        : null,
      // TeamPointBaseline đã nhân theo scope (team = ×số người, cá nhân = ×1).
      pDeltaPct: m.TeamPointBaseline > 0
        ? ((m.PointsPerMonth - m.TeamPointBaseline) / m.TeamPointBaseline) * 100
        : null,
      aiPct: m.DoneCount > 0 ? (m.AIUsedCount / m.DoneCount) * 100 : null,
      effort: m.ActualEffortCount > 0 && m.EstAIPairedTotal > 0
        ? { actual: m.ActualEffortTotal, est: m.EstAIPairedTotal,
            pct: ((m.ActualEffortTotal - m.EstAIPairedTotal) / m.EstAIPairedTotal) * 100 }
        : null,
      achieved: st.value.PITarget > 0 && m.PI >= st.value.PITarget,
    }
  }
  return [
    mk(0, 'Toàn team', data.value.team),
    ...data.value.members.map(p => mk(p.assigneeId, p.name, p.metrics)),
  ]
})

// ---- Biểu đồ xu hướng ----

const METRICS = [
  { key: 'pi', label: 'PI', fmt: v => v.toFixed(2), lowerBetter: false },
  { key: 'throughput', label: 'Throughput (task/tháng)', fmt: v => v.toFixed(1) },
  { key: 'points', label: 'Điểm/tháng (theo size)', fmt: v => v.toFixed(1) },
  { key: 'cycleTime', label: 'Cycle Time (ngày/task)', fmt: v => v.toFixed(1), lowerBetter: true },
  { key: 'doneCount', label: 'Task Done', fmt: v => String(Math.round(v)) },
  { key: 'bugRatio', label: 'Tỷ lệ bug/task', fmt: v => (v * 100).toFixed(0) + '%' },
]
const metricKey = ref('pi')
const metric = computed(() => METRICS.find(o => o.key === metricKey.value))
const chartMode = ref('chart') // 'chart' | 'table' — bảng số phục vụ đọc chính xác/a11y

// Ẩn/hiện series bằng cách bấm legend (id 0 = toàn team).
const hidden = ref(new Set())
function toggleSeries(id) {
  const next = new Set(hidden.value)
  next.has(id) ? next.delete(id) : next.add(id)
  hidden.value = next
}

const series = computed(() => {
  if (!trend.value) return []
  const all = [
    { id: 0, name: 'Toàn team', color: TEAM_COLOR, team: true, points: trend.value.team.points },
    ...trend.value.members.map(s => ({
      id: s.assigneeId,
      name: s.name,
      color: peopleMeta.value[s.assigneeId]?.color || '#8b949e',
      initials: peopleMeta.value[s.assigneeId]?.initials || '?',
      points: s.points,
    })),
  ]
  return all.map(s => ({ ...s, off: hidden.value.has(s.id) }))
})
const visibleSeries = computed(() => series.value.filter(s => !s.off))

// Kích thước SVG (viewBox — co giãn theo khung ngoài).
const W = 920
const H = 280
const PAD = { l: 46, r: 76, t: 14, b: 28 }

const monthTicks = computed(() => (trend.value?.months || []).map(k => {
  const [y, m] = k.split('-')
  return `${m}/${y.slice(2)}`
}))

const yMax = computed(() => {
  let max = 0
  for (const s of visibleSeries.value) {
    for (const p of s.points) max = Math.max(max, p[metricKey.value] ?? 0)
  }
  if (metricKey.value === 'pi' && trend.value?.piTarget) {
    max = Math.max(max, trend.value.piTarget)
  }
  return (max || 1) * 1.12
})

const nPoints = computed(() => trend.value?.months.length || 0)
const x = i => PAD.l + (nPoints.value > 1 ? (i * (W - PAD.l - PAD.r)) / (nPoints.value - 1) : 0)
const y = v => PAD.t + (H - PAD.t - PAD.b) * (1 - v / yMax.value)

function linePath(s) {
  return s.points
    .map((p, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(p[metricKey.value] ?? 0).toFixed(1)}`)
    .join(' ')
}

const yTicks = computed(() => {
  const n = 4
  return Array.from({ length: n + 1 }, (_, i) => (yMax.value / n) * i)
})

const targetY = computed(() => {
  if (metricKey.value !== 'pi' || !trend.value?.piTarget) return null
  return y(trend.value.piTarget)
})
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <div class="page-title">Team · So sánh &amp; xu hướng</div>
        <div class="page-sub">
          Chỉ số từng thành viên tính trên baseline 1 người ({{ st ? fmt(st.TBaseline, 1) : '…' }} task/tháng)
          · cùng cửa sổ tháng và "ngày tính" như Dashboard
        </div>
      </div>
      <div class="month-nav">
        <label class="asof" title="Chỉ đếm task có Done date ≤ ngày này.">
          Ngày tính
          <input v-model="asOf" type="date" @change="load" />
        </label>
        <button class="btn icon" @click="shift(-1)">◀</button>
        <span class="label">{{ monthLabel(month) }}</span>
        <button class="btn icon" @click="shift(1)">▶</button>
        <button class="btn" @click="shift(0)">Tháng này</button>
      </div>
    </div>

    <div v-if="error" class="err">{{ error }}</div>

    <template v-if="data">
      <!-- Bảng so sánh tháng đang chọn -->
      <div class="card">
        <div class="card-title">So sánh thành viên · {{ monthLabel(month) }}</div>
        <div class="table-wrap" style="border: none">
          <table class="task-table team-table">
            <thead>
              <tr>
                <th>Thành viên</th>
                <th title="Task Done trong tháng (không tính bug)">Done</th>
                <th title="Throughput tích lũy, task/tháng — % so với baseline">T</th>
                <th title="Điểm/tháng theo size task (S=1, M=3, L=6, XL=9; bug không tính) — % so với baseline điểm">P</th>
                <th title="Cycle Time trung bình, ngày/task — % so với baseline (thấp hơn = tốt)">CT</th>
                <th title="Lead Time trung bình, ngày">LT</th>
                <th title="Task đang In Progress / Blocked">WIP</th>
                <th title="Bug Done trong tháng (tỷ lệ bug/task)">Bug</th>
                <th title="Tỷ lệ task Done có dùng AI">AI</th>
                <th title="Effort thực tế đã nhập so với estimate AI của chính các task đó">Effort vs Est AI</th>
                <th title="Performance Index so với mục tiêu">PI</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="r in rows" :key="r.id" :class="{ 'team-row': r.isTeam }">
                <td>
                  <span class="tt-assignee">
                    <span
                      class="avatar"
                      :style="r.isTeam
                        ? 'background: var(--panel-2); color: var(--text-dim)'
                        : { background: peopleMeta[r.id]?.color, color: '#fff' }"
                    >{{ r.isTeam ? '👥' : peopleMeta[r.id]?.initials }}</span>
                    <b>{{ r.name }}</b>
                  </span>
                </td>
                <td>{{ r.m.DoneCount }}</td>
                <td>
                  {{ fmt(r.m.Throughput, 1) }}
                  <span v-if="r.tDeltaPct !== null" class="delta" :class="r.tDeltaPct >= 0 ? 'good' : 'bad'">
                    {{ r.tDeltaPct >= 0 ? '+' : '' }}{{ r.tDeltaPct.toFixed(0) }}%
                  </span>
                </td>
                <td>
                  {{ fmt(r.m.PointsPerMonth, 1) }}
                  <span v-if="r.pDeltaPct !== null" class="delta" :class="r.pDeltaPct >= 0 ? 'good' : 'bad'">
                    {{ r.pDeltaPct >= 0 ? '+' : '' }}{{ r.pDeltaPct.toFixed(0) }}%
                  </span>
                </td>
                <td>
                  <template v-if="r.m.CycleTime > 0">
                    {{ fmt(r.m.CycleTime, 1) }}
                    <span v-if="r.ctDeltaPct !== null" class="delta" :class="r.ctDeltaPct <= 0 ? 'good' : 'bad'">
                      {{ r.ctDeltaPct >= 0 ? '+' : '' }}{{ r.ctDeltaPct.toFixed(0) }}%
                    </span>
                  </template>
                  <template v-else>—</template>
                </td>
                <td>{{ r.m.LeadTime > 0 ? fmt(r.m.LeadTime, 1) : '—' }}</td>
                <td>{{ r.m.WIP }}</td>
                <td>
                  {{ r.m.BugDoneCount }}
                  <span v-if="r.m.DoneCount > 0" class="delta" :class="r.m.BugRatio === 0 ? 'good' : 'bad'">
                    {{ (r.m.BugRatio * 100).toFixed(0) }}%
                  </span>
                </td>
                <td>{{ r.aiPct !== null ? r.aiPct.toFixed(0) + '%' : '—' }}</td>
                <td>
                  <template v-if="r.effort">
                    {{ fmt(r.effort.actual, 1) }} / {{ fmt(r.effort.est, 1) }} ngày
                    <span class="delta" :class="Math.abs(r.effort.pct) <= 20 ? 'good' : 'bad'">
                      {{ r.effort.pct >= 0 ? '+' : '' }}{{ r.effort.pct.toFixed(0) }}%
                    </span>
                  </template>
                  <template v-else><span class="hint">chưa nhập</span></template>
                </td>
                <td>
                  <b>{{ r.m.PI.toFixed(2) }}</b>
                  <span class="chip" :class="r.achieved ? 'ok' : 'bad'" style="margin-left: 6px">
                    {{ r.achieved ? '✓' : '✗' }} /{{ st.PITarget.toFixed(2) }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="hint" style="margin-top: 10px">
          PI cá nhân đo trên baseline 1 người — dùng để so mức đóng góp tương đối, không phải KPI tuyệt đối:
          xem cùng cột Bug và Effort để tránh thiên về tốc độ. Task chưa gán nhân sự chỉ tính vào dòng Toàn team.
        </p>
      </div>

      <!-- Xu hướng theo tháng -->
      <div class="card" v-if="trend">
        <div class="card-title" style="display: flex; align-items: center; gap: 10px; flex-wrap: wrap">
          Xu hướng theo tháng
          <select v-model="metricKey" class="person-select">
            <option v-for="o in METRICS" :key="o.key" :value="o.key">{{ o.label }}</option>
          </select>
          <select v-model.number="trendMonths" class="person-select" @change="load">
            <option :value="6">6 tháng</option>
            <option :value="12">12 tháng</option>
          </select>
          <div class="view-toggle" style="margin-left: auto">
            <button :class="{ active: chartMode === 'chart' }" @click="chartMode = 'chart'">📈 Biểu đồ</button>
            <button :class="{ active: chartMode === 'table' }" @click="chartMode = 'table'">☰ Bảng số</button>
          </div>
        </div>

        <!-- Legend: bấm để ẩn/hiện -->
        <div class="legend trend-legend">
          <button
            v-for="s in series" :key="s.id"
            class="legend-toggle" :class="{ off: s.off }"
            @click="toggleSeries(s.id)"
          >
            <span class="dot" :style="{ background: s.color }"></span>{{ s.name }}
          </button>
        </div>

        <svg
          v-if="chartMode === 'chart'"
          class="trend-chart" :viewBox="`0 0 ${W} ${H}`"
          role="img" :aria-label="`Xu hướng ${metric.label} theo tháng`"
        >
          <!-- Lưới + trục -->
          <g v-for="(t, i) in yTicks" :key="'y' + i">
            <line :x1="PAD.l" :x2="W - PAD.r" :y1="y(t)" :y2="y(t)" class="grid" />
            <text :x="PAD.l - 8" :y="y(t) + 4" class="tick" text-anchor="end">{{ metric.fmt(t) }}</text>
          </g>
          <text
            v-for="(lbl, i) in monthTicks" :key="'x' + i"
            :x="x(i)" :y="H - 8" class="tick" text-anchor="middle"
          >{{ lbl }}</text>

          <!-- Ngưỡng mục tiêu PI -->
          <g v-if="targetY !== null">
            <line :x1="PAD.l" :x2="W - PAD.r" :y1="targetY" :y2="targetY" class="target-line" />
            <text :x="W - PAD.r + 6" :y="targetY + 4" class="tick" style="fill: var(--amber)">
              mục tiêu {{ trend.piTarget.toFixed(2) }}
            </text>
          </g>

          <!-- Đường + điểm -->
          <g v-for="s in visibleSeries" :key="s.id">
            <path :d="linePath(s)" fill="none" :stroke="s.color" :stroke-width="s.team ? 3 : 2" stroke-linejoin="round" />
            <g v-for="(p, i) in s.points" :key="i">
              <circle :cx="x(i)" :cy="y(p[metricKey] ?? 0)" r="3.2" :fill="s.color" />
              <!-- vùng hover rộng hơn điểm, tooltip native -->
              <circle :cx="x(i)" :cy="y(p[metricKey] ?? 0)" r="10" fill="transparent">
                <title>{{ s.name }} · {{ monthTicks[i] }}: {{ metric.fmt(p[metricKey] ?? 0) }}</title>
              </circle>
            </g>
            <!-- nhãn cuối đường: nhận diện không phụ thuộc màu -->
            <text
              :x="x(s.points.length - 1) + 8" :y="y(s.points[s.points.length - 1]?.[metricKey] ?? 0) + 4"
              class="line-label"
            >{{ s.team ? 'Team' : s.initials }}</text>
          </g>
        </svg>

        <!-- Bảng số: đọc chính xác giá trị từng tháng -->
        <div v-else class="table-wrap" style="border: none">
          <table class="task-table team-table">
            <thead>
              <tr>
                <th>{{ metric.label }}</th>
                <th v-for="(lbl, i) in monthTicks" :key="i">{{ lbl }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in visibleSeries" :key="s.id">
                <td>
                  <span class="tt-assignee">
                    <span class="dot" :style="{ background: s.color }"></span><b>{{ s.name }}</b>
                  </span>
                </td>
                <td v-for="(p, i) in s.points" :key="i">{{ metric.fmt(p[metricKey] ?? 0) }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <p class="hint" style="margin-top: 8px">
          Mỗi tháng đã qua được chốt trọn tháng; tháng hiện tại tính đến "ngày tính".
          {{ metric.lowerBetter ? 'Với chỉ số này, đường đi XUỐNG là tốt.' : '' }}
        </p>
      </div>
    </template>
  </div>
</template>
