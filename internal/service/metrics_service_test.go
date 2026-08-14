package service

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"taskmanager/internal/models"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Task{}, &models.Settings{},
		&models.TodoItem{}, &models.Activity{}, &models.StatusChange{},
		&models.User{}, &models.Workspace{}, &models.WorkspaceMember{},
		&models.Invitation{}, &models.Notification{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

type env struct {
	db      *gorm.DB
	tasks   *TaskService
	ws      *WorkspaceService
	metrics *MetricsService
	wsID    uint
	ownerID uint
}

// testEnv dựng workspace 1 thành viên (owner) — tương đương team 1 người.
func testEnv(t *testing.T) env {
	t.Helper()
	db := testDB(t)
	tasks := NewTaskService(db)
	settings := NewSettingsService(db)
	ws := NewWorkspaceService(db)

	owner := models.User{Username: "owner", PasswordHash: "x"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	w, err := ws.Create("test-ws", owner.ID)
	if err != nil {
		t.Fatalf("create ws: %v", err)
	}
	return env{
		db: db, tasks: tasks, ws: ws,
		metrics: NewMetricsService(tasks, ws, settings),
		wsID:    w.ID, ownerID: owner.ID,
	}
}

// addMember thêm 1 user mới vào workspace, trả về userID.
func (e *env) addMember(t *testing.T, username string) uint {
	t.Helper()
	u := models.User{Username: username, PasswordHash: "x"}
	if err := e.db.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := e.db.Create(&models.WorkspaceMember{
		WorkspaceID: e.wsID, UserID: u.ID, Role: "member",
	}).Error; err != nil {
		t.Fatalf("add member: %v", err)
	}
	return u.ID
}

func TestComputePI(t *testing.T) {
	e := testEnv(t)

	// Giữa tháng 7/2026: đã trôi qua 14 ngày = 2 tuần; cả tháng 31 ngày.
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)

	// 2 task Done tích lũy trong tháng 7 (31 ngày = 31/28 tháng chuẩn), cycle 6 ngày/task
	// → T tích lũy = 2 ÷ (31/28) ≈ 1.806 task/tháng, CT = 6 ngày/task.
	for i := 0; i < 2; i++ {
		done := time.Date(2026, 7, 8+i*5, 0, 0, 0, 0, time.Local)
		start := done.AddDate(0, 0, -6)
		task := models.Task{
			WorkspaceID: e.wsID,
			Title:       "task",
			Status:      models.StatusDone,
			StartDate:   &start,
			DoneDate:    &done,
		}
		if err := e.tasks.Save(&task); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	// Task Done của THÁNG TRƯỚC: không được đếm vào tháng 7.
	prevDone := time.Date(2026, 6, 20, 0, 0, 0, 0, time.Local)
	prevStart := prevDone.AddDate(0, 0, -3)
	if err := e.tasks.Save(&models.Task{
		WorkspaceID: e.wsID,
		Title:       "june task", Status: models.StatusDone, StartDate: &prevStart, DoneDate: &prevDone,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Task của WORKSPACE KHÁC: không được đếm.
	if err := e.tasks.Save(&models.Task{
		WorkspaceID: e.wsID + 99,
		Title:       "other ws", Status: models.StatusDone,
		StartDate: &prevStart, DoneDate: &prevDone,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Task In Progress nhưng lỡ có done_date: KHÔNG được đếm vào Done.
	strayDone := time.Date(2026, 7, 14, 0, 0, 0, 0, time.Local)
	strayStart := strayDone.AddDate(0, 0, -2)
	if err := e.tasks.Save(&models.Task{
		WorkspaceID: e.wsID,
		Title:       "in-progress with stray done_date", Status: models.StatusInProgress,
		StartDate: &strayStart, DoneDate: &strayDone,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	m, st, err := e.metrics.Compute(e.wsID, now, now, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.DoneCount != 2 {
		t.Fatalf("DoneCount = %d, want 2", m.DoneCount)
	}
	if m.WIP != 1 {
		t.Errorf("WIP = %d, want 1", m.WIP)
	}
	wantThroughput := 2.0 / (31.0 / 28)
	if math.Abs(m.Throughput-wantThroughput) > 1e-9 {
		t.Errorf("Throughput = %f, want %f (tích lũy trên cả tháng)", m.Throughput, wantThroughput)
	}
	if math.Abs(m.CycleTime-6) > 1e-9 {
		t.Errorf("CycleTime = %f, want 6 ngày/task", m.CycleTime)
	}
	if m.TeamSize != 1 {
		t.Fatalf("TeamSize = %d, want 1 (chỉ owner)", m.TeamSize)
	}
	wantPI := (m.Throughput / m.TeamTBaseline) * (st.CTBaseline / m.CycleTime)
	if math.Abs(m.PI-wantPI) > 1e-9 {
		t.Errorf("PI = %f, want %f", m.PI, wantPI)
	}

	// Chưa đạt → advice chỉ ra 2 hướng, ngưỡng khớp target.
	a := e.metrics.Advise(m, st)
	if a.Achieved {
		t.Fatalf("Advise: expected not achieved, PI=%f target=%f", m.PI, st.PITarget)
	}
	wantT := st.PITarget * m.TeamTBaseline * m.CycleTime / st.CTBaseline
	if math.Abs(a.RequiredThroughput-wantT) > 1e-9 {
		t.Errorf("RequiredThroughput = %f, want %f", a.RequiredThroughput, wantT)
	}
	wantCT := m.Throughput * st.CTBaseline / (st.PITarget * m.TeamTBaseline)
	if math.Abs(a.RequiredCycleTime-wantCT) > 1e-9 {
		t.Errorf("RequiredCycleTime = %f, want %f", a.RequiredCycleTime, wantCT)
	}

	// Thêm 1 thành viên → TeamSize = 2, baseline team gấp đôi → PI giảm một nửa.
	e.addMember(t, "member2")
	m2, _, err := e.metrics.Compute(e.wsID, now, now, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m2.TeamSize != 2 {
		t.Fatalf("TeamSize = %d, want 2 (đếm thành viên workspace)", m2.TeamSize)
	}
	if math.Abs(m2.PI-m.PI/2) > 1e-9 {
		t.Errorf("PI team 2 người = %f, want %f", m2.PI, m.PI/2)
	}
}

func TestComputePerAssignee(t *testing.T) {
	e := testEnv(t)
	an := e.ownerID
	binh := e.addMember(t, "binh")

	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)
	mk := func(day, cycle int, assignee uint, status models.TaskStatus) {
		done := time.Date(2026, 7, day, 0, 0, 0, 0, time.Local)
		start := done.AddDate(0, 0, -cycle)
		task := models.Task{WorkspaceID: e.wsID, Title: "t", Status: status, AssigneeID: &assignee, StartDate: &start}
		if status == models.StatusDone {
			task.DoneDate = &done
		}
		if err := e.tasks.Save(&task); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	mk(8, 4, an, models.StatusDone)
	mk(12, 4, an, models.StatusDone)
	mk(10, 2, binh, models.StatusDone)
	mk(14, 1, an, models.StatusInProgress)

	all, _, err := e.metrics.Compute(e.wsID, now, now, 0)
	if err != nil {
		t.Fatalf("compute all: %v", err)
	}
	if all.DoneCount != 3 || all.TeamSize != 2 || all.WIP != 1 {
		t.Errorf("all: Done=%d TeamSize=%d WIP=%d, want 3/2/1", all.DoneCount, all.TeamSize, all.WIP)
	}

	ma, st, err := e.metrics.Compute(e.wsID, now, now, an)
	if err != nil {
		t.Fatalf("compute an: %v", err)
	}
	if ma.DoneCount != 2 || ma.TeamSize != 1 || ma.WIP != 1 {
		t.Errorf("an: Done=%d TeamSize=%d WIP=%d, want 2/1/1", ma.DoneCount, ma.TeamSize, ma.WIP)
	}
	if math.Abs(ma.CycleTime-4) > 1e-9 {
		t.Errorf("an: CycleTime = %f, want 4", ma.CycleTime)
	}
	if math.Abs(ma.TeamTBaseline-st.TBaseline) > 1e-9 {
		t.Errorf("an: TeamTBaseline = %f, want %f (baseline 1 người)", ma.TeamTBaseline, st.TBaseline)
	}

	mb, _, err := e.metrics.Compute(e.wsID, now, now, binh)
	if err != nil {
		t.Fatalf("compute binh: %v", err)
	}
	if mb.DoneCount != 1 || mb.WIP != 0 {
		t.Errorf("binh: Done=%d WIP=%d, want 1/0", mb.DoneCount, mb.WIP)
	}
	if math.Abs(mb.CycleTime-2) > 1e-9 {
		t.Errorf("binh: CycleTime = %f, want 2", mb.CycleTime)
	}
}

func TestComputeAsOfDate(t *testing.T) {
	e := testEnv(t)

	// Task kế hoạch ghi Done date 20/07 (tương lai so với 15/07).
	month := time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local)
	done := time.Date(2026, 7, 20, 0, 0, 0, 0, time.Local)
	start := done.AddDate(0, 0, -6)
	if err := e.tasks.Save(&models.Task{
		WorkspaceID: e.wsID,
		Title:       "planned done", Status: models.StatusDone, StartDate: &start, DoneDate: &done,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Ngày tính 15/07: task Done tương lai CHƯA được đếm.
	asOf := time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)
	m, _, err := e.metrics.Compute(e.wsID, month, asOf, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.DoneCount != 0 || m.Throughput != 0 || m.PI != 0 {
		t.Errorf("asOf 15/07: Done=%d T=%f PI=%f, want 0/0/0", m.DoneCount, m.Throughput, m.PI)
	}

	// Ngày tính cuối tháng (mô phỏng chốt sổ): được đếm, T tích lũy cả tháng.
	asOf = time.Date(2026, 7, 31, 0, 0, 0, 0, time.Local)
	m, _, err = e.metrics.Compute(e.wsID, month, asOf, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.DoneCount != 1 {
		t.Fatalf("asOf 31/07: DoneCount = %d, want 1", m.DoneCount)
	}
	wantT := 1 / (31.0 / 28)
	if math.Abs(m.Throughput-wantT) > 1e-9 {
		t.Errorf("Throughput = %f, want %f (tích lũy cả tháng)", m.Throughput, wantT)
	}
}

func TestComputePIPastMonth(t *testing.T) {
	e := testEnv(t)

	// Xem lại tháng 6/2026 (30 ngày) từ giữa tháng 7: dùng trọn số tuần của tháng.
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)
	month := time.Date(2026, 6, 10, 0, 0, 0, 0, time.Local)

	done := time.Date(2026, 6, 20, 0, 0, 0, 0, time.Local)
	start := done.AddDate(0, 0, -3)
	if err := e.tasks.Save(&models.Task{
		WorkspaceID: e.wsID,
		Title:       "june task", Status: models.StatusDone, StartDate: &start, DoneDate: &done,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	m, _, err := e.metrics.Compute(e.wsID, month, now, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.DoneCount != 1 {
		t.Fatalf("DoneCount = %d, want 1", m.DoneCount)
	}
	if math.Abs(m.Throughput-1/(30.0/28)) > 1e-9 {
		t.Errorf("Throughput = %f, want %f", m.Throughput, 1/(30.0/28))
	}
}

// Bug Done không được tính vào T/CT/PI của task; có T (bug/tháng) và CT (ngày/bug) riêng.
func TestComputeBugSeparated(t *testing.T) {
	e := testEnv(t)
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)

	mk := func(day, cycle int, typ models.TaskType) {
		done := time.Date(2026, 7, day, 0, 0, 0, 0, time.Local)
		start := done.AddDate(0, 0, -cycle)
		if err := e.tasks.Save(&models.Task{
			WorkspaceID: e.wsID, Title: "t", Type: typ,
			Status: models.StatusDone, StartDate: &start, DoneDate: &done,
		}); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	// 2 task thường cycle 6 ngày + 3 bug cycle 2 ngày.
	mk(8, 6, models.TypePlan)
	mk(12, 6, models.TypePlanArising)
	mk(9, 2, models.TypeBug)
	mk(10, 2, models.TypeBug)
	mk(11, 2, models.TypeBug)

	m, st, err := e.metrics.Compute(e.wsID, now, now, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}

	// Chỉ số task: chỉ đếm 2 task thường.
	if m.DoneCount != 2 {
		t.Fatalf("DoneCount = %d, want 2 (không tính bug)", m.DoneCount)
	}
	if math.Abs(m.Throughput-2.0/(31.0/28)) > 1e-9 {
		t.Errorf("Throughput = %f, want %f (không tính bug)", m.Throughput, 2.0/(31.0/28))
	}
	if math.Abs(m.CycleTime-6) > 1e-9 {
		t.Errorf("CycleTime = %f, want 6 (bug cycle 2 không được trộn vào)", m.CycleTime)
	}
	wantPI := (m.Throughput / m.TeamTBaseline) * (st.CTBaseline / m.CycleTime)
	if math.Abs(m.PI-wantPI) > 1e-9 {
		t.Errorf("PI = %f, want %f (chỉ từ task thường)", m.PI, wantPI)
	}

	// Chỉ số bug riêng.
	if m.BugDoneCount != 3 {
		t.Fatalf("BugDoneCount = %d, want 3", m.BugDoneCount)
	}
	if math.Abs(m.BugThroughput-3.0/(31.0/28)) > 1e-9 {
		t.Errorf("BugThroughput = %f, want %f", m.BugThroughput, 3.0/(31.0/28))
	}
	if math.Abs(m.BugCycleTime-2) > 1e-9 {
		t.Errorf("BugCycleTime = %f, want 2 ngày/bug", m.BugCycleTime)
	}
	// Chất lượng: 3 bug / 2 task = 1.5 bug/task.
	if math.Abs(m.BugRatio-1.5) > 1e-9 {
		t.Errorf("BugRatio = %f, want 1.5", m.BugRatio)
	}
}

// Tháng chỉ có bug, không có task Done → BugRatio giữ 0 (không chia cho 0),
// người xem dựa vào BugDoneCount.
func TestComputeBugRatioNoTasks(t *testing.T) {
	e := testEnv(t)
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)

	done := time.Date(2026, 7, 10, 0, 0, 0, 0, time.Local)
	start := done.AddDate(0, 0, -2)
	if err := e.tasks.Save(&models.Task{
		WorkspaceID: e.wsID, Title: "bug", Type: models.TypeBug,
		Status: models.StatusDone, StartDate: &start, DoneDate: &done,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	m, _, err := e.metrics.Compute(e.wsID, now, now, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.DoneCount != 0 || m.BugDoneCount != 1 {
		t.Fatalf("Done=%d BugDone=%d, want 0/1", m.DoneCount, m.BugDoneCount)
	}
	if m.BugRatio != 0 {
		t.Errorf("BugRatio = %f, want 0 khi không có task Done", m.BugRatio)
	}
}

// Bug quy về task gốc qua RelatedTaskID: tính cho tháng task gốc Done, bất kể
// bug được fix tháng nào, chưa fix, hay nhập vào tracker lúc nào — created_at
// KHÔNG còn ảnh hưởng (xem TaskService.CountBugsByOrigin).
func TestComputeBugOrigin(t *testing.T) {
	e := testEnv(t)

	day := func(m, d int) time.Time { return time.Date(2026, time.Month(m), d, 0, 0, 0, 0, time.Local) }
	mkTask := func(title string, done time.Time) uint {
		start := done.AddDate(0, 0, -3)
		task := models.Task{
			WorkspaceID: e.wsID, Title: title,
			Status: models.StatusDone, StartDate: &start, DoneDate: &done,
		}
		if err := e.tasks.Save(&task); err != nil {
			t.Fatalf("save task: %v", err)
		}
		return task.ID
	}
	mkBug := func(origin uint, created time.Time, status models.TaskStatus, done *time.Time) uint {
		bug := models.Task{
			WorkspaceID: e.wsID, Title: "bug", Type: models.TypeBug,
			Status: status, RelatedTaskID: &origin,
			DoneDate: done, CreatedAt: created,
		}
		if err := e.tasks.Save(&bug); err != nil {
			t.Fatalf("save bug: %v", err)
		}
		return bug.ID
	}

	taskJuly := mkTask("july task", day(7, 8))
	taskJune := mkTask("june task", day(6, 20))

	augDone := day(8, 5)
	bug1 := mkBug(taskJuly, day(7, 5), models.StatusTodo, nil)       // chưa fix → vẫn tính
	bug2 := mkBug(taskJuly, day(7, 10), models.StatusDone, &augDone) // fix tháng 8 → vẫn tính cho tháng 7
	bug3 := mkBug(taskJuly, day(8, 20), models.StatusTodo, nil)      // NHẬP BÙ tháng 8 → vẫn phải tính
	mkBug(taskJune, day(7, 1), models.StatusTodo, nil)               // bug của task tháng 6, phát hiện tháng 7

	// Tháng 7, ngày tính 15/07: đủ 3 bug của taskJuly. Bug nhập bù ngày 20/08
	// vẫn tính — ngày gõ dữ liệu vào hệ thống không phải mốc nghiệp vụ.
	m, _, err := e.metrics.Compute(e.wsID, day(7, 15), day(7, 15), 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.DoneCount != 1 {
		t.Fatalf("DoneCount = %d, want 1 (chỉ july task)", m.DoneCount)
	}
	if m.OriginBugCount != 3 {
		t.Errorf("OriginBugCount = %d, want 3 (chưa fix + fix tháng 8 + nhập bù)", m.OriginBugCount)
	}
	if math.Abs(m.OriginBugRatio-3) > 1e-9 {
		t.Errorf("OriginBugRatio = %f, want 3", m.OriginBugRatio)
	}
	// Bug fix tháng 8 KHÔNG nằm trong BugDoneCount tháng 7 (chỉ số fix theo tháng).
	if m.BugDoneCount != 0 {
		t.Errorf("BugDoneCount = %d, want 0 (không bug nào fix xong trong cửa sổ)", m.BugDoneCount)
	}

	// Đổi "ngày tính" KHÔNG làm thay đổi số bug theo nguồn gốc: nó lọc task Done,
	// không lọc bug.
	m, _, err = e.metrics.Compute(e.wsID, day(7, 15), day(7, 31), 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.OriginBugCount != 3 {
		t.Errorf("OriginBugCount asOf 31/07 = %d, want 3", m.OriginBugCount)
	}

	// Tháng 6: bug phát hiện tháng 7 vẫn quy về task gốc tháng 6.
	m, _, err = e.metrics.Compute(e.wsID, day(6, 15), day(7, 31), 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.DoneCount != 1 || m.OriginBugCount != 1 {
		t.Errorf("tháng 6: Done=%d OriginBug=%d, want 1/1 (bug tháng 7 quy về task tháng 6)", m.DoneCount, m.OriginBugCount)
	}

	// Map chi tiết cho phụ lục báo cáo: đúng task, đúng số bug.
	done, err := e.metrics.DoneTasksAsOf(e.wsID, day(7, 15), day(7, 31), 0)
	if err != nil {
		t.Fatalf("done tasks: %v", err)
	}
	origin, err := e.metrics.BugsByOrigin(e.wsID, done)
	if err != nil {
		t.Fatalf("bugs by origin: %v", err)
	}
	// Map giữ ID từng bug (không chỉ số lượng) để báo cáo in "#89, #91" và trỏ
	// sang bảng bug — nên phải đúng cả ID lẫn thứ tự (tăng dần theo ID).
	if want := []uint{bug1, bug2, bug3}; !slices.Equal(origin[taskJuly], want) {
		t.Errorf("origin[taskJuly] = %v, want %v", origin[taskJuly], want)
	}
	// taskJune Done tháng 6 nên không có mặt trong kỳ này.
	if len(origin[taskJune]) != 0 {
		t.Errorf("origin[taskJune] = %v, want rỗng (task ngoài kỳ)", origin[taskJune])
	}
}

func TestComputePICapped(t *testing.T) {
	e := testEnv(t)

	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)
	// 20 task Done trong 2 tuần đầu tháng, cycle 2 ngày → PI thô rất lớn,
	// phải bị chặn ở capacity = 2.
	for i := 0; i < 20; i++ {
		done := time.Date(2026, 7, 1+(i%14), 0, 0, 0, 0, time.Local)
		start := done.AddDate(0, 0, -2)
		task := models.Task{WorkspaceID: e.wsID, Title: "task", Status: models.StatusDone, StartDate: &start, DoneDate: &done}
		if err := e.tasks.Save(&task); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	m, _, err := e.metrics.Compute(e.wsID, now, now, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.PI != 2 || !m.PICapped {
		t.Errorf("PI = %f capped=%v, want 2 capped=true", m.PI, m.PICapped)
	}
}

// TestComputePoints kiểm tra chỉ số Điểm/tháng: điểm theo size (S=1 M=3 L=6
// XL=9), size rỗng tính như M, bug không tính, baseline nhân theo số người.
func TestComputePoints(t *testing.T) {
	e := testEnv(t)
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)

	done := time.Date(2026, 7, 10, 0, 0, 0, 0, time.Local)
	start := done.AddDate(0, 0, -3)
	mk := func(size models.TaskSize, typ models.TaskType) {
		t.Helper()
		if err := e.tasks.Save(&models.Task{
			WorkspaceID: e.wsID, Title: "task", Status: models.StatusDone,
			Size: size, Type: typ, StartDate: &start, DoneDate: &done,
		}); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	mk(models.SizeS, models.TypePlan)  // 1 điểm
	mk(models.SizeM, models.TypePlan)  // 3 điểm
	mk(models.SizeL, models.TypePlan)  // 6 điểm
	mk(models.SizeXL, models.TypePlan) // 9 điểm
	mk("", models.TypePlan)            // không size → tính như M = 3 điểm
	mk(models.SizeXL, models.TypeBug)  // bug: KHÔNG tính điểm

	m, st, err := e.metrics.Compute(e.wsID, now, now, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}

	if m.DonePoints != 22 {
		t.Errorf("DonePoints = %v, want 22 (1+3+6+9+3, bug không tính)", m.DonePoints)
	}
	// Tháng 7 có 31 ngày = 31/28 tháng chuẩn → điểm tích lũy = 22 ÷ (31/28).
	wantPPM := 22 / (31.0 / 28.0)
	if math.Abs(m.PointsPerMonth-wantPPM) > 1e-9 {
		t.Errorf("PointsPerMonth = %v, want %v", m.PointsPerMonth, wantPPM)
	}
	// Settings mới tạo phải có PointBaseline mặc định 24; team 1 người.
	if st.PointBaseline != 24 {
		t.Errorf("PointBaseline = %v, want 24", st.PointBaseline)
	}
	if m.TeamPointBaseline != 24 {
		t.Errorf("TeamPointBaseline = %v, want 24 (1 người)", m.TeamPointBaseline)
	}

	// Thêm thành viên → baseline điểm của team nhân đôi.
	e.addMember(t, "dev2")
	m, _, err = e.metrics.Compute(e.wsID, now, now, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if m.TeamPointBaseline != 48 {
		t.Errorf("TeamPointBaseline = %v, want 48 (2 người)", m.TeamPointBaseline)
	}
}

// ROI ứng dụng AI: nhóm dùng AI (cycle ngắn) phải cho AICycleTime thấp hơn nhóm
// không AI. Bốn field cycle này là nguồn duy nhất của card "ROI ứng dụng AI" trên
// Dashboard (báo cáo xuất ra cố ý không in phần ROI), nên sai ở đây là sai ngay
// trên màn hình chính.
func TestComputeAIRoi(t *testing.T) {
	e := testEnv(t)
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.Local)

	mk := func(day, cycle int, aiUsed bool, actual, estAI float64) {
		done := time.Date(2026, 7, day, 0, 0, 0, 0, time.Local)
		start := done.AddDate(0, 0, -cycle)
		if err := e.tasks.Save(&models.Task{
			WorkspaceID: e.wsID, Title: "t", Type: models.TypePlan,
			Status: models.StatusDone, StartDate: &start, DoneDate: &done,
			AIUsed: aiUsed, ActualDays: actual, EstimateAIDays: estAI,
		}); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	// 2 task AI cycle 2 ngày (effort 2, est AI 2) + 2 task không AI cycle 6 ngày.
	mk(6, 2, true, 2, 2)
	mk(8, 2, true, 2, 2)
	mk(10, 6, false, 6, 4)
	mk(12, 6, false, 6, 4)

	m, _, err := e.metrics.Compute(e.wsID, now, now, 0)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}

	if m.AIUsedCount != 2 {
		t.Errorf("AIUsedCount = %d, want 2", m.AIUsedCount)
	}
	if m.AICycleCount != 2 || m.NonAICycleCount != 2 {
		t.Fatalf("cycle counts = %d/%d, want 2/2", m.AICycleCount, m.NonAICycleCount)
	}
	if math.Abs(m.AICycleTime-2) > 1e-9 {
		t.Errorf("AICycleTime = %f, want 2", m.AICycleTime)
	}
	if math.Abs(m.NonAICycleTime-6) > 1e-9 {
		t.Errorf("NonAICycleTime = %f, want 6", m.NonAICycleTime)
	}
	// Effort thực tế vẫn cộng chung (không còn tách theo nhóm AI).
	if m.ActualEffortCount != 4 || math.Abs(m.ActualEffortTotal-16) > 1e-9 {
		t.Errorf("effort chung = %d task/%f ngày, want 4/16", m.ActualEffortCount, m.ActualEffortTotal)
	}
}

// TestSettingsPointBaselineBackfill: bản ghi settings đời cũ (PointBaseline = 0)
// phải được điền mặc định khi đọc — chỉ số Điểm/tháng luôn có baseline.
func TestSettingsPointBaselineBackfill(t *testing.T) {
	db := testDB(t)
	settings := NewSettingsService(db)

	old := models.Settings{WorkspaceID: 7, TBaseline: 4, CTBaseline: 6, PITarget: 1.2, Capacity: 2}
	if err := db.Create(&old).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	st, err := settings.Get(7)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if st.PointBaseline != 24 {
		t.Errorf("PointBaseline sau backfill = %v, want 24", st.PointBaseline)
	}
	// Backfill phải được LƯU lại, không chỉ vá lúc đọc.
	var raw models.Settings
	if err := db.Where("workspace_id = ?", 7).First(&raw).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if raw.PointBaseline != 24 {
		t.Errorf("PointBaseline trong DB = %v, want 24", raw.PointBaseline)
	}
}
