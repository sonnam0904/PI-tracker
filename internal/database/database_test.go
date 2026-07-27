package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"taskmanager/internal/models"
)

// TestBackfillActivityWorkspace kiểm tra backfill điền đúng workspace_id cho các
// activity cũ (workspace_id = 0), giữ nguyên dòng đã có, bỏ qua dòng mồ côi
// (task đã xóa), và idempotent khi chạy lại. Đồng thời xác nhận SQL subquery
// tương quan chạy được trên SQLite (driver dùng trong app/test).
func TestBackfillActivityWorkspace(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Task{}, &models.Activity{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Task thuộc workspace 7.
	task := models.Task{WorkspaceID: 7, Title: "t"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("tạo task: %v", err)
	}

	// a1: activity cũ chưa có workspace_id (0) → phải được backfill = 7.
	a1 := models.Activity{TaskID: task.ID, Kind: "create", Content: "cũ"}
	// a2: đã có workspace_id (giả sử ghi sai = 99) → KHÔNG được đụng.
	a2 := models.Activity{WorkspaceID: 99, TaskID: task.ID, Kind: "update", Content: "đã set"}
	// a3: mồ côi (task_id không tồn tại) → giữ nguyên 0, không lỗi.
	a3 := models.Activity{TaskID: 9999, Kind: "todo", Content: "mồ côi"}
	for _, a := range []*models.Activity{&a1, &a2, &a3} {
		if err := db.Create(a).Error; err != nil {
			t.Fatalf("tạo activity: %v", err)
		}
	}

	run := func() {
		if err := backfillActivityWorkspace(db); err != nil {
			t.Fatalf("backfill: %v", err)
		}
	}
	run()
	run() // chạy lần 2 để chắc chắn idempotent

	get := func(id uint) uint {
		var a models.Activity
		if err := db.First(&a, id).Error; err != nil {
			t.Fatalf("đọc activity %d: %v", id, err)
		}
		return a.WorkspaceID
	}
	if got := get(a1.ID); got != 7 {
		t.Errorf("a1.workspace_id = %d, muốn 7 (backfill từ task)", got)
	}
	if got := get(a2.ID); got != 99 {
		t.Errorf("a2.workspace_id = %d, muốn giữ nguyên 99", got)
	}
	if got := get(a3.ID); got != 0 {
		t.Errorf("a3.workspace_id = %d, muốn 0 (mồ côi, không đụng)", got)
	}
}
