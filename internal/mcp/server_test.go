package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// startTestServer bật server trên một cổng cố định cho test và trả về status
// (địa chỉ + token) cùng hàm dọn dẹp.
func startTestServer(t *testing.T, tools []Tool) (Status, func()) {
	t.Helper()
	s := New("test", "0.0.0")
	// Cổng 0 để hệ điều hành cấp cổng rảnh — tránh đụng cổng thật khi chạy test.
	if err := s.Start(0, tools); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Đợi listener sẵn sàng.
	time.Sleep(50 * time.Millisecond)
	return s.Status(), func() { _ = s.Stop() }
}

// rpc gửi một request JSON-RPC tới server và trả về response giải mã.
func rpc(t *testing.T, url, token string, id int, method string, params any) rpcResponse {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		body["params"] = params
	}
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: status %d body %s", method, resp.StatusCode, raw)
	}
	var out rpcResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: decode %v (body %s)", method, err, raw)
	}
	return out
}

func TestServerLifecycleAndAuth(t *testing.T) {
	called := 0
	tools := []Tool{{
		Name:        "echo",
		Description: "trả lại tham số",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"msg": map[string]any{"type": "string"}}},
		Handler: func(args json.RawMessage) (any, error) {
			called++
			var in struct {
				Msg string `json:"msg"`
			}
			_ = json.Unmarshal(args, &in)
			return map[string]any{"echo": in.Msg}, nil
		},
	}}
	st, cleanup := startTestServer(t, tools)
	defer cleanup()

	url := "http://" + st.Addr + "/mcp"

	// Thiếu token → 401.
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping no-auth: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("thiếu token muốn 401, được %d", resp.StatusCode)
	}

	// initialize trả về protocolVersion.
	init := rpc(t, url, st.Token, 1, "initialize", nil)
	if init.Error != nil {
		t.Fatalf("initialize lỗi: %+v", init.Error)
	}
	resultBytes, _ := json.Marshal(init.Result)
	var initRes struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(resultBytes, &initRes)
	if initRes.ProtocolVersion != protocolVersion {
		t.Fatalf("protocolVersion = %q, muốn %q", initRes.ProtocolVersion, protocolVersion)
	}

	// tools/list liệt kê đúng công cụ đã đăng ký.
	list := rpc(t, url, st.Token, 2, "tools/list", nil)
	lb, _ := json.Marshal(list.Result)
	var listRes struct {
		Tools []toolSpec `json:"tools"`
	}
	_ = json.Unmarshal(lb, &listRes)
	if len(listRes.Tools) != 1 || listRes.Tools[0].Name != "echo" {
		t.Fatalf("tools/list = %+v", listRes.Tools)
	}

	// tools/call chạy handler và gói kết quả text.
	call := rpc(t, url, st.Token, 3, "tools/call", map[string]any{
		"name":      "echo",
		"arguments": map[string]any{"msg": "xin chào"},
	})
	if call.Error != nil {
		t.Fatalf("tools/call lỗi: %+v", call.Error)
	}
	cb, _ := json.Marshal(call.Result)
	var callRes struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(cb, &callRes)
	if callRes.IsError || len(callRes.Content) != 1 {
		t.Fatalf("tools/call kết quả bất ngờ: %+v", callRes)
	}
	if !bytes.Contains([]byte(callRes.Content[0].Text), []byte("xin chào")) {
		t.Fatalf("content không chứa echo: %s", callRes.Content[0].Text)
	}
	if called != 1 {
		t.Fatalf("handler gọi %d lần, muốn 1", called)
	}
}

func TestToolErrorIsWrapped(t *testing.T) {
	tools := []Tool{{
		Name:    "boom",
		Handler: func(json.RawMessage) (any, error) { return nil, fmt.Errorf("nổ rồi") },
	}}
	st, cleanup := startTestServer(t, tools)
	defer cleanup()
	url := "http://" + st.Addr + "/mcp"

	call := rpc(t, url, st.Token, 1, "tools/call", map[string]any{"name": "boom"})
	// Lỗi handler KHÔNG thành lỗi JSON-RPC mà là content isError=true.
	if call.Error != nil {
		t.Fatalf("muốn không có lỗi JSON-RPC, có %+v", call.Error)
	}
	cb, _ := json.Marshal(call.Result)
	var callRes struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(cb, &callRes)
	if !callRes.IsError || len(callRes.Content) == 0 || callRes.Content[0].Text != "nổ rồi" {
		t.Fatalf("lỗi handler chưa được gói đúng: %+v", callRes)
	}
}

func TestStopReleasesToken(t *testing.T) {
	st, cleanup := startTestServer(t, nil)
	url := "http://" + st.Addr + "/mcp"
	token := st.Token
	cleanup() // dừng server
	time.Sleep(30 * time.Millisecond)

	// Sau khi dừng, request tới địa chỉ cũ phải thất bại (server đóng).
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)))
	req.Header.Set("Authorization", "Bearer "+token)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
		t.Fatalf("server đã dừng nhưng vẫn nhận request (status %d)", resp.StatusCode)
	}
}
