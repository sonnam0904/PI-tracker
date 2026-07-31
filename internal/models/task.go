package models

import (
	"fmt"
	"time"
)

// TaskType — loại task. Lưu SỐ trong DB (không phải chuỗi) để lọc theo type
// nhanh và index gọn; nhãn hiển thị tiếng Việt lấy qua Label().
type TaskType int

const (
	TypePlan        TaskType = 1 // Theo plan
	TypeBug         TaskType = 2 // Phát sinh (bug)
	TypePlanArising TaskType = 3 // Phát sinh theo plan
)

// ValidTaskType báo t có phải giá trị loại task hợp lệ không.
func ValidTaskType(t TaskType) bool {
	return t >= TypePlan && t <= TypePlanArising
}

// Label trả về nhãn tiếng Việt cho hiển thị (báo cáo, lịch sử hoạt động).
func (t TaskType) Label() string {
	switch t {
	case TypePlan:
		return "Theo plan"
	case TypeBug:
		return "Phát sinh (bug)"
	case TypePlanArising:
		return "Phát sinh theo plan"
	}
	return fmt.Sprintf("Loại %d", int(t))
}

type TaskSize string

const (
	SizeS  TaskSize = "S"
	SizeM  TaskSize = "M"
	SizeL  TaskSize = "L"
	SizeXL TaskSize = "XL"
)

// SizePoints — trọng số điểm theo size cho chỉ số Điểm/tháng (points/month):
// S=1, M=3, L=6, XL=9. Task không có size (dữ liệu cũ) tính như M để không
// bị thiệt oan. Đổi thang điểm thì chỉ sửa duy nhất chỗ này.
func SizePoints(s TaskSize) float64 {
	switch s {
	case SizeS:
		return 1
	case SizeM:
		return 3
	case SizeL:
		return 6
	case SizeXL:
		return 9
	default:
		return 3
	}
}

type TaskStatus string

const (
	StatusTodo       TaskStatus = "Todo"
	StatusInProgress TaskStatus = "In Progress"
	StatusBlocked    TaskStatus = "Blocked"
	StatusDone       TaskStatus = "Done"
)

// TaskPriority — mức ưu tiên xử lý, P1 khẩn cấp nhất.
type TaskPriority string

const (
	PriorityP1 TaskPriority = "P1" // khẩn cấp
	PriorityP2 TaskPriority = "P2" // cao
	PriorityP3 TaskPriority = "P3" // trung bình (mặc định)
	PriorityP4 TaskPriority = "P4" // thấp
)

// ValidPriority báo cho biết p có phải giá trị ưu tiên hợp lệ không.
func ValidPriority(p TaskPriority) bool {
	switch p {
	case PriorityP1, PriorityP2, PriorityP3, PriorityP4:
		return true
	}
	return false
}

// BugSeverity — mức nghiêm trọng, chỉ dùng cho task loại bug.
type BugSeverity string

const (
	SeverityCritical BugSeverity = "Critical"
	SeverityMajor    BugSeverity = "Major"
	SeverityMinor    BugSeverity = "Minor"
)

// ValidSeverity chấp nhận rỗng (chưa phân loại) hoặc một trong ba mức.
func ValidSeverity(s BugSeverity) bool {
	switch s {
	case "", SeverityCritical, SeverityMajor, SeverityMinor:
		return true
	}
	return false
}

// BugResolution — cách đóng bug, ghi khi bug chuyển Done.
type BugResolution string

const (
	ResolutionFixed       BugResolution = "Fixed"
	ResolutionWontFix     BugResolution = "Won't Fix"
	ResolutionCannotRepro BugResolution = "Cannot Reproduce"
	ResolutionDuplicate   BugResolution = "Duplicate"
)

// ValidResolution chấp nhận rỗng (chưa kết luận) hoặc một trong bốn cách đóng.
func ValidResolution(r BugResolution) bool {
	switch r {
	case "", ResolutionFixed, ResolutionWontFix, ResolutionCannotRepro, ResolutionDuplicate:
		return true
	}
	return false
}

// Task is one unit of work a dev tracks.
//
// Two estimates are kept separate on purpose:
//   - EstimateCustomerDays: số ngày báo cho khách hàng (estimate thương mại)
//   - EstimateAIDays: số ngày dự kiến khi làm thực tế có AI hỗ trợ
type Task struct {
	ID          uint   `gorm:"primaryKey"`
	WorkspaceID uint   `gorm:"index"` // task thuộc workspace nào
	Title       string `gorm:"not null"`
	Description string

	Type     TaskType     `gorm:"default:1;index"` // lọc theo type nhiều → đánh index
	Size     TaskSize     `gorm:"default:'M'"`
	Status   TaskStatus   `gorm:"default:'Todo';index"`
	Priority TaskPriority `gorm:"default:'P3';index"`

	// Người phụ trách = User.ID của thành viên workspace, nil = chưa gán.
	AssigneeID *uint `gorm:"index"`

	// ---- Nhóm field bug tracking: chỉ có nghĩa khi Type = Phát sinh (bug),
	// SaveTask sẽ xóa sạch khi task đổi sang loại khác. ----

	// Người phát hiện/báo bug (User.ID), phân biệt với người fix (AssigneeID).
	ReporterID *uint       `gorm:"index"`
	Severity   BugSeverity // Critical | Major | Minor, rỗng = chưa phân loại
	// Cách đóng bug: Fixed | Won't Fix | Cannot Reproduce | Duplicate.
	Resolution BugResolution
	// Task gốc sinh ra bug (nếu có) — phục vụ phân tích chất lượng theo feature.
	RelatedTaskID *uint `gorm:"index"`

	EstimateCustomerDays float64 // estimate báo khách hàng (ngày)
	EstimateAIDays       float64 // estimate thực tế làm bằng AI (ngày)
	// ActualDays: effort thực tế (ngày công) nhập tay khi task Done, 0 = chưa
	// nhập. Khác CycleDays (thời gian lịch): dùng để đo độ chính xác estimate.
	ActualDays float64

	AIUsed  bool   // task có dùng AI hay không
	Blocker string // mô tả blocker nếu có
	// BlockedDays bị trừ khỏi Cycle Time khi phân tích.
	BlockedDays float64

	// Cả ba ngày đều đánh index: chúng là cột lọc của TaskDateFilter (list_tasks
	// theo kỳ, báo cáo tháng), nên quét bảng ở đây là quét theo số task của cả
	// workspace thay vì theo số task thuộc kỳ.
	StartDate *time.Time `gorm:"index"` // ngày bắt đầu code thực sự
	DueDate   *time.Time `gorm:"index"` // hạn chót cam kết, nil = không có deadline
	DoneDate  *time.Time `gorm:"index"` // ngày merge PR / deploy staging

	CreatedAt time.Time
	UpdatedAt time.Time
}

// CycleDays returns Done − Start minus blocked time, in days.
// Returns 0 and false when the task has no start/done date yet.
func (t *Task) CycleDays() (float64, bool) {
	if t.StartDate == nil || t.DoneDate == nil {
		return 0, false
	}
	d := t.DoneDate.Sub(*t.StartDate).Hours()/24 - t.BlockedDays
	if d < 0 {
		d = 0
	}
	return d, true
}

// IsBug báo task có phải bug không (nhóm field bug tracking chỉ dùng khi true).
func (t *Task) IsBug() bool { return t.Type == TypeBug }

// Overdue báo task đã trễ hạn tính đến thời điểm now:
// có DueDate, chưa Done, và now đã qua hết ngày DueDate.
func (t *Task) Overdue(now time.Time) bool {
	if t.DueDate == nil || t.Status == StatusDone {
		return false
	}
	endOfDue := time.Date(t.DueDate.Year(), t.DueDate.Month(), t.DueDate.Day(),
		0, 0, 0, 0, t.DueDate.Location()).AddDate(0, 0, 1)
	return !now.Before(endOfDue)
}

// LeadDays returns Done − Created in days.
func (t *Task) LeadDays() (float64, bool) {
	if t.DoneDate == nil {
		return 0, false
	}
	d := t.DoneDate.Sub(t.CreatedAt).Hours() / 24
	if d < 0 {
		d = 0
	}
	return d, true
}
