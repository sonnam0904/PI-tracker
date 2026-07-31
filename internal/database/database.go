package database

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"taskmanager/internal/config"
	"taskmanager/internal/models"
)

// connectTimeout là trần thời gian cho toàn bộ bước kết nối + migrate. DB ở xa
// không tới được (sai host, mạng/VPN, firewall) mà không có trần sẽ khiến dial
// TCP treo rất lâu — làm treo cả khởi động app LẪN bước `wails generate
// bindings` (Wails chạy chính app để trích xuất binding). Giữ ngắn để lỗi
// nhanh và hiện banner "Thử lại".
const connectTimeout = 8 * time.Second

// gormLogger dựng logger GORM theo DB_LOG.
//
// Mặc định IM LẶNG: lỗi DB đã hiển thị trên banner ở UI, không cần đổ SQL/lỗi
// kết nối ra stdout (gây nhiễu khi chạy ./task-manager từ terminal).
//
// DB_LOG=info in MỌI câu SQL kèm thời gian chạy và số hàng ra stdout — đây là
// cách kiểm một truy vấn có thật sự lọc ở DB hay đang tải hết về rồi lọc trong Go.
// Chạy bằng `wails dev` thì log hiện ở terminal đang chạy nó.
func gormLogger(cfg *config.Config) logger.Interface {
	level := logger.Silent
	switch strings.ToLower(strings.TrimSpace(cfg.LogLevel)) {
	case "info", "sql", "debug", "all":
		level = logger.Info
	case "warn", "slow":
		level = logger.Warn
	case "error":
		level = logger.Error
	}
	if level == logger.Silent {
		return logger.Default.LogMode(logger.Silent)
	}
	return logger.New(
		log.New(os.Stdout, "[sql] ", log.LstdFlags),
		logger.Config{
			SlowThreshold: time.Duration(cfg.SlowQueryMS) * time.Millisecond,
			LogLevel:      level,
			Colorful:      true,
		},
	)
}

// Connect opens the database chosen by DB_DRIVER and runs migrations, với trần
// thời gian để không bao giờ treo vô hạn khi DB không tới được.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	type result struct {
		db  *gorm.DB
		err error
	}
	ch := make(chan result, 1) // buffered: goroutine không kẹt nếu đã timeout
	go func() {
		db, err := connect(cfg)
		ch <- result{db, err}
	}()
	select {
	case r := <-ch:
		return r.db, r.err
	case <-time.After(connectTimeout):
		// Đã timeout nhưng connect vẫn chạy nền: dọn để không rò rỉ. Nếu nó
		// hoàn tất muộn và mở được DB, đóng handle (kèm pool kết nối) — tránh
		// để lại kết nối/migrate lơ lửng, nhất là khi người dùng Thử lại nhiều lần.
		go func() {
			if r := <-ch; r.db != nil {
				if sqlDB, err := r.db.DB(); err == nil {
					_ = sqlDB.Close()
				}
			}
		}()
		return nil, fmt.Errorf(
			"hết thời gian chờ kết nối cơ sở dữ liệu %s sau %s — kiểm tra máy chủ DB, mạng/VPN và cấu hình trong .env",
			cfg.Driver, connectTimeout)
	}
}

// connect mở kết nối và chạy migrate (không giới hạn thời gian — do Connect bọc).
func connect(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector

	switch cfg.Driver {
	case "postgres":
		dsn := fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable connect_timeout=5",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name,
		)
		// search_path đặt schema mặc định cho connection: AutoMigrate và mọi
		// query đọc/ghi bảng trong schema này. Rỗng = giữ mặc định (public).
		if cfg.Schema != "" {
			dsn += " search_path=" + cfg.Schema
		}
		dialector = postgres.Open(dsn)
	case "mysql":
		dsn := fmt.Sprintf(
			"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local&timeout=5s",
			cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Name,
		)
		dialector = mysql.Open(dsn)
	case "sqlite":
		dialector = sqlite.Open(cfg.SQLitePath)
	default:
		return nil, fmt.Errorf("unsupported DB_DRIVER %q (want sqlite, postgres or mysql)", cfg.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{Logger: gormLogger(cfg)})
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", cfg.Driver, err)
	}

	// search_path không tự tạo schema — tạo trước để AutoMigrate có nơi đặt bảng.
	if cfg.Driver == "postgres" && cfg.Schema != "" {
		if err := db.Exec("CREATE SCHEMA IF NOT EXISTS " + cfg.Schema).Error; err != nil {
			return nil, fmt.Errorf("create schema %q: %w", cfg.Schema, err)
		}
	}

	if err := migrateLegacy(db); err != nil {
		return nil, fmt.Errorf("migrate legacy: %w", err)
	}
	if err := db.AutoMigrate(&models.Task{}, &models.Settings{},
		&models.TodoItem{}, &models.Activity{}, &models.StatusChange{},
		&models.User{}, &models.Workspace{}, &models.WorkspaceMember{},
		&models.Invitation{}, &models.Notification{}, &models.SavedView{},
		&models.Session{}, &models.TaskDependency{},
		&models.Tag{}, &models.TaskTag{}); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := backfillActivityWorkspace(db); err != nil {
		return nil, fmt.Errorf("backfill activity workspace: %w", err)
	}
	return db, nil
}

// backfillActivityWorkspace điền activities.workspace_id (cột mới denormalize từ
// task) cho các dòng cũ tạo trước khi có cột này. Idempotent: chỉ đụng dòng còn
// workspace_id = 0. Subquery tương quan chạy được trên cả sqlite/postgres/mysql.
func backfillActivityWorkspace(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.Activity{}) {
		return nil
	}
	return db.Exec(`UPDATE activities
		SET workspace_id = (SELECT t.workspace_id FROM tasks t WHERE t.id = activities.task_id)
		WHERE (workspace_id = 0 OR workspace_id IS NULL)
		  AND task_id IN (SELECT id FROM tasks)`).Error
}

// migrateLegacy dọn schema đời cũ, chạy TRƯỚC AutoMigrate:
//   - tasks.type đổi từ chuỗi sang số (map nhãn cũ → hằng số TaskType) khi
//     cột còn kiểu chữ, để AutoMigrate đổi kiểu cột xong là dữ liệu đã đúng;
//   - bỏ bảng people .
//
// Mọi bước đều idempotent — chạy lại lần sau không đổi gì.
func migrateLegacy(db *gorm.DB) error {
	// Chỉ convert nhãn cũ khi cột type còn kiểu chữ.
	if db.Migrator().HasTable("tasks") && typeColumnIsText(db) {
		for label, code := range map[string]models.TaskType{
			"Theo plan":           models.TypePlan,
			"Phát sinh (bug)":     models.TypeBug,
			"Phát sinh theo plan": models.TypePlanArising,
		} {
			if err := db.Exec("UPDATE tasks SET type = ? WHERE type = ?", int(code), label).Error; err != nil {
				return err
			}
		}
		// Giá trị rỗng/NULL (nếu có) về mặc định Theo plan.
		if err := db.Exec("UPDATE tasks SET type = ? WHERE type = '' OR type IS NULL",
			int(models.TypePlan)).Error; err != nil {
			return err
		}
	}
	if db.Migrator().HasTable("people") {
		if err := db.Migrator().DropTable("people"); err != nil {
			return err
		}
	}
	return nil
}

// typeColumnIsText báo cột tasks.type có đang là kiểu chữ không (text/varchar/
// char). Dùng để quyết định có chạy bước convert nhãn cũ → số hay không: chỉ
// schema đời cũ (SQLite lưu type dạng chữ) mới cần, còn cột số thì bỏ qua.
// Không đọc được metadata coi như không phải kiểu chữ (an toàn: bỏ qua convert).
func typeColumnIsText(db *gorm.DB) bool {
	cols, err := db.Migrator().ColumnTypes("tasks")
	if err != nil {
		return false
	}
	for _, c := range cols {
		if c.Name() != "type" {
			continue
		}
		name := strings.ToLower(c.DatabaseTypeName())
		return strings.Contains(name, "char") || strings.Contains(name, "text")
	}
	return false
}
