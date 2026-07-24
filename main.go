package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"taskmanager/internal/config"
	"taskmanager/internal/database"
)

//go:embed all:frontend/dist
var assets embed.FS

// Version là phiên bản app, nhúng lúc build qua ldflags:
//
//	wails build -ldflags "-X main.Version=1.2.3"
//
// Bản dev để "dev" — updater sẽ bỏ qua việc kiểm tra khi ở giá trị này.
var Version = "dev"

func main() {
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
