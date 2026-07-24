package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

	"taskmanager/internal/config"
	"taskmanager/internal/database"
)

//go:embed all:frontend/dist
var assets embed.FS

// appIcon là icon cửa sổ (dùng cho Linux — Windows nhúng icon.ico, macOS bundle
// .app tự lấy từ appicon.png). Nhúng để cửa sổ/taskbar có icon ngay cả khi chạy
// binary trực tiếp, không chỉ khi cài qua .deb/.rpm.
//
//go:embed build/appicon.png
var appIcon []byte

// envExample là nội dung .env.example nhúng sẵn, dùng để seed vào thư mục dữ
// liệu (~/.pi-tracker) khi cài đặt — người dùng copy sang .env để cấu hình.
//
//go:embed .env.example
var envExample string

// Version là phiên bản app, nhúng lúc build qua ldflags:
//
//	wails build -ldflags "-X main.Version=1.2.3"
//
// Bản dev để "dev" — updater sẽ bỏ qua việc kiểm tra khi ở giá trị này.
var Version = "dev"

// setupDataDir chuyển thư mục làm việc về ~/.pi-tracker cho bản CÀI ĐẶT (release)
// để .env và database SQLite (đều là đường dẫn tương đối) nằm cố định một chỗ,
// không phụ thuộc CWD lúc mở app từ menu. Seed sẵn .env.example vào đó.
//
// Bản dev (Version=="dev", chạy `wails dev` / build/bin) GIỮ NGUYÊN CWD để tiện
// phát triển với .env và taskmanager.db trong repo.
func setupDataDir() {
	if Version == "dev" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		log.Printf("không xác định được thư mục home, giữ CWD hiện tại: %v", err)
		return
	}
	dir := filepath.Join(home, ".pi-tracker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("không tạo được thư mục dữ liệu %s: %v", dir, err)
		return
	}
	// Chỉ tạo .env.example nếu chưa có — không đè bản người dùng có thể đã sửa.
	exPath := filepath.Join(dir, ".env.example")
	if _, err := os.Stat(exPath); os.IsNotExist(err) {
		if err := os.WriteFile(exPath, []byte(envExample), 0o644); err != nil {
			log.Printf("không ghi được %s: %v", exPath, err)
		}
	}
	if err := os.Chdir(dir); err != nil {
		log.Printf("không chuyển được vào thư mục dữ liệu %s: %v", dir, err)
	}
}

func main() {
	// WebKitGTK trên nhiều GPU/driver/VM Linux render ra MÀN TRẮNG do bug DMABUF
	// renderer. Tắt nó là cách khắc phục chuẩn cho Wails trên Linux. Đặt TRƯỚC
	// khi webview khởi tạo (trước wails.Run) và chỉ khi người dùng chưa tự đặt.
	if runtime.GOOS == "linux" {
		if _, ok := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER"); !ok {
			_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
		}
	}

	// Đặt thư mục dữ liệu TRƯỚC khi đọc cấu hình / mở DB (cả hai dùng đường dẫn
	// tương đối theo CWD).
	setupDataDir()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	// Kết nối DB thất bại KHÔNG thoát app: vẫn mở cửa sổ và hiện banner lỗi cho
	// người dùng (kèm nút Thử lại) thay vì tắt lặng/màn trắng. db == nil →
	// App chạy ở chế độ suy giảm cho tới khi RetryDB thành công.
	db, err := database.Connect(cfg)
	app := NewApp(db)
	app.version = Version
	if err != nil {
		app.dbErr = friendlyDBError(cfg, err)
		log.Printf("database: %v (mở app ở chế độ báo lỗi)", err)
	}

	err = wails.Run(&options.App{
		Title:  "PI Tracker",
		Width:  1320,
		Height: 860,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 13, G: 17, B: 23, A: 1},
		OnStartup:        app.startup,
		// Icon + tên chương trình cho cửa sổ/taskbar Linux (ProgramName khớp
		// StartupWMClass trong task-manager.desktop để gom đúng icon trên taskbar).
		Linux: &linux.Options{
			Icon:        appIcon,
			ProgramName: "task-manager",
		},
		// Chạy im khi mở từ terminal: chỉ in lỗi, bỏ log INFO khởi động của Wails.
		LogLevel:           logger.ERROR,
		LogLevelProduction: logger.ERROR,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatalf("wails: %v", err)
	}
}
