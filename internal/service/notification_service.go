package service

import (
	"gorm.io/gorm"

	"taskmanager/internal/models"
)

type NotificationService struct {
	db *gorm.DB
}

func NewNotificationService(db *gorm.DB) *NotificationService {
	return &NotificationService{db: db}
}

// NotificationView — notification kèm trạng thái lời mời (để render nút
// Chấp nhận/Từ chối chỉ khi lời mời còn pending).
type NotificationView struct {
	models.Notification
	InvitationStatus string `json:"invitationStatus"`
}

// ListForUser trả về thông báo mới nhất trước (giới hạn 50).
func (s *NotificationService) ListForUser(userID uint) ([]NotificationView, error) {
	var rows []NotificationView
	err := s.db.Model(&models.Notification{}).
		Select("notifications.*, COALESCE(invitations.status, '') as invitation_status").
		Joins("LEFT JOIN invitations ON invitations.id = notifications.invitation_id").
		Where("notifications.user_id = ?", userID).
		Order("notifications.created_at DESC, notifications.id DESC").
		Limit(50).
		Scan(&rows).Error
	return rows, err
}

// Create tạo thông báo mới (không dedup — mỗi lần nhắc tới là một thông báo).
func (s *NotificationService) Create(n models.Notification) (models.Notification, error) {
	err := s.db.Create(&n).Error
	return n, err
}

// CreateIfAbsent tạo thông báo nếu user chưa có bản ghi cùng kind + content —
// dedup bền qua các phiên chạy app (dùng cho nhắc hạn chót: mỗi task chỉ nhắc
// một lần cho mỗi nội dung; đổi hạn chót → nội dung khác → nhắc lại).
// Bản ghi trùng nhưng thiếu liên kết task (dữ liệu đời cũ) được vá lại.
// Trả về notification và true nếu có tạo mới.
func (s *NotificationService) CreateIfAbsent(n models.Notification) (models.Notification, bool, error) {
	var existing models.Notification
	err := s.db.Where("user_id = ? AND kind = ? AND content = ?", n.UserID, n.Kind, n.Content).
		First(&existing).Error
	if err == nil {
		if existing.TaskID == nil && n.TaskID != nil {
			existing.TaskID, existing.WorkspaceID = n.TaskID, n.WorkspaceID
			if err := s.db.Save(&existing).Error; err != nil {
				return existing, false, err
			}
		}
		return existing, false, nil
	}
	if err != gorm.ErrRecordNotFound {
		return models.Notification{}, false, err
	}
	if err := s.db.Create(&n).Error; err != nil {
		return models.Notification{}, false, err
	}
	return n, true, nil
}

// NewerThan trả về thông báo của user có ID lớn hơn afterID, cũ trước mới
// sau — phục vụ đẩy notification Hệ điều hành theo đúng thứ tự phát sinh.
func (s *NotificationService) NewerThan(userID, afterID uint) ([]models.Notification, error) {
	var rows []models.Notification
	err := s.db.Where("user_id = ? AND id > ?", userID, afterID).
		Order("id ASC").Limit(20).Find(&rows).Error
	return rows, err
}

// MaxID trả về ID thông báo lớn nhất của user (0 nếu chưa có) — làm mốc
// baseline khi bắt đầu theo dõi để không notify lại backlog cũ.
func (s *NotificationService) MaxID(userID uint) (uint, error) {
	var id uint
	err := s.db.Model(&models.Notification{}).
		Where("user_id = ?", userID).
		Select("COALESCE(MAX(id), 0)").Scan(&id).Error
	return id, err
}

func (s *NotificationService) UnreadCount(userID uint) (int64, error) {
	var n int64
	err := s.db.Model(&models.Notification{}).
		Where("user_id = ? AND read = ?", userID, false).Count(&n).Error
	return n, err
}

// MarkAllRead đánh dấu đã đọc mọi thông báo KHÔNG phải lời mời đang chờ
// (lời mời pending giữ unread để user còn thấy cần xử lý).
func (s *NotificationService) MarkAllRead(userID uint) error {
	return s.db.Model(&models.Notification{}).
		Where("user_id = ? AND read = ? AND (invitation_id IS NULL OR invitation_id NOT IN (?))",
			userID, false,
			s.db.Model(&models.Invitation{}).Select("id").Where("status = 'pending'"),
		).
		Update("read", true).Error
}
