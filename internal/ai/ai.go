// Package ai gợi ý estimate cho task bằng một mô hình ngôn ngữ (LLM).
//
// Hỗ trợ nhiều nhà cung cấp qua cấu hình .env (AI_PROVIDER, AI_API_KEY,
// AI_MODEL, AI_BASE_URL). Đa số nhà cung cấp (OpenAI, Qwen, Deepseek, GLM/z.ai,
// Gemini) đều phơi endpoint tương thích OpenAI /chat/completions nên dùng chung
// một đường đi; riêng Anthropic dùng /v1/messages với định dạng khác.
//
// Package này KHÔNG phụ thuộc service/models: đầu vào là các struct thuần
// (Draft, Example) do tầng app map sang, giữ layering sạch và test không cần DB.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ErrDisabled trả về khi chưa cấu hình đủ để gọi LLM (thiếu provider/key/model).
var ErrDisabled = errors.New("gợi ý AI chưa được cấu hình")

// Provider — nhà cung cấp LLM được hỗ trợ.
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
	ProviderQwen      Provider = "qwen"
	ProviderDeepseek  Provider = "deepseek"
	ProviderGLM       Provider = "glm"
	ProviderGemini    Provider = "gemini"
)

// wireFormat — định dạng request/response của nhà cung cấp.
type wireFormat int

const (
	formatOpenAI    wireFormat = iota // POST {base}/chat/completions, Bearer token
	formatAnthropic                   // POST {base}/messages, x-api-key + anthropic-version
)

// providerSpec là mặc định của mỗi provider: endpoint, model, và định dạng wire.
// AI_BASE_URL / AI_MODEL trong .env sẽ ghi đè baseURL / model tương ứng.
type providerSpec struct {
	baseURL string
	model   string
	format  wireFormat
}

// providerSpecs — bảng tra mặc định. baseURL không có dấu "/" ở cuối.
var providerSpecs = map[Provider]providerSpec{
	ProviderOpenAI:    {"https://api.openai.com/v1", "gpt-4o-mini", formatOpenAI},
	ProviderDeepseek:  {"https://api.deepseek.com/v1", "deepseek-chat", formatOpenAI},
	ProviderQwen:      {"https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen-plus", formatOpenAI},
	ProviderGLM:       {"https://open.bigmodel.cn/api/paas/v4", "glm-4-flash", formatOpenAI},
	ProviderGemini:    {"https://generativelanguage.googleapis.com/v1beta/openai", "gemini-1.5-flash", formatOpenAI},
	ProviderAnthropic: {"https://api.anthropic.com/v1", "claude-3-5-haiku-latest", formatAnthropic},
}

// Config gom cấu hình LLM đã phân giải (đã áp mặc định theo provider).
type Config struct {
	Provider Provider
	APIKey   string
	Model    string
	BaseURL  string // không có "/" ở cuối
	Timeout  time.Duration
}

// Load đọc cấu hình LLM từ biến môi trường (config.Load đã nạp .env từ trước).
// Provider không nhận diện được → Config rỗng và Enabled() = false.
func Load() Config {
	p := Provider(strings.ToLower(strings.TrimSpace(os.Getenv("AI_PROVIDER"))))
	spec, known := providerSpecs[p]

	model := strings.TrimSpace(os.Getenv("AI_MODEL"))
	if model == "" && known {
		model = spec.model
	}
	base := strings.TrimSpace(os.Getenv("AI_BASE_URL"))
	if base == "" && known {
		base = spec.baseURL
	}
	return Config{
		Provider: p,
		APIKey:   strings.TrimSpace(os.Getenv("AI_API_KEY")),
		Model:    model,
		BaseURL:  strings.TrimRight(base, "/"),
		Timeout:  45 * time.Second,
	}
}

// Enabled báo cấu hình đã đủ để gọi LLM: provider hợp lệ + có key + có model.
func (c Config) Enabled() bool {
	_, known := providerSpecs[c.Provider]
	return known && c.APIKey != "" && c.Model != ""
}

func (c Config) format() wireFormat { return providerSpecs[c.Provider].format }

// Client gọi LLM qua HTTP theo đúng định dạng của provider.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient dựng client theo cfg. Timeout <= 0 → mặc định 45s.
func NewClient(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 45 * time.Second
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}
}

// Complete gửi (system, user) tới LLM và trả về nội dung text của phản hồi.
func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	if !c.cfg.Enabled() {
		return "", ErrDisabled
	}
	if c.cfg.format() == formatAnthropic {
		return c.completeAnthropic(ctx, system, user)
	}
	return c.completeOpenAI(ctx, system, user)
}

// completeOpenAI gọi endpoint tương thích OpenAI /chat/completions.
func (c *Client) completeOpenAI(ctx context.Context, system, user string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":       c.cfg.Model,
		"temperature": 0.2,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	data, err := c.do(req)
	if err != nil {
		return "", err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("phản hồi LLM không đọc được: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("LLM trả về phản hồi rỗng")
	}
	return out.Choices[0].Message.Content, nil
}

// completeAnthropic gọi Anthropic Messages API (/v1/messages).
func (c *Client) completeAnthropic(ctx context.Context, system, user string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      c.cfg.Model,
		"max_tokens": 4096,
		"system":     system,
		"messages": []map[string]string{
			{"role": "user", "content": user},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	data, err := c.do(req)
	if err != nil {
		return "", err
	}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("phản hồi LLM không đọc được: %w", err)
	}
	var sb strings.Builder
	for _, blk := range out.Content {
		sb.WriteString(blk.Text)
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("LLM trả về phản hồi rỗng")
	}
	return sb.String(), nil
}

// do thực thi request và trả body; lỗi HTTP >= 300 kèm trích đoạn body để debug.
func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gọi LLM thất bại: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("LLM trả mã %d: %s", resp.StatusCode, snippet(data))
	}
	return data, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
