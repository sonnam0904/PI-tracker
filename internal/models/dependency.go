package models

import "time"

// TaskDependency — quan hệ chặn giữa 2 task trong cùng workspace theo kiểu
// finish-to-start: BlockedByID phải hoàn thành TRƯỚC khi TaskID bắt đầu.
// Mỗi hàng là một mũi tên phụ thuộc trên Gantt (BlockedBy → Task).
//
// Cặp (TaskID, BlockedByID) là duy nhất để không tạo cạnh trùng. Việc chống
// tự phụ thuộc và phụ thuộc vòng do service (DependencyService) đảm nhiệm.
type TaskDependency struct {
	ID          uint `gorm:"primaryKey"`
	WorkspaceID uint `gorm:"index"` // để lọc/dọn theo workspace
	// TaskID bị chặn bởi BlockedByID.
	TaskID      uint `gorm:"not null;uniqueIndex:idx_dep_pair"`
	BlockedByID uint `gorm:"not null;uniqueIndex:idx_dep_pair"`
	CreatedAt   time.Time
}
