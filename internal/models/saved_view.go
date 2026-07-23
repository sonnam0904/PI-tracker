package models

import "time"

// SavedView — bộ lọc đã lưu của một user trong một workspace, hiển thị thành
// tab trên trang Tasks (kiểu Lark). Filters là chuỗi JSON có shape do frontend
// định nghĩa (frontend/src/lib/taskFilters.js); backend chỉ lưu hộ, không đọc.
type SavedView struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	WorkspaceID uint      `gorm:"index:idx_view_ws_user;not null" json:"workspaceId"`
	UserID      uint      `gorm:"index:idx_view_ws_user;not null" json:"userId"`
	Name        string    `gorm:"not null" json:"name"`
	Filters     string    `json:"filters"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
