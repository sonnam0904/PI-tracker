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

// Log ghi một dòng lịch sử cho task. wsID (workspace của task) được lưu kèm để
// poll đồng bộ realtime lấy MAX(id) theo workspace mà không phải join tasks.
func (s *ActivityService) Log(wsID, taskID uint, actorName, kind, content string) error {
	return s.db.Create(&models.Activity{
		WorkspaceID: wsID, TaskID: taskID, ActorName: actorName, Kind: kind, Content: content,
	}).Error
}

// LogComment ghi bình luận gốc, trả về bản ghi (ID dùng gắn vào notification).
func (s *ActivityService) LogComment(wsID, taskID uint, actorName, content string) (models.Activity, error) {
	act := models.Activity{WorkspaceID: wsID, TaskID: taskID, ActorName: actorName, Kind: "comment", Content: content}
	err := s.db.Create(&act).Error
	return act, err
}

// LogReply ghi bình luận trả lời một bình luận khác (parentID), trả về bản ghi.
func (s *ActivityService) LogReply(wsID, taskID uint, actorName, content string, parentID uint) (models.Activity, error) {
	act := models.Activity{
		WorkspaceID: wsID, TaskID: taskID, ActorName: actorName, Kind: "comment", Content: content,
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

// MaxIDForWorkspace trả về id lớn nhất trong bảng activities của một workspace.
// Activity là sổ ghi chung: mọi thao tác (sửa task, toggle/thêm/xóa checklist,
// bình luận, đổi trạng thái) đều tạo một dòng, nên id lớn nhất tăng lên là tín
// hiệu rẻ để poll phát hiện thay đổi. Nhờ cột workspace_id denormalize + index
// (workspace_id, id), truy vấn là index-only MAX — O(log n), không join. 0 khi
// workspace chưa có activity.
func (s *ActivityService) MaxIDForWorkspace(wsID uint) (uint, error) {
	var maxID uint
	err := s.db.Model(&models.Activity{}).
		Where("workspace_id = ?", wsID).
		Select("COALESCE(MAX(id), 0)").
		Scan(&maxID).Error
	return maxID, err
}
