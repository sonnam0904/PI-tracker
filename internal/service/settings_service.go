package service

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"taskmanager/internal/models"
)

type SettingsService struct {
	db *gorm.DB
}

func NewSettingsService(db *gorm.DB) *SettingsService {
	return &SettingsService{db: db}
}

// Get returns the settings row of a workspace, creating defaults on first use.
func (s *SettingsService) Get(wsID uint) (models.Settings, error) {
	var st models.Settings
	err := s.db.Where("workspace_id = ?", wsID).First(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		st = models.DefaultSettings()
		st.WorkspaceID = wsID
		// Create idempotent: lúc startup Wails có thể gọi Get đồng thời từ nhiều
		// binding — cả hai thấy trống rồi cùng Create. ON CONFLICT DO NOTHING để
		// lời gọi thua race không lỗi duplicate key; sau đó đọc lại bản đã có.
		err = s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "workspace_id"}},
			DoNothing: true,
		}).Create(&st).Error
		if err == nil && st.ID == 0 {
			err = s.db.Where("workspace_id = ?", wsID).First(&st).Error
		}
	}
	// Vá bản ghi đời cũ: cột PointBaseline thêm sau nên đang là 0 —
	// điền mặc định và lưu lại để chỉ số Điểm/tháng có baseline ngay.
	if err == nil && st.PointBaseline <= 0 {
		st.PointBaseline = models.DefaultSettings().PointBaseline
		err = s.db.Save(&st).Error
	}
	return st, err
}

func (s *SettingsService) Save(st *models.Settings) error {
	return s.db.Save(st).Error
}
