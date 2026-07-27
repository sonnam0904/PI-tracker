package service

import (
	"testing"

	"taskmanager/internal/models"
)

// onCreate (dùng để phát Postgres NOTIFY realtime) phải fire cho Create (mention/
// reply) nhưng KHÔNG fire cho CreateIfAbsent (due) — vì checkDueTasks đã tự toast
// due, broadcast thêm sẽ khiến chính client đó toast trùng. Xem ghi chú trong
// CreateIfAbsent.
func TestNotificationOnCreateScope(t *testing.T) {
	db := testDB(t)
	svc := NewNotificationService(db)
	fires := 0
	svc.SetOnCreate(func() { fires++ })

	// Create → fire đúng 1 lần.
	if _, err := svc.Create(models.Notification{UserID: 1, Kind: "mention", Content: "a"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if fires != 1 {
		t.Fatalf("Create phải fire onCreate 1 lần, có %d", fires)
	}

	// CreateIfAbsent tạo bản ghi MỚI → KHÔNG được fire (tránh toast trùng due).
	if _, isNew, err := svc.CreateIfAbsent(models.Notification{UserID: 1, Kind: "due", Content: "b"}); err != nil || !isNew {
		t.Fatalf("CreateIfAbsent (mới): isNew=%v err=%v", isNew, err)
	}
	if fires != 1 {
		t.Fatalf("CreateIfAbsent KHÔNG được fire onCreate, nhưng fires=%d", fires)
	}

	// CreateIfAbsent trùng nội dung → không tạo, cũng không fire.
	if _, isNew, err := svc.CreateIfAbsent(models.Notification{UserID: 1, Kind: "due", Content: "b"}); err != nil || isNew {
		t.Fatalf("CreateIfAbsent (trùng): isNew=%v err=%v", isNew, err)
	}
	if fires != 1 {
		t.Fatalf("CreateIfAbsent trùng không được fire, fires=%d", fires)
	}
}
