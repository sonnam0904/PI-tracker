package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoadResolvesProviderDefaults(t *testing.T) {
	t.Setenv("AI_PROVIDER", "deepseek")
	t.Setenv("AI_API_KEY", "sk-test")
	t.Setenv("AI_MODEL", "")
	t.Setenv("AI_BASE_URL", "")

	cfg := Load()
	if !cfg.Enabled() {
		t.Fatal("cfg phải Enabled khi có provider + key")
	}
	if cfg.Model != "deepseek-chat" {
		t.Errorf("model mặc định deepseek sai: %q", cfg.Model)
	}
	if cfg.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("baseURL mặc định sai: %q", cfg.BaseURL)
	}
}

func TestLoadOverrideAndUnknownProvider(t *testing.T) {
	t.Setenv("AI_PROVIDER", "openai")
	t.Setenv("AI_API_KEY", "k")
	t.Setenv("AI_MODEL", "gpt-4o")
	t.Setenv("AI_BASE_URL", "https://proxy.local/v1/")

	cfg := Load()
	if cfg.Model != "gpt-4o" {
		t.Errorf("AI_MODEL không override: %q", cfg.Model)
	}
	if cfg.BaseURL != "https://proxy.local/v1" { // cắt dấu "/" cuối
		t.Errorf("AI_BASE_URL không override/không cắt slash: %q", cfg.BaseURL)
	}

	t.Setenv("AI_PROVIDER", "khong-ton-tai")
	if Load().Enabled() {
		t.Error("provider lạ phải khiến Enabled() = false")
	}
}

func TestLoadDisabledWithoutKey(t *testing.T) {
	t.Setenv("AI_PROVIDER", "openai")
	t.Setenv("AI_API_KEY", "")
	if Load().Enabled() {
		t.Error("thiếu API key phải Enabled() = false")
	}
}

func TestParseSuggestion(t *testing.T) {
	cases := []struct {
		name, raw string
		wantAI    float64
		wantSize  string
	}{
		{"plain", `{"estimateAiDays":2,"estimateCustomerDays":3,"size":"m","confidence":"high","rationale":"x"}`, 2, "M"},
		{"fenced", "```json\n{\"estimateAiDays\":1.5,\"estimateCustomerDays\":2,\"size\":\"S\",\"confidence\":\"low\",\"rationale\":\"y\"}\n```", 1.5, "S"},
		{"chatty", "Đây là gợi ý của tôi:\n{\"estimateAiDays\":6,\"estimateCustomerDays\":9,\"size\":\"L\"} \nHy vọng giúp ích.", 6, "L"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := parseSuggestion(c.raw)
			if err != nil {
				t.Fatalf("parse lỗi: %v", err)
			}
			if s.EstimateAIDays != c.wantAI {
				t.Errorf("estimateAiDays = %v, muốn %v", s.EstimateAIDays, c.wantAI)
			}
			if s.Size != c.wantSize {
				t.Errorf("size = %q, muốn %q (phải in hoa)", s.Size, c.wantSize)
			}
		})
	}
}

func TestParseSuggestionChecklist(t *testing.T) {
	raw := `{"size":"M","checklist":["- Thiết kế schema DB","1. Viết API /login","Viết API /login","","  • Viết unit test  ","3D modeling"]}`
	s, err := parseSuggestion(raw)
	if err != nil {
		t.Fatalf("parse lỗi: %v", err)
	}
	want := []string{"Thiết kế schema DB", "Viết API /login", "Viết unit test", "3D modeling"}
	if len(s.Checklist) != len(want) {
		t.Fatalf("checklist = %#v, muốn %#v", s.Checklist, want)
	}
	for i := range want {
		if s.Checklist[i] != want[i] {
			t.Errorf("checklist[%d] = %q, muốn %q (bullet phải bị cắt, rỗng/trùng bị loại, '3D' giữ nguyên)", i, s.Checklist[i], want[i])
		}
	}
}

func TestParseSuggestionErrors(t *testing.T) {
	if _, err := parseSuggestion("không có json ở đây"); err == nil {
		t.Error("thiếu JSON phải trả lỗi")
	}
	if _, err := parseSuggestion(`{"estimateAiDays":-1}`); err == nil {
		t.Error("estimate âm phải trả lỗi")
	}
}

func TestBuildUserPromptIncludesContext(t *testing.T) {
	p := buildUserPrompt(
		Draft{Title: "Thêm export CSV", Description: "cho trang task", Type: "Theo plan"},
		[]Example{{Title: "Export Excel", Type: "Theo plan", Size: "M", EstAIDays: 2, ActualDays: 2.5, CycleDays: 3}},
	)
	for _, want := range []string{"Thêm export CSV", "cho trang task", "Export Excel", "effort thực 2.5", "size M"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt thiếu %q\n%s", want, p)
		}
	}
}

func TestBuildUserPromptNoExamples(t *testing.T) {
	p := buildUserPrompt(Draft{Title: "abc"}, nil)
	if !strings.Contains(p, "Chưa có task mẫu") {
		t.Errorf("prompt không nhắc thiếu mẫu:\n%s", p)
	}
}

func TestCompleteOpenAIWire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path sai: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization sai: %q", got)
		}
		var body struct {
			Model    string              `json:"model"`
			Messages []map[string]string `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "gpt-4o-mini" || len(body.Messages) != 2 {
			t.Errorf("body sai: %+v", body)
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"estimateAiDays\":3,\"size\":\"M\"}"}}]}`)
	}))
	defer srv.Close()

	c := NewClient(Config{Provider: ProviderOpenAI, APIKey: "k", Model: "gpt-4o-mini", BaseURL: srv.URL, Timeout: 5 * time.Second})
	out, err := c.Complete(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatalf("Complete lỗi: %v", err)
	}
	s, err := parseSuggestion(out)
	if err != nil || s.EstimateAIDays != 3 {
		t.Fatalf("kết quả sai: %+v err=%v", s, err)
	}
}

func TestCompleteAnthropicWire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("path sai: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk" {
			t.Errorf("x-api-key sai: %q", got)
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("thiếu anthropic-version")
		}
		io.WriteString(w, `{"content":[{"type":"text","text":"{\"estimateAiDays\":4,\"size\":\"L\"}"}]}`)
	}))
	defer srv.Close()

	c := NewClient(Config{Provider: ProviderAnthropic, APIKey: "sk", Model: "claude-x", BaseURL: srv.URL, Timeout: 5 * time.Second})
	out, err := c.Complete(context.Background(), "sys", "usr")
	if err != nil {
		t.Fatalf("Complete lỗi: %v", err)
	}
	if !strings.Contains(out, "estimateAiDays") {
		t.Errorf("nội dung Anthropic sai: %q", out)
	}
}

func TestCompleteDisabled(t *testing.T) {
	c := NewClient(Config{Provider: ProviderOpenAI, Model: "m"}) // thiếu key
	if _, err := c.Complete(context.Background(), "s", "u"); err != ErrDisabled {
		t.Errorf("phải trả ErrDisabled, nhận %v", err)
	}
}

func TestCompleteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"bad key"}`)
	}))
	defer srv.Close()
	c := NewClient(Config{Provider: ProviderOpenAI, APIKey: "k", Model: "m", BaseURL: srv.URL, Timeout: 5 * time.Second})
	if _, err := c.Complete(context.Background(), "s", "u"); err == nil {
		t.Error("HTTP 401 phải trả lỗi")
	}
}
