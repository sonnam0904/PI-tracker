<script setup>
import { ref, computed, onMounted } from 'vue'
import { MCPStatus, MCPTools, StartMCPServer, StopMCPServer } from '../../wailsjs/go/main/App'
import { ClipboardSetText } from '../../wailsjs/runtime'

// Trạng thái MCP server (running/url/token/port) — nguồn chân lý từ backend.
const status = ref({ running: false, url: '', token: '', port: 0 })
const error = ref('')
const busy = ref(false)
const copied = ref('') // key vừa copy để hiện "✓ Đã chép" tạm thời

// Danh sách công cụ ĐỌC TỪ BACKEND (MCPTools) để không bao giờ liệt kê thiếu khi
// thêm tool mới — bản hardcode trước đây đã bị lệch, thiếu list_tags.
const tools = ref([])

// Mô tả ngắn dành cho người đọc. Mô tả trong mcp.go viết cho LLM nên dài và
// nhiều ràng buộc; ở đây ưu tiên bản ngắn, tool nào chưa có thì dùng luôn mô tả
// của backend (vẫn hiện đủ, chỉ dài hơn).
const SHORT_DESC = {
  get_session: 'Xem phiên đang đăng nhập (user + workspace) mà công cụ thao tác dưới danh nghĩa.',
  list_people: 'Danh sách thành viên workspace — lấy id để gán người phụ trách.',
  list_tasks: 'Liệt kê toàn bộ task của workspace hiện tại.',
  get_task: 'Chi tiết đầy đủ: thông tin task, checklist, bình luận + hoạt động, lịch sử trạng thái.',
  create_task: 'Tạo task mới (kèm checklist khởi tạo nếu muốn).',
  update_task: 'Sửa thông tin task đã có.',
  delete_task: 'Xóa task (kèm checklist, hoạt động, lịch sử).',
  add_todo: 'Thêm mục vào checklist của task.',
  toggle_todo: 'Đánh dấu hoàn thành / bỏ hoàn thành một mục checklist.',
  delete_todo: 'Xóa một mục checklist.',
  add_comment: 'Bình luận vào task (hỗ trợ trả lời và @nhắc thành viên).',
  list_tags: 'Liệt kê toàn bộ tag phân loại đang có trong workspace.',
  create_tag: 'Tạo tag phân loại mới (tên đã có thì dùng lại tag cũ, không tạo trùng).',
}
const descOf = t => SHORT_DESC[t.name] || t.description

// Đoạn cấu hình dán vào client MCP (Claude Code/Desktop, Cursor…) — transport
// Streamable HTTP kèm bearer token. Chỉ có nghĩa khi server đang chạy.
const configSnippet = computed(() => {
  if (!status.value.running) return ''
  return JSON.stringify(
    {
      mcpServers: {
        'pi-tracker': {
          type: 'http',
          url: status.value.url,
          headers: { Authorization: 'Bearer ' + status.value.token },
        },
      },
    },
    null,
    2,
  )
})

async function load() {
  error.value = ''
  try {
    ;[status.value, tools.value] = await Promise.all([MCPStatus(), MCPTools()])
  } catch (e) {
    error.value = String(e)
  }
}
onMounted(load)

async function toggle() {
  error.value = ''
  busy.value = true
  try {
    status.value = status.value.running ? await StopMCPServer() : await StartMCPServer()
  } catch (e) {
    error.value = String(e)
  } finally {
    busy.value = false
  }
}

async function copy(key, text) {
  try {
    await ClipboardSetText(text)
  } catch {
    try { await navigator.clipboard.writeText(text) } catch { /* bỏ qua */ }
  }
  copied.value = key
  setTimeout(() => { if (copied.value === key) copied.value = '' }, 2000)
}
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <div class="page-title">MCP Server</div>
        <div class="page-sub">
          Kích hoạt MCP server chạy tại máy để trợ lý AI (Claude, Cursor…) tạo/sửa task,
          đọc chi tiết task, checklist, bình luận và lịch sử — dưới quyền tài khoản đang đăng nhập.
        </div>
      </div>
    </div>

    <div v-if="error" class="err">{{ error }}</div>

    <div class="card">
      <div class="mcp-status-row">
        <div class="mcp-status">
          <span class="mcp-dot" :class="{ on: status.running }"></span>
          <div>
            <div class="card-title" style="margin: 0">
              {{ status.running ? 'Đang chạy' : 'Đã tắt' }}
            </div>
            <div class="hint" style="margin: 0">
              {{ status.running ? status.url : 'Cổng localhost ' + status.port + ' — chỉ máy này truy cập được' }}
            </div>
          </div>
        </div>
        <button class="btn" :class="status.running ? 'ghost-danger' : 'primary'" :disabled="busy" @click="toggle">
          {{ busy ? '…' : status.running ? '■ Tắt server' : '▶ Bật server' }}
        </button>
      </div>

      <p class="hint" style="margin-top: 14px">
        Server chỉ lắng nghe trên <code>127.0.0.1</code> (không ra mạng ngoài) và yêu cầu
        <b>bearer token</b> ở mỗi request. Token cố định theo máy (không đổi giữa các lần bật lại) nên
        cấu hình client dán một lần là dùng mãi; khi tắt server thì mọi request bị từ chối cho tới khi bật lại.
        Mọi thao tác của công cụ chạy dưới workspace &amp; tài khoản bạn đang đăng nhập trên app.
      </p>
    </div>

    <div class="card" v-if="status.running">
      <div class="card-title">Kết nối</div>

      <div class="mcp-field">
        <label>URL</label>
        <div class="mcp-copy">
          <code class="mcp-code">{{ status.url }}</code>
          <button class="btn sm ghost" @click="copy('url', status.url)">
            {{ copied === 'url' ? '✓ Đã chép' : 'Chép' }}
          </button>
        </div>
      </div>

      <div class="mcp-field">
        <label>Bearer token</label>
        <div class="mcp-copy">
          <code class="mcp-code">{{ status.token }}</code>
          <button class="btn sm ghost" @click="copy('token', status.token)">
            {{ copied === 'token' ? '✓ Đã chép' : 'Chép' }}
          </button>
        </div>
      </div>

      <div class="mcp-field">
        <label>Cấu hình client (Streamable HTTP)</label>
        <div class="mcp-snippet-wrap">
          <pre class="mcp-snippet">{{ configSnippet }}</pre>
          <button class="btn sm ghost mcp-snippet-copy" @click="copy('config', configSnippet)">
            {{ copied === 'config' ? '✓ Đã chép' : 'Chép' }}
          </button>
        </div>
        <p class="hint" style="margin-top: 8px">
          Dán vào cấu hình MCP của client (ví dụ tệp <code>.mcp.json</code> của Claude Code, hoặc mục
          MCP Servers trong Cursor/Claude Desktop). Client sẽ thấy các công cụ bên dưới.
        </p>
      </div>
    </div>

    <div class="card">
      <div class="card-title">Công cụ cung cấp ({{ tools.length }})</div>
      <div class="mcp-tools">
        <div v-for="t in tools" :key="t.name" class="mcp-tool">
          <code class="mcp-tool-name">{{ t.name }}</code>
          <span class="mcp-tool-desc">{{ descOf(t) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mcp-status-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}
.mcp-status {
  display: flex;
  align-items: center;
  gap: 12px;
}
.mcp-dot {
  width: 11px;
  height: 11px;
  border-radius: 50%;
  background: var(--muted, #6b7280);
  box-shadow: 0 0 0 3px rgba(107, 114, 128, 0.18);
  flex: none;
}
.mcp-dot.on {
  background: #22c55e;
  box-shadow: 0 0 0 3px rgba(34, 197, 94, 0.22);
}
.mcp-field {
  margin-top: 14px;
}
.mcp-field > label {
  display: block;
  font-size: 12px;
  color: var(--muted, #94a3b8);
  margin-bottom: 5px;
}
.mcp-copy {
  display: flex;
  align-items: center;
  gap: 8px;
}
.mcp-code {
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  white-space: nowrap;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm, 8px);
  padding: 8px 11px;
  font-size: 12.5px;
}
.mcp-snippet-wrap {
  position: relative;
}
.mcp-snippet {
  margin: 0;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm, 8px);
  padding: 12px 13px;
  overflow-x: auto;
  font-size: 12.5px;
  line-height: 1.5;
}
.mcp-snippet-copy {
  position: absolute;
  top: 8px;
  right: 8px;
}
.mcp-tools {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-top: 6px;
}
.mcp-tool {
  display: flex;
  gap: 12px;
  align-items: baseline;
  padding: 7px 0;
  border-bottom: 1px solid var(--border);
}
.mcp-tool:last-child {
  border-bottom: none;
}
.mcp-tool-name {
  flex: none;
  min-width: 130px;
  color: var(--accent, #60a5fa);
  font-size: 12.5px;
}
.mcp-tool-desc {
  color: var(--muted, #94a3b8);
  font-size: 13px;
}
</style>
