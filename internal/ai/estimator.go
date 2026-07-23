package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Draft là bản nháp task cần xin gợi ý (chỉ những trường có ý nghĩa cho estimate).
type Draft struct {
	Title       string
	Description string
	Type        string // nhãn loại task tiếng Việt, ví dụ "Theo plan"
}

// Example là một task đã Done dùng làm mẫu tham chiếu (grounding) cho mô hình,
// để estimate bám vào lịch sử thật của team thay vì đoán khơi khơi.
type Example struct {
	Title      string
	Type       string
	Size       string
	EstAIDays  float64 // estimate AI đã ghi
	ActualDays float64 // effort thực tế nhập tay (0 = chưa nhập)
	CycleDays  float64 // thời gian lịch thực tế (0 = không tính được)
}

// Suggestion là gợi ý do LLM trả về: estimate + mô tả yêu cầu đã viết lại.
type Suggestion struct {
	EstimateAIDays       float64 `json:"estimateAiDays"`       // số ngày dự kiến khi làm có AI
	EstimateCustomerDays float64 `json:"estimateCustomerDays"` // số ngày báo khách hàng
	Size                 string  `json:"size"`                 // S | M | L | XL
	Confidence           string  `json:"confidence"`           // low | medium | high
	Rationale            string  `json:"rationale"`            // giải thích ngắn (tiếng Việt)
	// Description: yêu cầu đã được phân tích & viết lại chi tiết (markdown tiếng
	// Việt) để ghi vào ô Mô tả của task. Giữ nguyên link có trong input.
	Description string `json:"description"`
	// Checklist: các đầu việc con để tạo sẵn thành todo checklist của task.
	Checklist []string `json:"checklist"`
}

// Estimator sinh gợi ý estimate từ bản nháp task + các task mẫu.
type Estimator struct {
	client *Client
}

// NewEstimator bọc một Client. client == nil → Estimator luôn ở trạng thái tắt.
func NewEstimator(client *Client) *Estimator {
	return &Estimator{client: client}
}

// Enabled báo estimator đã sẵn sàng gọi LLM.
func (e *Estimator) Enabled() bool {
	return e != nil && e.client != nil && e.client.cfg.Enabled()
}

// Info trả về provider/model đang dùng và trạng thái bật, phục vụ hiển thị.
func (e *Estimator) Info() (provider, model string, enabled bool) {
	if e == nil || e.client == nil {
		return "", "", false
	}
	return string(e.client.cfg.Provider), e.client.cfg.Model, e.client.cfg.Enabled()
}

const systemPrompt = `Bạn là trợ lý phân tích và ước lượng công việc cho team phát triển phần mềm.

Input gồm:

- title
- description
- danh sách task tương tự đã hoàn thành (nếu có)

Mục tiêu:

1. Phân tích yêu cầu.
2. Viết lại mô tả chi tiết.
3. Ước lượng ngày công.
4. Trả về DUY NHẤT một object JSON hợp lệ.

Output:

{
  "description": "...",
  "checklist": ["...", "..."],
  "estimateAiDays": 0,
  "estimateCustomerDays": 0,
  "size": "S|M|L|XL",
  "confidence": "low|medium|high",
  "rationale": ""
}

description viết bằng Markdown gồm:

## Mục tiêu

## Phạm vi

## Hạng mục công việc

(danh sách bullet)

## Giả định

(nếu có)

## Tiêu chí hoàn thành

(Definition of Done)

checklist là mảng đầu việc con để chia task thành các bước làm được:

- mỗi phần tử là 1 câu ngắn tiếng Việt, bắt đầu bằng động từ, kiểm được xong/chưa
  (vd "Thiết kế schema DB", "Viết API /login", "Viết unit test").
- từ 3 đến 8 mục, không trùng lặp; bám theo mục "Hạng mục công việc" trong description.
- nếu task quá mơ hồ thì trả mảng rỗng [].

Quy tắc:

- Giữ nguyên mọi URL/link trong input.
- Nếu thông tin nằm trong Google Docs/Sheets hoặc link không thể đọc thì KHÔNG suy diễn, ghi:
  "Chi tiết xem tại: <link>".
- Không bịa chức năng.
- Nếu thiếu thông tin thì chỉ mô tả ở mức khung và confidence = low.
- Nếu có task tương tự thì ưu tiên hiệu chỉnh estimate theo effort trung bình của các task đó.
- estimateAiDays chỉ bao gồm:
  - coding
  - self review
  - unit test
- estimateCustomerDays bao gồm:
  - phát triển
  - review
  - QA
  - fix bug
  - buffer
- estimateCustomerDays luôn >= estimateAiDays.
- Ngày công có thể lẻ 0.5.
- size:
  - S: <=1 ngày AI
  - M: >1 đến <=3 ngày AI
  - L: >3 đến <=7 ngày AI
  - XL: >7 ngày AI
- confidence:
  - high: nhiều task tương tự và mô tả rõ
  - medium: có một vài task tương tự
  - low: ít hoặc không có task tương tự, hoặc mô tả thiếu
- rationale tối đa 150 ký tự.
- Chỉ trả về object JSON hợp lệ, không thêm markdown hay bất kỳ nội dung nào khác.`

// Suggest gọi LLM và trả về gợi ý estimate đã phân tích.
func (e *Estimator) Suggest(ctx context.Context, draft Draft, examples []Example) (Suggestion, error) {
	if !e.Enabled() {
		return Suggestion{}, ErrDisabled
	}
	raw, err := e.client.Complete(ctx, systemPrompt, buildUserPrompt(draft, examples))
	if err != nil {
		return Suggestion{}, err
	}
	return parseSuggestion(raw)
}

// buildUserPrompt dựng phần user: mô tả task cần ước lượng + bảng task mẫu.
func buildUserPrompt(draft Draft, examples []Example) string {
	var b strings.Builder
	b.WriteString("TASK CẦN ƯỚC LƯỢNG:\n")
	fmt.Fprintf(&b, "- Loại: %s\n", fallback(draft.Type, "Theo plan"))
	fmt.Fprintf(&b, "- Tiêu đề: %s\n", strings.TrimSpace(draft.Title))
	if d := strings.TrimSpace(draft.Description); d != "" {
		fmt.Fprintf(&b, "- Mô tả: %s\n", d)
	}

	if len(examples) > 0 {
		b.WriteString("\nCÁC TASK TƯƠNG TỰ ĐÃ HOÀN THÀNH (tham chiếu):\n")
		for _, ex := range examples {
			fmt.Fprintf(&b, "- [%s | size %s] %s",
				fallback(ex.Type, "?"), fallback(ex.Size, "?"), strings.TrimSpace(ex.Title))
			if ex.EstAIDays > 0 {
				fmt.Fprintf(&b, " | est AI %.1f ngày", ex.EstAIDays)
			}
			if ex.ActualDays > 0 {
				fmt.Fprintf(&b, " | effort thực %.1f ngày", ex.ActualDays)
			}
			if ex.CycleDays > 0 {
				fmt.Fprintf(&b, " | cycle %.1f ngày", ex.CycleDays)
			}
			b.WriteByte('\n')
		}
	} else {
		b.WriteString("\n(Chưa có task mẫu — hãy ước lượng thận trọng, confidence thấp.)\n")
	}

	b.WriteString("\nTrả về JSON theo đúng schema: " +
		`{"description":string,"checklist":[string],"estimateAiDays":number,"estimateCustomerDays":number,"size":"S|M|L|XL","confidence":"low|medium|high","rationale":string}`)
	return b.String()
}

func fallback(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// parseSuggestion tách JSON từ phản hồi LLM (chấp nhận rào ```json hoặc text
// thừa quanh object) và chuẩn hóa size về in hoa.
func parseSuggestion(raw string) (Suggestion, error) {
	js := extractJSONObject(raw)
	if js == "" {
		return Suggestion{}, fmt.Errorf("không tìm thấy JSON trong phản hồi AI: %s", snippet([]byte(raw)))
	}
	var s Suggestion
	if err := json.Unmarshal([]byte(js), &s); err != nil {
		return Suggestion{}, fmt.Errorf("phản hồi AI không đúng định dạng: %w", err)
	}
	s.Size = strings.ToUpper(strings.TrimSpace(s.Size))
	s.Confidence = strings.ToLower(strings.TrimSpace(s.Confidence))
	s.Rationale = strings.TrimSpace(s.Rationale)
	s.Description = strings.TrimSpace(s.Description)
	s.Checklist = cleanChecklist(s.Checklist)
	if s.EstimateAIDays < 0 || s.EstimateCustomerDays < 0 {
		return Suggestion{}, fmt.Errorf("estimate âm không hợp lệ")
	}
	return s, nil
}

// stripBullet cắt MỘT tiền tố bullet/số thứ tự ở đầu chuỗi ("- ", "* ", "• ",
// "1. ", "2) "...) nếu có, tránh cắt nhầm nội dung như "3D modeling".
func stripBullet(s string) string {
	// Bullet ký hiệu: -, *, • theo sau bởi khoảng trắng.
	if len(s) >= 2 && (s[0] == '-' || s[0] == '*' || strings.HasPrefix(s, "•")) {
		rest := strings.TrimPrefix(s, "•")
		if rest == s { // không phải "•": bỏ 1 ký tự -/*
			rest = s[1:]
		}
		if t := strings.TrimLeft(rest, " \t"); t != rest {
			return strings.TrimSpace(t)
		}
	}
	// Số thứ tự "12." hoặc "3)" theo sau bởi khoảng trắng.
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s) && (s[i] == '.' || s[i] == ')') {
		if j := i + 1; j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			return strings.TrimSpace(s[j:])
		}
	}
	return s
}

// cleanChecklist chuẩn hóa danh sách đầu việc do LLM trả về: bỏ khoảng trắng,
// cắt ký tự bullet/số thứ tự thừa ("- ", "* ", "1. "), loại rỗng và trùng lặp,
// giới hạn tối đa 20 mục để tránh danh sách khổng lồ.
func cleanChecklist(items []string) []string {
	const maxItems = 20
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		s := stripBullet(strings.TrimSpace(it))
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

// extractJSONObject lấy object JSON đầu tiên: từ dấu '{' đầu tới '}' cuối cùng.
// Bỏ qua rào markdown và mọi text mô tả mà mô hình có thể chèn thêm.
func extractJSONObject(raw string) string {
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end < start {
		return ""
	}
	return raw[start : end+1]
}
