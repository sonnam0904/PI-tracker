package service

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"taskmanager/internal/models"
)

type SavedViewService struct {
	db *gorm.DB
}

func NewSavedViewService(db *gorm.DB) *SavedViewService {
	return &SavedViewService{db: db}
}

// List trả về các view của user trong workspace, theo thứ tự tab (position, id).
func (s *SavedViewService) List(wsID, userID uint) ([]models.SavedView, error) {
	var views []models.SavedView
	err := s.db.Where("workspace_id = ? AND user_id = ?", wsID, userID).
		Order("position, id").Find(&views).Error
	return views, err
}

// Get trả về một view theo id (dùng cho kiểm tra quyền sở hữu ở tầng binding).
func (s *SavedViewService) Get(id uint) (models.SavedView, error) {
	var v models.SavedView
	err := s.db.First(&v, id).Error
	return v, err
}

// Create thêm view mới vào cuối dãy tab.
func (s *SavedViewService) Create(wsID, userID uint, name, filters string) (models.SavedView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.SavedView{}, fmt.Errorf("tên view không được để trống")
	}
	var maxPos int
	if err := s.db.Model(&models.SavedView{}).
		Where("workspace_id = ? AND user_id = ?", wsID, userID).
		Select("COALESCE(MAX(position), 0)").Scan(&maxPos).Error; err != nil {
		return models.SavedView{}, err
	}
	v := models.SavedView{
		WorkspaceID: wsID, UserID: userID,
		Name: name, Filters: filters, Position: maxPos + 1,
	}
	err := s.db.Create(&v).Error
	return v, err
}

// Update đổi tên và/hoặc bộ lọc của view (đổi tên giữ filters cũ thì truyền
// lại filters hiện tại — binding luôn gửi đủ cả hai).
func (s *SavedViewService) Update(id uint, name, filters string) (models.SavedView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.SavedView{}, fmt.Errorf("tên view không được để trống")
	}
	var v models.SavedView
	if err := s.db.First(&v, id).Error; err != nil {
		return v, err
	}
	v.Name, v.Filters = name, filters
	err := s.db.Save(&v).Error
	return v, err
}

func (s *SavedViewService) Delete(id uint) error {
	return s.db.Delete(&models.SavedView{}, id).Error
}

// Fingerprint tạo dấu vân tay cho toàn bộ view của (workspace, user): bắt tạo,
// xóa, đổi tên, sửa bộ lọc và đổi thứ tự tab. View mỗi user chỉ vài cái nên nạp
// rồi băm nội dung (id + vị trí + tên + filters) là rẻ và chạy được trên mọi
// driver — tránh phụ thuộc MAX(updated_at) vốn scan lệch kiểu trên sqlite.
func (s *SavedViewService) Fingerprint(wsID, userID uint) (string, error) {
	views, err := s.List(wsID, userID)
	if err != nil {
		return "", err
	}
	h := fnv.New64a()
	for _, v := range views {
		// \x1f ngăn field, \x1e ngăn bản ghi — tránh nhập nhằng khi nối chuỗi.
		fmt.Fprintf(h, "%d\x1f%d\x1f%s\x1f%s\x1e", v.ID, v.Position, v.Name, v.Filters)
	}
	return strconv.FormatUint(h.Sum64(), 36), nil
}
