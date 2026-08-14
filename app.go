package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gen2brain/beeep"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"gorm.io/gorm"

	"taskmanager/internal/config"
	"taskmanager/internal/database"
	"taskmanager/internal/machinecrypto"
	"taskmanager/internal/mcp"
	"taskmanager/internal/models"
	"taskmanager/internal/report"
	"taskmanager/internal/service"
)

const dateLayout = "2006-01-02"

// App exposes backend methods to the Vue frontend via Wails bindings.
type App struct {
	ctx context.Context

	// db là kết nối hiện tại; nil khi kết nối lúc khởi động thất bại (chế độ
	// suy giảm: chỉ hiện banner lỗi + nút Thử lại). dbErr giữ thông báo thân
	// thiện để frontend hiển thị. dbMu bảo vệ cả ba: RetryDB chạy trên goroutine
	// Wails, đọc/ghi song song với DBStatus và startup. dbConnecting chặn hai
	// lần RetryDB chồng nhau (double-click) → khỏi start watcher trùng.
	dbMu         sync.Mutex
	db           *gorm.DB
	dbErr        string
	dbConnecting bool

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
	dependencies  *service.DependencyService
	tags          *service.TagService

	// mcp — MCP server localhost, bật/tắt từ trang "MCP". Tồn tại suốt vòng đời
	// app; các công cụ của nó thao tác dưới session đang đăng nhập bên dưới.
	mcp *mcp.Server

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

	// Trạng thái poller đồng bộ dữ liệu: workspace đang theo dõi + fingerprint
	// lần quét trước. Fingerprint đổi = client khác vừa sửa dữ liệu workspace.
	dataMu   sync.Mutex
	dataWsID uint
	dataFP   string
}

func NewApp(db *gorm.DB) *App {
	a := &App{
		// Notification native của HĐH (Linux: D-Bus/notify-send, Windows:
		// toast, macOS: notification center) — hiện cả khi app chạy nền.
		osNotify: func(title, body string) { _ = beeep.Notify(title, body, "") },
	}
	a.attachDB(db)
	return a
}

// mcpServer trả về MCP server, khởi tạo lười lần đầu. Init lười để serverInfo
// lấy được a.version (main gán SAU NewApp). Khóa a.mu vì Wails gọi mỗi phương
// thức bound trên goroutine riêng — hai lời gọi đồng thời có thể đua ghi a.mcp.
func (a *App) mcpServer() *mcp.Server {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mcp == nil {
		a.mcp = mcp.New("PI Tracker Task Manager", a.version)
	}
	return a.mcp
}

// attachDB gắn (hoặc thay) kết nối DB và dựng lại toàn bộ service trên nó.
// db == nil → giữ chế độ suy giảm, không dựng service (mọi thao tác cần DB
// sẽ không được gọi vì frontend đang ở màn báo lỗi). Dùng chung cho khởi tạo
// và RetryDB khi kết nối lại thành công.
func (a *App) attachDB(db *gorm.DB) {
	a.db = db
	if db == nil {
		return
	}
	tasks := service.NewTaskService(db)
	settings := service.NewSettingsService(db)
	workspaces := service.NewWorkspaceService(db)
	a.tasks = tasks
	a.settings = settings
	a.workspaces = workspaces
	a.metrics = service.NewMetricsService(tasks, workspaces, settings)
	a.todos = service.NewTodoService(db)
	a.activities = service.NewActivityService(db)
	a.statuses = service.NewStatusService(db)
	a.auth = service.NewAuthService(db)
	a.notifications = service.NewNotificationService(db)
	// Có notification mới → phát Postgres NOTIFY để client khác refresh chuông
	// ngay (no-op nếu không phải Postgres — driver khác dựa vào poll 10s).
	a.notifications.SetOnCreate(a.notifyNotifBroadcast)
	a.savedViews = service.NewSavedViewService(db)
	a.dependencies = service.NewDependencyService(db)
	a.tags = service.NewTagService(db)
}

// DBStatusDTO cho frontend biết trạng thái kết nối DB. Tách hai khái niệm:
//   - Configured: đã có kết nối (a.db != nil). false = thất bại lúc khởi động
//     → frontend hiện MÀN CHẶN (chỉ có nút Thử lại).
//   - Ok: ping thật thành công (kết nối còn khỏe). Configured && !Ok = kết nối
//     đã mở nhưng DB rớt lúc đang chạy → frontend hiện banner runtime, tự tắt
//     khi Ok trở lại. Nhờ tách vậy, sức khỏe DB không còn trộn vào banner lỗi chung.
type DBStatusDTO struct {
	Ok         bool   `json:"ok"`
	Configured bool   `json:"configured"`
	Error      string `json:"error"`
}

// pingDB kiểm tra kết nối còn sống không (bounded 3s). Dùng chung cho DBStatus
// và RetryDB để hai bên hiểu "khỏe" y hệt nhau.
func pingDB(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return sqlDB.PingContext(ctx)
}

// dbReady báo DB đã có kết nối (a.db != nil, các service đã được dựng). Dùng để
// chặn sớm ở lớp MCP: client ngoài có thể gọi khi app đang ở chế độ suy giảm
// (chưa/mất kết nối lúc khởi động), lúc đó service là nil và sẽ nil-deref. Chỉ
// kiểm tra con trỏ (không ping) nên rẻ, gọi được ở mỗi tool call.
func (a *App) dbReady() bool {
	a.dbMu.Lock()
	defer a.dbMu.Unlock()
	return a.db != nil
}

// DBStatus trả trạng thái kết nối DB hiện tại bằng cách PING thật, nên phản
// ánh cả trường hợp kết nối đã mở nhưng DB rớt lúc đang chạy.
func (a *App) DBStatus() DBStatusDTO {
	// Chụp nhanh con trỏ dưới khóa rồi ping NGOÀI khóa (ping có thể mất tới 3s).
	a.dbMu.Lock()
	db, dbErr := a.db, a.dbErr
	a.dbMu.Unlock()
	if db == nil {
		msg := dbErr
		if msg == "" {
			msg = "Chưa kết nối cơ sở dữ liệu"
		}
		return DBStatusDTO{Ok: false, Configured: false, Error: msg}
	}
	if err := pingDB(db); err != nil {
		return DBStatusDTO{Ok: false, Configured: true, Error: "Mất kết nối cơ sở dữ liệu: " + err.Error()}
	}
	return DBStatusDTO{Ok: true, Configured: true}
}

// RetryDB thống nhất với DBStatus về khái niệm "khỏe":
//   - Đã có kết nối (a.db != nil): PING. Khỏe → xóa cờ lỗi, xong. Chưa khỏe →
//     trả lỗi (chính lần ping này đã "đánh thức" pool database/sql để nó tự
//     dial lại kết nối chết; health-watcher ở frontend sẽ tự tắt banner khi
//     pool hồi phục). KHÔNG dựng lại *gorm.DB ở đây: watcher/handler đang dùng
//     service pointer hiện tại, thay nóng sẽ gây data race — để pool tự lành.
//   - Chưa có kết nối (thất bại lúc khởi động): kết nối lần đầu + start watcher.
//
// Chống gọi song song bằng dbConnecting: double-click "Thử lại" không dựng
// service/watcher hai lần. Bước Connect (tới connectTimeout) chạy NGOÀI khóa.
func (a *App) RetryDB() error {
	a.dbMu.Lock()
	db := a.db
	a.dbMu.Unlock()

	if db != nil {
		if err := pingDB(db); err != nil {
			return fmt.Errorf("cơ sở dữ liệu vẫn chưa phản hồi: %v", err)
		}
		a.dbMu.Lock()
		a.dbErr = ""
		a.dbMu.Unlock()
		return nil
	}

	// Giành quyền kết nối lần đầu.
	a.dbMu.Lock()
	if a.db != nil { // một lần gọi khác vừa kết nối xong
		a.dbMu.Unlock()
		return nil
	}
	if a.dbConnecting {
		a.dbMu.Unlock()
		return fmt.Errorf("đang thử kết nối lại, vui lòng đợi…")
	}
	a.dbConnecting = true
	a.dbMu.Unlock()

	cfg, err := config.Load()
	var newDB *gorm.DB
	if err == nil {
		newDB, err = database.Connect(cfg)
	}

	a.dbMu.Lock()
	a.dbConnecting = false
	if err != nil {
		if cfg != nil {
			a.dbErr = friendlyDBError(cfg, err)
		} else {
			a.dbErr = "Cấu hình không hợp lệ: " + err.Error()
		}
		msg := a.dbErr
		a.dbMu.Unlock()
		return fmt.Errorf("%s", msg)
	}
	a.attachDB(newDB) // ghi a.db + service pointers dưới khóa
	a.dbErr = ""
	a.dbMu.Unlock()

	// Bật watcher nền như lúc startup (ctx đã có sau OnStartup). Chỉ tới đây một
	// lần cho mỗi lần kết nối-lần-đầu thành công nhờ guard dbConnecting ở trên.
	if a.ctx != nil {
		go a.watchNotifications(a.ctx)
		go a.watchDueTasks(a.ctx)
		a.startDataSync(a.ctx)
	}
	return nil
}

// friendlyDBError chuyển lỗi kết nối thành thông báo tiếng Việt dễ hiểu, nêu
// đúng chỗ cần kiểm tra theo loại DB.
func friendlyDBError(cfg *config.Config, err error) string {
	switch cfg.Driver {
	case "postgres", "mysql":
		return fmt.Sprintf(
			"Không kết nối được cơ sở dữ liệu %s tại %s:%s (db: %s). Kiểm tra máy chủ DB, mạng/VPN và thông tin trong .env. Chi tiết: %v",
			cfg.Driver, cfg.Host, cfg.Port, cfg.Name, err)
	default:
		return fmt.Sprintf(
			"Không mở được cơ sở dữ liệu SQLite (%s). Kiểm tra đường dẫn và quyền ghi trong .env. Chi tiết: %v",
			cfg.SQLitePath, err)
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.dbMu.Lock()
	hasDB := a.db != nil
	a.dbMu.Unlock()
	if !hasDB {
		return // chưa kết nối DB: chờ người dùng bấm Thử lại (RetryDB)
	}
	go a.watchNotifications(ctx)
	go a.watchDueTasks(ctx)
	a.startDataSync(ctx)
}

// shutdown chạy khi app đóng: tắt MCP server nếu đang bật để nhả cổng localhost.
func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	srv := a.mcp
	a.mu.Unlock()
	if srv != nil {
		_ = srv.Stop()
	}
}

// notifPollInterval — nhịp quét thông báo mới trong DB cho sqlite/mysql. Thông
// báo do instance của user khác ghi vào DB chung, mà hai driver này không có
// kênh push nào, nên chỉ còn cách poll.
const notifPollInterval = 10 * time.Second

// notifPollIntervalPg — nhịp quét trên Postgres. Ở đây NOTIFY mới là đường
// chính (SetOnCreate → notifyNotifBroadcast → checkNewNotifications), nên poll
// chỉ còn là LƯỚI AN TOÀN, không phải cơ chế phát hiện. Vì vậy giãn ra hẳn:
// quét 10s như sqlite là đốt query định kỳ cho việc đã có push lo.
//
// Vẫn giữ chứ không bỏ hẳn, vì hai đường vẫn lọt qua NOTIFY:
//   - nhắc hạn chót đi qua CreateIfAbsent, nơi CỐ Ý không phát NOTIFY (xem
//     notification_service.go) — client khác của cùng user chỉ thấy nhờ poll;
//   - lúc connection LISTEN rớt, NOTIFY phát ra trong khoảng đó mất luôn.
const notifPollIntervalPg = 60 * time.Second

// primeNotifBaseline chụp MỐC thông báo (ID lớn nhất hiện tại của user) NGAY khi
// đăng nhập, để nhịp NOTIFY/poll đầu tiên so với mốc này và báo được thông báo
// mới — tránh nhịp đầu bị "nuốt" làm baseline (mất realtime cho invite/mention
// ngay sau đăng nhập, nhất là trên Postgres khi NOTIFY tới trước nhịp poll 10s).
// Backlog cũ vẫn không spam vì đã nằm dưới mốc. Cùng logic baseline với
// checkNewNotifications, chỉ khác là chạy chủ động sớm.
func (a *App) primeNotifBaseline() {
	if !a.dbReady() {
		return
	}
	a.mu.Lock()
	uid := a.userID
	a.mu.Unlock()
	if uid == 0 {
		return
	}
	a.notifMu.Lock()
	defer a.notifMu.Unlock()
	if uid == a.notifUserID {
		return // đã có mốc cho user này
	}
	maxID, err := a.notifications.MaxID(uid)
	if err != nil {
		return
	}
	a.notifUserID, a.notifLastID = uid, maxID
}

// notifInterval chọn nhịp poll thông báo theo driver: Postgres đã có NOTIFY nên
// poll chỉ là lưới an toàn và được giãn ra; sqlite/mysql thì poll là đường duy
// nhất nên phải nhanh.
func (a *App) notifInterval() time.Duration {
	if a.usePgNotify() {
		return notifPollIntervalPg
	}
	return notifPollInterval
}

// watchNotifications chạy nền suốt vòng đời app: phát hiện thông báo mới của
// user đang đăng nhập và đẩy ra Hệ điều hành, kể cả khi cửa sổ đang ở nền.
func (a *App) watchNotifications(ctx context.Context) {
	t := time.NewTicker(a.notifInterval())
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

// dataPollInterval — nhịp quét thay đổi dữ liệu workspace (task/checklist/bình
// luận…) do client khác ghi vào DB chung. Không có push nên phải poll.
const dataPollInterval = 5 * time.Second

// watchWorkspaceData chạy nền suốt vòng đời app: phát hiện dữ liệu workspace
// hiện tại đổi (client khác vừa sửa) và báo frontend nạp lại tại chỗ.
func (a *App) watchWorkspaceData(ctx context.Context) {
	t := time.NewTicker(dataPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.checkWorkspaceData()
		}
	}
}

// checkWorkspaceData là một nhịp poll: tính fingerprint rẻ của workspace hiện
// tại; lần đầu thấy một workspace chỉ ghi baseline (không bắn event vì view sẽ
// tự nạp khi đổi workspace), các nhịp sau nếu fingerprint đổi thì báo frontend
// refresh qua event "tasks:changed".
// primeDataBaseline chụp fingerprint workspace hiện tại làm MỐC cho poller NGAY
// khi vào/đổi workspace (cùng thời điểm frontend nạp dữ liệu). Nhờ vậy nhịp poll
// đầu tiên so với mốc này thay vì tự lấy mốc trễ 5s — không bỏ lỡ thay đổi của
// client khác trong khoảng [frontend nạp, nhịp poll đầu tiên]. Chỉ cần cho đường
// poll (sqlite/mysql); Postgres dùng NOTIFY nên bỏ qua để khỏi query thừa.
func (a *App) primeDataBaseline() {
	if a.usePgNotify() || !a.dbReady() {
		return
	}
	a.mu.Lock()
	uid, wsID := a.userID, a.wsID
	a.mu.Unlock()
	if uid == 0 || wsID == 0 {
		return
	}
	fp, err := a.workspaceFingerprint(wsID, uid)
	if err != nil {
		return
	}
	a.dataMu.Lock()
	a.dataWsID, a.dataFP = wsID, fp
	a.dataMu.Unlock()
}

func (a *App) checkWorkspaceData() {
	a.mu.Lock()
	uid, wsID := a.userID, a.wsID
	a.mu.Unlock()
	if uid == 0 || wsID == 0 || !a.dbReady() {
		return
	}
	fp, err := a.workspaceFingerprint(wsID, uid)
	if err != nil {
		return
	}
	a.dataMu.Lock()
	sameWs := wsID == a.dataWsID
	prev := a.dataFP
	a.dataWsID, a.dataFP = wsID, fp
	a.dataMu.Unlock()
	if sameWs && fp != prev && a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "tasks:changed")
	}
}

// workspaceFingerprint gộp vài đại lượng rẻ để phát hiện MỌI thay đổi dữ liệu
// mà client hiện tại cần thấy: COUNT + MAX(id) của tasks (bắt tạo/xóa) và
// MAX(activities.id) — activity là sổ ghi chung nên bắt được sửa task, toggle/
// thêm/xóa checklist, bình luận, đổi trạng thái; cộng dấu vân tay saved-view của
// (workspace, user) để bắt tạo/xóa/đổi tên/sửa bộ lọc/đổi thứ tự tab; cộng
// COUNT+MAX(id) của tag vì XÓA tag không ghi activity nào. (Sửa CHỈ phụ thuộc —
// hiếm — có thể không ghi activity, sẽ đồng bộ ở thay đổi kế tiếp.)
func (a *App) workspaceFingerprint(wsID, userID uint) (string, error) {
	n, maxID, err := a.tasks.Fingerprint(wsID)
	if err != nil {
		return "", err
	}
	actMax, err := a.activities.MaxIDForWorkspace(wsID)
	if err != nil {
		return "", err
	}
	viewsFP, err := a.savedViews.Fingerprint(wsID, userID)
	if err != nil {
		return "", err
	}
	tagN, tagMax, err := a.tags.Fingerprint(wsID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d:%s:%d:%d", n, maxID, actMax, viewsFP, tagN, tagMax), nil
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

// requireOwner như requireWorkspace nhưng thêm điều kiện phải là OWNER — dùng
// chặn ở backend cho các thao tác quản trị (settings, đổi chế độ observer).
func (a *App) requireOwner() (uint, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return 0, err
	}
	a.mu.Lock()
	role := a.wsRole
	a.mu.Unlock()
	if role != "owner" {
		return 0, fmt.Errorf("chỉ owner của workspace mới có quyền này")
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

	// Chốt mốc thông báo NGAY sau đăng nhập để nhịp NOTIFY/poll đầu tiên báo
	// được thông báo mới (không nuốt vào baseline).
	a.primeNotifBaseline()

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
	list, err := a.workspaces.ListForUser(uid)
	if err != nil {
		return nil, err
	}
	if list == nil {
		// User chưa thuộc workspace nào: trả mảng rỗng thay vì nil để JSON là
		// [] (không phải null) — frontend đọc .length khỏi nổ.
		list = []service.WorkspaceInfo{}
	}
	return list, nil
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
			a.primeDataBaseline()
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
	if err := a.workspaces.Invite(wsID, uid, username, a.notifications); err != nil {
		return err
	}
	// Notification lời mời được tạo bằng tx.Create trong service (không qua
	// NotificationService.Create) nên phải tự báo realtime ở đây, SAU khi tx
	// commit — người được mời refresh chuông ngay.
	a.notifyNotifBroadcast()
	return nil
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
	if err := a.workspaces.SetMemberLock(wsID, uid, userID, locked); err != nil {
		return err
	}
	a.notifyNotifBroadcast() // thành viên bị khóa/mở khóa nhận notif (tạo trong tx)
	return nil
}

// SetMemberObserver bật/tắt chế độ "chỉ quan sát" cho thành viên (owner-only,
// service kiểm tra lại quyền). Observer không tính vào baseline PI của team.
func (a *App) SetMemberObserver(userID uint, observer bool) error {
	uid, err := a.requireUser()
	if err != nil {
		return err
	}
	wsID, err := a.requireWorkspace()
	if err != nil {
		return err
	}
	return a.workspaces.SetMemberObserver(wsID, uid, userID, observer)
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
	if err := a.workspaces.Respond(invitationID, uid, accept); err != nil {
		return err
	}
	a.notifyNotifBroadcast() // báo lại người mời (notif tạo trong tx của service)
	return nil
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
	v, err := a.savedViews.Create(wsID, uid, name, filters)
	if err == nil {
		a.notifyChange(wsID, uid, changeView)
	}
	return v, err
}

func (a *App) UpdateSavedView(id uint, name, filters string) (models.SavedView, error) {
	owned, err := a.savedViewOwned(id)
	if err != nil {
		return models.SavedView{}, err
	}
	v, err := a.savedViews.Update(id, name, filters)
	if err == nil {
		a.notifyChange(owned.WorkspaceID, owned.UserID, changeView)
	}
	return v, err
}

func (a *App) DeleteSavedView(id uint) error {
	owned, err := a.savedViewOwned(id)
	if err != nil {
		return err
	}
	if err := a.savedViews.Delete(id); err != nil {
		return err
	}
	a.notifyChange(owned.WorkspaceID, owned.UserID, changeView)
	return nil
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
	// DependsOn: các task phải hoàn thành trước task này (finish-to-start).
	// Đổ khi ListTasks để vẽ mũi tên Gantt; nhận lại khi SaveTask để lưu.
	DependsOn []uint `json:"dependsOn"`
	// Tags: tên các tag phân loại gắn cho task. Đi bằng TÊN (không phải ID) để
	// frontend chỉ cần gửi chuỗi người dùng gõ — tên chưa có thì backend tự tạo
	// tag mới, tên đã có thì dùng lại tag cũ (TagService.EnsureByNames).
	Tags []string `json:"tags"`
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

// ListTasks trả về TOÀN BỘ task của workspace hiện tại, không lọc ngày.
//
// Chỉ dùng cho luồng cần trọn workspace (MCP list_tasks không tham số). Trang
// Tasks KHÔNG dùng hàm này — nó gọi ListTasksInMonth, vì tải cả lịch sử về client
// sẽ nặng dần theo tuổi của workspace chứ không theo lượng việc đang xem.
func (a *App) ListTasks() ([]TaskDTO, error) {
	return a.listTasks(service.TaskDateFilter{})
}

// monthBounds đổi "YYYY-MM" thành ngày đầu và ngày CUỐI tháng (cả hai đều tính
// vào kỳ). Dùng chung cho binding của trang Tasks và tham số month của MCP để hai
// đường không lệch nhau về định nghĩa "một tháng".
func monthBounds(month string) (from, to time.Time, err error) {
	start, err := time.ParseInLocation("2006-01", strings.TrimSpace(month), time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("tháng %q không hợp lệ (định dạng YYYY-MM)", month)
	}
	return start, start.AddDate(0, 1, -1), nil
}

// ListTasksInMonth trả về task cần cho khung tháng của trang Tasks: task có
// khoảng sống (start → done) giao với tháng, cộng task chưa có ngày bắt đầu.
//
// Đây là TẬP BAO của bộ lọc `rows` trong GanttView, cố ý rộng hơn một chút (xem
// TaskDateFilter.overlapWhere): nhát cắt chính xác — gồm cả phần suy đoán
// barEnd = start + estimateAiDays — vẫn do client giữ, nên giao diện không đổi.
//
// month dạng "YYYY-MM".
func (a *App) ListTasksInMonth(month string) ([]TaskDTO, error) {
	from, to, err := monthBounds(month)
	if err != nil {
		return nil, err
	}
	return a.listTasks(service.TaskDateFilter{
		Field: service.TaskDateOverlap, From: &from, To: &to,
	})
}

// ListTaskRefs trả về id + tiêu đề của MỌI task trong workspace — nguồn cho các
// combobox chọn task trong TaskModal (phụ thuộc, task gốc sinh bug).
//
// Danh sách này phải trọn workspace, không lọc tháng: một task tháng 7 vẫn được
// phép phụ thuộc vào task tháng 3, và tiêu đề của task đã chọn phải hiện ra được
// dù nó nằm ngoài tháng đang xem. Bù lại nó chỉ SELECT hai cột, nên nhẹ hơn
// ListTasks nhiều lần (description mới là phần chiếm chỗ).
func (a *App) ListTaskRefs() ([]service.TaskRef, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return nil, err
	}
	return a.tasks.ListRefs(wsID)
}

// GetTask trả về một task của workspace hiện tại theo id, kèm checklist/phụ
// thuộc/tag như trong danh sách.
//
// Cần cho việc mở task từ thông báo: từ khi trang Tasks chỉ nạp theo tháng, task
// được nhắc có thể không nằm trong tháng đang xem — không có hàm này thì UI sẽ
// báo "không tìm thấy task" cho một task vẫn còn tồn tại.
func (a *App) GetTask(taskID uint) (TaskDTO, error) {
	t, _, err := a.taskInWorkspace(taskID)
	if err != nil {
		return TaskDTO{}, err
	}
	dto := taskToDTO(t)
	if deps, err := a.dependencies.PredecessorsOf(t.ID); err == nil {
		dto.DependsOn = deps
	}
	// Phải đổ tag vào đây: update_task lấy DTO này làm nền rồi merge, nên thiếu
	// tag ở nền là update một phần sẽ xóa trắng tag của task.
	if names, err := a.tags.NamesOf(t.ID); err == nil {
		dto.Tags = names
	}
	if counts, err := a.todos.CountsForTasks([]uint{t.ID}); err == nil {
		if c, ok := counts[t.ID]; ok {
			dto.TodoTotal, dto.TodoDone = c[0], c[1]
		}
	}
	return dto, nil
}

// listTasks là thân dùng chung của ListTasks và nhánh lọc theo kỳ của MCP.
// Khoảng ngày đi xuống SQL (TaskService.ListFiltered), rồi ba query đi kèm
// (checklist, phụ thuộc, tag) chỉ hỏi đúng các task vừa lấy được — nếu chúng vẫn
// gộp theo cả workspace thì việc lọc task ở DB thành vô nghĩa về chi phí.
func (a *App) listTasks(filter service.TaskDateFilter) ([]TaskDTO, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return nil, err
	}
	tasks, err := a.tasks.ListFiltered(wsID, filter)
	if err != nil {
		return nil, err
	}
	ids := make([]uint, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	counts, err := a.todos.CountsForTasks(ids)
	if err != nil {
		return nil, err
	}
	deps, err := a.dependencies.DependsOnMapForTasks(wsID, ids)
	if err != nil {
		return nil, err
	}
	tagNames, err := a.tags.NamesByTaskIDs(wsID, ids)
	if err != nil {
		return nil, err
	}
	dtos := make([]TaskDTO, len(tasks))
	for i, t := range tasks {
		dtos[i] = taskToDTO(t)
		if c, ok := counts[t.ID]; ok {
			dtos[i].TodoTotal, dtos[i].TodoDone = c[0], c[1]
		}
		dtos[i].DependsOn = deps[t.ID]
		dtos[i].Tags = tagNames[t.ID]
	}
	return dtos, nil
}

// ListTags trả về toàn bộ tag của workspace hiện tại — danh sách để combobox
// "chọn lại tag cũ đã tạo" đổ vào.
func (a *App) ListTags() ([]models.Tag, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return nil, err
	}
	return a.tags.List(wsID)
}

// TagResultDTO là kết quả tạo tag. Created cho biết tag vừa được tạo thật, hay
// tên đã tồn tại nên trả về tag cũ — caller cần phân biệt để không tưởng mình
// vừa tạo một tag mới trong khi thực ra đang dùng lại tag sẵn có.
type TagResultDTO struct {
	Tag     models.Tag `json:"tag"`
	Created bool       `json:"created"`
}

// CreateTag tạo tag mới cho workspace hiện tại mà không cần gắn vào task nào.
// Idempotent: tên đã có (không phân biệt chữ hoa/thường) thì trả về tag cũ với
// created=false thay vì báo lỗi.
func (a *App) CreateTag(name string) (TagResultDTO, error) {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return TagResultDTO{}, err
	}
	tag, created, err := a.tags.Create(wsID, name)
	if err != nil {
		return TagResultDTO{}, err
	}
	if created {
		a.notifyChange(wsID, 0, changeData)
	}
	return TagResultDTO{Tag: tag, Created: created}, nil
}

// DeleteTag xóa một tag khỏi workspace và bỏ nó khỏi mọi task đang gắn — dùng
// khi tag bị tạo nhầm (gõ sai chính tả) và cần dọn khỏi từ vựng workspace.
func (a *App) DeleteTag(tagID uint) error {
	wsID, err := a.requireWorkspace()
	if err != nil {
		return err
	}
	if err := a.tags.Delete(wsID, tagID); err != nil {
		return err
	}
	a.notifyChange(wsID, 0, changeData)
	return nil
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

	// Phụ thuộc (finish-to-start): các task phải xong trước task này. Khử 0/
	// trùng/chính-nó, xác nhận tồn tại trong workspace, và chặn vòng lặp NGAY
	// để không lưu task rồi mới báo lỗi. Task mới (ID==0) chưa tồn tại nên
	// không thể nằm trong vòng — bỏ qua bước cycle, chỉ kiểm tra tồn tại.
	depIDs := make([]uint, 0, len(dto.DependsOn))
	seenDep := make(map[uint]bool)
	for _, id := range dto.DependsOn {
		if id == 0 || id == dto.ID || seenDep[id] {
			continue
		}
		seenDep[id] = true
		dep, err := a.tasks.Get(id)
		if err != nil || dep.WorkspaceID != wsID {
			return fmt.Errorf("không tìm thấy task phụ thuộc id %d trong workspace hiện tại", id)
		}
		depIDs = append(depIDs, id)
	}
	if dto.ID != 0 {
		cyc, err := a.dependencies.WouldCycle(wsID, dto.ID, depIDs)
		if err != nil {
			return err
		}
		if cyc {
			return fmt.Errorf("phụ thuộc tạo thành vòng lặp — task không thể (gián tiếp) chờ chính nó")
		}
	}

	// Tag phân loại: đổi tên → ID (tên mới thì tạo tag mới, tên cũ dùng lại).
	// Làm TRƯỚC khi lưu task để tên tag sai (quá dài) báo lỗi ngay thay vì lưu
	// task xong mới báo.
	tagIDs, err := a.tags.EnsureByNames(wsID, dto.Tags)
	if err != nil {
		return err
	}

	// Lấy bản cũ trước khi lưu để ghi lịch sử thay đổi (và xác nhận quyền).
	var old *models.Task
	var oldTags []string
	if t.ID != 0 {
		prev, _, err := a.taskInWorkspace(t.ID)
		if err != nil {
			return err
		}
		old = &prev
		oldTags, _ = a.tags.NamesOf(t.ID)
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

	// Lưu phụ thuộc sau khi có t.ID (task mới): đọc bản cũ để ghi lịch sử, rồi
	// thay bằng danh sách mới. Cycle đã kiểm tra ở trên; SetForTask kiểm lại
	// (phòng thủ) và ghi trong transaction.
	oldDeps, _ := a.dependencies.PredecessorsOf(t.ID)
	if err := a.dependencies.SetForTask(wsID, t.ID, depIDs); err != nil {
		return err
	}

	// Tag lưu sau khi có t.ID (task mới) — cùng lý do như phụ thuộc.
	if err := a.tags.SetForTask(wsID, t.ID, tagIDs); err != nil {
		return err
	}

	names, _ := a.workspaces.MemberNames(wsID)
	if old == nil {
		_ = a.activities.Log(wsID, t.ID, a.actorName(), "create", "tạo task")
		_ = a.statuses.Log(t.ID, "", t.Status, a.actorName())
		// Checklist tạo sẵn (vd AI gợi ý) chỉ áp khi tạo mới — bỏ mục rỗng.
		for _, title := range dto.InitialTodos {
			if strings.TrimSpace(title) == "" {
				continue
			}
			if item, err := a.todos.Add(t.ID, title); err == nil {
				_ = a.activities.Log(wsID, t.ID, a.actorName(), "todo", "thêm việc: "+item.Title)
			}
		}
	} else {
		if changes := taskChanges(*old, t, names); len(changes) > 0 {
			_ = a.activities.Log(wsID, t.ID, a.actorName(), "update", strings.Join(changes, "\n"))
		}
		if old.Status != t.Status {
			_ = a.statuses.Log(t.ID, old.Status, t.Status, a.actorName())
		}
	}
	if autoBlockedNote != "" {
		_ = a.activities.Log(wsID, t.ID, a.actorName(), "update", autoBlockedNote)
	}
	if note := depChangeNote(oldDeps, depIDs); note != "" {
		_ = a.activities.Log(wsID, t.ID, a.actorName(), "update", note)
	}
	// Tag chỉ diff được ở đây (không nằm trên models.Task nên taskChanges không
	// thấy) — đọc lại tên sau khi lưu để lấy đúng tên tag vừa tạo.
	if old != nil {
		if newTags, err := a.tags.NamesOf(t.ID); err == nil {
			if note := tagChangeNote(oldTags, newTags); note != "" {
				_ = a.activities.Log(wsID, t.ID, a.actorName(), "update", note)
			}
		}
	}
	// Đặt/đổi hạn chót có hiệu lực ngay: quét nhắc việc luôn thay vì bắt
	// người dùng đợi nhịp quét theo giờ (task đã nhắc rồi không nhắc lại).
	if t.DueDate != nil && t.Status != models.StatusDone {
		a.checkDueTasks()
	}
	a.notifyChange(wsID, 0, changeData)
	return nil
}

func (a *App) DeleteTask(id uint) error {
	_, wsID, err := a.taskInWorkspace(id)
	if err != nil {
		return err
	}
	if err := a.tasks.Delete(id); err != nil {
		return err
	}
	_ = a.todos.DeleteForTask(id)
	_ = a.activities.DeleteForTask(id)
	_ = a.statuses.DeleteForTask(id)
	_ = a.dependencies.DeleteForTask(id)
	_ = a.tags.DeleteForTask(id)
	a.notifyChange(wsID, 0, changeData)
	return nil
}

// depChangeNote trả về dòng lịch sử "Phụ thuộc: cũ → mới" khi danh sách
// predecessor thay đổi, hoặc "" nếu không đổi. So sánh theo tập (không phụ
// thuộc thứ tự) vì cả hai đầu vào đều đã sắp xếp/khử trùng.
func depChangeNote(old, new []uint) string {
	refs := func(ids []uint) string {
		if len(ids) == 0 {
			return "—"
		}
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = fmt.Sprintf("#%d", id)
		}
		return strings.Join(parts, ", ")
	}
	if len(old) == len(new) {
		set := make(map[uint]bool, len(old))
		for _, id := range old {
			set[id] = true
		}
		same := true
		for _, id := range new {
			if !set[id] {
				same = false
				break
			}
		}
		if same {
			return ""
		}
	}
	return fmt.Sprintf("Phụ thuộc: %s → %s", refs(old), refs(new))
}

// tagChangeNote trả về dòng lịch sử "Phân loại tag: cũ → mới" khi tập tag thay
// đổi, hoặc "" nếu không đổi. So sánh không phân biệt thứ tự và chữ hoa/thường
// vì TagService coi "Hạ tầng" và "hạ tầng" là cùng một tag.
func tagChangeNote(old, new []string) string {
	list := func(names []string) string {
		if len(names) == 0 {
			return "—"
		}
		return strings.Join(service.SortedNames(names), ", ")
	}
	norm := func(names []string) map[string]bool {
		set := make(map[string]bool, len(names))
		for _, n := range names {
			set[strings.ToLower(n)] = true
		}
		return set
	}
	o, n := norm(old), norm(new)
	if len(o) == len(n) {
		same := true
		for k := range n {
			if !o[k] {
				same = false
				break
			}
		}
		if same {
			return ""
		}
	}
	return fmt.Sprintf("Phân loại tag: %s → %s", list(old), list(new))
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
	_, wsID, err := a.taskInWorkspace(taskID)
	if err != nil {
		return err
	}
	item, err := a.todos.Add(taskID, title)
	if err != nil {
		return err
	}
	err = a.activities.Log(wsID, taskID, a.actorName(), "todo", "thêm việc: "+item.Title)
	a.notifyChange(wsID, 0, changeData)
	return err
}

func (a *App) ToggleTodo(id uint, done bool) error {
	// Kiểm quyền theo TASK chứa mục này: mục checklist chỉ được truy cập bằng
	// todoId nên phải phân giải task rồi xác nhận task thuộc workspace hiện tại
	// (chặn client MCP sửa mục ở workspace khác bằng cách đoán/tái dùng id).
	cur, err := a.todos.Get(id)
	if err != nil {
		return fmt.Errorf("không tìm thấy mục checklist id %d", id)
	}
	_, wsID, err := a.taskInWorkspace(cur.TaskID)
	if err != nil {
		return err
	}
	item, err := a.todos.SetDone(id, done)
	if err != nil {
		return err
	}
	msg := "hoàn thành việc: " + item.Title
	if !done {
		msg = "bỏ hoàn thành việc: " + item.Title
	}
	err = a.activities.Log(wsID, item.TaskID, a.actorName(), "todo", msg)
	a.notifyChange(wsID, 0, changeData)
	return err
}

func (a *App) DeleteTodo(id uint) error {
	// Cùng lý do như ToggleTodo: kiểm task của mục thuộc workspace hiện tại
	// trước khi xóa, vì đường vào chỉ có todoId.
	cur, err := a.todos.Get(id)
	if err != nil {
		return fmt.Errorf("không tìm thấy mục checklist id %d", id)
	}
	_, wsID, err := a.taskInWorkspace(cur.TaskID)
	if err != nil {
		return err
	}
	item, err := a.todos.Delete(id)
	if err != nil {
		return err
	}
	err = a.activities.Log(wsID, item.TaskID, a.actorName(), "todo", "xóa việc: "+item.Title)
	a.notifyChange(wsID, 0, changeData)
	return err
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
		act, err = a.activities.LogReply(wsID, taskID, actor, content, parentID)
	} else {
		act, err = a.activities.LogComment(wsID, taskID, actor, content)
	}
	if err != nil {
		return err
	}
	a.notifyComment(t, wsID, act.ID, content, replyToName)
	a.notifyChange(wsID, 0, changeData)
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
	// Cài đặt workspace chỉ owner được sửa (member không truy cập trang này).
	wsID, err := a.requireOwner()
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

// reportScope gom những thứ KHÔNG phụ thuộc người phụ trách trong một lần xuất
// file, nạp đúng một lần rồi dùng lại cho cả bản team lẫn từng sheet cá nhân.
// Trước đây mỗi thứ ở đây là một query lặp theo từng thành viên: một team 20
// người phải trả giá vài chục lượt truy vấn trùng ngay trong đường xuất file.
type reportScope struct {
	members  []service.Member // đã sắp theo username
	names    map[uint]string
	taskTags map[uint][]string

	// tasks: task Done trong tháng ở phạm vi rộng nhất mà lần xuất này cần, và
	// scopeID là phạm vi đó (0 = cả team). originBugs tính trên chính tasks.
	// Task của một thành viên là TẬP CON của tasks và mọi key origin của người đó
	// nằm trong originBugs, nên bản cá nhân lọc trong bộ nhớ chứ không query lại.
	scopeID    uint
	tasks      []models.Task
	originBugs map[uint][]uint
}

// tasksOf trả về task Done của một người, lọc từ danh sách đã nạp. Điều kiện lọc
// khớp đúng với "assignee_id = ?" của TaskService.DoneBetween.
func (sc reportScope) tasksOf(assigneeID uint) []models.Task {
	if assigneeID == sc.scopeID {
		return sc.tasks
	}
	out := make([]models.Task, 0, len(sc.tasks))
	for _, t := range sc.tasks {
		if t.AssigneeID != nil && *t.AssigneeID == assigneeID {
			out = append(out, t)
		}
	}
	return out
}

// originBugsOf thu hẹp map bug-theo-task-gốc về đúng các task được truyền vào.
func (sc reportScope) originBugsOf(tasks []models.Task) map[uint][]uint {
	out := make(map[uint][]uint, len(tasks))
	for _, t := range tasks {
		if ids := sc.originBugs[t.ID]; len(ids) > 0 {
			out[t.ID] = ids
		}
	}
	return out
}

// loadReportScope nạp phần dùng chung cho một lần xuất báo cáo.
//
// assigneeID quyết định phạm vi task nạp về: bản toàn team (0) cần task của mọi
// người để tách ra từng sheet cá nhân, bản một người chỉ cần task của người đó.
func (a *App) loadReportScope(wsID uint, month, now time.Time, assigneeID uint) (reportScope, error) {
	members, err := a.workspaces.Members(wsID)
	if err != nil {
		return reportScope{}, err
	}
	names := make(map[uint]string, len(members))
	for _, mb := range members {
		names[mb.ID] = mb.Name
	}
	tasks, err := a.metrics.DoneTasksAsOf(wsID, month, now, assigneeID)
	if err != nil {
		return reportScope{}, err
	}
	// Phân tích nguồn gốc: task Done trong tháng đã sinh ra bao nhiêu bug
	// (cột "Bug phát sinh" trong phụ lục) — cùng nguồn số liệu với Metrics.
	originBugs, err := a.metrics.BugsByOrigin(wsID, tasks)
	if err != nil {
		return reportScope{}, err
	}
	// Tag phân loại cho cột "Tag" của phụ lục — nhiều-nhiều nên không nằm trên
	// models.Task, phải nạp riêng theo workspace.
	taskTags, err := a.tags.NamesByTask(wsID)
	if err != nil {
		return reportScope{}, err
	}
	return reportScope{
		members: members, names: names, taskTags: taskTags,
		scopeID: assigneeID, tasks: tasks, originBugs: originBugs,
	}, nil
}

// buildReportData dựng report.Data cho một phạm vi (assigneeID = 0 là toàn team).
//
// MỘT chỗ duy nhất biết report.Data gồm những gì: bản team và các sheet cá nhân
// nằm trong CÙNG một file, nên nếu dựng ở hai nơi thì thêm một field mới mà chỉ
// sửa một nơi sẽ làm hai bên lệch nhau mà không có gì báo.
//
// Chỉ số vẫn tính riêng từng người qua MetricsService.Compute (baseline 1 người,
// WIP riêng), KHÔNG chia nhỏ số của team: T/CT/PI không cộng trừ tuyến tính được.
func (a *App) buildReportData(wsID uint, month, now time.Time, assigneeID uint, assigneeName string, sc reportScope) (report.Data, error) {
	metrics, st, err := a.metrics.Compute(wsID, month, now, assigneeID)
	if err != nil {
		return report.Data{}, err
	}
	tasks := sc.tasksOf(assigneeID)
	return report.Data{
		Month: month, AsOf: now, AssigneeName: assigneeName,
		Metrics: metrics, Advice: a.metrics.Advise(metrics, st),
		Settings: st, Tasks: tasks, People: sc.names,
		OriginBugs: sc.originBugsOf(tasks), TaskTags: sc.taskTags,
	}, nil
}

// memberReports dựng báo cáo riêng của TỪNG thành viên cho bản toàn team —
// .xlsx cho mỗi người một sheet, .pdf cho mỗi người một trang.
//
// Lấy mọi thành viên LÀM VIỆC, kể cả người tháng này chưa Done task nào: thiếu
// tên trong file dễ bị đọc thành bỏ sót người, còn sheet PI 0.00 thì tự nó đã
// là thông tin.
//
// TRỪ observer (người quan sát/quản lý): họ không nhận task nên báo cáo cá nhân
// chỉ toàn số 0, mà chấm PI người không làm task là sai. Cùng quy ước với
// TeamSize trong MetricsService.Compute — observer không kể vào baseline team.
func (a *App) memberReports(wsID uint, month, now time.Time, sc reportScope) ([]report.Data, error) {
	out := make([]report.Data, 0, len(sc.members))
	for _, mem := range sc.members {
		if mem.Observer {
			continue
		}
		d, err := a.buildReportData(wsID, month, now, mem.ID, mem.Name, sc)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
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
	sc, err := a.loadReportScope(wsID, m, now, assigneeID)
	if err != nil {
		return "", err
	}
	assigneeName := ""
	if assigneeID != 0 {
		assigneeName = sc.names[assigneeID]
		if assigneeName == "" {
			return "", fmt.Errorf("không tìm thấy thành viên id %d", assigneeID)
		}
	}
	data, err := a.buildReportData(wsID, m, now, assigneeID, assigneeName, sc)
	if err != nil {
		return "", err
	}

	// Bản toàn team kèm báo cáo riêng của TỪNG thành viên.
	if assigneeID == 0 {
		data.Members, err = a.memberReports(wsID, m, now, sc)
		if err != nil {
			return "", err
		}
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

// RevealInFileManager mở trình quản lý tệp của hệ điều hành tại thư mục chứa
// path và CHỌN SẴN file đó — dùng cho nút "Mở thư mục" sau khi xuất báo cáo.
//
// Mỗi hệ điều hành một cách chọn file; nếu không chọn được thì vẫn phải mở được
// thư mục, vì mục đích của nút là đưa người dùng tới chỗ file nằm:
//   - macOS:   open -R <file>
//   - Windows: explorer /select,<file> — explorer trả exit code 1 cả khi thành
//     công, nên bỏ qua lỗi thoát ở đây (xem bên dưới).
//   - Linux:   DBus FileManager1.ShowItems (Nautilus/Dolphin/Nemo… đều hỗ trợ),
//     hỏng thì rơi về xdg-open mở thư mục mà không chọn file.
//
// Tham số là đường dẫn file, không phải lệnh: mọi lời gọi đều đi qua exec.Command
// với đối số tách rời, không qua shell, nên tên file có dấu cách hay ký tự lạ
// không thể biến thành lệnh khác.
func (a *App) RevealInFileManager(path string) error {
	if path == "" {
		return fmt.Errorf("chưa có đường dẫn file")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// Kiểm tra trước để báo đúng nguyên nhân: file bị xóa/di chuyển sau khi xuất
	// thì mở thư mục cũng vô nghĩa.
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("không còn thấy file %s: %w", abs, err)
	}

	runErr := revealCommand(goruntime.GOOS, abs).Run()
	switch goruntime.GOOS {
	case "windows":
		// explorer.exe hầu như luôn trả exit code 1 kể cả khi mở thành công, nên
		// báo lỗi theo MÃ THOÁT ở đây là báo nhầm — bỏ qua đúng loại lỗi đó thôi.
		// Lỗi trước khi process kịp chạy (không tìm thấy explorer, bị policy
		// chặn) là lỗi thật: nuốt luôn thì người dùng bấm nút, không có gì mở ra
		// mà cũng không có thông báo nào để lần theo.
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return nil
		}
		return runErr
	case "darwin":
		return runErr
	default:
		if runErr == nil {
			return nil
		}
		// Không có DBus hoặc file manager không đăng ký giao diện đó: chấp nhận
		// mở thư mục mà không chọn sẵn file, còn hơn không mở được gì.
		return exec.Command("xdg-open", filepath.Dir(abs)).Run()
	}
}

// revealCommand dựng lệnh mở file manager và chọn sẵn file abs (đường dẫn tuyệt
// đối). Tách riêng khỏi RevealInFileManager để test được cả ba hệ điều hành mà
// không cần chạy trên hệ đó.
func revealCommand(goos, abs string) *exec.Cmd {
	switch goos {
	case "darwin":
		return exec.Command("open", "-R", abs)
	case "windows":
		// Đúng dạng "/select,<path>": explorer KHÔNG nhận đường dẫn ở đối số riêng.
		return exec.Command("explorer", "/select,"+abs)
	default:
		// URI phải được escape đúng chuẩn, KHÔNG nối chuỗi: tên file người dùng
		// tự đặt ở hộp thoại Lưu có thể chứa '#' (bên nhận cắt thành fragment,
		// chọn sai file) hoặc '%' (thành escape sequence hỏng). url.URL lo phần
		// này — "a b#c" → "a%20b%23c".
		uri := (&url.URL{Scheme: "file", Path: abs}).String()
		// --print-reply là BẮT BUỘC để biết lệnh có tới đích hay không: thiếu nó,
		// dbus-send chỉ gửi rồi thoát 0 ngay cả khi không có service nào đăng ký
		// FileManager1 (máy chạy i3/sway, WM tối giản…) — runErr = nil, nhánh
		// xdg-open bên dưới không bao giờ chạy, nút bấm im lặng không làm gì.
		// --reply-timeout chặn trần chờ: ca hỏng phổ biến nhất (ServiceUnknown)
		// trả lời ngay, 10s chỉ để chừa chỗ cho file manager khởi động nguội,
		// thay vì đứng chờ 25s mặc định của dbus-send.
		return exec.Command("dbus-send", "--session", "--print-reply", "--reply-timeout=10000",
			"--dest=org.freedesktop.FileManager1",
			"--type=method_call", "/org/freedesktop/FileManager1",
			"org.freedesktop.FileManager1.ShowItems",
			"array:string:"+uri, "string:")
	}
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
		if p.Observer {
			continue // người quan sát/quản lý không hiện ở bảng so sánh chỉ số
		}
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
