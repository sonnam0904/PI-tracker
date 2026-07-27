package models

import "time"

// TodoItem — một mục việc trong checklist của task.
type TodoItem struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TaskID    uint      `gorm:"index;not null" json:"taskId"`
	Title     string    `gorm:"not null" json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"createdAt"`
}

// Activity — lịch sử hoạt động của task: tạo, thay đổi thông tin,
// checklist, bình luận. Hiển thị chung một feed trong chi tiết task.
type Activity struct {
	ID uint `gorm:"primaryKey;index:idx_activity_ws_id,priority:2" json:"id"`
	// WorkspaceID denormalize từ task: cho phép poll đồng bộ realtime lấy
	// MAX(id) theo workspace bằng index (workspace_id, id) — O(log n), khỏi
	// join sang tasks. 0 với dữ liệu cũ chưa backfill.
	WorkspaceID uint `gorm:"index:idx_activity_ws_id,priority:1" json:"workspaceId"`
	TaskID      uint `gorm:"index;not null" json:"taskId"`
	// Denormalize tên người thực hiện để lịch sử còn nguyên khi nhân sự bị xóa.
	ActorName string `json:"actorName"`
	Kind      string `gorm:"index" json:"kind"` // "create" | "update" | "todo" | "comment"
	Content   string `json:"content"`
	// ParentID: bình luận này trả lời bình luận nào (thread 1 cấp — reply của
	// reply được gắn về comment gốc), nil = comment gốc / hoạt động thường.
	ParentID  *uint     `gorm:"index" json:"parentId"`
	CreatedAt time.Time `json:"createdAt"`
}
