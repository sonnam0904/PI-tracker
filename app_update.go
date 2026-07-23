package main

import (
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"taskmanager/internal/updater"
)

// AppVersion trả về phiên bản đang chạy (nhúng lúc build, mặc định "dev").
func (a *App) AppVersion() string {
	return a.version
}

// CheckUpdate so phiên bản hiện tại với release mới nhất trên GitHub. Trả về
// trạng thái để frontend quyết định hiện nút cập nhật. Bản "dev" luôn báo
// không có bản mới (không làm phiền lúc phát triển).
func (a *App) CheckUpdate() (updater.Status, error) {
	return updater.CheckUpdate(a.ctx, a.version)
}

// ApplyUpdate tải bản mới nhất khớp HĐH, thay file thực thi rồi khởi động lại
// ứng dụng. Nếu thành công, tiến trình hiện tại sẽ thoát và bản mới mở lên —
// lời gọi từ frontend có thể không kịp nhận kết quả (đó là điều bình thường).
func (a *App) ApplyUpdate() error {
	exe, err := updater.DownloadAndApply(a.ctx)
	if err != nil {
		return err
	}
	if err := updater.Restart(exe); err != nil {
		return err
	}
	// Nhường một nhịp cho tiến trình mới khởi động rồi mới thoát bản cũ.
	go func() {
		time.Sleep(300 * time.Millisecond)
		wruntime.Quit(a.ctx)
	}()
	return nil
}
