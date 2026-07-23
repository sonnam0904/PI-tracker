package report

import (
	"testing"
	"time"

	"taskmanager/internal/models"
	"taskmanager/internal/service"
)

func sampleData() Data {
	loc := time.Local
	month := time.Date(2026, 7, 1, 0, 0, 0, 0, loc)
	d1s := time.Date(2026, 7, 3, 0, 0, 0, 0, loc)
	d1d := time.Date(2026, 7, 6, 0, 0, 0, 0, loc)
	d2s := time.Date(2026, 7, 10, 0, 0, 0, 0, loc)
	d2d := time.Date(2026, 7, 12, 0, 0, 0, 0, loc)
	a1 := uint(1)

	return Data{
		Month: month,
		AsOf:  time.Date(2026, 7, 15, 0, 0, 0, 0, loc),
		Metrics: service.Metrics{
			MonthStart: month, MonthEnd: month.AddDate(0, 1, 0),
			ElapsedWeeks: 2, FullWeeks: 31.0 / 7,
			TeamSize: 2, TeamTBaseline: 8.9, TeamPointBaseline: 48,
			DoneCount: 2, Throughput: 4, CycleTime: 2.5,
			DonePoints: 4, PointsPerMonth: 3.6,
			LeadTime: 4, WIP: 1, PI: 1.18,
			EstCustomerTotal: 8, EstAITotal: 3, SavedDays: 5, ActualCycleTotal: 5,
		},
		Advice: service.Advice{
			TargetPI: 1.2, GapPI: 0.02,
			RequiredThroughput: 4.1, AdditionalInMonth: 1, RequiredCycleTime: 2.45,
		},
		Settings: models.Settings{TBaseline: 4.45, CTBaseline: 6.56, PointBaseline: 24, PITarget: 1.2, Capacity: 2},
		Tasks: []models.Task{
			{ID: 1, Title: "Task tiếng Việt: sửa lỗi hiển thị", Type: models.TypePlan, Size: models.SizeM,
				Status: models.StatusDone, AssigneeID: &a1, AIUsed: true,
				EstimateCustomerDays: 5, EstimateAIDays: 2, StartDate: &d1s, DoneDate: &d1d},
			{ID: 2, Title: "Task không AI", Type: models.TypeBug, Size: models.SizeS,
				Status: models.StatusDone, EstimateCustomerDays: 3, EstimateAIDays: 1,
				StartDate: &d2s, DoneDate: &d2d},
		},
		People: map[uint]string{1: "Sơn"},
	}
}

func TestBuildExcel(t *testing.T) {
	b, err := BuildExcel(sampleData())
	if err != nil {
		t.Fatalf("BuildExcel: %v", err)
	}
	if len(b) < 1000 {
		t.Errorf("Excel quá nhỏ: %d bytes", len(b))
	}
}

func TestBuildPDF(t *testing.T) {
	b, err := BuildPDF(sampleData())
	if err != nil {
		t.Fatalf("BuildPDF: %v", err)
	}
	if len(b) < 1000 {
		t.Errorf("PDF quá nhỏ: %d bytes", len(b))
	}
	if string(b[:5]) != "%PDF-" {
		t.Errorf("thiếu PDF header, got %q", b[:5])
	}
}

func TestBuildPersonalReport(t *testing.T) {
	d := sampleData()
	d.AssigneeName = "Sơn"
	d.Metrics.TeamSize = 1
	d.Metrics.TeamTBaseline = d.Settings.TBaseline
	if _, err := BuildExcel(d); err != nil {
		t.Fatalf("BuildExcel cá nhân: %v", err)
	}
	if _, err := BuildPDF(d); err != nil {
		t.Fatalf("BuildPDF cá nhân: %v", err)
	}
}
