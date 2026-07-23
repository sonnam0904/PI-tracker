package service

import (
	"time"

	"gorm.io/gorm"

	"taskmanager/internal/models"
)

type StatusService struct {
	db *gorm.DB
}

func NewStatusService(db *gorm.DB) *StatusService {
	return &StatusService{db: db}
}

// Log ghi một lần chuyển trạng thái (from = "" khi task vừa tạo).
func (s *StatusService) Log(taskID uint, from, to models.TaskStatus, actorName string) error {
	return s.db.Create(&models.StatusChange{
		TaskID: taskID, FromStatus: from, ToStatus: to, ActorName: actorName,
	}).Error
}

// List trả về lịch sử trạng thái của task, cũ nhất trước. Sắp theo id
// (thứ tự ghi) — chính là thứ tự chuyển trạng thái thực tế.
func (s *StatusService) List(taskID uint) ([]models.StatusChange, error) {
	var changes []models.StatusChange
	err := s.db.Where("task_id = ?", taskID).
		Order("id ASC").Find(&changes).Error
	return changes, err
}

// LastEntered trả về thời điểm gần nhất task chuyển VÀO trạng thái status,
// nil nếu chưa từng (task tạo trước khi có lịch sử trạng thái).
func (s *StatusService) LastEntered(taskID uint, status models.TaskStatus) (*time.Time, error) {
	var change models.StatusChange
	err := s.db.Where("task_id = ? AND to_status = ?", taskID, status).
		Order("created_at DESC, id DESC").First(&change).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &change.CreatedAt, nil
}

func (s *StatusService) DeleteForTask(taskID uint) error {
	return s.db.Where("task_id = ?", taskID).Delete(&models.StatusChange{}).Error
}
