package models

import "time"

// Tag — nhãn phân loại do người dùng tự tạo, dùng chung trong một workspace.
// Đây là "từ vựng" của workspace: người dùng gõ tên mới thì tag được tạo, gõ
// tên đã có thì chọn lại tag cũ (xem TagService.EnsureByNames).
//
// Cặp (WorkspaceID, Name) là duy nhất để hai người không tạo trùng một tag, và
// để việc "tạo mới hay chọn lại" luôn cho ra cùng một hàng. Tên lưu nguyên dạng
// người dùng gõ; NameKey là dạng chuẩn hóa (lowercase, trim) dùng để so trùng
// nên "Hạ tầng" và "hạ tầng" không thành hai tag khác nhau.
type Tag struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	WorkspaceID uint      `gorm:"uniqueIndex:idx_tag_ws_name;not null" json:"workspaceId"`
	NameKey     string    `gorm:"uniqueIndex:idx_tag_ws_name;not null" json:"-"`
	Name        string    `gorm:"not null" json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
}

// TaskTag — quan hệ nhiều-nhiều giữa task và tag. Dùng bảng join tường minh
// (không dùng gorm many2many) cho giống TaskDependency: có WorkspaceID để lọc/
// dọn theo workspace, và cặp (TaskID, TagID) là duy nhất để không gắn trùng.
type TaskTag struct {
	ID          uint `gorm:"primaryKey"`
	WorkspaceID uint `gorm:"index"`
	TaskID      uint `gorm:"not null;uniqueIndex:idx_task_tag_pair"`
	TagID       uint `gorm:"not null;uniqueIndex:idx_task_tag_pair"`
	CreatedAt   time.Time
}
