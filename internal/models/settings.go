package models

// Settings stores the PI baseline and target for the current dev.
// Defaults come from the team's measured baseline (1/7/2025 → 10/06/2026).
type Settings struct {
	ID          uint `gorm:"primaryKey"`
	WorkspaceID uint `gorm:"uniqueIndex"` // mỗi workspace một bộ cài đặt

	TBaseline  float64 // Throughput baseline (task/tháng/người; 1 tháng chuẩn = 4 tuần)
	CTBaseline float64 // Cycle time baseline (ngày/task)

	// Baseline chỉ số Điểm/tháng (điểm/tháng/người) — điểm tính theo size
	// task (SizePoints: S=1, M=3, L=6, XL=9). Mặc định 24 ≈ 4 task L/tháng.
	PointBaseline float64

	PITarget float64 // mục tiêu PI
	Capacity float64 // mức trần PI (mặc định 2)
}

func DefaultSettings() Settings {
	return Settings{
		TBaseline:     4.454810496, // 1.113702624 task/tuần × 4
		CTBaseline:    6.560209424, // ngày/task
		PointBaseline: 24,          // ≈ 4 task L (6 điểm) mỗi tháng
		PITarget:      1.20,
		Capacity:      2,
	}
}
