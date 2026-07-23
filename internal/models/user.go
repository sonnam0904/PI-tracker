package models

import "time"

// User — tài khoản đăng nhập (mật khẩu băm Argon2id, không bao giờ lưu plaintext).
type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;not null" json:"username"`
	PasswordHash string    `json:"-"` // không bao giờ trả về frontend
	CreatedAt    time.Time `json:"createdAt"`
}

// Workspace — không gian làm việc; mọi task/settings/thành viên scope theo đây.
type Workspace struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"not null" json:"name"`
	OwnerID   uint      `gorm:"index" json:"ownerId"`
	CreatedAt time.Time `json:"createdAt"`
}

// WorkspaceMember — thành viên của workspace (chính là "nhân sự" của team).
type WorkspaceMember struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	WorkspaceID uint   `gorm:"uniqueIndex:idx_ws_user;not null" json:"workspaceId"`
	UserID      uint   `gorm:"uniqueIndex:idx_ws_user;not null" json:"userId"`
	Role        string `json:"role"` // "owner" | "member"
	// Locked: owner khóa thành viên — bị khóa thì không thao tác được gì
	// trong workspace này cho tới khi được mở khóa (membership vẫn giữ).
	Locked    bool      `json:"locked"`
	CreatedAt time.Time `json:"createdAt"`
}

// Invitation — lời mời vào workspace theo username.
type Invitation struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	WorkspaceID uint      `gorm:"index;not null" json:"workspaceId"`
	InviterID   uint      `json:"inviterId"`
	InviteeID   uint      `gorm:"index;not null" json:"inviteeId"`
	Status      string    `gorm:"index" json:"status"` // "pending" | "accepted" | "declined"
	CreatedAt   time.Time `json:"createdAt"`
}

// Notification — thông báo trong app (chuông): lời mời, kết quả lời mời,
// nhắc hạn chót…
type Notification struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	UserID       uint   `gorm:"index;not null" json:"userId"` // người nhận
	Kind         string `json:"kind"`                         // "invite" | "info" | "due"
	Content      string `json:"content"`
	InvitationID *uint  `json:"invitationId"` // với kind=invite
	// Với kind=due/mention/reply: task được nhắc + workspace chứa nó, để UI
	// click vào thông báo là nhảy thẳng tới task (đổi workspace nếu cần).
	TaskID      *uint `json:"taskId"`
	WorkspaceID *uint `json:"workspaceId"`
	// Với kind=mention/reply: bình luận cụ thể — UI scroll tới và làm nổi bật.
	ActivityID *uint     `json:"activityId"`
	Read        bool      `gorm:"index" json:"read"`
	CreatedAt   time.Time `json:"createdAt"`
}
