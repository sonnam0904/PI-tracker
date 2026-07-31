package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"taskmanager/internal/models"
)

func day(t *testing.T, s string) *time.Time {
	t.Helper()
	return mustParseDay(t, s)
}

// sqlRecorder là logger GORM chỉ để giữ lại câu SQL cuối cùng đã chạy.
type sqlRecorder struct {
	logger.Interface
	last string
}

func (r *sqlRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	r.last, _ = fc()
}

// TestListFilteredPushesFilterToSQL là chốt chống hồi quy quan trọng nhất của
// TaskDateFilter: khoảng ngày phải nằm trong CÂU SQL mà ListFiltered thật sự chạy.
// Nếu ai đó đổi lại thành Find() hết rồi lọc trong Go, mọi test về kết quả vẫn
// xanh — chỉ test này đỏ.
func TestListFilteredPushesFilterToSQL(t *testing.T) {
	db := testDB(t)
	rec := &sqlRecorder{Interface: logger.Discard}
	svc := NewTaskService(db.Session(&gorm.Session{Logger: rec}))

	if _, err := svc.ListFiltered(7, TaskDateFilter{
		Field: TaskDateTouched, From: day(t, "2026-06-01"), To: day(t, "2026-06-30"),
	}); err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}

	sql := rec.last
	for _, want := range []string{
		"workspace_id = 7",
		`start_date >= "2026-06-01`, `start_date < "2026-07-01`,
		`done_date >= "2026-06-01`, `done_date < "2026-07-01`,
		" OR ",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL thieu %q — dieu kien ngay khong xuong tang DB.\nSQL: %s", want, sql)
		}
	}
	// Cận trên phải NỬA MỞ (< to + 1 ngày), không phải <= to: created_at mang cả
	// giờ-phút nên "<= ngày cuối kỳ" sẽ cắt mất task tạo sau 00:00 hôm đó.
	if strings.Contains(sql, "<=") {
		t.Errorf("dung can tren dong (<=) — createdDate se bi cat mat ngay cuoi ky.\nSQL: %s", sql)
	}
	// Điều kiện ngày phải được bọc ngoặc, nếu không thì "ws = ? AND (a OR b)" biến
	// thành "(ws = ? AND a) OR b" — lọt task của workspace khác.
	if !strings.Contains(sql, "AND ((") {
		t.Errorf("dieu kien ngay khong duoc boc ngoac — OR se pha vo dieu kien workspace.\nSQL: %s", sql)
	}

	// Không lọc thì không được sinh điều kiện ngày nào.
	if _, err := svc.ListFiltered(7, TaskDateFilter{}); err != nil {
		t.Fatalf("ListFiltered khong loc: %v", err)
	}
	if strings.Contains(rec.last, "start_date") {
		t.Errorf("khong loc ma van co dieu kien ngay.\nSQL: %s", rec.last)
	}
}

// TestListFilteredResults kiểm ngữ nghĩa trên dữ liệu thật, gồm ca biên mà bộ lọc
// dễ sai: task chạm vào hai kỳ, và task chưa có ngày.
func TestListFilteredResults(t *testing.T) {
	db := testDB(t)
	svc := NewTaskService(db)

	mk := func(title string, start, done string) uint {
		task := models.Task{WorkspaceID: 1, Title: title, Type: models.TypePlan}
		if start != "" {
			task.StartDate = day(t, start)
		}
		if done != "" {
			task.DoneDate = day(t, done)
		}
		if err := db.Create(&task).Error; err != nil {
			t.Fatalf("tao task %q: %v", title, err)
		}
		return task.ID
	}
	// Task chạm hai kỳ: bắt đầu 30/06, xong 03/07.
	gapID := mk("gac ky", "2026-06-30", "2026-07-03")
	junID := mk("trong t6", "2026-06-01", "2026-06-05")
	julID := mk("trong t7", "2026-07-10", "2026-07-15")
	noDateID := mk("chua co ngay", "", "")
	// Task workspace khác — không được lọt vào kết quả.
	otherWS := models.Task{WorkspaceID: 2, Title: "ws khac", Type: models.TypePlan, StartDate: day(t, "2026-06-15")}
	if err := db.Create(&otherWS).Error; err != nil {
		t.Fatalf("tao task ws khac: %v", err)
	}

	has := func(tasks []models.Task, id uint) bool {
		for _, x := range tasks {
			if x.ID == id {
				return true
			}
		}
		return false
	}

	jun := TaskDateFilter{From: day(t, "2026-06-01"), To: day(t, "2026-06-30")}
	got, err := svc.ListFiltered(1, jun)
	if err != nil {
		t.Fatalf("ListFiltered thang 6: %v", err)
	}
	if len(got) != 2 || !has(got, gapID) || !has(got, junID) {
		t.Errorf("thang 6 (touched) tra %d task, muon dung {gac ky, trong t6}", len(got))
	}
	if has(got, noDateID) {
		t.Error("task chua co ngay khong duoc lot vao ky nao")
	}
	if has(got, otherWS.ID) {
		t.Error("lot task cua workspace khac — bo loc ngay da lam mat dieu kien workspace")
	}

	jul := TaskDateFilter{From: day(t, "2026-07-01"), To: day(t, "2026-07-31")}
	got, err = svc.ListFiltered(1, jul)
	if err != nil {
		t.Fatalf("ListFiltered thang 7: %v", err)
	}
	if len(got) != 2 || !has(got, gapID) || !has(got, julID) {
		t.Errorf("thang 7 (touched) tra %d task, muon dung {gac ky, trong t7} — task gac ky phai thuoc CA hai ky", len(got))
	}

	// Không lọc = trả đủ workspace, giữ nguyên hành vi của List().
	all, err := svc.ListFiltered(1, TaskDateFilter{})
	if err != nil {
		t.Fatalf("ListFiltered khong loc: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("khong loc tra %d task, muon 4", len(all))
	}

	if _, err := svc.ListFiltered(1, TaskDateFilter{Field: "khong_ton_tai", From: day(t, "2026-06-01")}); err == nil {
		t.Error("field ngay khong hop le phai bao loi")
	}
}

// TestOverlapIsSupersetOfGanttFilter là chốt của yêu cầu "giữ nguyên logic hiển
// thị": TaskDateOverlap phải BAO TRÙM đúng tập mà bộ lọc rows của GanttView giữ
// lại. Nếu SQL hẹp hơn dù chỉ một task, task đó biến mất khỏi UI mà không có lỗi
// nào — nên test này mô phỏng lại nguyên văn điều kiện của client rồi so hai tập.
//
// Điều kiện client (GanttView.vue rows + barEnd):
//
//	start == null                                        -> giu
//	barEnd > mStart && start < mEnd                       -> giu
//	barEnd = done ?? start + max(estimateAiDays, 1) ngay
func TestOverlapIsSupersetOfGanttFilter(t *testing.T) {
	db := testDB(t)
	svc := NewTaskService(db)

	type seed struct {
		title      string
		start, don string
		estAI      float64
	}
	seeds := []seed{
		{"xong trong ky", "2026-07-05", "2026-07-10", 3},
		{"gac dau ky", "2026-06-28", "2026-07-02", 2},
		{"gac cuoi ky", "2026-07-30", "2026-08-04", 3},
		{"xong truoc ky", "2026-05-01", "2026-05-10", 5},
		{"xong sau ky", "2026-08-05", "2026-08-09", 2},
		{"con mo, bat dau truoc ky", "2026-05-20", "", 90},
		{"con mo, estimate ngan", "2026-05-20", "", 1},
		{"con mo trong ky", "2026-07-15", "", 4},
		{"con mo sau ky", "2026-08-20", "", 4},
		{"chua co ngay bat dau", "", "", 3},
		{"xong dung ngay dau ky", "2026-06-15", "2026-07-01", 2},
		{"bat dau dung ngay cuoi ky", "2026-07-31", "", 1},
		{"khong estimate", "2026-07-31", "", 0},
	}
	day := func(s string) *time.Time {
		if s == "" {
			return nil
		}
		return mustParseDay(t, s)
	}
	byID := map[uint]seed{}
	for _, sd := range seeds {
		task := models.Task{
			WorkspaceID: 1, Title: sd.title, Type: models.TypePlan,
			StartDate: day(sd.start), DoneDate: day(sd.don), EstimateAIDays: sd.estAI,
		}
		if err := db.Create(&task).Error; err != nil {
			t.Fatalf("tao task %q: %v", sd.title, err)
		}
		byID[task.ID] = sd
	}

	// keptByClient mô phỏng nguyên văn rows + barEnd của GanttView.
	keptByClient := func(sd seed, mStart, mEnd time.Time) bool {
		if sd.start == "" {
			return true
		}
		start := *mustParseDay(t, sd.start)
		var barEnd time.Time
		if sd.don != "" {
			barEnd = *mustParseDay(t, sd.don)
		} else {
			d := sd.estAI
			if d < 1 {
				d = 1
			}
			barEnd = start.Add(time.Duration(d * 24 * float64(time.Hour)))
		}
		return barEnd.After(mStart) && start.Before(mEnd)
	}

	// Chạy qua nhiều tháng để bắt cả ca lệch biên đầu/cuối kỳ.
	for _, ym := range []string{"2026-05", "2026-06", "2026-07", "2026-08", "2026-09"} {
		mStart := *mustParseDay(t, ym+"-01")
		mEnd := mStart.AddDate(0, 1, 0)
		to := mEnd.AddDate(0, 0, -1)

		got, err := svc.ListFiltered(1, TaskDateFilter{
			Field: TaskDateOverlap, From: &mStart, To: &to,
		})
		if err != nil {
			t.Fatalf("%s: ListFiltered: %v", ym, err)
		}
		fromSQL := map[uint]bool{}
		for _, x := range got {
			fromSQL[x.ID] = true
		}
		for id, sd := range byID {
			if keptByClient(sd, mStart, mEnd) && !fromSQL[id] {
				t.Errorf("%s: SQL BO SOT task %q (#%d) mà UI đang hiển thị — task se bien mat khoi giao dien",
					ym, sd.title, id)
			}
		}
	}
}

func mustParseDay(t *testing.T, s string) *time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		t.Fatalf("ngay %q: %v", s, err)
	}
	return &v
}

// TestTagNamesOrderIsStableAndDBIndependent khóa thứ tự tag hiển thị:
//   - giống nhau giữa NamesOf (một task) và NamesByTask (danh sách) — hai đường
//     cùng đổ ra chip trên UI, lệch nhau là chip nhảy chỗ khi mở/đóng modal;
//   - giống SortedNames — thứ tự mà nhật ký thay đổi tag đang dùng;
//   - KHÔNG theo collation của DB (byte order xếp chữ hoa lên trước).
func TestTagNamesOrderIsStableAndDBIndependent(t *testing.T) {
	db := testDB(t)
	if err := db.AutoMigrate(&models.Tag{}, &models.TaskTag{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := NewTagService(db)

	task := models.Task{WorkspaceID: 1, Title: "t", Type: models.TypePlan}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("tao task: %v", err)
	}
	// Trộn hoa/thường + tiếng Việt có dấu: đây là bộ làm lộ khác biệt giữa sắp
	// theo byte và sắp không phân biệt hoa/thường.
	names := []string{"Zalo", "hạ tầng", "API", "báo cáo"}
	ids, err := svc.EnsureByNames(1, names)
	if err != nil {
		t.Fatalf("EnsureByNames: %v", err)
	}
	if err := svc.SetForTask(1, task.ID, ids); err != nil {
		t.Fatalf("SetForTask: %v", err)
	}

	want := SortedNames(names) // [API báo cáo hạ tầng Zalo]
	same := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	one, err := svc.NamesOf(task.ID)
	if err != nil {
		t.Fatalf("NamesOf: %v", err)
	}
	if !same(one, want) {
		t.Errorf("NamesOf = %v, muon %v", one, want)
	}

	many, err := svc.NamesByTask(1)
	if err != nil {
		t.Fatalf("NamesByTask: %v", err)
	}
	if !same(many[task.ID], want) {
		t.Errorf("NamesByTask = %v, muon %v", many[task.ID], want)
	}

	scoped, err := svc.NamesByTaskIDs(1, []uint{task.ID})
	if err != nil {
		t.Fatalf("NamesByTaskIDs: %v", err)
	}
	if !same(scoped[task.ID], want) {
		t.Errorf("NamesByTaskIDs = %v, muon %v", scoped[task.ID], want)
	}

	// Chốt lại điều đang được sửa: thứ tự byte của DB (chữ hoa truoc) KHÔNG được
	// là thứ tự hiển thị. Nếu ai đó trả ORDER BY tags.name về SQL, dòng này đỏ.
	if same(one, []string{"API", "Zalo", "báo cáo", "hạ tầng"}) {
		t.Error("dang sap theo byte order cua DB — thu tu chip se doi theo collation cua DB")
	}
}
