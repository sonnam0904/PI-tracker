<script setup>
// Trang hướng dẫn cài plugin Claude Code chứa skill "bao-cao-thang".
// Thuần tĩnh: không gọi backend, vì việc cài plugin diễn ra trong Claude Code
// chứ không phải trong app này. Nhiệm vụ của trang là cho người dùng đúng hai
// lệnh cần gõ và giải thích những chỗ dễ hiểu sai.
import { ref } from 'vue'
import { ClipboardSetText } from '../../wailsjs/runtime'

const copied = ref('')

async function copy(key, text) {
  try {
    await ClipboardSetText(text)
  } catch {
    try { await navigator.clipboard.writeText(text) } catch { /* bỏ qua */ }
  }
  copied.value = key
  setTimeout(() => { if (copied.value === key) copied.value = '' }, 2000)
}

const REPO_URL = 'https://github.com/sonnam0904/PI-tracker'
// Ba tên độc lập nhau, phải khớp đúng manifest:
//   MARKETPLACE = field `name` trong .claude-plugin/marketplace.json
//   PLUGIN      = field `name` trong plugins/pi-tracker/.claude-plugin/plugin.json
// Skill goi bang /<plugin>:<skill> = /pi-tracker:bao-cao-thang 
// Cú pháp /plugin install luôn là <plugin>@<marketplace>, không đảo được.
const MARKETPLACE = 'pi-tracker'
const PLUGIN = 'pi-tracker'

const cmdAdd = `/plugin marketplace add sonnam0904/PI-tracker`
const cmdInstall = `/plugin install ${PLUGIN}@${MARKETPLACE}`
const cmdUpdate = `/plugin marketplace update ${MARKETPLACE}`
const cmdLocal = `/plugin marketplace add <đường dẫn thư mục repo trên máy>`
// Command của plugin, nên chỉ tồn tại SAU khi cài — vì vậy là bước 3, không phải bước 0.
const cmdSetup = `/${PLUGIN}:setup`

const steps = [
  {
    n: 1,
    title: 'Thêm marketplace từ GitHub',
    cmd: cmdAdd,
    key: 'add',
    desc: '',
  },
  {
    n: 2,
    title: 'Cài plugin',
    cmd: cmdInstall,
    key: 'install',
    desc: 'Sau khi cài, skill gọi bằng /pi-tracker:bao-cao-thang — tiền tố là tên plugin. Hoặc chỉ cần ' +
          'nhắc "làm báo cáo tháng" là Claude tự chọn skill này.',
  },
  {
    n: 3,
    title: 'Kiểm tra môi trường (chạy một lần)',
    cmd: cmdSetup,
    key: 'setup',
    desc: 'Dò python3 trên máy và hướng dẫn cài nếu thiếu. Cả 3 script của skill chạy bằng python3 — ' +
          'thiếu nó thì bước lấy dữ liệu không chạy được. Đã có python3 rồi thì lệnh này chỉ báo ' +
          '“sẵn sàng” rồi dừng, không cài gì.',
  },
]

const tools = [
  ['/plugin', 'Xem plugin đã cài, bật/tắt từng plugin.'],
  [cmdUpdate, 'Kéo bản mới nhất của marketplace về sau khi repo có commit mới.'],
  ['/plugin uninstall ' + PLUGIN + '@' + MARKETPLACE, 'Bỏ cài plugin.'],
]
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <div class="page-title">Claude Plugin</div>
        <div class="page-sub">
          Cài skill <b>bao-cao-thang</b> vào Claude Code để sinh báo cáo công việc hằng tháng
          từ dữ liệu PI Tracker — bản Excel ngắn gọn để trình bày và bản Markdown đầy đủ kèm nhận định.
        </div>
      </div>
    </div>

    <div class="card">
      <div class="card-title">Cần có trước</div>
      <ul class="pl-list">
        <li><b>Claude Code</b> đã đăng nhập (CLI, app desktop hoặc extension IDE).</li>
        <li>
          <b>MCP server của app này đang bật</b> — skill đọc task qua MCP. Sang trang
          <b>MCP</b> bấm “Bật server” và dán đoạn cấu hình vào client.
        </li>
        <li>
          <b>python3</b> trên máy. Không cần cài thêm thư viện nào — chưa có thì
          bước <b>3</b> bên dưới (<code>{{ cmdSetup }}</code>) sẽ hướng dẫn cài.
        </li>
      </ul>
    </div>

    <div class="card">
      <div class="card-title">Cài đặt — 3 lệnh, gõ trong Claude Code</div>

      <div v-for="s in steps" :key="s.key" class="pl-step">
        <div class="pl-step-head">
          <span class="pl-num">{{ s.n }}</span>
          <span class="pl-step-title">{{ s.title }}</span>
        </div>
        <div class="mcp-copy">
          <code class="mcp-code">{{ s.cmd }}</code>
          <button class="btn sm ghost" @click="copy(s.key, s.cmd)">
            {{ copied === s.key ? '✓ Đã chép' : 'Chép' }}
          </button>
        </div>
        <p class="hint pl-step-desc">{{ s.desc }}</p>
      </div>
    </div>

    <div class="card">
      <div class="card-title">Dùng skill</div>
      <ul class="pl-list">
        <li>Gọi trực tiếp: <code>/pi-tracker:bao-cao-thang</code></li>
        <li>Hoặc nói tự nhiên: “tổng hợp công việc tháng 7”, “xuất báo cáo excel theo giải pháp”.</li>
      </ul>
      <p class="hint" style="margin-top: 12px">
        Skill sinh ra hai file trong thư mục làm việc: <code>bao-cao-thang-&lt;MM&gt;-&lt;YYYY&gt;.xlsx</code>
        (số liệu, mỗi giải pháp một sheet) và <code>bao-cao-thang-&lt;MM&gt;-&lt;YYYY&gt;.md</code>
        (nhận định, xu hướng, rủi ro, khuyến nghị).
      </p>
      <p class="hint">
        Hạng mục công việc trong báo cáo lấy từ trường <b>Phân loại tag</b> của task — hãy gắn tag
        cho task trong app trước khi chạy, task chưa gắn tag sẽ bị gom vào nhóm “(chưa gắn tag)”.
      </p>
    </div>

    <div class="card">
      <div class="card-title">Cập nhật &amp; quản lý</div>
      <div class="mcp-tools">
        <div v-for="[cmd, desc] in tools" :key="cmd" class="mcp-tool">
          <code class="mcp-tool-name">{{ cmd }}</code>
          <span class="mcp-tool-desc">{{ desc }}</span>
        </div>
      </div>
      <p class="hint" style="margin-top: 14px">
        Đang tự sửa skill trên máy thì trỏ marketplace vào thư mục repo local để không phải push
        mỗi lần đổi: <code>{{ cmdLocal }}</code>
      </p>
    </div>

    <div class="card">
      <div class="card-title">Mã nguồn</div>
      <div class="mcp-copy">
        <code class="mcp-code">{{ REPO_URL }}</code>
        <button class="btn sm ghost" @click="copy('repo', REPO_URL)">
          {{ copied === 'repo' ? '✓ Đã chép' : 'Chép' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pl-list {
  margin: 6px 0 0;
  padding-left: 20px;
  font-size: 13px;
  line-height: 1.75;
  color: var(--text-dim);
}
.pl-list b { color: var(--text); }
/* Tên lệnh nhắc inline trong bullet. Không dùng .mcp-code được — nó là khối có
   viền và padding, nhét vào giữa câu sẽ phá dòng. Lấy màu accent cho khớp
   .mcp-tool-name, để mọi chỗ hiện tên lệnh đều đọc ra là "lệnh". */
.pl-list code { color: var(--accent); font-size: 12.5px; }

.pl-step { margin-top: 16px; }
.pl-step:first-of-type { margin-top: 8px; }
.pl-step-head {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-bottom: 7px;
}
.pl-num {
  flex: none;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 11.5px;
  font-weight: 700;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
.pl-step-title { font-size: 13px; font-weight: 600; }
.pl-step-desc { margin: 7px 0 0; }

.pl-warn {
  margin-top: 18px;
  padding: 11px 13px;
  border-left: 2px solid var(--accent);
  background: var(--accent-soft);
  border-radius: var(--radius-sm);
  font-size: 12.5px;
  line-height: 1.65;
  color: var(--text-dim);
}
.pl-warn > b { color: var(--text); display: block; margin-bottom: 3px; }
.pl-names {
  border-collapse: collapse;
  margin: 8px 0 10px;
}
.pl-names td {
  padding: 3px 14px 3px 0;
  vertical-align: baseline;
}
.pl-names td:first-child { white-space: nowrap; }
.pl-names b { color: var(--text); }
.pl-pos { color: var(--text-faint); font-size: 11.5px; }

/* Dùng lại .mcp-copy / .mcp-code / .mcp-tools từ McpView: hai trang cùng kiểu
   "khối lệnh + nút chép" nên giữ chung khai báo cho khỏi lệch nhau. */
.mcp-copy { display: flex; align-items: center; gap: 8px; }
.mcp-code {
  flex: 1; min-width: 0; overflow-x: auto; white-space: nowrap;
  background: var(--bg); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 8px 11px; font-size: 12.5px;
}
.mcp-tools { display: flex; flex-direction: column; gap: 2px; margin-top: 6px; }
.mcp-tool {
  display: flex; gap: 12px; align-items: baseline;
  padding: 7px 0; border-bottom: 1px solid var(--border);
}
.mcp-tool:last-child { border-bottom: none; }
.mcp-tool-name { color: var(--accent); font-size: 12.5px; flex: none; min-width: 210px; }
.mcp-tool-desc { font-size: 12.5px; color: var(--text-dim); }
</style>
