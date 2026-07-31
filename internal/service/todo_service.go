package service

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"taskmanager/internal/models"
)

type TodoService struct {
	db *gorm.DB
}

func NewTodoService(db *gorm.DB) *TodoService {
	return &TodoService{db: db}
}

func (s *TodoService) List(taskID uint) ([]models.TodoItem, error) {
	var items []models.TodoItem
	err := s.db.Where("task_id = ?", taskID).Order("id").Find(&items).Error
	return items, err
}

func (s *TodoService) Add(taskID uint, title string) (models.TodoItem, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return models.TodoItem{}, fmt.Errorf("nội dung việc cần làm không được để trống")
	}
	item := models.TodoItem{TaskID: taskID, Title: title}
	err := s.db.Create(&item).Error
	return item, err
}

// Get nạp một mục checklist theo id — dùng để kiểm quyền (mục thuộc task nào,
// task đó có thuộc workspace hiện tại không) TRƯỚC khi sửa/xóa.
func (s *TodoService) Get(id uint) (models.TodoItem, error) {
	var item models.TodoItem
	err := s.db.First(&item, id).Error
	return item, err
}

// SetDone đổi trạng thái một mục và trả về mục đó (để ghi log).
func (s *TodoService) SetDone(id uint, done bool) (models.TodoItem, error) {
	var item models.TodoItem
	if err := s.db.First(&item, id).Error; err != nil {
		return item, err
	}
	item.Done = done
	err := s.db.Model(&item).Update("done", done).Error
	return item, err
}

// Delete xóa một mục và trả về mục đã xóa (để ghi log).
func (s *TodoService) Delete(id uint) (models.TodoItem, error) {
	var item models.TodoItem
	if err := s.db.First(&item, id).Error; err != nil {
		return item, err
	}
	err := s.db.Delete(&models.TodoItem{}, id).Error
	return item, err
}

func (s *TodoService) DeleteForTask(taskID uint) error {
	return s.db.Where("task_id = ?", taskID).Delete(&models.TodoItem{}).Error
}

// Counts trả về map taskID → [tổng số mục, số mục done] cho badge trên board.
//
// CẢNH BÁO: không có WHERE nào — gộp cả bảng todo_items của MỌI workspace. Chỉ
// dùng khi thật sự cần toàn bộ; có danh sách task trong tay thì gọi CountsForTasks.
func (s *TodoService) Counts() (map[uint][2]int, error) {
	return s.counts(nil)
}

// CountsForTasks như Counts nhưng chỉ gộp trong các task được nêu tên. Cần thiết
// khi danh sách task đã bị lọc: lọc task ở SQL rồi lại gộp checklist toàn bảng
// thì công sức lọc mất sạch, vì query đắt nhất vẫn quét đủ mọi hàng.
func (s *TodoService) CountsForTasks(taskIDs []uint) (map[uint][2]int, error) {
	if len(taskIDs) == 0 {
		return map[uint][2]int{}, nil
	}
	return s.counts(taskIDs)
}

// counts gộp checklist theo task; taskIDs nil = không giới hạn.
func (s *TodoService) counts(taskIDs []uint) (map[uint][2]int, error) {
	var rows []struct {
		TaskID uint
		Total  int
		Done   int
	}
	q := s.db.Model(&models.TodoItem{}).
		Select("task_id, COUNT(*) as total, SUM(CASE WHEN done THEN 1 ELSE 0 END) as done")
	if taskIDs != nil {
		q = q.Where("task_id IN ?", taskIDs)
	}
	if err := q.Group("task_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[uint][2]int, len(rows))
	for _, r := range rows {
		m[r.TaskID] = [2]int{r.Total, r.Done}
	}
	return m, nil
}
