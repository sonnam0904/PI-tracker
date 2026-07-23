package report

import (
	"os"
	"testing"
	"time"
)

// TestDumpPDF ghi PDF mẫu ra file để kiểm tra layout bằng mắt.
// Chỉ chạy khi đặt REPORT_DUMP=<path>.
func TestDumpPDF(t *testing.T) {
	out := os.Getenv("REPORT_DUMP")
	if out == "" {
		t.Skip("set REPORT_DUMP to write sample pdf")
	}
	d := sampleData()
	// Thêm task tiêu đề rất dài + task done nil để ép wrap và edge case.
	long := d.Tasks[0]
	long.ID = 99
	long.Title = "trầng ngọc thụy sử dụng trầng ngọc thụy sử dụng trầng ngọc thụy sử dụng trầng ngọc thụy sử dụng thêm nữa cho dài hẳn"
	s := time.Date(2026, 7, 14, 0, 0, 0, 0, time.Local)
	e := time.Date(2026, 7, 16, 0, 0, 0, 0, time.Local)
	long.StartDate, long.DoneDate = &s, &e
	d.Tasks = append(d.Tasks, long)

	b, err := BuildPDF(d)
	if err != nil {
		t.Fatalf("BuildPDF: %v", err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
