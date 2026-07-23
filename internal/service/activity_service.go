package service

import (
	"gorm.io/gorm"

	"taskmanager/internal/models"
)

type ActivityService struct {
	db *gorm.DB
}

func NewActivityService(db *gorm.DB) *ActivityService {
	return &ActivityService{db: db}
}

// Log ghi một dòng lịch sử cho task.
func (s *ActivityService) Log(taskID uint, actorName, kind, content string) error {
	return s.db.Create(&models.Activity{
		TaskID: taskID, ActorName: actorName, Kind: kind, Content: content,
	}).Error
}

// LogComment ghi bình luận gốc, trả về bản ghi (ID dùng gắn vào notification).
func (s *ActivityService) LogComment(taskID uint, actorName, content string) (models.Activity, error) {
	act := models.Activity{TaskID: taskID, ActorName: actorName, Kind: "comment", Content: content}
	err := s.db.Create(&act).Error
	return act, err
}

// LogReply ghi bình luận trả lời một bình luận khác (parentID), trả về bản ghi.
func (s *ActivityService) LogReply(taskID uint, actorName, content string, parentID uint) (models.Activity, error) {
	act := models.Activity{
		TaskID: taskID, ActorName: actorName, Kind: "comment", Content: content,
		ParentID: &parentID,
	}
	err := s.db.Create(&act).Error
	return act, err
}

// Get trả về một dòng lịch sử theo id.
func (s *ActivityService) Get(id uint) (models.Activity, error) {
	var act models.Activity
	err := s.db.First(&act, id).Error
	return act, err
}

// List trả về hoạt động của task, mới nhất trước.
func (s *ActivityService) List(taskID uint) ([]models.Activity, error) {
	var acts []models.Activity
	err := s.db.Where("task_id = ?", taskID).
		Order("created_at DESC, id DESC").Find(&acts).Error
	return acts, err
}

func (s *ActivityService) DeleteForTask(taskID uint) error {
	return s.db.Where("task_id = ?", taskID).Delete(&models.Activity{}).Error
}
