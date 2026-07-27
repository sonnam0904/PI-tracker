// Package mcp là một MCP (Model Context Protocol) server tối giản, tự chứa,
// chạy trên localhost và giao tiếp JSON-RPC 2.0 qua Streamable HTTP (POST /mcp).
// Không phụ thuộc thư viện ngoài — chỉ dùng net/http + encoding/json.
//
// Thiết kế: gói này CHỈ lo phần giao vận + giao thức MCP (initialize, tools/list,
// tools/call, ping) và xác thực bằng bearer token. Các công cụ (Tool) do lớp
// gọi (package main) đăng ký, mỗi Tool ôm sẵn handler bind vào session/service
// hiện có nên gói này không biết gì về domain task — tránh phụ thuộc vòng.
package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"taskmanager/internal/machinecrypto"
)

// protocolVersion — phiên bản MCP mà server khai báo khi initialize. Dùng bản
// ổn định, được các client (Claude Code/Desktop, Cursor…) hỗ trợ rộng rãi.
const protocolVersion = "2024-11-05"

// Tool là một công cụ MCP: tên, mô tả, JSON Schema mô tả tham số đầu vào, và
// handler thực thi. Handler nhận tham số thô (arguments) và trả về dữ liệu bất
// kỳ (sẽ được mã hóa JSON làm nội dung trả về) hoặc lỗi.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(args json.RawMessage) (any, error)
}

// Server là MCP server localhost. Zero value không dùng được — gọi New().
type Server struct {
	name    string
	version string

	mu      sync.Mutex
	httpSrv *http.Server
	tools   []Tool
	toolIdx map[string]Tool
	token   string
	addr    string // "127.0.0.1:<port>" khi đang chạy, "" khi dừng
	running bool
}

// New tạo Server chưa chạy. name/version hiện trong khối serverInfo lúc initialize.
func New(name, version string) *Server {
	return &Server{name: name, version: version}
}

// Status là ảnh chụp trạng thái hiện tại của server, an toàn để đọc song song.
type Status struct {
	Running bool
	Addr    string // "127.0.0.1:<port>"
	Token   string
}

// Status trả về trạng thái hiện tại (đang chạy chưa, địa chỉ, token).
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Running: s.running, Addr: s.addr, Token: s.token}
}

// Running báo server có đang chạy không.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Start khởi động server trên 127.0.0.1:port với bộ công cụ tools. Token cố
// định theo máy (xem machinecrypto.MCPToken) — không đổi giữa các lần bật.
// Bind vào loopback nên chỉ tiến trình trên chính máy này mới truy cập được;
// thêm bearer token để tiến trình lạ trên máy không tự gọi được. Lỗi nếu đã
// chạy hoặc port bận.
func (s *Server) Start(port int, tools []Tool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return fmt.Errorf("MCP server đã chạy tại %s", s.addr)
	}

	// Bind loopback trước để bắt lỗi "port bận" ngay, trả về cho UI.
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("không mở được cổng %d trên localhost: %w", port, err)
	}

	token, err := machinecrypto.MCPToken()
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("không tạo được token: %w", err)
	}

	idx := make(map[string]Tool, len(tools))
	for _, t := range tools {
		idx[t.Name] = t
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCP)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.httpSrv = srv
	s.tools = tools
	s.toolIdx = idx
	s.token = token
	// Ghi địa chỉ THỰC listener bind vào (quan trọng khi port=0 → HĐH cấp cổng).
	s.addr = ln.Addr().String()
	s.running = true

	// Phục vụ trên goroutine riêng; ln.Close/Shutdown sẽ khiến Serve trả về.
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// Stop dừng server (đóng listener). An toàn khi gọi lúc chưa chạy (no-op).
func (s *Server) Stop() error {
	s.mu.Lock()
	srv := s.httpSrv
	s.httpSrv = nil
	s.running = false
	s.addr = ""
	s.token = ""
	s.tools = nil
	s.toolIdx = nil
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// ---- JSON-RPC 2.0 ----

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // vắng mặt = notification (không cần trả lời)
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Mã lỗi JSON-RPC chuẩn.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// handleMCP là endpoint Streamable HTTP duy nhất. Client POST JSON-RPC; server
// trả JSON-RPC trực tiếp dưới dạng application/json (không cần SSE cho các thao
// tác request/response ngắn). GET (SSE do server chủ động) không hỗ trợ → 405.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "chỉ hỗ trợ POST", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	token := s.token
	s.mu.Unlock()
	if !authorized(r, token) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body := http.MaxBytesReader(w, r.Body, 4<<20) // trần 4MB chống payload lớn
	var raw json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		writeJSON(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: codeParseError, Message: "JSON không hợp lệ"}})
		return
	}

	// Hỗ trợ cả batch (mảng) lẫn request đơn.
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var batch []rpcRequest
		if err := json.Unmarshal(raw, &batch); err != nil {
			writeJSON(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: codeParseError, Message: "batch không hợp lệ"}})
			return
		}
		var out []rpcResponse
		for _, req := range batch {
			if resp, ok := s.dispatch(req); ok {
				out = append(out, resp)
			}
		}
		if len(out) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, out)
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeJSON(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: codeInvalidRequest, Message: "request không hợp lệ"}})
		return
	}
	resp, ok := s.dispatch(req)
	if !ok {
		// Notification (không có id): ack rỗng, không body.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, resp)
}

// dispatch xử lý một request JSON-RPC. ok=false nghĩa là notification (không id)
// → caller không gửi response.
func (s *Server) dispatch(req rpcRequest) (rpcResponse, bool) {
	isNotification := len(req.ID) == 0
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.name, "version": s.version},
		}
	case "notifications/initialized", "notifications/cancelled":
		return resp, false // notification thuần
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.listTools()}
	case "tools/call":
		result, rerr := s.callTool(req.Params)
		if rerr != nil {
			resp.Error = rerr
		} else {
			resp.Result = result
		}
	default:
		if isNotification {
			return resp, false // notification lạ → lặng lẽ bỏ qua
		}
		resp.Error = &rpcError{Code: codeMethodNotFound, Message: "method không hỗ trợ: " + req.Method}
	}

	if isNotification {
		return resp, false
	}
	return resp, true
}

// toolSpec là hình dạng một tool trong tools/list (name/description/inputSchema).
type toolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func (s *Server) listTools() []toolSpec {
	s.mu.Lock()
	tools := s.tools
	s.mu.Unlock()
	out := make([]toolSpec, 0, len(tools))
	for _, t := range tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, toolSpec{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return out
}

// callTool chạy tools/call: tra tool theo tên, gọi handler, gói kết quả theo
// định dạng MCP (content[] + isError). Lỗi handler KHÔNG trả về lỗi JSON-RPC mà
// gói vào content với isError=true để client/LLM đọc được nội dung lỗi.
func (s *Server) callTool(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "params tools/call không hợp lệ"}
	}
	s.mu.Lock()
	tool, ok := s.toolIdx[p.Name]
	s.mu.Unlock()
	if !ok {
		return nil, &rpcError{Code: codeMethodNotFound, Message: "không có công cụ tên " + p.Name}
	}

	result, err := tool.Handler(p.Arguments)
	if err != nil {
		return toolResult(err.Error(), true), nil
	}
	text, mErr := json.MarshalIndent(result, "", "  ")
	if mErr != nil {
		return toolResult("lỗi mã hóa kết quả: "+mErr.Error(), true), nil
	}
	return toolResult(string(text), false), nil
}

// toolResult tạo khối kết quả tools/call theo chuẩn MCP.
func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// authorized kiểm tra header Authorization: Bearer <token>. token rỗng (server
// vừa dừng) → từ chối.
func authorized(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	// So sánh constant-time để không rò rỉ token qua thời gian phản hồi.
	got := strings.TrimSpace(h[len(prefix):])
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
