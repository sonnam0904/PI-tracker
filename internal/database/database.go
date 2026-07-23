package database

import (
	"fmt"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"taskmanager/internal/config"
	"taskmanager/internal/models"
)

// Connect opens the database chosen by DB_DRIVER and runs migrations.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector

	switch cfg.Driver {
	case "postgres":
		dsn := fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
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
			"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Name,
		)
		dialector = mysql.Open(dsn)
	case "sqlite":
		dialector = sqlite.Open(cfg.SQLitePath)
	default:
		return nil, fmt.Errorf("unsupported DB_DRIVER %q (want sqlite, postgres or mysql)", cfg.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{})
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
		&models.Session{}, &models.TaskDependency{}); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// migrateLegacy dọn schema đời cũ, chạy TRƯỚC AutoMigrate:
//   - tasks.type đổi từ chuỗi sang số (map nhãn cũ → hằng số TaskType) khi
//     cột còn kiểu chữ, để AutoMigrate đổi kiểu cột xong là dữ liệu đã đúng;
//   - bỏ bảng people (đã thay bằng users + workspace_members từ khi có
//     đăng nhập/workspace, không còn code nào đọc).
//
// Mọi bước đều idempotent — chạy lại lần sau không đổi gì.
func migrateLegacy(db *gorm.DB) error {
	// Chỉ convert nhãn cũ khi cột type còn kiểu chữ. Trên Postgres cột đã là
	// bigint nên so sánh type = 'Theo plan' sẽ lỗi kiểu (SQLSTATE 22P02); khi
	// đó dữ liệu vốn đã ở dạng số, không cần (và không được) chạy bước này.
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
