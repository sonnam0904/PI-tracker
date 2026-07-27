package main

import (
	"encoding/json"
	"fmt"

	"taskmanager/internal/mcp"
	"taskmanager/internal/models"
)

// mcpDefaultPort — cổng localhost mặc định cho MCP server. Chọn số cao ít va
// chạm; UI hiển thị lại URL đầy đủ nên người dùng không phải nhớ.
const mcpDefaultPort = 8765

// MCPStatusDTO — trạng thái MCP server đẩy ra frontend cho trang "MCP".
//   - URL: endpoint client MCP điền vào cấu hình (http://127.0.0.1:<port>/mcp).
//   - Token: bearer token phải gửi kèm (Authorization: Bearer <token>). Cố
//     định theo máy (không đổi giữa các lần bật); chỉ có nghĩa khi Running.
type MCPStatusDTO struct {
	Running bool   `json:"running"`
	URL     string `json:"url"`
	Token   string `json:"token"`
	Port    int    `json:"port"`
}

func (a *App) mcpStatusDTO() MCPStatusDTO {
	st := a.mcpServer().Status()
	dto := MCPStatusDTO{Running: st.Running, Token: st.Token, Port: mcpDefaultPort}
	if st.Running {
		dto.URL = "http://" + st.Addr + "/mcp"
	}
	return dto
}

// MCPStatus trả về trạng thái MCP server hiện tại (cho trang MCP khởi tạo).
func (a *App) MCPStatus() MCPStatusDTO {
	return a.mcpStatusDTO()
}

// StartMCPServer bật MCP server localhost với bộ công cụ thao tác task bind vào
// session ĐANG đăng nhập của app. Yêu cầu đã đăng nhập + đã chọn workspace vì
// mọi công cụ đều chạy dưới quyền của session này. Trả về trạng thái (URL +
// token) để UI hiển thị và người dùng dán vào cấu hình client MCP.
func (a *App) StartMCPServer() (MCPStatusDTO, error) {
	if !a.dbReady() {
		return MCPStatusDTO{}, fmt.Errorf("cơ sở dữ liệu chưa sẵn sàng — mở lại app khi đã kết nối được")
	}
	if _, err := a.requireWorkspace(); err != nil {
		return MCPStatusDTO{}, err
	}
	if err := a.mcpServer().Start(mcpDefaultPort, a.mcpTools()); err != nil {
		return MCPStatusDTO{}, err
	}
	return a.mcpStatusDTO(), nil
}

// StopMCPServer tắt MCP server (token hiện tại mất hiệu lực ngay).
func (a *App) StopMCPServer() (MCPStatusDTO, error) {
	if err := a.mcpServer().Stop(); err != nil {
		return MCPStatusDTO{}, err
	}
	return a.mcpStatusDTO(), nil
}

// ---- Định nghĩa công cụ MCP ----
//
// Mỗi công cụ chỉ là lớp vỏ mỏng gọi lại các phương thức App có sẵn (SaveTask,
// ListTodos, AddComment…) nên tái dùng toàn bộ kiểm tra quyền (requireWorkspace,
// taskInWorkspace), ghi lịch sử hoạt động và thông báo — MCP không đi vòng qua
// DB, đảm bảo hành vi y hệt khi thao tác trên UI.

// mcpTaskDetail gộp toàn bộ thông tin một task để trả cho get_task: thông tin
// task (tất cả field), checklist, feed hoạt động + bình luận, và lịch sử trạng
// thái — đúng những gì trang chi tiết task hiển thị.
type mcpTaskDetail struct {
	Task          TaskDTO               `json:"task"`
	Checklist     []models.TodoItem     `json:"checklist"`
	Activities    []models.Activity     `json:"activities"` // gồm cả comment (kind=comment)
	StatusHistory []models.StatusChange `json:"statusHistory"`
}

func (a *App) mcpTools() []mcp.Tool {
	// decode giải mã arguments vào đích, trả lỗi thân thiện nếu sai định dạng.
	decode := func(args json.RawMessage, dst any) error {
		if len(args) == 0 {
			return nil
		}
		if err := json.Unmarshal(args, dst); err != nil {
			return fmt.Errorf("tham số không hợp lệ: %w", err)
		}
		return nil
	}

	// Schema JSON cho các field của task — dùng chung cho create/update.
	taskProps := map[string]any{
		"title":                map[string]any{"type": "string", "description": "Tiêu đề task (bắt buộc khi tạo)"},
		"description":          map[string]any{"type": "string", "description": "Mô tả chi tiết"},
		"type":                 map[string]any{"type": "integer", "enum": []int{1, 2, 3}, "description": "1 Theo plan | 2 Phát sinh (bug) | 3 Phát sinh theo plan"},
		"size":                 map[string]any{"type": "string", "enum": []string{"S", "M", "L", "XL"}},
		"status":               map[string]any{"type": "string", "enum": []string{"Todo", "In Progress", "Blocked", "Done"}},
		"priority":             map[string]any{"type": "string", "enum": []string{"P1", "P2", "P3", "P4"}},
		"assigneeId":           map[string]any{"type": "integer", "description": "User.ID người phụ trách (0 = chưa gán). Xem list_people."},
		"estimateCustomerDays": map[string]any{"type": "number", "description": "Estimate báo khách (ngày)"},
		"estimateAiDays":       map[string]any{"type": "number", "description": "Estimate làm bằng AI (ngày)"},
		"actualDays":           map[string]any{"type": "number", "description": "Effort thực tế (ngày công)"},
		"aiUsed":               map[string]any{"type": "boolean"},
		"blocker":              map[string]any{"type": "string"},
		"blockedDays":          map[string]any{"type": "number"},
		"startDate":            map[string]any{"type": "string", "description": "YYYY-MM-DD"},
		"dueDate":              map[string]any{"type": "string", "description": "Hạn chót YYYY-MM-DD"},
		"doneDate":             map[string]any{"type": "string", "description": "YYYY-MM-DD (đặt khi status=Done)"},
		"severity":             map[string]any{"type": "string", "enum": []string{"Critical", "Major", "Minor"}, "description": "Chỉ dùng cho bug"},
		"resolution":           map[string]any{"type": "string", "description": "Chỉ dùng cho bug: Fixed | Won't Fix | Cannot Reproduce | Duplicate"},
		"relatedTaskId":        map[string]any{"type": "integer", "description": "Task gốc sinh bug"},
		"reporterId":           map[string]any{"type": "integer", "description": "User.ID người báo bug"},
		"dependsOn":            map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "ID các task phải hoàn thành trước (finish-to-start)"},
		"initialTodos":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Checklist tạo sẵn — CHỈ khi tạo task mới"},
	}
	objSchema := func(props map[string]any, required ...string) map[string]any {
		s := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}

	// guard bọc handler: chặn sớm nếu DB chưa sẵn sàng (app ở chế độ suy giảm khi
	// mất kết nối) để service nil không gây panic trên goroutine HTTP — trả lỗi
	// thân thiện cho client thay vì 500. requireWorkspace bên trong cũng đọc
	// a.workspaces nên phải chặn TRƯỚC khi vào handler.
	guard := func(h func(json.RawMessage) (any, error)) func(json.RawMessage) (any, error) {
		return func(args json.RawMessage) (any, error) {
			if !a.dbReady() {
				return nil, fmt.Errorf("cơ sở dữ liệu chưa sẵn sàng — thử lại sau khi kết nối được khôi phục")
			}
			return h(args)
		}
	}

	tools := []mcp.Tool{
		{
			Name:        "get_session",
			Description: "Trả về phiên đang đăng nhập trên app (user + workspace hiện hành) mà mọi công cụ MCP thao tác dưới danh nghĩa của nó.",
			InputSchema: objSchema(map[string]any{}),
			Handler: func(json.RawMessage) (any, error) {
				return a.GetSession(), nil
			},
		},
		{
			Name:        "list_people",
			Description: "Danh sách thành viên workspace hiện tại ({id, name}) — dùng để lấy assigneeId/reporterId khi tạo/sửa task.",
			InputSchema: objSchema(map[string]any{}),
			Handler: func(json.RawMessage) (any, error) {
				return a.ListPeople()
			},
		},
		{
			Name:        "list_tasks",
			Description: "Liệt kê tất cả task của workspace hiện tại (kèm tiến độ checklist và phụ thuộc).",
			InputSchema: objSchema(map[string]any{}),
			Handler: func(json.RawMessage) (any, error) {
				return a.ListTasks()
			},
		},
		{
			Name:        "get_task",
			Description: "Chi tiết đầy đủ một task theo id: toàn bộ thông tin task, checklist, feed hoạt động + bình luận, và lịch sử chuyển trạng thái.",
			InputSchema: objSchema(map[string]any{
				"taskId": map[string]any{"type": "integer", "description": "ID task"},
			}, "taskId"),
			Handler: func(args json.RawMessage) (any, error) {
				var in struct {
					TaskID uint `json:"taskId"`
				}
				if err := decode(args, &in); err != nil {
					return nil, err
				}
				return a.mcpGetTaskDetail(in.TaskID)
			},
		},
		{
			Name:        "create_task",
			Description: "Tạo task mới trong workspace hiện tại. title bắt buộc. Trả về danh sách task sau khi tạo để lấy id mới.",
			InputSchema: objSchema(taskProps, "title"),
			Handler: func(args json.RawMessage) (any, error) {
				var dto TaskDTO
				if err := decode(args, &dto); err != nil {
					return nil, err
				}
				dto.ID = 0 // ép tạo mới
				if err := a.SaveTask(dto); err != nil {
					return nil, err
				}
				return a.ListTasks()
			},
		},
		{
			Name:        "update_task",
			Description: "Cập nhật một task đã có. Bắt buộc có id. Chỉ cần truyền các field muốn thay đổi — field không truyền sẽ giữ nguyên giá trị hiện tại (tự merge với task hiện có).",
			InputSchema: func() map[string]any {
				p := map[string]any{"id": map[string]any{"type": "integer", "description": "ID task cần sửa"}}
				for k, v := range taskProps {
					if k == "initialTodos" {
						continue // chỉ có nghĩa khi tạo mới, không dùng lúc update
					}
					p[k] = v
				}
				return objSchema(p, "id")
			}(),
			Handler: func(args json.RawMessage) (any, error) {
				// Lấy id trước để nạp task hiện tại làm nền.
				var idOnly struct {
					ID uint `json:"id"`
				}
				if err := decode(args, &idOnly); err != nil {
					return nil, err
				}
				if idOnly.ID == 0 {
					return nil, fmt.Errorf("cần id của task để cập nhật")
				}
				// Nạp task hiện tại làm nền rồi CHỈ ghi đè các field xuất hiện
				// trong arguments: json.Unmarshal chỉ chạm tới key có trong JSON
				// nên field không truyền giữ nguyên giá trị cũ (tránh xóa trắng
				// mô tả/estimate/ngày… khi client gửi update một phần).
				cur, err := a.mcpGetTaskDetail(idOnly.ID)
				if err != nil {
					return nil, err
				}
				dto := cur.Task
				dto.InitialTodos = nil // chỉ dùng khi tạo mới, không áp lúc update
				if err := decode(args, &dto); err != nil {
					return nil, err
				}
				dto.ID = idOnly.ID
				if err := a.SaveTask(dto); err != nil {
					return nil, err
				}
				return a.mcpGetTaskDetail(dto.ID)
			},
		},
		{
			Name:        "delete_task",
			Description: "Xóa hẳn một task (kèm checklist, hoạt động, lịch sử trạng thái, phụ thuộc).",
			InputSchema: objSchema(map[string]any{
				"taskId": map[string]any{"type": "integer"},
			}, "taskId"),
			Handler: func(args json.RawMessage) (any, error) {
				var in struct {
					TaskID uint `json:"taskId"`
				}
				if err := decode(args, &in); err != nil {
					return nil, err
				}
				if err := a.DeleteTask(in.TaskID); err != nil {
					return nil, err
				}
				return map[string]any{"deleted": in.TaskID}, nil
			},
		},
		{
			Name:        "add_todo",
			Description: "Thêm một mục vào checklist của task.",
			InputSchema: objSchema(map[string]any{
				"taskId": map[string]any{"type": "integer"},
				"title":  map[string]any{"type": "string", "description": "Nội dung việc cần làm"},
			}, "taskId", "title"),
			Handler: func(args json.RawMessage) (any, error) {
				var in struct {
					TaskID uint   `json:"taskId"`
					Title  string `json:"title"`
				}
				if err := decode(args, &in); err != nil {
					return nil, err
				}
				if err := a.AddTodo(in.TaskID, in.Title); err != nil {
					return nil, err
				}
				return a.ListTodos(in.TaskID)
			},
		},
		{
			Name:        "toggle_todo",
			Description: "Đánh dấu hoàn thành / bỏ hoàn thành một mục checklist theo todoId.",
			InputSchema: objSchema(map[string]any{
				"todoId": map[string]any{"type": "integer"},
				"done":   map[string]any{"type": "boolean"},
			}, "todoId", "done"),
			Handler: func(args json.RawMessage) (any, error) {
				var in struct {
					TodoID uint `json:"todoId"`
					Done   bool `json:"done"`
				}
				if err := decode(args, &in); err != nil {
					return nil, err
				}
				if err := a.ToggleTodo(in.TodoID, in.Done); err != nil {
					return nil, err
				}
				return map[string]any{"todoId": in.TodoID, "done": in.Done}, nil
			},
		},
		{
			Name:        "delete_todo",
			Description: "Xóa một mục checklist theo todoId.",
			InputSchema: objSchema(map[string]any{
				"todoId": map[string]any{"type": "integer"},
			}, "todoId"),
			Handler: func(args json.RawMessage) (any, error) {
				var in struct {
					TodoID uint `json:"todoId"`
				}
				if err := decode(args, &in); err != nil {
					return nil, err
				}
				if err := a.DeleteTodo(in.TodoID); err != nil {
					return nil, err
				}
				return map[string]any{"deleted": in.TodoID}, nil
			},
		},
		{
			Name:        "add_comment",
			Description: "Thêm bình luận vào task. parentId != 0 để trả lời một bình luận. Hỗ trợ @username để nhắc thành viên (họ nhận thông báo).",
			InputSchema: objSchema(map[string]any{
				"taskId":   map[string]any{"type": "integer"},
				"content":  map[string]any{"type": "string"},
				"parentId": map[string]any{"type": "integer", "description": "ID bình luận muốn trả lời (0 = bình luận gốc)"},
			}, "taskId", "content"),
			Handler: func(args json.RawMessage) (any, error) {
				var in struct {
					TaskID   uint   `json:"taskId"`
					Content  string `json:"content"`
					ParentID uint   `json:"parentId"`
				}
				if err := decode(args, &in); err != nil {
					return nil, err
				}
				if err := a.AddComment(in.TaskID, in.Content, in.ParentID); err != nil {
					return nil, err
				}
				return a.ListActivities(in.TaskID)
			},
		},
	}

	// Bọc mọi handler bằng guard kiểm tra DB sẵn sàng trước khi thao tác.
	for i := range tools {
		tools[i].Handler = guard(tools[i].Handler)
	}
	return tools
}

// mcpGetTaskDetail gom toàn bộ dữ liệu chi tiết của một task, tái dùng các
// binding có sẵn (mỗi cái đã tự kiểm tra task thuộc workspace hiện tại).
func (a *App) mcpGetTaskDetail(taskID uint) (mcpTaskDetail, error) {
	t, _, err := a.taskInWorkspace(taskID)
	if err != nil {
		return mcpTaskDetail{}, err
	}
	dto := taskToDTO(t)
	if deps, err := a.dependencies.PredecessorsOf(t.ID); err == nil {
		dto.DependsOn = deps
	}
	if counts, err := a.todos.Counts(); err == nil {
		if c, ok := counts[t.ID]; ok {
			dto.TodoTotal, dto.TodoDone = c[0], c[1]
		}
	}

	todos, err := a.todos.List(taskID)
	if err != nil {
		return mcpTaskDetail{}, err
	}
	acts, err := a.activities.List(taskID)
	if err != nil {
		return mcpTaskDetail{}, err
	}
	changes, err := a.statuses.List(taskID)
	if err != nil {
		return mcpTaskDetail{}, err
	}
	return mcpTaskDetail{Task: dto, Checklist: todos, Activities: acts, StatusHistory: changes}, nil
}
