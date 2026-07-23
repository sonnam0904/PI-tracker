package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gen2brain/beeep"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"gorm.io/gorm"

	"taskmanager/internal/ai"
	"taskmanager/internal/machinecrypto"
	"taskmanager/internal/models"
	"taskmanager/internal/report"
	"taskmanager/internal/service"
)

const dateLayout = "2006-01-02"

// App exposes backend methods to the Vue frontend via Wails bindings.
type App struct {
	ctx           context.Context
	tasks         *service.TaskService
	settings      *service.SettingsService
	metrics       *service.MetricsService
	todos         *service.TodoService
	activities    *service.ActivityService
	statuses      *service.StatusService
	auth          *service.AuthService
	workspaces    *service.WorkspaceService
	notifications *service.NotificationService
	savedViews    *service.SavedViewService
	estimator     *ai.Estimator

	// version — phiên bản app nhúng lúc build (main.Version), dùng cho updater.
	version string

	// Session trong bộ nhớ: đăng nhập lại mỗi lần mở app.
	mu       sync.Mutex
	userID   uint
	username string
	wsID     uint
	wsName   string
	wsRole   string

	// Đẩy notification ra Hệ điều hành (test thay bằng stub).
	osNotify func(title, body string)
	// Trạng thái poller thông báo: user đang theo dõi + ID lớn nhất đã thấy.
	notifMu     sync.Mutex
	notifUserID uint
	notifLastID uint
}

func NewApp(db *gorm.DB) *App {
	tasks := service.NewTaskService(db)
	settings := service.NewSettingsService(db)
	workspaces := service.NewWorkspaceService(db)
	return &App{
		tasks:         tasks,
		settings:      settings,
		workspaces:    workspaces,
		metrics:       service.NewMetricsService(tasks, workspaces, settings),
		todos:         service.NewTodoService(db),
		activities:    service.NewActivityService(db),
		statuses:      service.NewStatusService(db),
		auth:          service.NewAuthService(db),
		notifications: service.NewNotificationService(db),
		savedViews:    service.NewSavedViewService(db),
		// Gợi ý estimate bằng LLM — cấu hình qua .env (AI_PROVIDER/AI_API_KEY/
		// AI_MODEL). Chưa cấu hình thì estimator vẫn tồn tại nhưng Enabled()=false.
		estimator: ai.NewEstimator(ai.NewClient(ai.Load())),
		// Notification native của HĐH (Linux: D-Bus/notify-send, Windows:
		// toast, macOS: notification center) — hiện cả khi app chạy nền.
		osNotify: func(title, body string) { _ = beeep.Notify(title, body, "") },
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.watchNotifications(ctx)
	go a.watchDueTasks(ctx)
}

// notifPollInterval — nhịp quét thông báo mới trong DB. Thông báo do instance
// của user khác ghi vào DB chung, nên phải poll chứ không có push.
const notifPollInterval = 10 * time.Second

// watchNotifications chạy nền suốt vòng đời app: phát hiện thông báo mới của
// user đang đăng nhập và đẩy ra Hệ điều hành, kể cả khi cửa sổ đang ở nền.
func (a *App) watchNotifications(ctx context.Context) {
	t := time.NewTicker(notifPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.checkNewNotifications()
		}
	}
}

// checkNewNotifications là một nhịp poll. Lần đầu thấy user (mới đăng nhập /
// đổi tài khoản) chỉ ghi mốc baseline — backlog cũ hiển thị ở chuông, không
// spam HĐH. Các nhịp sau notify mọi bản ghi mới hơn mốc và báo frontend
// refresh chuông ngay qua event "notifications:new".
func (a *App) checkNewNotifications() {
	a.mu.Lock()
	uid := a.userID
	a.mu.Unlock()

	a.notifMu.Lock()
	defer a.notifMu.Unlock()

	if uid == 0 {
		a.notifUserID, a.notifLastID = 0, 0
		return
	}
	if uid != a.notifUserID {
		maxID, err := a.notifications.MaxID(uid)
		if err != nil {
			return
		}
		a.notifUserID, a.notifLastID = uid, maxID
		return
	}

	fresh, err := a.notifications.NewerThan(uid, a.notifLastID)
	if err != nil || len(fresh) == 0 {
		return
	}
	for _, n := range fresh {
		if n.ID > a.notifLastID {
			a.notifLastID = n.ID
		}
		title := "PI Tracker"
		switch n.Kind {
		case "invite":
			title = "PI Tracker — Lời mời workspace"
		case "mention":
			title = "PI Tracker — Có người nhắc tới bạn"
		case "reply":
			title = "PI Tracker — Trả lời bình luận"
		case "due":
			title = "PI Tracker — Task đến hạn"
		}
		a.osNotify(title, n.Content)
	}
	// Chuông trên UI cập nhật ngay, không phải đợi nhịp poll 30s của JS.
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "notifications:new")
	}
}

// dueSoonWindow — nhắc trước hạn chót bao lâu; dueCheckInterval — nhịp quét.
const (
	dueSoonWindow    = 2 * 24 * time.Hour
	dueCheckInterval = time.Hour
)

// watchDueTasks chạy nền suốt vòng đời app: nhắc task sắp/quá hạn của user
// đang đăng nhập qua notification HĐH (mỗi lần đăng nhập cũng quét ngay).
func (a *App) watchDueTasks(ctx context.Context) {
	a.checkDueTasks()
	t := time.NewTicker(dueCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.checkDueTasks()
		}
	}
}

// checkDueTasks là một nhịp quét: task gán cho user đang đăng nhập, chưa Done,
// quá hạn hoặc đến hạn trong 2 ngày tới → ghi vào chuông thông báo trong app
// (bền, còn dấu vết) + toast HĐH. Dedup bằng DB: mỗi task chỉ nhắc một lần cho
// mỗi nội dung, kể cả qua nhiều lần khởi động app; đổi hạn chót → nhắc lại.
func (a *App) checkDueTasks() {
	a.mu.Lock()
	uid := a.userID
	a.mu.Unlock()
	if uid == 0 {
		return
	}

	now := time.Now()
	tasks, err := a.tasks.DueSoonForUser(uid, now.Add(dueSoonWindow))
	if err != nil {
		return
	}
	created := false
	for _, t := range tasks {
		var title, content string
		if t.Overdue(now) {
			title = "PI Tracker — Task quá hạn"
			content = fmt.Sprintf("⏰ Task #%d %q đã quá hạn chót %s", t.ID, t.Title, t.DueDate.Format("02/01/2006"))
		} else {
			title = "PI Tracker — Task sắp đến hạn"
			content = fmt.Sprintf("⏱ Task #%d %q sắp đến hạn chót %s", t.ID, t.Title, t.DueDate.Format("02/01/2006"))
		}
		n, isNew, err := a.notifications.CreateIfAbsent(models.Notification{
			UserID: uid, Kind: "due", Content: content,
			TaskID: &t.ID, WorkspaceID: &t.WorkspaceID,
		})
		if err != nil || !isNew {
			continue
		}
		created = true
		a.osNotify(title, content)
		// Poller đừng toast lại bản ghi instance này vừa tạo và đã toast.
		a.notifMu.Lock()
		if a.notifUserID == uid && n.ID > a.notifLastID {
			a.notifLastID = n.ID
		}
		a.notifMu.Unlock()
	}
	// Chuông trên UI cập nhật ngay khi có nhắc mới.
	if created && a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "notifications:new")
	}
}

// ---------- Auth & Session ----------

// SessionDTO — trạng thái đăng nhập hiện tại (userId = 0 nếu chưa đăng nhập).
type SessionDTO struct {
	UserID        uint   `json:"userId"`
	Username      string `json:"username"`
	WorkspaceID   uint   `json:"workspaceId"`
	WorkspaceName string `json:"workspaceName"`
	Role          string `json:"role"`
}

func (a *App) session() SessionDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	return SessionDTO{
		UserID: a.userID, Username: a.username,
		WorkspaceID: a.wsID, WorkspaceName: a.wsName, Role: a.wsRole,
	}
}

func (a *App) requireUser() (uint, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.userID == 0 {
		return 0, fmt.Errorf("chưa đăng nhập")
	}
	return a.userID, nil
}

// requireWorkspace là chokepoint của MỌI thao tác trong workspace: xác nhận
// đã đăng nhập, đã chọn workspace, và thành viên không bị owner khóa.
func (a *App) requireWorkspace() (uint, error) {
	a.mu.Lock()
	uid, wsID := a.userID, a.wsID
	a.mu.Unlock()
	if uid == 0 {
		return 0, fmt.Errorf("chưa đăng nhập")
	}
	if wsID == 0 {
		return 0, fmt.Errorf("chưa chọn workspace")
	}
	locked, err := a.workspaces.IsLocked(wsID, uid)
	if err != nil {
		return 0, err
	}
	if locked {
		return 0, fmt.Errorf("bạn đã bị khóa trong workspace này — liên hệ owner để được mở khóa")
	}
	return wsID, nil
}

func (a *App) actorName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.username == "" {
		return "Không rõ"
	}
	return a.username
}

// GetSession trả về session hiện tại cho frontend khởi động.
func (a *App) GetSession() SessionDTO {
	return a.session()
}

// Register tạo tài khoản (Argon2id) và đăng nhập luôn.
func (a *App) Register(username, password string) (SessionDTO, error) {
	u, err := a.auth.Register(username, password)
	if err != nil {
		return SessionDTO{}, err
	}
	return a.setUser(u)
}

// Login xác thực và tự chọn workspace đầu tiên (nếu có).
func (a *App) Login(username, password string) (SessionDTO, error) {
	u, err := a.auth.Login(username, password)
	if err != nil {
		return SessionDTO{}, err
	}
	return a.setUser(u)
}

func (a *App) setUser(u models.User) (SessionDTO, error) {
	a.mu.Lock()
	a.userID, a.username = u.ID, u.Username
	a.wsID, a.wsName, a.wsRole = 0, "", ""
	a.mu.Unlock()

	// Tự chọn workspace đầu tiên nếu user đã là thành viên đâu đó.
	if list, err := a.workspaces.ListForUser(u.ID); err == nil && len(list) > 0 {
		_ = a.SelectWorkspace(list[0].ID)
	}
	// Đăng nhập xong quét nhắc hạn chót ngay, không đợi nhịp giờ.
	// Gọi đồng bộ: sqlite :memory: (test) mở connection mới cho goroutine
	// song song sẽ thành DB rỗng; một query nhẹ không đáng kể với login.
	a.checkDueTasks()
	return a.session(), nil
}

// RememberMe tạo phiên "ghi nhớ đăng nhập" cho user hiện tại và trả token đã
// MÃ HÓA THEO MÁY về cho frontend lưu ở local. Chỉ ciphertext được lưu local —
// không username/mật khẩu, và ciphertext chỉ giải mã được trên chính máy này.
// Gọi sau khi Login thành công nếu người dùng tick "Lưu phiên đăng nhập".
func (a *App) RememberMe() (string, error) {
	uid, err := a.requireUser()
	if err != nil {
		return "", err
	}
	token, err := a.auth.CreateSession(uid)
	if err != nil {
		return "", err
	}
	return machinecrypto.Encrypt(token)
}

// ResumeSession khôi phục đăng nhập từ token đã lưu local (lúc mở lại app hoặc
// khi chuyển tài khoản). Giải mã theo máy trước, rồi tra token trong DB. Không
// giải mã được (token của máy khác), token không hợp lệ/hết hạn → lỗi để
// frontend xóa token khỏi local.
func (a *App) ResumeSession(enc string) (SessionDTO, error) {
	token, err := machinecrypto.Decrypt(enc)
	if err != nil {
		return SessionDTO{}, err
	}
	u, err := a.auth.ResolveSession(token)
	if err != nil {
		return SessionDTO{}, err
	}
	return a.setUser(u)
}

// SavedAccountDTO — một tài khoản đã ghi nhớ trên máy, để hiện màn chọn/chuyển
// tài khoản. Token là ciphertext theo máy (dùng cho ResumeSession); username
// được backend giải ra lúc chạy — KHÔNG lưu ở local.
type SavedAccountDTO struct {
	Token    string `json:"token"`
	UserID   uint   `json:"userId"`
	Username string `json:"username"`
}

// ListSavedAccounts giải mã & xác thực danh sách token đã lưu local, trả về các
// tài khoản còn hợp lệ (bỏ token hỏng/hết hạn/của máy khác; gộp trùng theo user
// — giữ token xuất hiện trước). Frontend dùng để hiện danh sách chọn tài khoản
// và tự prune lại danh sách token theo kết quả. Không đổi session hiện tại.
func (a *App) ListSavedAccounts(encTokens []string) []SavedAccountDTO {
	out := []SavedAccountDTO{}
	seen := map[uint]bool{}
	for _, enc := range encTokens {
		token, err := machinecrypto.Decrypt(enc)
		if err != nil {
			continue
		}
		u, err := a.auth.ResolveSession(token)
		if err != nil || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		out = append(out, SavedAccountDTO{Token: enc, UserID: u.ID, Username: u.Username})
	}
	return out
}

// ForgetAccount xóa MỘT tài khoản đã ghi nhớ theo token, KHÔNG đụng tới session
// đang đăng nhập (dùng khi "quên" một tài khoản khác trong danh sách).
func (a *App) ForgetAccount(enc string) {
	if enc == "" {
		return
	}
	if token, err := machinecrypto.Decrypt(enc); err == nil {
		a.auth.DeleteSession(token)
	}
}

// Logout đăng xuất session trong bộ nhớ và xóa phiên đã ghi nhớ (nếu có token
// đã mã hóa theo máy). Token giải mã được thì xóa đúng bản ghi trong DB.
func (a *App) Logout(enc string) {
	if enc != "" {
		if token, err := machinecrypto.Decrypt(enc); err == nil {
			a.auth.DeleteSession(token)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.userID, a.username, a.wsID, a.wsName, a.wsRole = 0, "", 0, "", ""
}

// ---------- Workspace ----------

func (a *App) ListWorkspaces() ([]service.WorkspaceInfo, error) {
	uid, err := a.requireUser()
	if err != nil {
		return nil, err
	}
	return a.workspaces.ListForUser(uid)
}

// CreateWorkspace tạo và chuyển sang workspace mới.
func (a *App) CreateWorkspace(name string) (SessionDTO, error) {
	uid, err := a.requireUser()
	if err != nil {
		return SessionDTO{}, err
	}
	ws, err := a.workspaces.Create(name, uid)
	if err != nil {
		return SessionDTO{}, err
	}
	if err := a.SelectWorkspace(ws.ID); err != nil {
		return SessionDTO{}, err
	}
	return a.session(), nil
}

// SelectWorkspace chuyển workspace hiện hành (phải là thành viên).
func (a *App) SelectWorkspace(id uint) error {
	uid, err := a.requireUser()
	if err != nil {
		return err
	}
	list, err := a.workspaces.ListForUser(uid)
	if err != nil {
		return err
	}
	for _, ws := range list {
		if ws.ID == id {
			a.mu.Lock()
			a.wsID, a.wsName, a.wsRole = ws.ID, ws.Name, ws.Role
			a.mu.Unlock()
			return nil
		}
	}
	return fmt.Errorf("bạn không phải thành viên workspace này")
}

// InviteMember mời username vào workspace hiện tại; họ nhận notification.
func (a *App) InviteMember(username string) error {
	uid, err := a.requireUser()
	if err != nil {
		return err
	}
	wsID, err := a.requireWorkspace()
	if err != nil {
		return err
	}
	return a.workspaces.Invite(wsID, uid, username, a.notifications)
}

// SetMemberLock khóa/mở khóa thành viên trong workspace hiện tại (chỉ owner).
// Thành viên bị khóa không thao tác được trong workspace và nhận notification.
func (a *App) SetMemberLock(userID uint, locked bool) error {
	uid, err := a.requireUser()
	if err != nil {
		return err
	}
	wsID, err := a.requireWorkspace()
	if err != nil {
		return err
	}
	return a.workspaces.SetMemberLock(wsID, uid, userID, locked)
}

// ListPeople trả về thành viên workspace hiện tại (shape {ID, Name} như cũ
// để toàn bộ frontend picker/avatar dùng lại không đổi).
func (a *App) ListPeople() ([]service.Member, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return nil, err
	}
	return a.workspaces.Members(wsID)
}

// ---------- Notifications ----------

func (a *App) ListNotifications() ([]service.NotificationView, error) {
	uid, err := a.requireUser()
	if err != nil {
		return nil, err
	}
	return a.notifications.ListForUser(uid)
}

func (a *App) UnreadNotifications() (int64, error) {
	uid, err := a.requireUser()
	if err != nil {
		return 0, err
	}
	return a.notifications.UnreadCount(uid)
}

func (a *App) MarkNotificationsRead() error {
	uid, err := a.requireUser()
	if err != nil {
		return err
	}
	return a.notifications.MarkAllRead(uid)
}

// RespondInvitation chấp nhận/từ chối lời mời từ chuông thông báo.
func (a *App) RespondInvitation(invitationID uint, accept bool) error {
	uid, err := a.requireUser()
	if err != nil {
		return err
	}
	return a.workspaces.Respond(invitationID, uid, accept)
}

// ---------- Saved views (tab bộ lọc trên trang Tasks) ----------

// savedViewOwned trả về view sau khi xác nhận nó thuộc user hiện tại trong
// workspace hiện tại — chặn sửa/xóa view của người khác qua id đoán mò.
func (a *App) savedViewOwned(id uint) (models.SavedView, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return models.SavedView{}, err
	}
	uid, err := a.requireUser()
	if err != nil {
		return models.SavedView{}, err
	}
	v, err := a.savedViews.Get(id)
	if err != nil {
		return models.SavedView{}, err
	}
	if v.WorkspaceID != wsID || v.UserID != uid {
		return models.SavedView{}, fmt.Errorf("view không tồn tại trong workspace này")
	}
	return v, nil
}

func (a *App) ListSavedViews() ([]models.SavedView, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return nil, err
	}
	uid, err := a.requireUser()
	if err != nil {
		return nil, err
	}
	return a.savedViews.List(wsID, uid)
}

func (a *App) CreateSavedView(name, filters string) (models.SavedView, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return models.SavedView{}, err
	}
	uid, err := a.requireUser()
	if err != nil {
		return models.SavedView{}, err
	}
	return a.savedViews.Create(wsID, uid, name, filters)
}

func (a *App) UpdateSavedView(id uint, name, filters string) (models.SavedView, error) {
	if _, err := a.savedViewOwned(id); err != nil {
		return models.SavedView{}, err
	}
	return a.savedViews.Update(id, name, filters)
}

func (a *App) DeleteSavedView(id uint) error {
	if _, err := a.savedViewOwned(id); err != nil {
		return err
	}
	return a.savedViews.Delete(id)
}

// ---------- Tasks ----------

// TaskDTO mirrors models.Task with dates as "YYYY-MM-DD" strings ("" = chưa có).
type TaskDTO struct {
	ID                   uint    `json:"id"`
	Title                string  `json:"title"`
	Description          string  `json:"description"`
	Type                 int     `json:"type"` // models.TaskType: 1 Theo plan | 2 Phát sinh (bug) | 3 Phát sinh theo plan
	Size                 string  `json:"size"`
	Status               string  `json:"status"`
	Priority             string  `json:"priority"`   // P1..P4, "" khi lưu = mặc định P3
	AssigneeID           uint    `json:"assigneeId"` // User.ID thành viên, 0 = chưa gán
	EstimateCustomerDays float64 `json:"estimateCustomerDays"`
	EstimateAIDays       float64 `json:"estimateAiDays"`
	ActualDays           float64 `json:"actualDays"` // effort thực tế (ngày công), 0 = chưa nhập
	AIUsed               bool    `json:"aiUsed"`
	Blocker              string  `json:"blocker"`
	BlockedDays          float64 `json:"blockedDays"`
	CreatedDate          string  `json:"createdDate"`
	StartDate            string  `json:"startDate"`
	DueDate              string  `json:"dueDate"` // hạn chót, "" = không đặt
	DoneDate             string  `json:"doneDate"`
	// Nhóm bug tracking — chỉ có nghĩa khi Type = "Phát sinh (bug)".
	ReporterID    uint   `json:"reporterId"`    // người báo bug, 0 = chưa ghi
	Severity      string `json:"severity"`      // Critical | Major | Minor | ""
	Resolution    string `json:"resolution"`    // Fixed | Won't Fix | Cannot Reproduce | Duplicate | ""
	RelatedTaskID uint   `json:"relatedTaskId"` // task gốc sinh bug, 0 = không liên kết
	// Checklist progress (chỉ đổ khi ListTasks, phục vụ badge trên board).
	TodoTotal int `json:"todoTotal"`
	TodoDone  int `json:"todoDone"`
	// InitialTodos: checklist tạo sẵn (vd do AI gợi ý) — chỉ dùng khi TẠO MỚI
	// task (ID == 0); bỏ qua khi cập nhật để không nhân đôi.
	InitialTodos []string `json:"initialTodos"`
}

func taskToDTO(t models.Task) TaskDTO {
	dto := TaskDTO{
		ID:                   t.ID,
		Title:                t.Title,
		Description:          t.Description,
		Type:                 int(t.Type),
		Size:                 string(t.Size),
		Status:               string(t.Status),
		Priority:             string(t.Priority),
		EstimateCustomerDays: t.EstimateCustomerDays,
		EstimateAIDays:       t.EstimateAIDays,
		ActualDays:           t.ActualDays,
		AIUsed:               t.AIUsed,
		Blocker:              t.Blocker,
		BlockedDays:          t.BlockedDays,
		CreatedDate:          t.CreatedAt.Format(dateLayout),
		Severity:             string(t.Severity),
		Resolution:           string(t.Resolution),
	}
	if t.AssigneeID != nil {
		dto.AssigneeID = *t.AssigneeID
	}
	if t.ReporterID != nil {
		dto.ReporterID = *t.ReporterID
	}
	if t.RelatedTaskID != nil {
		dto.RelatedTaskID = *t.RelatedTaskID
	}
	if t.StartDate != nil {
		dto.StartDate = t.StartDate.Format(dateLayout)
	}
	if t.DueDate != nil {
		dto.DueDate = t.DueDate.Format(dateLayout)
	}
	if t.DoneDate != nil {
		dto.DoneDate = t.DoneDate.Format(dateLayout)
	}
	return dto
}

// startOfDay chuẩn hóa mốc về 00:00 local để so sánh theo NGÀY,
// vì CreatedAt có thể mang giờ-phút còn Start/Done date luôn là 00:00.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func parseDate(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.ParseInLocation(dateLayout, s, time.Local)
	if err != nil {
		return nil, fmt.Errorf("ngày %q không hợp lệ (định dạng YYYY-MM-DD)", s)
	}
	return &t, nil
}

func (a *App) ListTasks() ([]TaskDTO, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return nil, err
	}
	tasks, err := a.tasks.List(wsID)
	if err != nil {
		return nil, err
	}
	counts, err := a.todos.Counts()
	if err != nil {
		return nil, err
	}
	dtos := make([]TaskDTO, len(tasks))
	for i, t := range tasks {
		dtos[i] = taskToDTO(t)
		if c, ok := counts[t.ID]; ok {
			dtos[i].TodoTotal, dtos[i].TodoDone = c[0], c[1]
		}
	}
	return dtos, nil
}

// taskInWorkspace xác nhận task thuộc workspace hiện tại.
func (a *App) taskInWorkspace(taskID uint) (models.Task, uint, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return models.Task{}, 0, err
	}
	t, err := a.tasks.Get(taskID)
	if err != nil {
		return models.Task{}, 0, fmt.Errorf("không tìm thấy task id %d", taskID)
	}
	if t.WorkspaceID != wsID {
		return models.Task{}, 0, fmt.Errorf("task không thuộc workspace hiện tại")
	}
	return t, wsID, nil
}

// SaveTask creates (id == 0) or updates a task, enforcing the same rules
// as before: title required, Done ⇔ Done date consistency.
func (a *App) SaveTask(dto TaskDTO) error {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return err
	}
	if strings.TrimSpace(dto.Title) == "" {
		return fmt.Errorf("tiêu đề không được để trống")
	}

	// Loại task: bỏ trống (0) = Theo plan, còn lại phải là giá trị hợp lệ.
	taskType := models.TaskType(dto.Type)
	if taskType == 0 {
		taskType = models.TypePlan
	}
	if !models.ValidTaskType(taskType) {
		return fmt.Errorf("loại task %d không hợp lệ (1 Theo plan | 2 Phát sinh (bug) | 3 Phát sinh theo plan)", dto.Type)
	}

	// Ưu tiên: bỏ trống = P3, còn lại phải là P1..P4.
	priority := models.TaskPriority(strings.TrimSpace(dto.Priority))
	if priority == "" {
		priority = models.PriorityP3
	}
	if !models.ValidPriority(priority) {
		return fmt.Errorf("ưu tiên %q không hợp lệ (P1 | P2 | P3 | P4)", dto.Priority)
	}

	t := models.Task{
		ID:                   dto.ID,
		WorkspaceID:          wsID,
		Title:                strings.TrimSpace(dto.Title),
		Description:          dto.Description,
		Type:                 taskType,
		Size:                 models.TaskSize(dto.Size),
		Status:               models.TaskStatus(dto.Status),
		Priority:             priority,
		EstimateCustomerDays: dto.EstimateCustomerDays,
		EstimateAIDays:       dto.EstimateAIDays,
		ActualDays:           dto.ActualDays,
		AIUsed:               dto.AIUsed,
		Blocker:              dto.Blocker,
		BlockedDays:          dto.BlockedDays,
	}
	if t.ActualDays < 0 {
		return fmt.Errorf("effort thực tế phải ≥ 0")
	}
	if dto.AssigneeID != 0 {
		id := dto.AssigneeID
		t.AssigneeID = &id
	}

	// Nhóm field bug chỉ giữ khi task là bug; đổi sang loại khác thì xóa sạch
	// để không còn severity/resolution mồ côi.
	if t.IsBug() {
		t.Severity = models.BugSeverity(dto.Severity)
		if !models.ValidSeverity(t.Severity) {
			return fmt.Errorf("mức độ bug %q không hợp lệ (Critical | Major | Minor)", dto.Severity)
		}
		t.Resolution = models.BugResolution(dto.Resolution)
		if !models.ValidResolution(t.Resolution) {
			return fmt.Errorf("cách đóng bug %q không hợp lệ (Fixed | Won't Fix | Cannot Reproduce | Duplicate)", dto.Resolution)
		}
		if dto.ReporterID != 0 {
			id := dto.ReporterID
			t.ReporterID = &id
		} else if dto.ID == 0 {
			// Tạo bug mới chưa ghi người báo → mặc định người đang đăng nhập.
			uid, err := a.requireUser()
			if err != nil {
				return err
			}
			t.ReporterID = &uid
		}
		if dto.RelatedTaskID != 0 {
			if dto.RelatedTaskID == dto.ID {
				return fmt.Errorf("task gốc không được là chính bug này")
			}
			rel, err := a.tasks.Get(dto.RelatedTaskID)
			if err != nil || rel.WorkspaceID != wsID {
				return fmt.Errorf("không tìm thấy task gốc id %d trong workspace hiện tại", dto.RelatedTaskID)
			}
			id := dto.RelatedTaskID
			t.RelatedTaskID = &id
		}
	}

	created, err := parseDate(dto.CreatedDate)
	if err != nil {
		return fmt.Errorf("ngày tạo: %w", err)
	}
	if created == nil {
		now := time.Now()
		created = &now
	}
	t.CreatedAt = *created

	if t.StartDate, err = parseDate(dto.StartDate); err != nil {
		return fmt.Errorf("start date: %w", err)
	}
	if t.DueDate, err = parseDate(dto.DueDate); err != nil {
		return fmt.Errorf("hạn chót: %w", err)
	}
	if t.DoneDate, err = parseDate(dto.DoneDate); err != nil {
		return fmt.Errorf("done date: %w", err)
	}

	// Đánh dấu Done thì tự set Done date hôm nay nếu bỏ trống.
	if t.Status == models.StatusDone && t.DoneDate == nil {
		now := time.Now()
		t.DoneDate = &now
	}
	// Chặn dữ liệu mâu thuẫn: có Done date nhưng trạng thái chưa phải Done.
	if t.DoneDate != nil && t.Status != models.StatusDone {
		return fmt.Errorf("task có Done date nhưng trạng thái không phải Done — chuyển trạng thái sang Done hoặc xóa Done date")
	}
	// Chặn mốc thời gian lệch logic (làm sai Lead Time / Cycle Time).
	if t.StartDate != nil && t.StartDate.Before(startOfDay(t.CreatedAt)) {
		return fmt.Errorf("start date (%s) trước ngày tạo task (%s) — sửa lại ngày tạo hoặc start date",
			t.StartDate.Format(dateLayout), t.CreatedAt.Format(dateLayout))
	}
	if t.DoneDate != nil && t.StartDate != nil && t.DoneDate.Before(*t.StartDate) {
		return fmt.Errorf("done date (%s) trước start date (%s)",
			t.DoneDate.Format(dateLayout), t.StartDate.Format(dateLayout))
	}

	// Lấy bản cũ trước khi lưu để ghi lịch sử thay đổi (và xác nhận quyền).
	var old *models.Task
	if t.ID != 0 {
		prev, _, err := a.taskInWorkspace(t.ID)
		if err != nil {
			return err
		}
		old = &prev
	}

	// Rời trạng thái Blocked → tự cộng thời gian đã nằm ở Blocked (theo lịch
	// sử trạng thái) vào BlockedDays, trừ khi người dùng tự sửa BlockedDays
	// trong chính lần lưu này (tôn trọng số nhập tay).
	autoBlockedNote := ""
	if old != nil && old.Status == models.StatusBlocked && t.Status != models.StatusBlocked &&
		t.BlockedDays == old.BlockedDays {
		if since, err := a.statuses.LastEntered(t.ID, models.StatusBlocked); err == nil && since != nil {
			days := math.Round(time.Since(*since).Hours()/24*10) / 10
			if days > 0 {
				t.BlockedDays += days
				autoBlockedNote = fmt.Sprintf(
					"tự cộng %s ngày blocked (Blocked từ %s) — sửa lại trường 'Thời gian blocked' nếu chưa đúng",
					strconv.FormatFloat(days, 'f', -1, 64), since.Format(dateLayout))
			}
		}
	}

	if err := a.tasks.Save(&t); err != nil {
		return err
	}

	names, _ := a.workspaces.MemberNames(wsID)
	if old == nil {
		_ = a.activities.Log(t.ID, a.actorName(), "create", "tạo task")
		_ = a.statuses.Log(t.ID, "", t.Status, a.actorName())
		// Checklist tạo sẵn (vd AI gợi ý) chỉ áp khi tạo mới — bỏ mục rỗng.
		for _, title := range dto.InitialTodos {
			if strings.TrimSpace(title) == "" {
				continue
			}
			if item, err := a.todos.Add(t.ID, title); err == nil {
				_ = a.activities.Log(t.ID, a.actorName(), "todo", "thêm việc: "+item.Title)
			}
		}
	} else {
		if changes := taskChanges(*old, t, names); len(changes) > 0 {
			_ = a.activities.Log(t.ID, a.actorName(), "update", strings.Join(changes, "\n"))
		}
		if old.Status != t.Status {
			_ = a.statuses.Log(t.ID, old.Status, t.Status, a.actorName())
		}
	}
	if autoBlockedNote != "" {
		_ = a.activities.Log(t.ID, a.actorName(), "update", autoBlockedNote)
	}
	// Đặt/đổi hạn chót có hiệu lực ngay: quét nhắc việc luôn thay vì bắt
	// người dùng đợi nhịp quét theo giờ (task đã nhắc rồi không nhắc lại).
	if t.DueDate != nil && t.Status != models.StatusDone {
		a.checkDueTasks()
	}
	return nil
}

// AIStatusDTO cho biết tính năng gợi ý AI đã cấu hình chưa (để frontend
// ẩn/hiện nút "Gợi ý AI" và thông báo phù hợp).
type AIStatusDTO struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// AIStatus trả về trạng thái cấu hình LLM hiện tại.
func (a *App) AIStatus() AIStatusDTO {
	provider, model, enabled := a.estimator.Info()
	return AIStatusDTO{Enabled: enabled, Provider: provider, Model: model}
}

// SuggestEstimate gọi LLM đề xuất estimate cho bản nháp task đang mở, dựa trên
// mô tả và các task Done gần đây của workspace làm dữ liệu tham chiếu.
func (a *App) SuggestEstimate(dto TaskDTO) (ai.Suggestion, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return ai.Suggestion{}, err
	}
	if !a.estimator.Enabled() {
		return ai.Suggestion{}, fmt.Errorf("gợi ý AI chưa được cấu hình — đặt AI_PROVIDER, AI_API_KEY (và AI_MODEL nếu cần) trong file .env rồi mở lại app")
	}
	if strings.TrimSpace(dto.Title) == "" {
		return ai.Suggestion{}, fmt.Errorf("nhập tiêu đề task trước khi xin gợi ý")
	}

	taskType := models.TaskType(dto.Type)
	if taskType == 0 {
		taskType = models.TypePlan
	}

	recent, err := a.tasks.RecentDone(wsID, 30)
	if err != nil {
		return ai.Suggestion{}, err
	}
	examples := make([]ai.Example, 0, len(recent))
	for _, t := range recent {
		ex := ai.Example{
			Title:      t.Title,
			Type:       t.Type.Label(),
			Size:       string(t.Size),
			EstAIDays:  t.EstimateAIDays,
			ActualDays: t.ActualDays,
		}
		if c, ok := t.CycleDays(); ok {
			ex.CycleDays = c
		}
		examples = append(examples, ex)
	}

	draft := ai.Draft{
		Title:       dto.Title,
		Description: dto.Description,
		Type:        taskType.Label(),
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	return a.estimator.Suggest(ctx, draft, examples)
}

func (a *App) DeleteTask(id uint) error {
	if _, _, err := a.taskInWorkspace(id); err != nil {
		return err
	}
	if err := a.tasks.Delete(id); err != nil {
		return err
	}
	_ = a.todos.DeleteForTask(id)
	_ = a.activities.DeleteForTask(id)
	_ = a.statuses.DeleteForTask(id)
	return nil
}

// taskChanges liệt kê khác biệt giữa 2 bản task, dạng "Trường: cũ → mới".
func taskChanges(old, new models.Task, names map[uint]string) []string {
	var c []string
	add := func(label, o, n string) {
		if o != n {
			if o == "" {
				o = "—"
			}
			if n == "" {
				n = "—"
			}
			c = append(c, fmt.Sprintf("%s: %s → %s", label, o, n))
		}
	}
	nameOf := func(id *uint) string {
		if id == nil {
			return ""
		}
		return names[*id]
	}
	date := func(p *time.Time) string {
		if p == nil {
			return ""
		}
		return p.Format(dateLayout)
	}
	num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	boolStr := func(b bool) string {
		if b {
			return "Có"
		}
		return "Không"
	}
	trunc := func(s string) string {
		r := []rune(s)
		if len(r) > 60 {
			return string(r[:60]) + "…"
		}
		return s
	}

	taskRef := func(id *uint) string {
		if id == nil {
			return ""
		}
		return fmt.Sprintf("#%d", *id)
	}

	add("Tiêu đề", old.Title, new.Title)
	add("Mô tả", trunc(old.Description), trunc(new.Description))
	add("Loại", old.Type.Label(), new.Type.Label())
	add("Size", string(old.Size), string(new.Size))
	add("Trạng thái", string(old.Status), string(new.Status))
	add("Ưu tiên", string(old.Priority), string(new.Priority))
	add("Người phụ trách", nameOf(old.AssigneeID), nameOf(new.AssigneeID))
	add("Est báo khách (ngày)", num(old.EstimateCustomerDays), num(new.EstimateCustomerDays))
	add("Est AI (ngày)", num(old.EstimateAIDays), num(new.EstimateAIDays))
	add("Effort thực tế (ngày)", num(old.ActualDays), num(new.ActualDays))
	add("Dùng AI", boolStr(old.AIUsed), boolStr(new.AIUsed))
	add("Blocker", old.Blocker, new.Blocker)
	add("Blocked (ngày)", num(old.BlockedDays), num(new.BlockedDays))
	add("Ngày tạo", old.CreatedAt.Format(dateLayout), new.CreatedAt.Format(dateLayout))
	add("Start date", date(old.StartDate), date(new.StartDate))
	add("Hạn chót", date(old.DueDate), date(new.DueDate))
	add("Done date", date(old.DoneDate), date(new.DoneDate))
	add("Người báo bug", nameOf(old.ReporterID), nameOf(new.ReporterID))
	add("Mức độ bug", string(old.Severity), string(new.Severity))
	add("Cách đóng bug", string(old.Resolution), string(new.Resolution))
	add("Task gốc", taskRef(old.RelatedTaskID), taskRef(new.RelatedTaskID))
	return c
}

// ---------- Checklist & Comment & Activity ----------

func (a *App) ListTodos(taskID uint) ([]models.TodoItem, error) {
	if _, _, err := a.taskInWorkspace(taskID); err != nil {
		return nil, err
	}
	return a.todos.List(taskID)
}

func (a *App) AddTodo(taskID uint, title string) error {
	if _, _, err := a.taskInWorkspace(taskID); err != nil {
		return err
	}
	item, err := a.todos.Add(taskID, title)
	if err != nil {
		return err
	}
	return a.activities.Log(taskID, a.actorName(), "todo", "thêm việc: "+item.Title)
}

func (a *App) ToggleTodo(id uint, done bool) error {
	item, err := a.todos.SetDone(id, done)
	if err != nil {
		return err
	}
	msg := "hoàn thành việc: " + item.Title
	if !done {
		msg = "bỏ hoàn thành việc: " + item.Title
	}
	return a.activities.Log(item.TaskID, a.actorName(), "todo", msg)
}

func (a *App) DeleteTodo(id uint) error {
	item, err := a.todos.Delete(id)
	if err != nil {
		return err
	}
	return a.activities.Log(item.TaskID, a.actorName(), "todo", "xóa việc: "+item.Title)
}

// AddComment lưu bình luận cho task; parentID != 0 = trả lời bình luận đó
// (thread 1 cấp: trả lời một reply sẽ được gắn về comment gốc của thread).
func (a *App) AddComment(taskID uint, content string, parentID uint) error {
	t, wsID, err := a.taskInWorkspace(taskID)
	if err != nil {
		return err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("bình luận không được để trống")
	}

	// Xác nhận comment cha hợp lệ; người được trả lời = tác giả comment cha.
	replyToName := ""
	if parentID != 0 {
		parent, err := a.activities.Get(parentID)
		if err != nil || parent.TaskID != taskID || parent.Kind != "comment" {
			return fmt.Errorf("không tìm thấy bình luận được trả lời trong task này")
		}
		replyToName = parent.ActorName
		if parent.ParentID != nil {
			parentID = *parent.ParentID // reply của reply → gắn về comment gốc
		}
	}

	actor := a.actorName()
	var act models.Activity
	if parentID != 0 {
		act, err = a.activities.LogReply(taskID, actor, content, parentID)
	} else {
		act, err = a.activities.LogComment(taskID, actor, content)
	}
	if err != nil {
		return err
	}
	a.notifyComment(t, wsID, act.ID, content, replyToName)
	return nil
}

// notifyComment gửi thông báo chuông sau một bình luận: người được trả lời
// (kind "reply") và các thành viên được nhắc "@username" (kind "mention") —
// trừ chính người viết, mỗi người tối đa một thông báo. Thông báo gắn task
// để click là nhảy tới; lỗi khi gửi không chặn bình luận (đã lưu xong).
func (a *App) notifyComment(t models.Task, wsID, actID uint, comment, replyToName string) {
	a.mu.Lock()
	uid := a.userID
	a.mu.Unlock()

	members, err := a.workspaces.Members(wsID)
	if err != nil {
		return
	}
	msg := comment
	if r := []rune(msg); len(r) > 80 {
		msg = string(r[:80]) + "…"
	}
	actor := a.actorName()

	notified := map[uint]bool{uid: true}
	for _, mem := range members {
		if mem.Name == replyToName && !notified[mem.ID] {
			notified[mem.ID] = true
			_, _ = a.notifications.Create(models.Notification{
				UserID: mem.ID, Kind: "reply",
				Content: fmt.Sprintf("↩ %s đã trả lời bình luận của bạn trong task #%d %q: %s", actor, t.ID, t.Title, msg),
				TaskID:  &t.ID, WorkspaceID: &wsID, ActivityID: &actID,
			})
		}
	}
	for _, mem := range members {
		if notified[mem.ID] || !mentionsUser(comment, mem.Name) {
			continue
		}
		notified[mem.ID] = true
		_, _ = a.notifications.Create(models.Notification{
			UserID: mem.ID, Kind: "mention",
			Content: fmt.Sprintf("💬 %s nhắc tới bạn trong task #%d %q: %s", actor, t.ID, t.Title, msg),
			TaskID:  &t.ID, WorkspaceID: &wsID, ActivityID: &actID,
		})
	}
}

// mentionsUser báo content có chứa "@username" như một token trọn vẹn không:
// ký tự liền trước @ và liền sau username không phải chữ/số/gạch dưới —
// tránh "@sonnn" bị khớp nhầm cho user "son", hay email "a@son.vn".
func mentionsUser(content, username string) bool {
	if username == "" {
		return false
	}
	needle := "@" + username
	for i := 0; i+len(needle) <= len(content); {
		j := strings.Index(content[i:], needle)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(needle)
		beforeOK := start == 0
		if !beforeOK {
			r, _ := utf8.DecodeLastRuneInString(content[:start])
			beforeOK = !isMentionWordChar(r)
		}
		afterOK := end == len(content)
		if !afterOK {
			r, _ := utf8.DecodeRuneInString(content[end:])
			afterOK = !isMentionWordChar(r)
		}
		if beforeOK && afterOK {
			return true
		}
		i = start + 1
	}
	return false
}

func isMentionWordChar(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (a *App) ListActivities(taskID uint) ([]models.Activity, error) {
	if _, _, err := a.taskInWorkspace(taskID); err != nil {
		return nil, err
	}
	return a.activities.List(taskID)
}

// ListStatusChanges trả về timeline trạng thái của task, cũ nhất trước.
func (a *App) ListStatusChanges(taskID uint) ([]models.StatusChange, error) {
	if _, _, err := a.taskInWorkspace(taskID); err != nil {
		return nil, err
	}
	return a.statuses.List(taskID)
}

// ---------- Settings ----------

func (a *App) GetSettings() (models.Settings, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return models.Settings{}, err
	}
	return a.settings.Get(wsID)
}

func (a *App) SaveSettings(st models.Settings) error {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return err
	}
	cur, err := a.settings.Get(wsID)
	if err != nil {
		return err
	}
	// Không cho ghi đè settings của workspace khác.
	st.ID, st.WorkspaceID = cur.ID, cur.WorkspaceID
	if st.TBaseline <= 0 || st.CTBaseline <= 0 {
		return fmt.Errorf("baseline phải > 0")
	}
	if st.PITarget <= 0 || st.Capacity <= 0 {
		return fmt.Errorf("mục tiêu PI và capacity phải > 0")
	}
	return a.settings.Save(&st)
}

// ---------- Metrics & Report ----------

// MetricsResult bundles everything the dashboard needs for one month.
type MetricsResult struct {
	Metrics  service.Metrics `json:"metrics"`
	Advice   service.Advice  `json:"advice"`
	Settings models.Settings `json:"settings"`
}

// parseMonthAsOf đọc cặp tham số dùng chung của các binding metrics/report:
// month "YYYY-MM" và asOf "YYYY-MM-DD" ("" = hôm nay) — trả về mốc tháng và
// "ngày tính".
func parseMonthAsOf(month, asOf string) (time.Time, time.Time, error) {
	m, err := time.ParseInLocation("2006-01", month, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("tháng %q không hợp lệ (định dạng YYYY-MM)", month)
	}
	now := time.Now()
	if strings.TrimSpace(asOf) != "" {
		p, err := parseDate(asOf)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("ngày tính: %w", err)
		}
		now = *p
	}
	return m, now, nil
}

// GetMetrics computes indicators for month given as "YYYY-MM".
// assigneeID != 0: dashboard riêng của thành viên đó (baseline 1 người).
// asOf ("YYYY-MM-DD", rỗng = hôm nay): "ngày tính".
func (a *App) GetMetrics(month string, assigneeID uint, asOf string) (MetricsResult, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return MetricsResult{}, err
	}
	m, now, err := parseMonthAsOf(month, asOf)
	if err != nil {
		return MetricsResult{}, err
	}
	metrics, st, err := a.metrics.Compute(wsID, m, now, assigneeID)
	if err != nil {
		return MetricsResult{}, err
	}
	advice := a.metrics.Advise(metrics, st)
	return MetricsResult{Metrics: metrics, Advice: advice, Settings: st}, nil
}

// ExportReport xuất báo cáo PI của tháng (format: "xlsx" | "pdf").
func (a *App) ExportReport(month, format, asOf string, assigneeID uint) (string, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return "", err
	}
	m, now, err := parseMonthAsOf(month, asOf)
	if err != nil {
		return "", err
	}
	metrics, st, err := a.metrics.Compute(wsID, m, now, assigneeID)
	if err != nil {
		return "", err
	}
	advice := a.metrics.Advise(metrics, st)
	tasks, err := a.metrics.DoneTasksAsOf(wsID, m, now, assigneeID)
	if err != nil {
		return "", err
	}
	names, err := a.workspaces.MemberNames(wsID)
	if err != nil {
		return "", err
	}
	assigneeName := ""
	if assigneeID != 0 {
		assigneeName = names[assigneeID]
		if assigneeName == "" {
			return "", fmt.Errorf("không tìm thấy thành viên id %d", assigneeID)
		}
	}

	// Phân tích nguồn gốc: task Done trong tháng đã sinh ra bao nhiêu bug
	// (cột "Bug phát sinh" trong phụ lục) — cùng nguồn số liệu với Metrics.
	originBugs, err := a.metrics.BugsByOrigin(wsID, tasks, now)
	if err != nil {
		return "", err
	}

	data := report.Data{
		Month: m, AsOf: now, AssigneeName: assigneeName,
		Metrics: metrics, Advice: advice,
		Settings: st, Tasks: tasks, People: names,
		OriginBugs: originBugs,
	}

	var content []byte
	var display string
	switch format {
	case "xlsx":
		content, err = report.BuildExcel(data)
		display = "Excel (*.xlsx)"
	case "pdf":
		content, err = report.BuildPDF(data)
		display = "PDF (*.pdf)"
	default:
		return "", fmt.Errorf("định dạng %q không hỗ trợ (xlsx | pdf)", format)
	}
	if err != nil {
		return "", fmt.Errorf("tạo báo cáo: %w", err)
	}

	suffix := ""
	if assigneeName != "" {
		suffix = "-" + strings.ReplaceAll(assigneeName, " ", "-")
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "Lưu báo cáo PI",
		DefaultFilename: fmt.Sprintf("bao-cao-pi-%s%s.%s", month, suffix, format),
		Filters:         []wruntime.FileFilter{{DisplayName: display, Pattern: "*." + format}},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil // người dùng hủy
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("ghi file: %w", err)
	}
	return path, nil
}

// ---------- Team metrics (trang Team) ----------

// MemberMetrics — chỉ số tháng của một thành viên (baseline tính theo 1 người).
type MemberMetrics struct {
	AssigneeID uint            `json:"assigneeId"`
	Name       string          `json:"name"`
	Metrics    service.Metrics `json:"metrics"`
}

// TeamMetricsResult gộp chỉ số toàn team + từng thành viên trong một lần gọi,
// cho bảng so sánh của trang Team.
type TeamMetricsResult struct {
	Team     service.Metrics `json:"team"`
	Members  []MemberMetrics `json:"members"`
	Settings models.Settings `json:"settings"`
}

// GetTeamMetrics tính chỉ số tháng cho toàn team và lặp theo từng thành viên
// (cùng cửa sổ tháng + "ngày tính" như GetMetrics).
func (a *App) GetTeamMetrics(month, asOf string) (TeamMetricsResult, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return TeamMetricsResult{}, err
	}
	m, now, err := parseMonthAsOf(month, asOf)
	if err != nil {
		return TeamMetricsResult{}, err
	}

	team, st, err := a.metrics.Compute(wsID, m, now, 0)
	if err != nil {
		return TeamMetricsResult{}, err
	}
	res := TeamMetricsResult{Team: team, Settings: st}

	members, err := a.workspaces.Members(wsID)
	if err != nil {
		return TeamMetricsResult{}, err
	}
	for _, p := range members {
		mm, _, err := a.metrics.Compute(wsID, m, now, p.ID)
		if err != nil {
			return TeamMetricsResult{}, err
		}
		res.Members = append(res.Members, MemberMetrics{AssigneeID: p.ID, Name: p.Name, Metrics: mm})
	}
	return res, nil
}

// TrendPoint — chỉ số rút gọn của một tháng cho biểu đồ xu hướng.
type TrendPoint struct {
	Month      string  `json:"month"` // "YYYY-MM"
	PI         float64 `json:"pi"`
	Throughput float64 `json:"throughput"`
	Points     float64 `json:"points"` // Điểm/tháng (điểm size tích lũy)
	CycleTime  float64 `json:"cycleTime"`
	LeadTime   float64 `json:"leadTime"`
	DoneCount  int     `json:"doneCount"`
	BugRatio   float64 `json:"bugRatio"`
}

// TrendSeries — chuỗi điểm theo tháng của toàn team hoặc một thành viên.
type TrendSeries struct {
	AssigneeID uint         `json:"assigneeId"` // 0 = toàn team
	Name       string       `json:"name"`
	Points     []TrendPoint `json:"points"`
}

// TeamTrendResult — dữ liệu biểu đồ xu hướng của trang Team.
type TeamTrendResult struct {
	Months   []string      `json:"months"`
	Team     TrendSeries   `json:"team"`
	Members  []TrendSeries `json:"members"`
	PITarget float64       `json:"piTarget"`
}

func trendPoint(monthKey string, m service.Metrics) TrendPoint {
	return TrendPoint{
		Month:      monthKey,
		PI:         m.PI,
		Throughput: m.Throughput,
		Points:     m.PointsPerMonth,
		CycleTime:  m.CycleTime,
		LeadTime:   m.LeadTime,
		DoneCount:  m.DoneCount,
		BugRatio:   m.BugRatio,
	}
}

// GetTeamTrend tính chuỗi chỉ số theo tháng — kết thúc ở endMonth ("YYYY-MM"),
// lùi về trước tổng cộng months tháng (kẹp 2..24) — cho toàn team và từng
// thành viên. asOf chỉ ảnh hưởng tháng chứa "ngày tính" (giống dashboard);
// các tháng đã qua luôn được chốt sổ trọn tháng.
func (a *App) GetTeamTrend(endMonth string, months int, asOf string) (TeamTrendResult, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return TeamTrendResult{}, err
	}
	end, now, err := parseMonthAsOf(endMonth, asOf)
	if err != nil {
		return TeamTrendResult{}, err
	}
	if months < 2 {
		months = 2
	}
	if months > 24 {
		months = 24
	}

	members, err := a.workspaces.Members(wsID)
	if err != nil {
		return TeamTrendResult{}, err
	}

	res := TeamTrendResult{Team: TrendSeries{Name: "Toàn team"}}
	for _, p := range members {
		res.Members = append(res.Members, TrendSeries{AssigneeID: p.ID, Name: p.Name})
	}

	for i := months - 1; i >= 0; i-- {
		mo := end.AddDate(0, -i, 0)
		key := mo.Format("2006-01")
		res.Months = append(res.Months, key)

		team, st, err := a.metrics.Compute(wsID, mo, now, 0)
		if err != nil {
			return TeamTrendResult{}, err
		}
		res.PITarget = st.PITarget
		res.Team.Points = append(res.Team.Points, trendPoint(key, team))

		for j, p := range members {
			mm, _, err := a.metrics.Compute(wsID, mo, now, p.ID)
			if err != nil {
				return TeamTrendResult{}, err
			}
			res.Members[j].Points = append(res.Members[j].Points, trendPoint(key, mm))
		}
	}
	return res, nil
}
