package service

import (
	"fmt"

	"gorm.io/gorm"

	"taskmanager/internal/models"
)

type DependencyService struct {
	db *gorm.DB
}

func NewDependencyService(db *gorm.DB) *DependencyService {
	return &DependencyService{db: db}
}

// DependsOnMap trả về map taskID → danh sách task phải xong trước (predecessor),
// cho cả workspace trong một query — dùng dựng mũi tên Gantt và cờ phụ thuộc.
func (s *DependencyService) DependsOnMap(wsID uint) (map[uint][]uint, error) {
	return s.dependsOnMap(wsID, nil)
}

// DependsOnMapForTasks như DependsOnMap nhưng chỉ lấy phụ thuộc CỦA các task được
// nêu tên — dùng khi danh sách task đã bị lọc theo kỳ, để số hàng đọc lên tỉ lệ
// với kết quả trả về chứ không với kích thước workspace.
func (s *DependencyService) DependsOnMapForTasks(wsID uint, taskIDs []uint) (map[uint][]uint, error) {
	if len(taskIDs) == 0 {
		return map[uint][]uint{}, nil
	}
	return s.dependsOnMap(wsID, taskIDs)
}

func (s *DependencyService) dependsOnMap(wsID uint, taskIDs []uint) (map[uint][]uint, error) {
	q := s.db.Where("workspace_id = ?", wsID)
	if taskIDs != nil {
		q = q.Where("task_id IN ?", taskIDs)
	}
	var deps []models.TaskDependency
	if err := q.Order("blocked_by_id").Find(&deps).Error; err != nil {
		return nil, err
	}
	m := make(map[uint][]uint, len(deps))
	for _, d := range deps {
		m[d.TaskID] = append(m[d.TaskID], d.BlockedByID)
	}
	return m, nil
}

// PredecessorsOf trả về các task phải xong trước taskID (đã sắp xếp).
func (s *DependencyService) PredecessorsOf(taskID uint) ([]uint, error) {
	var deps []models.TaskDependency
	err := s.db.Where("task_id = ?", taskID).
		Order("blocked_by_id").Find(&deps).Error
	if err != nil {
		return nil, err
	}
	ids := make([]uint, len(deps))
	for i, d := range deps {
		ids[i] = d.BlockedByID
	}
	return ids, nil
}

// WouldCycle báo việc cho taskID phụ thuộc vào blockedBy có tạo thành vòng
// không: dựa trên đồ thị "phụ thuộc" hiện có (đỉnh → các predecessor của nó),
// nếu một predecessor p đề xuất lại (gián tiếp) phụ thuộc ngược về taskID thì
// thêm cạnh p → taskID sẽ khép vòng.
func (s *DependencyService) WouldCycle(wsID, taskID uint, blockedBy []uint) (bool, error) {
	adj, err := s.DependsOnMap(wsID)
	if err != nil {
		return false, err
	}
	for _, p := range blockedBy {
		if p == taskID {
			return true, nil
		}
		if reaches(adj, p, taskID) {
			return true, nil
		}
	}
	return false, nil
}

// reaches báo từ from có đi tới target qua các cạnh "phụ thuộc" (đỉnh → các
// predecessor) hay không — DFS lặp, chống lặp vô hạn bằng tập đã thăm.
func reaches(adj map[uint][]uint, from, target uint) bool {
	seen := make(map[uint]bool)
	stack := []uint{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == target {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		stack = append(stack, adj[n]...)
	}
	return false
}

// SetForTask thay toàn bộ predecessor của taskID bằng blockedBy (bỏ 0 và chính
// nó, khử trùng lặp). Chặn phụ thuộc vòng trước khi ghi; ghi trong transaction
// để không để lại trạng thái dở nếu lỗi giữa chừng.
func (s *DependencyService) SetForTask(wsID, taskID uint, blockedBy []uint) error {
	clean := make([]uint, 0, len(blockedBy))
	seen := make(map[uint]bool)
	for _, p := range blockedBy {
		if p == 0 || p == taskID || seen[p] {
			continue
		}
		seen[p] = true
		clean = append(clean, p)
	}
	if cyc, err := s.WouldCycle(wsID, taskID, clean); err != nil {
		return err
	} else if cyc {
		return fmt.Errorf("phụ thuộc tạo thành vòng lặp — task không thể (gián tiếp) chờ chính nó")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", taskID).
			Delete(&models.TaskDependency{}).Error; err != nil {
			return err
		}
		for _, p := range clean {
			if err := tx.Create(&models.TaskDependency{
				WorkspaceID: wsID, TaskID: taskID, BlockedByID: p,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteForTask xóa mọi cạnh phụ thuộc liên quan taskID (cả chiều bị chặn lẫn
// chiều chặn task khác) — gọi khi xóa task để không còn cạnh mồ côi.
func (s *DependencyService) DeleteForTask(taskID uint) error {
	return s.db.Where("task_id = ? OR blocked_by_id = ?", taskID, taskID).
		Delete(&models.TaskDependency{}).Error
}
