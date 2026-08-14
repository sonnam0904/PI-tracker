package service

import (
	"fmt"
	"strings"
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

// TaskDateField chọn ngày nào của task dùng để đối chiếu với khoảng lọc.
type TaskDateField string

const (
	// TaskDateTouched — task CHẠM vào kỳ: bắt đầu HOẶC hoàn thành trong khoảng.
	// Đây là ngữ nghĩa của một kỳ báo cáo: task bắt đầu cuối tháng trước rồi mới
	// xong trong tháng này vẫn là công việc của tháng này.
	TaskDateTouched TaskDateField = "touched"
	TaskDateStart   TaskDateField = "startDate"
	TaskDateDone    TaskDateField = "doneDate"
	TaskDateCreated TaskDateField = "createdDate"
	TaskDateDue     TaskDateField = "dueDate"
	// TaskDateOverlap — khoảng sống của task (start → done) GIAO với kỳ, kể cả
	// task chưa xong và task chưa có ngày bắt đầu. Rộng hơn Touched: task bắt đầu
	// từ tháng 1 mà đến giờ chưa xong thì vẫn thuộc mọi tháng sau đó. Đây là dạng
	// khung Gantt cần — xem overlapWhere.
	TaskDateOverlap TaskDateField = "overlap"
)

// columns đổi tên field (theo cách gọi ở API/DTO) thành các cột DB cần so sánh.
// Nhiều cột = OR lại với nhau. Danh sách này là whitelist đóng — tên cột KHÔNG
// bao giờ lấy từ input, nên ghép vào câu SQL bên dưới là an toàn.
func (f TaskDateField) columns() ([]string, bool) {
	switch f {
	case "", TaskDateTouched:
		return []string{"start_date", "done_date"}, true
	case TaskDateStart:
		return []string{"start_date"}, true
	case TaskDateDone:
		return []string{"done_date"}, true
	case TaskDateCreated:
		return []string{"created_at"}, true
	case TaskDateDue:
		return []string{"due_date"}, true
	}
	return nil, false
}

// TaskDateFilter giới hạn danh sách task theo khoảng ngày. Hai mốc đều tùy chọn
// (nil = không chặn đầu đó) và tính CẢ ngày biên; Field rỗng = TaskDateTouched.
type TaskDateFilter struct {
	Field TaskDateField
	From  *time.Time
	To    *time.Time
}

// Empty báo bộ lọc không chặn gì — caller dùng để bỏ qua hẳn nhánh lọc.
func (f TaskDateFilter) Empty() bool { return f.From == nil && f.To == nil }

// where dựng mệnh đề WHERE cho khoảng ngày, đã bọc ngoặc sẵn để AND với các
// điều kiện khác không bị hiểu sai thứ tự.
//
// Cận trên dùng NỬA MỞ (`< to + 1 ngày`) chứ không phải `<= to`: created_at mang
// cả giờ-phút, nên `<= 2026-06-30` sẽ cắt mất mọi task tạo sau 00:00 ngày cuối kỳ.
// start_date/done_date luôn là 00:00 nên lỗi này không lộ ra ở đó — đúng kiểu sai
// chỉ hiện khi lọc theo createdDate.
//
// Không cần điều kiện IS NULL riêng: trong SQL, NULL không thỏa bất kỳ phép so
// sánh nào nên task chưa có ngày tương ứng tự bị loại khi đang lọc.
func (f TaskDateFilter) where() (string, []any, error) {
	if f.Field == TaskDateOverlap {
		return f.overlapWhere()
	}
	cols, ok := f.Field.columns()
	if !ok {
		return "", nil, fmt.Errorf("trường ngày %q không hợp lệ", f.Field)
	}
	ors := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols)*2)
	for _, c := range cols {
		conds := make([]string, 0, 2)
		if f.From != nil {
			conds = append(conds, c+" >= ?")
			args = append(args, *f.From)
		}
		if f.To != nil {
			conds = append(conds, c+" < ?")
			args = append(args, f.To.AddDate(0, 0, 1))
		}
		ors = append(ors, "("+strings.Join(conds, " AND ")+")")
	}
	return "(" + strings.Join(ors, " OR ") + ")", args, nil
}

// Get returns one task by id.
func (s *TaskService) Get(id uint) (models.Task, error) {
	var t models.Task
	err := s.db.First(&t, id).Error
	return t, err
}

// overlapWhere dựng điều kiện cho TaskDateOverlap.
//
// Đây là TẬP BAO (superset) của bộ lọc `rows` mà GanttView đang chạy ở client:
//
//	client giữ task khi:  start == null
//	                      HOẶC (start < mEnd VÀ barEnd > mStart)
//	                      với barEnd = done ?? start + max(estimateAiDays, 1)
//
// Nhánh `barEnd` có phép cộng ngày theo estimate — không dịch sang SQL được cho
// cả sqlite/postgres/mysql mà không viết ba nhánh dialect. Nên ở đây nới thành
// `done IS NULL` (task chưa xong thì luôn coi là còn sống): rộng hơn điều kiện
// client, KHÔNG BAO GIỜ hẹp hơn. Nhờ vậy client vẫn cắt chính xác như trước và
// giao diện không đổi một dòng nào, còn DB thì thôi phải trả về cả lịch sử.
//
// `done >= from` (không phải `>`) cũng là cố ý nới thêm một ngày biên, cùng lý do:
// thà bao rộng rồi để client cắt, hơn là để lệch một task so với UI hiện tại.
//
// Task chưa có start date luôn được kèm vì client hiện chúng ở MỌI tháng (để
// người dùng còn thấy mà xử lý); bỏ đi là chúng biến mất khỏi UI.
func (f TaskDateFilter) overlapWhere() (string, []any, error) {
	conds := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if f.To != nil {
		conds = append(conds, "start_date < ?")
		args = append(args, f.To.AddDate(0, 0, 1))
	}
	if f.From != nil {
		conds = append(conds, "(done_date IS NULL OR done_date >= ?)")
		args = append(args, *f.From)
	}
	return "(start_date IS NULL OR (" + strings.Join(conds, " AND ") + "))", args, nil
}

// List trả về TOÀN BỘ task của một workspace.
func (s *TaskService) List(wsID uint) ([]models.Task, error) {
	return s.ListFiltered(wsID, TaskDateFilter{})
}

// ListFiltered như List nhưng đẩy khoảng ngày xuống WHERE, để DB chỉ trả về
// những hàng thật sự thuộc kỳ. Lọc ở đây (không phải sau khi Find) là điều kiện
// để danh sách task lớn lên mà bộ nhớ và băng thông không tăng theo — index sẵn
// trên start_date/done_date/due_date lo phần còn lại.
func (s *TaskService) ListFiltered(wsID uint, f TaskDateFilter) ([]models.Task, error) {
	q := s.db.Where("workspace_id = ?", wsID)
	if !f.Empty() {
		cond, args, err := f.where()
		if err != nil {
			return nil, err
		}
		q = q.Where(cond, args...)
	}
	var tasks []models.Task
	err := q.Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

// TaskRef là task rút gọn còn id + tiêu đề — vừa đủ cho combobox chọn task
// (phụ thuộc, task gốc sinh bug).
type TaskRef struct {
	ID    uint   `json:"id"`
	Title string `json:"title"`
}

// ListRefs trả về id + tiêu đề của toàn bộ task trong workspace. Chỉ SELECT hai
// cột: picker cần đủ danh sách để chọn/tìm, nhưng không cần description — vốn
// chiếm phần lớn kích thước một hàng task.
func (s *TaskService) ListRefs(wsID uint) ([]TaskRef, error) {
	var refs []TaskRef
	err := s.db.Model(&models.Task{}).
		Select("id, title").
		Where("workspace_id = ?", wsID).
		Order("id DESC").Scan(&refs).Error
	return refs, err
}

func (s *TaskService) Save(task *models.Task) error {
	return s.db.Save(task).Error
}

// Fingerprint trả về (số task, id task lớn nhất) của một workspace — tín hiệu rẻ
// để poll phát hiện thay đổi cho đồng bộ realtime. COUNT bắt tạo/xóa, MAX(id)
// bắt tạo mới. Dùng thuần số nguyên (không MAX(updated_at)) để scan an toàn trên
// cả sqlite/postgres/mysql; phần "task bị sửa nội dung" đã do MAX(activities.id)
// lo (mọi lần sửa đều ghi một dòng activity).
func (s *TaskService) Fingerprint(wsID uint) (count int64, maxID int64, err error) {
	var row struct {
		N     int64
		MaxID int64
	}
	err = s.db.Model(&models.Task{}).
		Select("COUNT(*) AS n, COALESCE(MAX(id), 0) AS max_id").
		Where("workspace_id = ?", wsID).
		Scan(&row).Error
	return row.N, row.MaxID, err
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

// BugIDsByOrigin trả về map task gốc → ID các bug quy về nó qua related_task_id
// (tăng dần theo ID). Trả về chính ID chứ không chỉ số lượng vì báo cáo cần in
// "#89" ở cột "Bug phát sinh" rồi trỏ sang bảng bug; số lượng chỉ là len().
//
// Tính MỌI bug đã liên kết: mọi trạng thái (bug còn mở vẫn là lỗi task đó sinh
// ra) và không phân biệt bug được nhập vào tracker lúc nào.
//
// Trước đây có lọc thêm created_at < "ngày tính". Bỏ đi vì created_at là lúc GÕ
// DỮ LIỆU vào hệ thống, không phải mốc nghiệp vụ: nhập bù task tháng trước —
// cách dùng bình thường ở đây — làm mọi bug rơi ra ngoài mốc chốt sổ, nên cột
// "Bug phát sinh" của task cha và chỉ số "Tỷ lệ bug theo nguồn gốc" luôn bằng 0
func (s *TaskService) BugIDsByOrigin(wsID uint, originIDs []uint) (map[uint][]uint, error) {
	byOrigin := make(map[uint][]uint, len(originIDs))
	if len(originIDs) == 0 {
		return byOrigin, nil
	}
	var rows []struct {
		ID            uint
		RelatedTaskID uint
	}
	err := s.db.Model(&models.Task{}).
		Select("id, related_task_id").
		Where("workspace_id = ? AND type = ? AND related_task_id IN ?",
			wsID, models.TypeBug, originIDs).
		Order("id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		byOrigin[r.RelatedTaskID] = append(byOrigin[r.RelatedTaskID], r.ID)
	}
	return byOrigin, nil
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
