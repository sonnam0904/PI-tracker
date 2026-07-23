package service

import (
	"time"

	"gorm.io/gorm"

	"taskmanager/internal/models"
)

type TaskService struct {
	db *gorm.DB
}

func NewTaskService(db *gorm.DB) *TaskService {
	return &TaskService{db: db}
}

// Get returns one task by id.
func (s *TaskService) Get(id uint) (models.Task, error) {
	var t models.Task
	err := s.db.First(&t, id).Error
	return t, err
}

// List trả về task của một workspace.
func (s *TaskService) List(wsID uint) ([]models.Task, error) {
	var tasks []models.Task
	err := s.db.Where("workspace_id = ?", wsID).Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

func (s *TaskService) Save(task *models.Task) error {
	return s.db.Save(task).Error
}

func (s *TaskService) Delete(id uint) error {
	return s.db.Delete(&models.Task{}, id).Error
}

// DoneBetween returns tasks that are actually Done with from <= DoneDate < to.
// assigneeID != 0 lọc theo người phụ trách.
func (s *TaskService) DoneBetween(wsID uint, from, to time.Time, assigneeID uint) ([]models.Task, error) {
	var tasks []models.Task
	q := s.db.Where("workspace_id = ? AND status = ? AND done_date IS NOT NULL AND done_date >= ? AND done_date < ?",
		wsID, models.StatusDone, from, to)
	if assigneeID != 0 {
		q = q.Where("assignee_id = ?", assigneeID)
	}
	err := q.Find(&tasks).Error
	return tasks, err
}

// CountBugsByOrigin đếm số bug quy về từng task gốc qua related_task_id.
// Tính MỌI trạng thái bug (bug còn mở vẫn là lỗi task đó sinh ra), nhưng chỉ
// bug tạo trước mốc createdBefore để tôn trọng "ngày tính" khi chốt sổ báo cáo.
func (s *TaskService) CountBugsByOrigin(wsID uint, originIDs []uint, createdBefore time.Time) (map[uint]int, error) {
	counts := make(map[uint]int, len(originIDs))
	if len(originIDs) == 0 {
		return counts, nil
	}
	var rows []struct {
		RelatedTaskID uint
		N             int
	}
	err := s.db.Model(&models.Task{}).
		Select("related_task_id, COUNT(*) AS n").
		Where("workspace_id = ? AND type = ? AND related_task_id IN ? AND created_at < ?",
			wsID, models.TypeBug, originIDs, createdBefore).
		Group("related_task_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		counts[r.RelatedTaskID] = r.N
	}
	return counts, nil
}

// DueSoonForUser trả về task gán cho user (mọi workspace) chưa Done và có
// hạn chót trước mốc deadline — nguồn cho nhắc việc sắp/quá hạn.
func (s *TaskService) DueSoonForUser(userID uint, deadline time.Time) ([]models.Task, error) {
	var tasks []models.Task
	err := s.db.Where("assignee_id = ? AND status <> ? AND due_date IS NOT NULL AND due_date < ?",
		userID, models.StatusDone, deadline).
		Order("due_date ASC").Find(&tasks).Error
	return tasks, err
}

// RecentDone trả về tối đa limit task Done gần nhất của workspace (mới Done
// trước), làm dữ liệu tham chiếu (grounding) cho gợi ý estimate bằng AI.
func (s *TaskService) RecentDone(wsID uint, limit int) ([]models.Task, error) {
	if limit <= 0 {
		limit = 30
	}
	var tasks []models.Task
	err := s.db.
		Where("workspace_id = ? AND status = ? AND done_date IS NOT NULL", wsID, models.StatusDone).
		Order("done_date DESC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// CountWIP đếm task đang In Progress/Blocked; assigneeID != 0 lọc theo người phụ trách.
func (s *TaskService) CountWIP(wsID uint, assigneeID uint) (int64, error) {
	q := s.db.Model(&models.Task{}).
		Where("workspace_id = ? AND status IN ?", wsID,
			[]models.TaskStatus{models.StatusInProgress, models.StatusBlocked})
	if assigneeID != 0 {
		q = q.Where("assignee_id = ?", assigneeID)
	}
	var n int64
	err := q.Count(&n).Error
	return n, err
}
