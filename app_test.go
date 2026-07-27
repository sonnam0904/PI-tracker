package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"taskmanager/internal/models"
)

func timeNowDate() string { return time.Now().Format(dateLayout) }

func testAppDB(t *testing.T) (*App, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Task{}, &models.Settings{},
		&models.TodoItem{}, &models.Activity{}, &models.StatusChange{},
		&models.User{}, &models.Workspace{}, &models.WorkspaceMember{},
		&models.Invitation{}, &models.Notification{}, &models.SavedView{},
		&models.Session{}, &models.TaskDependency{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewApp(db), db
}

func testApp(t *testing.T) *App {
	t.Helper()
	app, _ := testAppDB(t)
	return app
}

// loginApp đăng ký user + tạo workspace để các binding task dùng được.
func loginApp(t *testing.T, app *App, username string) {
	t.Helper()
	if _, err := app.Register(username, "secret123"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := app.CreateWorkspace("ws-" + username); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
}

func TestAuth(t *testing.T) {
	app := testApp(t)

	// Đăng ký + hash Argon2id.
	s, err := app.Register("son", "secret123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if s.UserID == 0 || s.Username != "son" {
		t.Fatalf("session sau register: %+v", s)
	}

	// Trùng username → từ chối.
	if _, err := app.Register("son", "khac12345"); err == nil {
		t.Fatal("register trùng username phải lỗi")
	}
	// Mật khẩu ngắn → từ chối.
	if _, err := app.Register("khac", "123"); err == nil {
		t.Fatal("mật khẩu ngắn phải lỗi")
	}

	app.Logout("")
	if app.GetSession().UserID != 0 {
		t.Fatal("logout chưa xóa session")
	}

	// Sai mật khẩu → từ chối, đúng → vào.
	if _, err := app.Login("son", "saimatkhau"); err == nil {
		t.Fatal("sai mật khẩu phải lỗi")
	}
	if _, err := app.Login("son", "secret123"); err != nil {
		t.Fatalf("login đúng: %v", err)
	}

	// Chưa chọn workspace → binding task báo lỗi rõ ràng.
	if _, err := app.ListTasks(); err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("muốn lỗi chưa chọn workspace, nhận: %v", err)
	}
}

func TestRememberSession(t *testing.T) {
	// Bí mật mã hóa theo máy dùng file tạm, không đụng config thật.
	t.Setenv("PI_SESSION_KEY_FILE", t.TempDir()+"/session.key")
	app := testApp(t)
	if _, err := app.Register("son", "secret123"); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Ghi nhớ phiên → nhận token đã mã hóa, đăng xuất khỏi bộ nhớ.
	token, err := app.RememberMe()
	if err != nil || token == "" {
		t.Fatalf("RememberMe: token=%q err=%v", token, err)
	}
	if !strings.HasPrefix(token, "v1:") {
		t.Fatalf("token phải được mã hóa (prefix v1:), nhận: %q", token)
	}
	app.Logout("") // đăng xuất nhưng KHÔNG xóa token đã ghi nhớ
	if app.GetSession().UserID != 0 {
		t.Fatal("logout chưa xóa session bộ nhớ")
	}

	// Mở lại app: khôi phục phiên từ token.
	s, err := app.ResumeSession(token)
	if err != nil || s.UserID == 0 || s.Username != "son" {
		t.Fatalf("ResumeSession: %+v err=%v", s, err)
	}

	// Đăng xuất kèm token → xóa phiên, token không dùng lại được.
	app.Logout(token)
	if _, err := app.ResumeSession(token); err == nil {
		t.Fatal("token sau khi Logout phải hết hiệu lực")
	}

	// Token rác → lỗi.
	if _, err := app.ResumeSession("khong-ton-tai"); err == nil {
		t.Fatal("token không hợp lệ phải lỗi")
	}
}

func TestMultiAccount(t *testing.T) {
	t.Setenv("PI_SESSION_KEY_FILE", t.TempDir()+"/session.key")
	app := testApp(t)

	// Ghi nhớ 2 tài khoản trên cùng một máy.
	if _, err := app.Register("alice", "secret123"); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	tokA, err := app.RememberMe()
	if err != nil {
		t.Fatalf("remember alice: %v", err)
	}
	if _, err := app.Register("bob", "secret123"); err != nil { // auto-login bob
		t.Fatalf("register bob: %v", err)
	}
	tokB, err := app.RememberMe()
	if err != nil {
		t.Fatalf("remember bob: %v", err)
	}

	// Liệt kê tài khoản đã ghi nhớ → đủ 2, đúng tên.
	accts := app.ListSavedAccounts([]string{tokA, tokB})
	if len(accts) != 2 {
		t.Fatalf("muốn 2 tài khoản, nhận %d: %+v", len(accts), accts)
	}
	names := map[string]bool{accts[0].Username: true, accts[1].Username: true}
	if !names["alice"] || !names["bob"] {
		t.Fatalf("thiếu tài khoản: %+v", accts)
	}

	// Ghi nhớ bob lần nữa → token khác nhưng gộp trùng theo user (vẫn 2).
	tokB2, err := app.RememberMe()
	if err != nil {
		t.Fatalf("remember bob 2: %v", err)
	}
	if got := app.ListSavedAccounts([]string{tokA, tokB, tokB2}); len(got) != 2 {
		t.Fatalf("gộp trùng theo user thất bại, nhận %d", len(got))
	}

	// Chuyển sang alice bằng token (không cần mật khẩu).
	if s, err := app.ResumeSession(tokA); err != nil || s.Username != "alice" {
		t.Fatalf("switch alice: %+v err=%v", s, err)
	}

	// Quên tài khoản bob → không đụng session alice hiện tại.
	app.ForgetAccount(tokB)
	if app.GetSession().Username != "alice" {
		t.Fatal("ForgetAccount không được đổi session hiện tại")
	}
	if got := app.ListSavedAccounts([]string{tokB}); len(got) != 0 {
		t.Fatalf("token bob đã quên vẫn còn: %+v", got)
	}
	// alice vẫn ghi nhớ được.
	if got := app.ListSavedAccounts([]string{tokA}); len(got) != 1 {
		t.Fatalf("token alice phải còn hợp lệ, nhận %d", len(got))
	}
}

func TestInviteFlow(t *testing.T) {
	app := testApp(t)

	// A tạo workspace, B là user thường.
	loginApp(t, app, "alice")
	if _, err := app.Register("bob", "secret123"); err != nil {
		t.Fatalf("register bob: %v", err)
	}

	// Đang là session bob (register auto-login) → quay lại alice để mời.
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	if err := app.InviteMember("bob"); err != nil {
		t.Fatalf("invite: %v", err)
	}
	// Mời lại khi đang pending → lỗi.
	if err := app.InviteMember("bob"); err == nil {
		t.Fatal("mời trùng phải lỗi")
	}

	// Bob thấy notification lời mời (unread).
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	n, _ := app.UnreadNotifications()
	if n != 1 {
		t.Fatalf("bob unread = %d, want 1", n)
	}
	notifs, err := app.ListNotifications()
	if err != nil || len(notifs) != 1 {
		t.Fatalf("list notifs: %v (%d)", err, len(notifs))
	}
	if notifs[0].Kind != "invite" || notifs[0].InvitationID == nil || notifs[0].InvitationStatus != "pending" {
		t.Fatalf("notification lời mời sai: %+v", notifs[0])
	}

	// Bob chấp nhận → thành thành viên, workspace xuất hiện trong danh sách.
	if err := app.RespondInvitation(*notifs[0].InvitationID, true); err != nil {
		t.Fatalf("accept: %v", err)
	}
	wss, _ := app.ListWorkspaces()
	if len(wss) != 1 || wss[0].Name != "ws-alice" || wss[0].Role != "member" {
		t.Fatalf("bob workspaces: %+v", wss)
	}
	if err := app.SelectWorkspace(wss[0].ID); err != nil {
		t.Fatalf("select ws: %v", err)
	}
	members, _ := app.ListPeople()
	if len(members) != 2 {
		t.Fatalf("members = %d, want 2", len(members))
	}

	// Alice nhận notification kết quả.
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	notifs, _ = app.ListNotifications()
	if len(notifs) == 0 || !strings.Contains(notifs[0].Content, "chấp nhận") {
		t.Fatalf("alice chưa nhận thông báo chấp nhận: %+v", notifs)
	}

	// Xử lý lời mời 2 lần → lỗi.
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	if err := app.RespondInvitation(*findInviteID(t, app), true); err == nil {
		t.Fatal("respond lần 2 phải lỗi")
	}
}

func findInviteID(t *testing.T, app *App) *uint {
	t.Helper()
	notifs, err := app.ListNotifications()
	if err != nil {
		t.Fatalf("list notifs: %v", err)
	}
	for _, n := range notifs {
		if n.InvitationID != nil {
			return n.InvitationID
		}
	}
	t.Fatal("không thấy notification lời mời")
	return nil
}

func TestSaveTaskDateValidation(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "tester")

	// Start date trước ngày tạo → từ chối.
	err := app.SaveTask(TaskDTO{
		Title:       "t",
		Status:      "Todo",
		CreatedDate: "2026-07-16",
		StartDate:   "2026-07-14",
	})
	if err == nil || !strings.Contains(err.Error(), "trước ngày tạo") {
		t.Fatalf("muốn lỗi start < created, nhận: %v", err)
	}

	// Done date trước start date → từ chối.
	err = app.SaveTask(TaskDTO{
		Title:       "t",
		Status:      "Done",
		CreatedDate: "2026-07-10",
		StartDate:   "2026-07-14",
		DoneDate:    "2026-07-12",
	})
	if err == nil || !strings.Contains(err.Error(), "trước start date") {
		t.Fatalf("muốn lỗi done < start, nhận: %v", err)
	}

	// Start cùng ngày tạo → hợp lệ.
	if err := app.SaveTask(TaskDTO{
		Title:       "t",
		Status:      "In Progress",
		CreatedDate: "2026-07-16",
		StartDate:   "2026-07-16",
	}); err != nil {
		t.Fatalf("start cùng ngày tạo phải hợp lệ, nhận: %v", err)
	}

	// Ngày tạo bỏ trống → mặc định hôm nay, start hôm nay vẫn hợp lệ.
	if err := app.SaveTask(TaskDTO{
		Title:     "t2",
		Status:    "In Progress",
		StartDate: timeNowDate(),
	}); err != nil {
		t.Fatalf("start = hôm nay với created mặc định phải hợp lệ, nhận: %v", err)
	}
}

func TestActivitiesAndTodos(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "son")

	// Tạo task → activity "create" ghi tên user đăng nhập.
	if err := app.SaveTask(TaskDTO{Title: "task A", Status: "Todo", CreatedDate: "2026-07-10"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	list, _ := app.ListTasks()
	if len(list) != 1 {
		t.Fatalf("list = %d task, want 1", len(list))
	}
	id := list[0].ID

	acts, _ := app.ListActivities(id)
	if len(acts) != 1 || acts[0].Kind != "create" || acts[0].ActorName != "son" {
		t.Fatalf("sau tạo: %+v", acts)
	}

	// Sửa tiêu đề + trạng thái → activity "update" ghi rõ thay đổi.
	dto := list[0]
	dto.Title = "task B"
	dto.Status = "In Progress"
	dto.StartDate = "2026-07-12"
	if err := app.SaveTask(dto); err != nil {
		t.Fatalf("update: %v", err)
	}
	acts, _ = app.ListActivities(id)
	if acts[0].Kind != "update" {
		t.Fatalf("muốn update, nhận %q", acts[0].Kind)
	}
	for _, want := range []string{"Tiêu đề: task A → task B", "Trạng thái: Todo → In Progress", "Start date: — → 2026-07-12"} {
		if !strings.Contains(acts[0].Content, want) {
			t.Errorf("update content thiếu %q, có:\n%s", want, acts[0].Content)
		}
	}

	// Lưu lại không đổi gì → KHÔNG thêm activity.
	if err := app.SaveTask(dto); err != nil {
		t.Fatalf("no-op save: %v", err)
	}
	if acts2, _ := app.ListActivities(id); len(acts2) != len(acts) {
		t.Errorf("no-op save không được sinh activity (%d → %d)", len(acts), len(acts2))
	}

	// Checklist: thêm 2, tick 1 → badge 1/2.
	if err := app.AddTodo(id, "viết test"); err != nil {
		t.Fatalf("add todo: %v", err)
	}
	if err := app.AddTodo(id, "viết docs"); err != nil {
		t.Fatalf("add todo: %v", err)
	}
	todos, _ := app.ListTodos(id)
	if len(todos) != 2 {
		t.Fatalf("todos = %d, want 2", len(todos))
	}
	if err := app.ToggleTodo(todos[0].ID, true); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	list, _ = app.ListTasks()
	if list[0].TodoTotal != 2 || list[0].TodoDone != 1 {
		t.Errorf("badge = %d/%d, want 1/2", list[0].TodoDone, list[0].TodoTotal)
	}

	// Comment hiển thị chung feed.
	if err := app.AddComment(id, "cần review lại estimate", 0); err != nil {
		t.Fatalf("comment: %v", err)
	}
	acts, _ = app.ListActivities(id)
	if acts[0].Kind != "comment" || acts[0].Content != "cần review lại estimate" || acts[0].ActorName != "son" {
		t.Errorf("comment đầu feed sai: %+v", acts[0])
	}

	// Xóa task → dọn sạch todos + activities.
	if err := app.DeleteTask(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if todos, _ = app.ListTodos(id); len(todos) != 0 {
		t.Errorf("todos còn %d sau khi xóa task", len(todos))
	}
}

// InitialTodos (vd checklist AI gợi ý) chỉ tạo todo khi TẠO MỚI task, và bỏ
// mục rỗng; lần lưu sau (update) không được nhân đôi checklist.
func TestSaveTaskInitialTodos(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "son")

	if err := app.SaveTask(TaskDTO{
		Title: "Task có checklist AI", Status: "Todo", CreatedDate: "2026-07-10",
		InitialTodos: []string{"Thiết kế schema", "  ", "Viết API"},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	list, _ := app.ListTasks()
	if len(list) != 1 {
		t.Fatalf("list = %d, want 1", len(list))
	}
	id := list[0].ID

	todos, _ := app.ListTodos(id)
	if len(todos) != 2 {
		t.Fatalf("todos = %d, want 2 (mục rỗng bị bỏ)", len(todos))
	}
	if todos[0].Title != "Thiết kế schema" || todos[1].Title != "Viết API" {
		t.Errorf("nội dung todo sai: %+v", todos)
	}

	// Update lại kèm InitialTodos → KHÔNG nhân đôi (chỉ áp khi tạo mới).
	dto := list[0]
	dto.Title = "đổi tên"
	dto.InitialTodos = []string{"Không được thêm"}
	if err := app.SaveTask(dto); err != nil {
		t.Fatalf("update: %v", err)
	}
	if todos, _ = app.ListTodos(id); len(todos) != 2 {
		t.Errorf("sau update todos = %d, want vẫn 2 (không nhân đôi)", len(todos))
	}
}

// findTask tìm task theo ID trong ListTasks (thứ tự danh sách có thể đổi khi update).
func findTask(t *testing.T, app *App, id uint) TaskDTO {
	t.Helper()
	list, err := app.ListTasks()
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, dto := range list {
		if dto.ID == id {
			return dto
		}
	}
	t.Fatalf("không thấy task id %d", id)
	return TaskDTO{}
}

// Priority mặc định P3, giá trị lạ bị từ chối, DueDate round-trip qua DTO.
func TestSaveTaskPriorityAndDueDate(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "tester")

	// Bỏ trống priority → mặc định P3.
	if err := app.SaveTask(TaskDTO{Title: "t", Status: "Todo", DueDate: "2026-07-20"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	list, _ := app.ListTasks()
	if list[0].Priority != "P3" {
		t.Errorf("priority mặc định = %q, want P3", list[0].Priority)
	}
	if list[0].DueDate != "2026-07-20" {
		t.Errorf("dueDate = %q, want 2026-07-20", list[0].DueDate)
	}

	// Giá trị priority lạ → từ chối.
	if err := app.SaveTask(TaskDTO{Title: "t2", Status: "Todo", Priority: "P9"}); err == nil ||
		!strings.Contains(err.Error(), "ưu tiên") {
		t.Fatalf("muốn lỗi priority không hợp lệ, nhận: %v", err)
	}

	// Đặt P1 + xóa hạn chót → lưu đúng.
	dto := list[0]
	dto.Priority = "P1"
	dto.DueDate = ""
	if err := app.SaveTask(dto); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, _ = app.ListTasks()
	if list[0].Priority != "P1" || list[0].DueDate != "" {
		t.Errorf("sau update: priority=%q dueDate=%q, want P1 + rỗng", list[0].Priority, list[0].DueDate)
	}

	// Lịch sử ghi rõ thay đổi ưu tiên và hạn chót.
	acts, _ := app.ListActivities(list[0].ID)
	for _, want := range []string{"Ưu tiên: P3 → P1", "Hạn chót: 2026-07-20 → —"} {
		if !strings.Contains(acts[0].Content, want) {
			t.Errorf("update content thiếu %q, có:\n%s", want, acts[0].Content)
		}
	}
}

// Nhóm field bug: reporter mặc định là người tạo, severity/resolution phải hợp lệ,
// task gốc phải cùng workspace, đổi loại khác bug thì xóa sạch nhóm field.
func TestSaveTaskBugFields(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "tester")

	// Task thường làm task gốc.
	if err := app.SaveTask(TaskDTO{Title: "feature A", Status: "Done", DoneDate: "2026-07-10"}); err != nil {
		t.Fatalf("tạo feature: %v", err)
	}
	list, _ := app.ListTasks()
	featureID := list[0].ID

	// Tạo bug: không ghi reporter → mặc định người đang đăng nhập.
	if err := app.SaveTask(TaskDTO{
		Title: "bug login", Status: "Todo", Type: int(models.TypeBug),
		Severity: "Critical", RelatedTaskID: featureID,
	}); err != nil {
		t.Fatalf("tạo bug: %v", err)
	}
	list, _ = app.ListTasks()
	bug := list[0] // mới nhất lên đầu
	if bug.Title != "bug login" {
		t.Fatalf("task đầu danh sách = %q, want bug login", bug.Title)
	}
	if bug.ReporterID == 0 {
		t.Error("reporter phải mặc định là người tạo")
	}
	if bug.Severity != "Critical" || bug.RelatedTaskID != featureID {
		t.Errorf("bug fields: severity=%q related=%d, want Critical/%d", bug.Severity, bug.RelatedTaskID, featureID)
	}

	// Severity lạ → từ chối.
	if err := app.SaveTask(TaskDTO{Title: "b", Status: "Todo", Type: int(models.TypeBug), Severity: "Huge"}); err == nil ||
		!strings.Contains(err.Error(), "mức độ") {
		t.Fatalf("muốn lỗi severity, nhận: %v", err)
	}
	// Resolution lạ → từ chối.
	if err := app.SaveTask(TaskDTO{Title: "b", Status: "Todo", Type: int(models.TypeBug), Resolution: "Maybe"}); err == nil ||
		!strings.Contains(err.Error(), "cách đóng") {
		t.Fatalf("muốn lỗi resolution, nhận: %v", err)
	}
	// Task gốc không tồn tại → từ chối.
	if err := app.SaveTask(TaskDTO{Title: "b", Status: "Todo", Type: int(models.TypeBug), RelatedTaskID: 9999}); err == nil ||
		!strings.Contains(err.Error(), "task gốc") {
		t.Fatalf("muốn lỗi task gốc, nhận: %v", err)
	}
	// Bug tự liên kết chính nó → từ chối.
	self := bug
	self.RelatedTaskID = bug.ID
	if err := app.SaveTask(self); err == nil || !strings.Contains(err.Error(), "chính bug") {
		t.Fatalf("muốn lỗi tự liên kết, nhận: %v", err)
	}

	// Đóng bug với resolution hợp lệ.
	done := bug
	done.Status = "Done"
	done.DoneDate = "2026-07-15"
	done.Resolution = "Fixed"
	if err := app.SaveTask(done); err != nil {
		t.Fatalf("đóng bug: %v", err)
	}
	closed := findTask(t, app, bug.ID)
	if closed.Resolution != "Fixed" {
		t.Errorf("resolution = %q, want Fixed", closed.Resolution)
	}

	// Đổi loại từ bug sang plan → nhóm field bug bị xóa sạch.
	plain := closed
	plain.Type = int(models.TypePlan)
	if err := app.SaveTask(plain); err != nil {
		t.Fatalf("đổi loại: %v", err)
	}
	got := findTask(t, app, bug.ID)
	if got.ReporterID != 0 || got.Severity != "" || got.Resolution != "" || got.RelatedTaskID != 0 {
		t.Errorf("đổi loại phải xóa bug fields, còn: reporter=%d severity=%q resolution=%q related=%d",
			got.ReporterID, got.Severity, got.Resolution, got.RelatedTaskID)
	}
}

// Task gốc thuộc workspace khác → từ chối liên kết.
func TestSaveTaskRelatedTaskCrossWorkspace(t *testing.T) {
	app := testApp(t)

	// Task nằm ở workspace của alice.
	loginApp(t, app, "alice")
	if err := app.SaveTask(TaskDTO{Title: "task của alice", Status: "Todo"}); err != nil {
		t.Fatalf("tạo task alice: %v", err)
	}
	list, _ := app.ListTasks()
	aliceTask := list[0].ID

	// Bob ở workspace riêng không được liên kết bug tới task đó.
	loginApp(t, app, "bob")
	if err := app.SaveTask(TaskDTO{
		Title: "bug", Status: "Todo", Type: int(models.TypeBug), RelatedTaskID: aliceTask,
	}); err == nil || !strings.Contains(err.Error(), "task gốc") {
		t.Fatalf("muốn lỗi khác workspace, nhận: %v", err)
	}
}

// Mục checklist chỉ truy cập qua todoId, nên phải chặn thao tác xuyên workspace:
// người ở workspace khác không được tick/xóa mục của task không thuộc workspace
// mình (đường vào qua MCP dùng chung ToggleTodo/DeleteTodo).
func TestTodoMutationCrossWorkspace(t *testing.T) {
	app := testApp(t)

	// alice tạo task + một mục checklist trong workspace của mình.
	loginApp(t, app, "alice")
	if err := app.SaveTask(TaskDTO{Title: "task alice", Status: "Todo"}); err != nil {
		t.Fatalf("tạo task alice: %v", err)
	}
	list, _ := app.ListTasks()
	if err := app.AddTodo(list[0].ID, "việc của alice"); err != nil {
		t.Fatalf("thêm todo alice: %v", err)
	}
	todos, _ := app.ListTodos(list[0].ID)
	todoID := todos[0].ID

	// bob ở workspace riêng KHÔNG được tick/xóa mục checklist của alice qua id.
	loginApp(t, app, "bob")
	if err := app.ToggleTodo(todoID, true); err == nil {
		t.Fatal("bob tick todo khác workspace phải lỗi")
	}
	if err := app.DeleteTodo(todoID); err == nil {
		t.Fatal("bob xóa todo khác workspace phải lỗi")
	}

	// alice quay lại: mục vẫn còn nguyên và chưa bị đánh done.
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login lại alice: %v", err)
	}
	todos, _ = app.ListTodos(list[0].ID)
	if len(todos) != 1 || todos[0].Done {
		t.Fatalf("todo của alice phải còn nguyên & chưa done, nhận: %+v", todos)
	}
}

// Fingerprint workspace phải đổi sau MỌI loại thay đổi (tạo/sửa/xóa task,
// checklist, bình luận) để poller đồng bộ realtime bắn "tasks:changed". Checklist
// và bình luận không đụng tasks.updated_at nên phải nhờ activity id lo phần đó.
func TestWorkspaceFingerprintChanges(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "alice")
	wsID := app.GetSession().WorkspaceID
	uid := app.GetSession().UserID

	fp := func(step string) string {
		s, err := app.workspaceFingerprint(wsID, uid)
		if err != nil {
			t.Fatalf("fingerprint (%s): %v", step, err)
		}
		return s
	}

	prev := fp("khởi đầu")
	assertChanged := func(step string) {
		cur := fp(step)
		if cur == prev {
			t.Fatalf("%s phải làm đổi fingerprint (vẫn %q)", step, cur)
		}
		prev = cur
	}

	if err := app.SaveTask(TaskDTO{Title: "t1", Status: "Todo"}); err != nil {
		t.Fatalf("tạo task: %v", err)
	}
	assertChanged("tạo task")

	list, _ := app.ListTasks()
	id := list[0].ID

	d := list[0]
	d.Title = "t1-đã-sửa"
	if err := app.SaveTask(d); err != nil {
		t.Fatalf("sửa task: %v", err)
	}
	assertChanged("sửa task")

	if err := app.AddTodo(id, "việc"); err != nil {
		t.Fatalf("thêm checklist: %v", err)
	}
	assertChanged("thêm checklist")

	if err := app.AddComment(id, "một bình luận", 0); err != nil {
		t.Fatalf("bình luận: %v", err)
	}
	assertChanged("bình luận")

	if err := app.DeleteTask(id); err != nil {
		t.Fatalf("xóa task: %v", err)
	}
	assertChanged("xóa task")

	// Saved view: tạo/sửa bộ lọc/đổi tên/xóa đều phải làm đổi fingerprint (đồng
	// bộ tab lọc realtime, kể cả khi cùng user đăng nhập ở nhiều client).
	v, err := app.CreateSavedView("view A", `{"status":["Todo"]}`)
	if err != nil {
		t.Fatalf("tạo view: %v", err)
	}
	assertChanged("tạo saved view")

	if _, err := app.UpdateSavedView(v.ID, "view A", `{"status":["Done"]}`); err != nil {
		t.Fatalf("sửa bộ lọc view: %v", err)
	}
	assertChanged("sửa bộ lọc view")

	if _, err := app.UpdateSavedView(v.ID, "view A đổi tên", `{"status":["Done"]}`); err != nil {
		t.Fatalf("đổi tên view: %v", err)
	}
	assertChanged("đổi tên view")

	if err := app.DeleteSavedView(v.ID); err != nil {
		t.Fatalf("xóa view: %v", err)
	}
	assertChanged("xóa saved view")
}

// primeDataBaseline (chạy khi vào/đổi workspace qua SelectWorkspace) phải chụp
// đúng fingerprint hiện tại làm mốc, để nhịp poll đầu tiên phát hiện được thay
// đổi của client khác thay vì nuốt vào baseline (khe hở [nạp, nhịp đầu]).
func TestPrimeDataBaselineCapturesFingerprint(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "alice") // Register + CreateWorkspace → SelectWorkspace → primeDataBaseline
	wsID := app.GetSession().WorkspaceID
	uid := app.GetSession().UserID

	// Sau prime, baseline (dataWsID/dataFP) phải khớp workspace + fingerprint hiện tại.
	fp0, err := app.workspaceFingerprint(wsID, uid)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	app.dataMu.Lock()
	baseWs, base := app.dataWsID, app.dataFP
	app.dataMu.Unlock()
	if baseWs != wsID || base != fp0 {
		t.Fatalf("prime chưa set mốc đúng: dataWsID=%d dataFP=%q, muốn %d/%q", baseWs, base, wsID, fp0)
	}

	// "Client khác" tạo task ngay sau khi vào workspace → fingerprint khác mốc,
	// nên nhịp poll đầu tiên sẽ phát hiện (không còn bị nuốt vào baseline).
	if err := app.SaveTask(TaskDTO{Title: "từ client khác", Status: "Todo"}); err != nil {
		t.Fatalf("tạo task: %v", err)
	}
	fp1, err := app.workspaceFingerprint(wsID, uid)
	if err != nil {
		t.Fatalf("fingerprint sau: %v", err)
	}
	if fp1 == base {
		t.Fatal("fingerprint sau thay đổi phải khác mốc — nếu bằng, poll sẽ bỏ lỡ")
	}
}

// primeNotifBaseline (chạy khi đăng nhập) phải chốt mốc thông báo ngay, để
// thông báo mới ngay sau đăng nhập được phát hiện thay vì bị nuốt làm baseline
// (nhịp NOTIFY/poll đầu tiên trên Postgres).
func TestPrimeNotifBaselineCapturesMax(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "alice") // Register → setUser → primeNotifBaseline
	uid := app.GetSession().UserID

	app.notifMu.Lock()
	baseUser, baseID := app.notifUserID, app.notifLastID
	app.notifMu.Unlock()
	if baseUser != uid {
		t.Fatalf("prime chưa chốt mốc: notifUserID=%d, muốn %d", baseUser, uid)
	}

	// "Client khác" tạo thông báo cho alice ngay sau đăng nhập.
	if _, err := app.notifications.Create(models.Notification{
		UserID: uid, Kind: "mention", Content: "nhắc bạn",
	}); err != nil {
		t.Fatalf("tạo notif: %v", err)
	}
	fresh, err := app.notifications.NewerThan(uid, baseID)
	if err != nil {
		t.Fatalf("NewerThan: %v", err)
	}
	if len(fresh) != 1 {
		t.Fatalf("phải phát hiện 1 thông báo mới hơn mốc, có %d — nhịp đầu sẽ bị nuốt nếu 0", len(fresh))
	}
}

// Khóa/mở khóa thành viên: chỉ owner, không khóa owner, thành viên bị khóa
// không thao tác được trong workspace nhưng vẫn xem được thông báo,
// và nhận notification khi bị khóa/mở khóa.
func TestMemberLock(t *testing.T) {
	app := testApp(t)

	// alice (owner) mời bob vào ws-alice.
	loginApp(t, app, "alice")
	bobSession, err := app.Register("bob", "secret123")
	if err != nil {
		t.Fatalf("register bob: %v", err)
	}
	bobID := bobSession.UserID
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	aliceID := app.GetSession().UserID
	if err := app.InviteMember("bob"); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	if err := app.RespondInvitation(*findInviteID(t, app), true); err != nil {
		t.Fatalf("accept: %v", err)
	}

	// Owner khóa bob.
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	if err := app.SetMemberLock(bobID, true); err != nil {
		t.Fatalf("khóa bob: %v", err)
	}
	// Khóa owner → từ chối.
	if err := app.SetMemberLock(aliceID, true); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("khóa owner phải lỗi, nhận: %v", err)
	}
	// Khóa lần nữa → báo đã khóa.
	if err := app.SetMemberLock(bobID, true); err == nil {
		t.Fatal("khóa trùng phải lỗi")
	}
	// Danh sách thành viên phản ánh trạng thái khóa.
	members, _ := app.ListPeople()
	for _, m := range members {
		if m.ID == bobID && !m.Locked {
			t.Error("ListPeople: bob phải có locked = true")
		}
	}

	// Bob bị khóa: chọn được workspace nhưng mọi thao tác bị chặn.
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	if _, err := app.ListTasks(); err == nil || !strings.Contains(err.Error(), "bị khóa") {
		t.Fatalf("bob bị khóa phải chặn ListTasks, nhận: %v", err)
	}
	if err := app.SaveTask(TaskDTO{Title: "x", Status: "Todo"}); err == nil || !strings.Contains(err.Error(), "bị khóa") {
		t.Fatalf("bob bị khóa phải chặn SaveTask, nhận: %v", err)
	}
	// Bob bị khóa cũng không khóa/mở khóa được ai.
	if err := app.SetMemberLock(aliceID, true); err == nil {
		t.Fatal("bob bị khóa phải bị chặn SetMemberLock")
	}
	// Nhưng thông báo (user-scoped) vẫn xem được và có tin bị khóa.
	notifs, err := app.ListNotifications()
	if err != nil {
		t.Fatalf("bob xem notifications: %v", err)
	}
	if len(notifs) == 0 || !strings.Contains(notifs[0].Content, "bị khóa") {
		t.Fatalf("bob chưa nhận thông báo bị khóa: %+v", notifs)
	}

	// Owner mở khóa → bob thao tác lại được + nhận thông báo mở khóa.
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	if err := app.SetMemberLock(bobID, false); err != nil {
		t.Fatalf("mở khóa bob: %v", err)
	}
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	if _, err := app.ListTasks(); err != nil {
		t.Fatalf("bob đã mở khóa phải dùng được, nhận: %v", err)
	}
	notifs, _ = app.ListNotifications()
	if len(notifs) == 0 || !strings.Contains(notifs[0].Content, "mở khóa") {
		t.Fatalf("bob chưa nhận thông báo mở khóa: %+v", notifs)
	}
	// Member thường không được khóa người khác.
	if err := app.SetMemberLock(aliceID, true); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("member khóa người khác phải lỗi, nhận: %v", err)
	}
}

// Observer + phân quyền owner: owner đặt member làm "chỉ quan sát" → member bị
// loại khỏi bảng so sánh và khỏi baseline (TeamSize); member không được
// SaveSettings hay đổi observer (chỉ owner).
func TestMemberObserverAndOwnerOnly(t *testing.T) {
	app := testApp(t)

	loginApp(t, app, "alice") // alice = owner
	bobSession, err := app.Register("bob", "secret123")
	if err != nil {
		t.Fatalf("register bob: %v", err)
	}
	bobID := bobSession.UserID
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	if err := app.InviteMember("bob"); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	if err := app.RespondInvitation(*findInviteID(t, app), true); err != nil {
		t.Fatalf("accept: %v", err)
	}

	// Trước khi đánh dấu observer: team 2 người.
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	month := time.Now().Format("2006-01")
	team, err := app.GetTeamMetrics(month, "")
	if err != nil {
		t.Fatalf("team metrics: %v", err)
	}
	if team.Team.TeamSize != 2 {
		t.Fatalf("TeamSize trước observer = %d, want 2", team.Team.TeamSize)
	}
	if len(team.Members) != 2 {
		t.Fatalf("số dòng thành viên = %d, want 2", len(team.Members))
	}

	// Owner đặt bob làm observer.
	if err := app.SetMemberObserver(bobID, true); err != nil {
		t.Fatalf("đặt observer: %v", err)
	}
	team, err = app.GetTeamMetrics(month, "")
	if err != nil {
		t.Fatalf("team metrics sau observer: %v", err)
	}
	if team.Team.TeamSize != 1 {
		t.Fatalf("TeamSize sau observer = %d, want 1 (observer không tính)", team.Team.TeamSize)
	}
	for _, m := range team.Members {
		if m.AssigneeID == bobID {
			t.Fatal("observer bob không được xuất hiện ở bảng so sánh")
		}
	}
	// ListPeople vẫn trả bob kèm cờ observer.
	people, _ := app.ListPeople()
	seen := false
	for _, p := range people {
		if p.ID == bobID {
			seen = true
			if !p.Observer {
				t.Error("bob phải có observer = true")
			}
		}
	}
	if !seen {
		t.Fatal("bob vẫn phải là thành viên (chỉ là observer)")
	}

	// Member (bob) không được đổi observer hay SaveSettings.
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	if err := app.SetMemberObserver(bobID, false); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("member đổi observer phải lỗi, nhận: %v", err)
	}
	if err := app.SaveSettings(models.Settings{TBaseline: 1, CTBaseline: 1, PITarget: 1, Capacity: 2}); err == nil ||
		!strings.Contains(err.Error(), "owner") {
		t.Fatalf("member SaveSettings phải bị chặn (chỉ owner), nhận: %v", err)
	}
}

// Poller thông báo HĐH: lần đầu thấy user chỉ baseline (không notify backlog),
// bản ghi mới sau đó được notify đúng một lần, logout thì reset.
func TestOSNotifications(t *testing.T) {
	app, db := testAppDB(t)
	var got []string
	app.osNotify = func(title, body string) { got = append(got, title+" | "+body) }

	// Chưa đăng nhập → nhịp poll không làm gì.
	app.checkNewNotifications()
	if len(got) != 0 {
		t.Fatalf("chưa đăng nhập mà đã notify: %v", got)
	}

	loginApp(t, app, "giang")
	uid := app.GetSession().UserID

	// Backlog tồn tại TRƯỚC khi phiên chốt mốc: đăng xuất, tạo backlog, rồi đăng
	// nhập lại — primeNotifBaseline chốt mốc GỒM cả backlog nên KHÔNG notify.
	// (Mốc chốt ngay lúc đăng nhập, không đợi nhịp poll đầu.)
	app.Logout("")
	app.checkNewNotifications() // uid==0 → reset mốc
	db.Create(&models.Notification{UserID: uid, Kind: "info", Content: "thông báo cũ"})
	if _, err := app.Login("giang", "secret123"); err != nil {
		t.Fatalf("login lại: %v", err)
	}
	app.checkNewNotifications()
	if len(got) != 0 {
		t.Fatalf("backlog cũ không được notify, nhận: %v", got)
	}

	// Bản ghi mới sau baseline → notify đủ, đúng thứ tự, đúng tiêu đề theo kind.
	db.Create(&models.Notification{UserID: uid, Kind: "invite", Content: `sonnn mời bạn vào workspace "R&D"`})
	db.Create(&models.Notification{UserID: uid, Kind: "info", Content: "tin thứ hai"})
	app.checkNewNotifications()
	if len(got) != 2 {
		t.Fatalf("notify = %d, want 2: %v", len(got), got)
	}
	if !strings.Contains(got[0], "Lời mời workspace") || !strings.Contains(got[0], `"R&D"`) {
		t.Errorf("notification lời mời sai: %q", got[0])
	}
	if !strings.Contains(got[1], "tin thứ hai") {
		t.Errorf("notification info sai: %q", got[1])
	}

	// Poll lại không notify trùng.
	app.checkNewNotifications()
	if len(got) != 2 {
		t.Fatalf("notify trùng: %v", got)
	}

	// Thông báo của user khác → không liên quan.
	db.Create(&models.Notification{UserID: uid + 99, Kind: "info", Content: "của người khác"})
	app.checkNewNotifications()
	if len(got) != 2 {
		t.Fatalf("notify nhầm user: %v", got)
	}

	// Logout → reset; đăng nhập lại thì backlog (gồm các tin trên) không bị notify lại.
	app.Logout("")
	app.checkNewNotifications()
	if _, err := app.Login("giang", "secret123"); err != nil {
		t.Fatalf("login lại: %v", err)
	}
	app.checkNewNotifications() // baseline lại
	app.checkNewNotifications()
	if len(got) != 2 {
		t.Fatalf("đăng nhập lại không được notify backlog: %v", got)
	}
}

// Kéo task từ cột Done về trạng thái khác trên kanban: status đổi + Done date xóa.
func TestSaveTaskReopenFromDone(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "tester")

	if err := app.SaveTask(TaskDTO{
		Title:       "done task",
		Status:      "Done",
		CreatedDate: "2026-07-10",
		StartDate:   "2026-07-12",
		DoneDate:    "2026-07-14",
	}); err != nil {
		t.Fatalf("tạo task Done: %v", err)
	}
	list, err := app.ListTasks()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v (%d)", err, len(list))
	}

	dto := list[0]
	dto.Status = "In Progress"
	dto.DoneDate = "" // kanban xóa Done date khi rời cột Done
	if err := app.SaveTask(dto); err != nil {
		t.Fatalf("Done → In Progress phải hợp lệ, nhận: %v", err)
	}

	list, _ = app.ListTasks()
	if list[0].Status != "In Progress" || list[0].DoneDate != "" {
		t.Errorf("sau reopen: status=%q doneDate=%q, want In Progress + rỗng", list[0].Status, list[0].DoneDate)
	}
}

// Lịch sử trạng thái: ghi khi tạo + mỗi lần đổi; rời Blocked tự cộng BlockedDays.
func TestStatusHistoryAndAutoBlockedDays(t *testing.T) {
	app, db := testAppDB(t)
	loginApp(t, app, "tester")

	if err := app.SaveTask(TaskDTO{Title: "task A", Status: "Todo", CreatedDate: timeNowDate()}); err != nil {
		t.Fatalf("create: %v", err)
	}
	list, _ := app.ListTasks()
	dto := list[0]

	// Todo → Blocked.
	dto.Status = "Blocked"
	dto.Blocker = "chờ API bên thứ ba"
	if err := app.SaveTask(dto); err != nil {
		t.Fatalf("→ Blocked: %v", err)
	}

	// Giả lập đã nằm ở Blocked 3 ngày: lùi created_at của bản ghi lịch sử.
	threeDaysAgo := time.Now().Add(-3 * 24 * time.Hour)
	if err := db.Model(&models.StatusChange{}).
		Where("task_id = ? AND to_status = ?", dto.ID, models.StatusBlocked).
		Update("created_at", threeDaysAgo).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// Blocked → In Progress, KHÔNG tự sửa BlockedDays → tự cộng ~3 ngày.
	dto.Status = "In Progress"
	if err := app.SaveTask(dto); err != nil {
		t.Fatalf("→ In Progress: %v", err)
	}
	list, _ = app.ListTasks()
	if bd := list[0].BlockedDays; bd < 2.9 || bd > 3.1 {
		t.Errorf("BlockedDays = %v, want ~3 (tự cộng từ lịch sử)", bd)
	}

	// Timeline đủ 3 bước, cũ nhất trước.
	changes, err := app.ListStatusChanges(dto.ID)
	if err != nil || len(changes) != 3 {
		t.Fatalf("status changes: %v (%d), want 3", err, len(changes))
	}
	if changes[0].FromStatus != "" || changes[0].ToStatus != models.StatusTodo ||
		changes[1].ToStatus != models.StatusBlocked ||
		changes[2].FromStatus != models.StatusBlocked || changes[2].ToStatus != models.StatusInProgress {
		t.Errorf("timeline sai: %+v", changes)
	}
	if changes[0].ActorName != "tester" {
		t.Errorf("actor = %q, want tester", changes[0].ActorName)
	}

	// Người dùng TỰ sửa BlockedDays trong lần rời Blocked → tôn trọng, không cộng.
	dto = list[0]
	dto.Status = "Blocked"
	if err := app.SaveTask(dto); err != nil {
		t.Fatalf("→ Blocked lần 2: %v", err)
	}
	list, _ = app.ListTasks()
	dto = list[0]
	dto.Status = "In Progress"
	dto.BlockedDays = 5 // nhập tay
	if err := app.SaveTask(dto); err != nil {
		t.Fatalf("→ In Progress lần 2: %v", err)
	}
	list, _ = app.ListTasks()
	if list[0].BlockedDays != 5 {
		t.Errorf("BlockedDays = %v, want 5 (giữ số nhập tay)", list[0].BlockedDays)
	}

	// Xóa task dọn cả lịch sử trạng thái.
	if err := app.DeleteTask(dto.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var n int64
	db.Model(&models.StatusChange{}).Where("task_id = ?", dto.ID).Count(&n)
	if n != 0 {
		t.Errorf("còn %d status change sau khi xóa task", n)
	}
}

// Effort thực tế (ActualDays): lưu, hiển thị lại, chặn số âm.
func TestSaveTaskActualDays(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "tester")

	if err := app.SaveTask(TaskDTO{Title: "t", Status: "Todo", ActualDays: -1}); err == nil {
		t.Fatal("effort âm phải lỗi")
	}
	if err := app.SaveTask(TaskDTO{
		Title: "t", Status: "Done",
		CreatedDate: timeNowDate(), StartDate: timeNowDate(), DoneDate: timeNowDate(),
		EstimateAIDays: 2, ActualDays: 2.5,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	list, _ := app.ListTasks()
	if list[0].ActualDays != 2.5 {
		t.Errorf("ActualDays = %v, want 2.5", list[0].ActualDays)
	}
}

// Trang Team: chỉ số theo từng thành viên + chuỗi xu hướng theo tháng.
func TestTeamMetricsAndTrend(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "owner")
	uid := app.GetSession().UserID

	if err := app.SaveTask(TaskDTO{
		Title: "done này tháng", Status: "Done", AssigneeID: uid,
		CreatedDate: timeNowDate(), StartDate: timeNowDate(), DoneDate: timeNowDate(),
		EstimateAIDays: 2, ActualDays: 3, AIUsed: true,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	month := time.Now().Format("2006-01")
	res, err := app.GetTeamMetrics(month, "")
	if err != nil {
		t.Fatalf("GetTeamMetrics: %v", err)
	}
	if res.Team.DoneCount != 1 || len(res.Members) != 1 {
		t.Fatalf("team done=%d members=%d, want 1/1", res.Team.DoneCount, len(res.Members))
	}
	mm := res.Members[0].Metrics
	if res.Members[0].AssigneeID != uid || mm.DoneCount != 1 {
		t.Errorf("member metrics sai: %+v", res.Members[0])
	}
	if mm.ActualEffortTotal != 3 || mm.EstAIPairedTotal != 2 || mm.ActualEffortCount != 1 {
		t.Errorf("effort: total=%v paired=%v count=%d, want 3/2/1",
			mm.ActualEffortTotal, mm.EstAIPairedTotal, mm.ActualEffortCount)
	}
	if mm.AIUsedCount != 1 {
		t.Errorf("AIUsedCount = %d, want 1", mm.AIUsedCount)
	}

	trend, err := app.GetTeamTrend(month, 3, "")
	if err != nil {
		t.Fatalf("GetTeamTrend: %v", err)
	}
	if len(trend.Months) != 3 || trend.Months[2] != month {
		t.Fatalf("months = %v, want 3 tháng kết thúc %s", trend.Months, month)
	}
	if len(trend.Team.Points) != 3 || trend.Team.Points[2].DoneCount != 1 || trend.Team.Points[0].DoneCount != 0 {
		t.Errorf("team points sai: %+v", trend.Team.Points)
	}
	if len(trend.Members) != 1 || len(trend.Members[0].Points) != 3 {
		t.Errorf("member series sai: %+v", trend.Members)
	}
	if trend.PITarget <= 0 {
		t.Errorf("PITarget = %v, want > 0", trend.PITarget)
	}
}

// Nhắc hạn chót: task quá hạn / sắp đến hạn notify một lần, không lặp —
// dedup bền trong DB (qua cả khởi động lại app) và có bản ghi ở chuông.
func TestDueReminders(t *testing.T) {
	app, db := testAppDB(t)

	var mu sync.Mutex
	var got []string
	app.osNotify = func(title, body string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, title+" | "+body)
	}

	loginApp(t, app, "tester")
	uid := app.GetSession().UserID

	tomorrow := time.Now().AddDate(0, 0, 1).Format(dateLayout)
	yesterday := time.Now().AddDate(0, 0, -1).Format(dateLayout)
	if err := app.SaveTask(TaskDTO{Title: "sắp đến hạn", Status: "In Progress", AssigneeID: uid,
		CreatedDate: yesterday, DueDate: tomorrow}); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	if err := app.SaveTask(TaskDTO{Title: "đã quá hạn", Status: "Todo", AssigneeID: uid,
		CreatedDate: yesterday, DueDate: yesterday}); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	// Task không gán cho user → không nhắc.
	if err := app.SaveTask(TaskDTO{Title: "của người khác", Status: "Todo",
		CreatedDate: yesterday, DueDate: yesterday}); err != nil {
		t.Fatalf("save 3: %v", err)
	}

	// Nhắc NGAY khi lưu task có hạn chót, không đợi nhịp quét theo giờ.
	mu.Lock()
	n := len(got)
	joined := strings.Join(got, "\n")
	mu.Unlock()
	if n != 2 {
		t.Fatalf("notify ngay sau save = %d, want 2:\n%s", n, joined)
	}
	if !strings.Contains(joined, "quá hạn") || !strings.Contains(joined, "sắp đến hạn") {
		t.Errorf("nội dung nhắc sai:\n%s", joined)
	}

	// Quét lại không nhắc trùng (dedup theo bản ghi chuông trong DB).
	app.checkDueTasks()
	mu.Lock()
	n = len(got)
	mu.Unlock()
	if n != 2 {
		t.Fatalf("nhắc trùng: %d", n)
	}

	// Nhắc việc nằm trong chuông thông báo (unread) để không bị lỡ toast.
	notifs, err := app.ListNotifications()
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	due := 0
	for _, nf := range notifs {
		if nf.Kind == "due" {
			due++
			if nf.Read {
				t.Errorf("nhắc việc phải unread: %+v", nf)
			}
			// Gắn task + workspace để UI click là nhảy tới task.
			if nf.TaskID == nil || *nf.TaskID == 0 || nf.WorkspaceID == nil || *nf.WorkspaceID == 0 {
				t.Errorf("nhắc việc thiếu liên kết task/workspace: %+v", nf)
			}
		}
	}
	if due != 2 {
		t.Fatalf("chuông có %d nhắc việc, want 2", due)
	}

	// Khởi động lại app (instance mới, cùng DB) + đăng nhập → không nhắc lại.
	app2 := NewApp(db)
	var got2 []string
	app2.osNotify = func(title, body string) {
		mu.Lock()
		defer mu.Unlock()
		got2 = append(got2, title)
	}
	if _, err := app2.Login("tester", "secret123"); err != nil {
		t.Fatalf("login lại: %v", err)
	}
	mu.Lock()
	n2 := len(got2)
	mu.Unlock()
	if n2 != 0 {
		t.Fatalf("khởi động lại vẫn nhắc trùng: %d (%v)", n2, got2)
	}
}

// mentionsUser: khớp token trọn vẹn, không khớp prefix hay email.
func TestMentionsUserBoundaries(t *testing.T) {
	cases := []struct {
		content, user string
		want          bool
	}{
		{"@bob xem giúp", "bob", true},
		{"chào @bob", "bob", true},
		{"(@bob)", "bob", true},
		{"@bob.", "bob", true},
		{"@bobby ơi", "bob", false},
		{"email a@bob.vn", "bob", false},
		{"@Bob", "bob", false}, // username phân biệt hoa thường
		{"không nhắc ai", "bob", false},
		{"@sonnn nhé", "son", false},
		{"@son rồi @sonnn", "sonnn", true},
	}
	for _, c := range cases {
		if got := mentionsUser(c.content, c.user); got != c.want {
			t.Errorf("mentionsUser(%q, %q) = %v, want %v", c.content, c.user, got, c.want)
		}
	}
}

// Mention trong bình luận: người được nhắc nhận thông báo chuông gắn task;
// người viết không tự nhận.
func TestMentionNotifications(t *testing.T) {
	app := testApp(t)

	loginApp(t, app, "alice")
	if _, err := app.Register("bob", "secret123"); err != nil {
		t.Fatalf("register bob: %v", err)
	}
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	if err := app.InviteMember("bob"); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	if err := app.RespondInvitation(*findInviteID(t, app), true); err != nil {
		t.Fatalf("accept: %v", err)
	}

	// Alice tạo task và bình luận nhắc bob (email không tính là mention).
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	if err := app.SaveTask(TaskDTO{Title: "task mention", Status: "Todo"}); err != nil {
		t.Fatalf("save task: %v", err)
	}
	list, _ := app.ListTasks()
	taskID := list[0].ID
	if err := app.AddComment(taskID, "@bob xem giúp nhé (mail a@bob.vn bỏ qua)", 0); err != nil {
		t.Fatalf("comment 1: %v", err)
	}
	if err := app.AddComment(taskID, "tự nhắc @alice thì không notify", 0); err != nil {
		t.Fatalf("comment 2: %v", err)
	}

	// Bob nhận đúng 1 mention, gắn task để click là mở.
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	notifs, err := app.ListNotifications()
	if err != nil {
		t.Fatalf("list notifs: %v", err)
	}
	mentions := 0
	for _, n := range notifs {
		if n.Kind != "mention" {
			continue
		}
		mentions++
		if n.TaskID == nil || *n.TaskID != taskID || n.WorkspaceID == nil {
			t.Errorf("mention thiếu liên kết task: %+v", n)
		}
		if n.ActivityID == nil || *n.ActivityID == 0 {
			t.Errorf("mention thiếu liên kết bình luận (để scroll tới): %+v", n)
		}
		if !strings.Contains(n.Content, "alice") || !strings.Contains(n.Content, "task mention") {
			t.Errorf("nội dung mention sai: %q", n.Content)
		}
	}
	if mentions != 1 {
		t.Fatalf("bob có %d mention, want 1", mentions)
	}

	// Alice không tự nhận mention của chính mình.
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	notifs, _ = app.ListNotifications()
	for _, n := range notifs {
		if n.Kind == "mention" {
			t.Errorf("alice không được tự nhận mention: %+v", n)
		}
	}
}

// Reply bình luận: thread 1 cấp, người được trả lời nhận thông báo gắn task.
func TestReplyComment(t *testing.T) {
	app := testApp(t)

	loginApp(t, app, "alice")
	if _, err := app.Register("bob", "secret123"); err != nil {
		t.Fatalf("register bob: %v", err)
	}
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	if err := app.InviteMember("bob"); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	if err := app.RespondInvitation(*findInviteID(t, app), true); err != nil {
		t.Fatalf("accept: %v", err)
	}

	// Alice tạo task + comment gốc.
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	if err := app.SaveTask(TaskDTO{Title: "task reply", Status: "Todo"}); err != nil {
		t.Fatalf("save task: %v", err)
	}
	list, _ := app.ListTasks()
	taskID := list[0].ID
	if err := app.AddComment(taskID, "comment gốc của alice", 0); err != nil {
		t.Fatalf("comment gốc: %v", err)
	}
	acts, _ := app.ListActivities(taskID)
	var rootID uint
	for _, a := range acts {
		if a.Kind == "comment" {
			rootID = a.ID
		}
	}
	if rootID == 0 {
		t.Fatal("không thấy comment gốc")
	}

	// Bob trả lời comment của alice.
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	if err := app.AddComment(taskID, "reply của bob", rootID); err != nil {
		t.Fatalf("reply: %v", err)
	}

	// Reply của reply → vẫn gắn về comment gốc (thread 1 cấp).
	acts, _ = app.ListActivities(taskID)
	var bobReplyID uint
	for _, a := range acts {
		if a.Kind == "comment" && a.ParentID != nil {
			if *a.ParentID != rootID {
				t.Errorf("reply gắn parent %d, want %d", *a.ParentID, rootID)
			}
			bobReplyID = a.ID
		}
	}
	if bobReplyID == 0 {
		t.Fatal("không thấy reply")
	}
	if _, err := app.Login("alice", "secret123"); err != nil {
		t.Fatalf("login alice: %v", err)
	}
	if err := app.AddComment(taskID, "alice trả lời reply của bob", bobReplyID); err != nil {
		t.Fatalf("reply lồng: %v", err)
	}
	acts, _ = app.ListActivities(taskID)
	nested := 0
	for _, a := range acts {
		if a.Kind == "comment" && a.ParentID != nil {
			nested++
			if *a.ParentID != rootID {
				t.Errorf("mọi reply phải gắn về comment gốc %d, nhận %d", rootID, *a.ParentID)
			}
		}
	}
	if nested != 2 {
		t.Fatalf("có %d reply, want 2", nested)
	}

	// Alice (chủ comment gốc) nhận thông báo reply từ bob, gắn task.
	notifs, _ := app.ListNotifications()
	replies := 0
	for _, n := range notifs {
		if n.Kind != "reply" {
			continue
		}
		replies++
		if n.TaskID == nil || *n.TaskID != taskID {
			t.Errorf("reply notif thiếu task: %+v", n)
		}
		if n.ActivityID == nil || *n.ActivityID == 0 {
			t.Errorf("reply notif thiếu liên kết bình luận: %+v", n)
		}
		if !strings.Contains(n.Content, "bob") {
			t.Errorf("nội dung reply notif sai: %q", n.Content)
		}
	}
	if replies != 1 {
		t.Fatalf("alice có %d thông báo reply, want 1", replies)
	}

	// Bob nhận thông báo khi alice trả lời reply của mình.
	if _, err := app.Login("bob", "secret123"); err != nil {
		t.Fatalf("login bob: %v", err)
	}
	notifs, _ = app.ListNotifications()
	replies = 0
	for _, n := range notifs {
		if n.Kind == "reply" {
			replies++
		}
	}
	if replies != 1 {
		t.Fatalf("bob có %d thông báo reply, want 1", replies)
	}

	// Parent không thuộc task / không tồn tại → từ chối.
	if err := app.AddComment(taskID, "reply mồ côi", 99999); err == nil {
		t.Fatal("reply vào comment không tồn tại phải lỗi")
	}
}

func TestSavedViews(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "son")

	// Chưa có view nào.
	views, err := app.ListSavedViews()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("có %d view, want 0", len(views))
	}

	// Tạo hai view — position tăng dần theo thứ tự tạo.
	v1, err := app.CreateSavedView("Bug P1 của tôi", `{"priority":"P1"}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	v2, err := app.CreateSavedView("Quá hạn", `{"due":"overdue"}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if v2.Position <= v1.Position {
		t.Fatalf("position không tăng: v1=%d v2=%d", v1.Position, v2.Position)
	}

	// Tên trống bị từ chối.
	if _, err := app.CreateSavedView("  ", "{}"); err == nil {
		t.Fatal("tạo view tên trống phải lỗi")
	}

	views, _ = app.ListSavedViews()
	if len(views) != 2 || views[0].ID != v1.ID || views[1].ID != v2.ID {
		t.Fatalf("list sai thứ tự: %+v", views)
	}

	// Đổi tên + filters.
	upd, err := app.UpdateSavedView(v1.ID, "Bug gấp", `{"priority":"P1","type":2}`)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Name != "Bug gấp" || !strings.Contains(upd.Filters, `"type":2`) {
		t.Fatalf("update không ăn: %+v", upd)
	}

	// User khác không thấy và không sửa/xóa được view của son.
	loginApp(t, app, "eve")
	views, _ = app.ListSavedViews()
	if len(views) != 0 {
		t.Fatalf("eve thấy %d view của son", len(views))
	}
	if _, err := app.UpdateSavedView(v1.ID, "hack", "{}"); err == nil {
		t.Fatal("eve sửa được view của son")
	}
	if err := app.DeleteSavedView(v1.ID); err == nil {
		t.Fatal("eve xóa được view của son")
	}

	// Chủ sở hữu xóa được.
	if _, err := app.Login("son", "secret123"); err != nil {
		t.Fatalf("login lại: %v", err)
	}
	if err := app.DeleteSavedView(v1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	views, _ = app.ListSavedViews()
	if len(views) != 1 || views[0].ID != v2.ID {
		t.Fatalf("sau xóa còn: %+v", views)
	}
}

// Phụ thuộc task: lưu được, ListTasks trả về DependsOn, chặn vòng lặp, và
// xóa task thì cạnh phụ thuộc cũng biến mất.
func TestTaskDependencies(t *testing.T) {
	app := testApp(t)
	loginApp(t, app, "alice")

	// Tạo 3 task A, B, C.
	mk := func(title string) uint {
		if err := app.SaveTask(TaskDTO{Title: title, Status: "Todo"}); err != nil {
			t.Fatalf("tạo %s: %v", title, err)
		}
		list, _ := app.ListTasks()
		return list[0].ID // List sắp xếp created_at DESC → mới nhất đầu
	}
	a := mk("A")
	b := mk("B")
	c := mk("C")

	// B phụ thuộc A; C phụ thuộc A và B.
	if err := app.SaveTask(TaskDTO{ID: b, Title: "B", Status: "Todo", DependsOn: []uint{a}}); err != nil {
		t.Fatalf("B depends A: %v", err)
	}
	if err := app.SaveTask(TaskDTO{ID: c, Title: "C", Status: "Todo", DependsOn: []uint{a, b}}); err != nil {
		t.Fatalf("C depends A,B: %v", err)
	}
	if got := findTask(t, app, c).DependsOn; len(got) != 2 {
		t.Fatalf("C.DependsOn = %v, want 2 phần tử", got)
	}

	// Tự phụ thuộc → bỏ qua (không lỗi, không lưu).
	if err := app.SaveTask(TaskDTO{ID: b, Title: "B", Status: "Todo", DependsOn: []uint{b, a}}); err != nil {
		t.Fatalf("B self-dep: %v", err)
	}
	if got := findTask(t, app, b).DependsOn; len(got) != 1 || got[0] != a {
		t.Fatalf("B.DependsOn = %v, want [%d]", got, a)
	}

	// Vòng lặp: A phụ thuộc C (C→B→A→C) phải bị từ chối.
	if err := app.SaveTask(TaskDTO{ID: a, Title: "A", Status: "Todo", DependsOn: []uint{c}}); err == nil ||
		!strings.Contains(err.Error(), "vòng lặp") {
		t.Fatalf("muốn lỗi vòng lặp, nhận: %v", err)
	}

	// Task phụ thuộc không tồn tại/khác workspace → lỗi.
	if err := app.SaveTask(TaskDTO{ID: b, Title: "B", Status: "Todo", DependsOn: []uint{9999}}); err == nil ||
		!strings.Contains(err.Error(), "task phụ thuộc") {
		t.Fatalf("muốn lỗi task không tồn tại, nhận: %v", err)
	}

	// Xóa A → cạnh phụ thuộc của B và C tới A biến mất.
	if err := app.DeleteTask(a); err != nil {
		t.Fatalf("xóa A: %v", err)
	}
	if got := findTask(t, app, c).DependsOn; len(got) != 1 || got[0] != b {
		t.Fatalf("sau xóa A, C.DependsOn = %v, want [%d]", got, b)
	}
	if got := findTask(t, app, b).DependsOn; len(got) != 0 {
		t.Fatalf("sau xóa A, B.DependsOn = %v, want rỗng", got)
	}
}
