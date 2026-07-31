package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"gorm.io/driver/postgres"
)

// ---- Đồng bộ realtime bằng Postgres LISTEN/NOTIFY ----
//
// Khi DB là Postgres, mọi client cùng LISTEN một kênh; mỗi mutation phát NOTIFY
// kèm payload (workspace + user + loại). Client nhận, lọc theo phiên hiện tại
// rồi bắn Wails event "tasks:changed" — thay cho polling định kỳ (poll chỉ còn
// dùng cho sqlite/mysql vốn không có NOTIFY). Nhờ vậy cập nhật gần như tức thời
// và không đốt query khi ngồi yên.

// pgNotifyChannel — tên kênh LISTEN/NOTIFY (identifier hợp lệ, hằng số nên an
// toàn khi nối vào câu LISTEN).
const pgNotifyChannel = "pi_tracker_change"

// pgReconnectDelay — chờ trước khi mở lại listener sau khi mất kết nối.
const pgReconnectDelay = 3 * time.Second

// changePayload là nội dung NOTIFY: workspace bị ảnh hưởng, user (chỉ có nghĩa
// với thay đổi saved-view vốn riêng theo user), và loại ("data" | "view").
type changePayload struct {
	Ws   uint   `json:"ws"`
	User uint   `json:"user"`
	Kind string `json:"kind"`
}

const (
	changeData  = "data"  // task/checklist/bình luận/trạng thái — cả workspace thấy
	changeView  = "view"  // saved-view (bộ lọc/sắp xếp/tab) — riêng theo user
	changeNotif = "notif" // có notification mới (mời/nhắc/trả lời/nhắc hạn)
)

// usePgNotify báo DB hiện tại có phải Postgres không (chỉ Postgres có
// LISTEN/NOTIFY). Đọc con trỏ db dưới khóa vì RetryDB có thể thay.
func (a *App) usePgNotify() bool {
	a.dbMu.Lock()
	db := a.db
	a.dbMu.Unlock()
	return db != nil && db.Dialector.Name() == "postgres"
}

// pgDSN lấy DSN từ dialector Postgres của GORM để mở connection listener riêng.
func (a *App) pgDSN() (string, bool) {
	a.dbMu.Lock()
	db := a.db
	a.dbMu.Unlock()
	if db == nil {
		return "", false
	}
	pg, ok := db.Dialector.(*postgres.Dialector)
	if !ok || pg.Config == nil {
		return "", false
	}
	return pg.Config.DSN, true
}

// notifyChange phát NOTIFY báo workspace vừa có thay đổi. No-op nếu không phải
// Postgres (driver khác dựa vào poller). Lỗi khi phát KHÔNG chặn mutation (đã
// commit xong) — chỉ làm mất một lần cập nhật realtime, không sai dữ liệu.
func (a *App) notifyChange(wsID, userID uint, kind string) {
	a.dbMu.Lock()
	db := a.db
	a.dbMu.Unlock()
	if db == nil || db.Dialector.Name() != "postgres" {
		return
	}
	payload, err := json.Marshal(changePayload{Ws: wsID, User: userID, Kind: kind})
	if err != nil {
		return
	}
	_ = db.Exec("SELECT pg_notify(?, ?)", pgNotifyChannel, string(payload)).Error
}

// notifyNotifBroadcast báo có notification mới để các client refresh chuông ngay.
// Broadcast (không kèm user/workspace) — mỗi client tự lọc bằng checkNewNotifications
// theo user của mình. No-op nếu không phải Postgres (driver khác dựa vào poll 10s).
func (a *App) notifyNotifBroadcast() {
	a.dbMu.Lock()
	db := a.db
	a.dbMu.Unlock()
	if db == nil || db.Dialector.Name() != "postgres" {
		return
	}
	payload, err := json.Marshal(changePayload{Kind: changeNotif})
	if err != nil {
		return
	}
	_ = db.Exec("SELECT pg_notify(?, ?)", pgNotifyChannel, string(payload)).Error
}

// watchPgNotify giữ một connection pgx riêng để LISTEN kênh thay đổi và bắn
// event cho frontend. Tự mở lại khi mất kết nối. Chạy nền suốt vòng đời app;
// chỉ khởi động khi DB là Postgres.
func (a *App) watchPgNotify(ctx context.Context) {
	dsn, ok := a.pgDSN()
	if !ok {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		// listenOnce chặn tới khi lỗi/hủy; lỗi kết nối thì chờ rồi mở lại.
		a.listenOnce(ctx, dsn)
		select {
		case <-ctx.Done():
			return
		case <-time.After(pgReconnectDelay):
		}
	}
}

// listenOnce mở connection, LISTEN rồi vòng lặp nhận notification tới khi lỗi.
// Ngay sau khi (re)subscribe thành công, bù cả HAI thứ có thể đã lỡ trong lúc
// mất kết nối listener: thay đổi dữ liệu (event tasks:changed) và thông báo mới
// (checkNewNotifications). NOTIFY không có hàng đợi — phát ra lúc không ai
// LISTEN là mất hẳn — nên nếu không bù ở đây thì thông báo trong khoảng đứt kết
// nối phải chờ nhịp poll kế tiếp mới tới.
func (a *App) listenOnce(ctx context.Context, dsn string) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, "LISTEN "+pgNotifyChannel); err != nil {
		return
	}
	a.emitTasksChanged()      // bù thay đổi dữ liệu lỡ mất khi vừa (re)kết nối
	a.checkNewNotifications() // bù thông báo lỡ mất trong lúc không ai LISTEN

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return // ctx hủy hoặc kết nối rớt → caller mở lại
		}
		a.handleChangeNotification(n.Payload)
	}
}

// handleChangeNotification lọc payload theo phiên hiện tại rồi báo frontend:
// chỉ refresh khi đúng workspace đang mở; với thay đổi saved-view còn phải đúng
// user (view riêng theo user).
func (a *App) handleChangeNotification(payload string) {
	var p changePayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return
	}
	// Notification: broadcast không kèm workspace — mỗi client tự kiểm theo user
	// của mình (checkNewNotifications lo OS notify + event "notifications:new",
	// dedup theo notifLastID nên không trùng với poll 10s).
	if p.Kind == changeNotif {
		a.checkNewNotifications()
		return
	}
	a.mu.Lock()
	uid, wsID := a.userID, a.wsID
	a.mu.Unlock()
	if p.Ws == 0 || p.Ws != wsID {
		return
	}
	if p.Kind == changeView && p.User != uid {
		return
	}
	a.emitTasksChanged()
}

// emitTasksChanged bắn event để view đang mở nạp lại tại chỗ (dùng chung một
// event với đường poll — frontend không phân biệt nguồn).
func (a *App) emitTasksChanged() {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "tasks:changed")
	}
}

// startDataSync chọn cơ chế đồng bộ realtime theo driver: Postgres dùng
// LISTEN/NOTIFY (tức thời, không đốt query); sqlite/mysql dùng poll fingerprint.
func (a *App) startDataSync(ctx context.Context) {
	if a.usePgNotify() {
		go a.watchPgNotify(ctx)
	} else {
		go a.watchWorkspaceData(ctx)
	}
}
