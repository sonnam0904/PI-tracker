package models

import "time"

// StatusChange ghi một lần chuyển trạng thái của task — nguồn dữ liệu cho
// timeline trạng thái trong task detail và cho việc tự cộng BlockedDays khi
// task rời trạng thái Blocked.
//
// ActorName lưu thẳng tên người thao tác (như Activity) để lịch sử vẫn đọc
// được nếu user bị xóa sau này.
type StatusChange struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	TaskID uint `gorm:"index" json:"taskId"`

	FromStatus TaskStatus `json:"fromStatus"` // "" khi task vừa được tạo
	ToStatus   TaskStatus `json:"toStatus"`
	ActorName  string     `json:"actorName"`

	CreatedAt time.Time `json:"createdAt"`
}
