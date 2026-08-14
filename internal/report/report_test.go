package report

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

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
				StartDate: &d2s, DoneDate: &d2d, RelatedTaskID: &a1},
		},
		People: map[uint]string{1: "Sơn"},
		// Bug #2 sinh ra từ task #1, cộng thêm bug #9 fix ngoài kỳ (không có dòng
		// nào trong phụ lục) để kiểm tra cả nhánh có link lẫn nhánh chỉ in ID.
		OriginBugs: map[uint][]uint{1: {2, 9}},
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

// Phụ lục phải tách làm hai bảng: task thường và bug, mỗi bảng một bộ cột.
func TestExcelTachBangBug(t *testing.T) {
	b, err := BuildExcel(sampleData()) // 1 task thường (#1) + 1 bug (#2)
	if err != nil {
		t.Fatalf("BuildExcel: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("đọc lại .xlsx: %v", err)
	}
	rows, err := f.GetRows("Task hoàn thành")
	if err != nil {
		t.Fatalf("đọc sheet phụ lục: %v", err)
	}

	// Vị trí hai tiêu đề bảng và hai dòng dữ liệu, theo thứ tự xuất hiện.
	idx := func(pred func([]string) bool) int {
		for i, r := range rows {
			if len(r) > 0 && pred(r) {
				return i
			}
		}
		return -1
	}
	tieuDeTask := idx(func(r []string) bool { return strings.HasPrefix(r[0], "TASK HOÀN THÀNH") })
	tieuDeBug := idx(func(r []string) bool { return strings.HasPrefix(r[0], "BUG PHÁT SINH") })
	dongTask := idx(func(r []string) bool { return r[0] == "#1" })
	dongBug := idx(func(r []string) bool { return r[0] == "#2" })
	if tieuDeTask < 0 || tieuDeBug < 0 || dongTask < 0 || dongBug < 0 {
		t.Fatalf("thiếu tiêu đề/dòng dữ liệu: task=%d bug=%d #1=%d #2=%d\n%v",
			tieuDeTask, tieuDeBug, dongTask, dongBug, rows)
	}
	// Bug phải nằm DƯỚI tiêu đề bảng bug, task thường nằm trên nó.
	if !(tieuDeTask < dongTask && dongTask < tieuDeBug && tieuDeBug < dongBug) {
		t.Errorf("thứ tự sai: tiêu đề task=%d, #1=%d, tiêu đề bug=%d, #2=%d",
			tieuDeTask, dongTask, tieuDeBug, dongBug)
	}
	if got, want := rows[tieuDeTask][0], "TASK HOÀN THÀNH (1 task)"; got != want {
		t.Errorf("tiêu đề bảng task = %q, muốn %q", got, want)
	}
	// Header của mỗi bảng đúng bộ cột của nó.
	if got := rows[tieuDeTask+1]; got[3] != "Loại" {
		t.Errorf("header bảng task cột 4 = %q, muốn %q", got[3], "Loại")
	}
	last := len(bugHeaders) - 1
	if got := rows[tieuDeBug+1]; got[3] != "Mức độ" || got[originColIdx-1] != "Task gốc" || got[last] != "Cách đóng" {
		t.Errorf("header bảng bug = %q, muốn cột Mức độ / Task gốc / Cách đóng", got)
	}
}

// Hai cột estimate bị ẩn khỏi MỌI báo cáo xuất ra — số nội bộ lúc lập kế hoạch,
// không phải thứ để bày ra từng dòng phụ lục (xem taskHeaders).
func TestKhongInCotEstimate(t *testing.T) {
	for _, h := range append(append([]string{}, taskHeaders...), bugHeaders...) {
		if strings.Contains(h, "Est") {
			t.Errorf("header %q vẫn còn cột estimate", h)
		}
	}
	for _, h := range append(append([]string{}, pdfTaskHeaders...), pdfBugHeaders...) {
		if strings.Contains(h, "Est") {
			t.Errorf("header PDF %q vẫn còn cột estimate", h)
		}
	}

	// Và không có ô nào trong file .xlsx in ra giá trị estimate của task mẫu
	// (5.0 / 2.0 ngày) ở phụ lục — dữ liệu phải rời khỏi file, không chỉ mất header.
	d := sampleData()
	d.Tasks[0].EstimateCustomerDays = 987.6 // giá trị không thể trùng chỉ số nào khác
	b, err := BuildExcel(d)
	if err != nil {
		t.Fatalf("BuildExcel: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("đọc lại .xlsx: %v", err)
	}
	rows, err := f.GetRows("Task hoàn thành")
	if err != nil {
		t.Fatalf("đọc sheet phụ lục: %v", err)
	}
	for i, r := range rows {
		for j, c := range r {
			if strings.Contains(c, "987.6") {
				t.Errorf("ô [%d][%d] = %q vẫn in estimate", i, j, c)
			}
		}
	}
}

// Bảng PDF: số cột phải khớp header bên .xlsx và tổng độ rộng phải vừa đúng khổ
// giấy — drawGridRow ghép cells[i] với widths[i] nên lệch là cả bảng lệch mà
// không có lỗi nào báo ra.
func TestPDFColWidths(t *testing.T) {
	cases := []struct {
		ten     string
		headers []string
		widths  []float64
		cols    int
	}{
		{"task", pdfTaskHeaders, pdfTaskColWidths, len(taskHeaders)},
		{"bug", pdfBugHeaders, pdfBugColWidths, len(bugHeaders)},
	}
	for _, c := range cases {
		if len(c.headers) != c.cols || len(c.widths) != c.cols {
			t.Errorf("bảng %s: %d header / %d độ rộng, muốn %d cột",
				c.ten, len(c.headers), len(c.widths), c.cols)
		}
		sum := 0.0
		for _, w := range c.widths {
			sum += w
		}
		if sum != pdfPageW {
			t.Errorf("bảng %s: tổng độ rộng = %.1f, muốn %.1f", c.ten, sum, pdfPageW)
		}
	}
}

// Cột "Bug phát sinh" phải in ID bug (không phải "N bug") và nối hai chiều với
// bảng bug bên dưới bằng hyperlink nội bộ.
func TestExcelLienKetBugVaTaskGoc(t *testing.T) {
	b, err := BuildExcel(sampleData()) // task #1 sinh bug #2 (trong kỳ) và #9 (ngoài kỳ)
	if err != nil {
		t.Fatalf("BuildExcel: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("đọc lại .xlsx: %v", err)
	}
	const sheet = "Task hoàn thành"
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("đọc sheet phụ lục: %v", err)
	}
	// Số dòng Excel (1-based) của dòng task #1 và dòng bug #2.
	rowOf := func(id string) int {
		for i, r := range rows {
			if len(r) > 0 && r[0] == id {
				return i + 1
			}
		}
		t.Fatalf("không thấy dòng %s trong phụ lục:\n%v", id, rows)
		return 0
	}
	dongTask, dongBug := rowOf("#1"), rowOf("#2")
	// Ô cột liên kết ("Bug phát sinh" / "Task gốc") của một dòng — lấy theo
	// originColIdx để test không phải sửa lại mỗi lần bảng thêm/bớt cột.
	oCell := func(row int) string {
		c, err := excelize.CoordinatesToCellName(originColIdx, row)
		if err != nil {
			t.Fatalf("tên ô cột %d: %v", originColIdx, err)
		}
		return c
	}

	// Ô "Bug phát sinh" liệt kê CẢ HAI id, kể cả bug không có dòng ở bảng dưới.
	if got, _ := f.GetCellValue(sheet, oCell(dongTask)); got != "#2, #9" {
		t.Errorf("ô Bug phát sinh = %q, muốn %q", got, "#2, #9")
	}
	// Link của task trỏ xuống dòng bug #2 — bug #9 không có dòng nên bị bỏ qua.
	ok, link, err := f.GetCellHyperLink(sheet, oCell(dongTask))
	if err != nil {
		t.Fatalf("đọc hyperlink cột Bug phát sinh: %v", err)
	}
	if want := fmt.Sprintf("'%s'!A%d", sheet, dongBug); !ok || link != want {
		t.Errorf("link Bug phát sinh = (%v, %q), muốn (true, %q)", ok, link, want)
	}

	// Chiều ngược: ô "Task gốc" của bug trỏ lên dòng task #1.
	if got, _ := f.GetCellValue(sheet, oCell(dongBug)); got != "#1" {
		t.Errorf("ô Task gốc = %q, muốn %q", got, "#1")
	}
	ok, link, err = f.GetCellHyperLink(sheet, oCell(dongBug))
	if err != nil {
		t.Fatalf("đọc hyperlink cột Task gốc: %v", err)
	}
	if want := fmt.Sprintf("'%s'!A%d", sheet, dongTask); !ok || link != want {
		t.Errorf("link Task gốc = (%v, %q), muốn (true, %q)", ok, link, want)
	}
}

// Sheet của từng thành viên có hai bảng nằm dưới báo cáo cá nhân, nên link phải
// tính theo dòng thật của sheet đó chứ không phải theo sheet phụ lục.
func TestExcelLienKetTrenSheetThanhVien(t *testing.T) {
	d := sampleData()
	d.Members = []Data{personalOf("Sơn")}
	b, err := BuildExcel(d)
	if err != nil {
		t.Fatalf("BuildExcel team: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("đọc lại .xlsx: %v", err)
	}
	rows, err := f.GetRows("Sơn")
	if err != nil {
		t.Fatalf("đọc sheet cá nhân: %v", err)
	}
	var dongTask, dongBug int
	for i, r := range rows {
		if len(r) > 0 && r[0] == "#1" {
			dongTask = i + 1
		}
		if len(r) > 0 && r[0] == "#2" {
			dongBug = i + 1
		}
	}
	if dongTask == 0 || dongBug == 0 {
		t.Fatalf("sheet cá nhân thiếu dòng task/bug: #1=%d #2=%d", dongTask, dongBug)
	}
	cell, err := excelize.CoordinatesToCellName(originColIdx, dongTask)
	if err != nil {
		t.Fatalf("tên ô cột %d: %v", originColIdx, err)
	}
	ok, link, err := f.GetCellHyperLink("Sơn", cell)
	if err != nil {
		t.Fatalf("đọc hyperlink: %v", err)
	}
	if want := fmt.Sprintf("'Sơn'!A%d", dongBug); !ok || link != want {
		t.Errorf("link trên sheet cá nhân = (%v, %q), muốn (true, %q)", ok, link, want)
	}
}

// personalOf dựng bản báo cáo cá nhân của một người từ dữ liệu mẫu.
func personalOf(name string) Data {
	d := sampleData()
	d.AssigneeName = name
	d.Metrics.TeamSize = 1
	d.Metrics.TeamTBaseline = d.Settings.TBaseline
	return d
}

// Bản toàn team phải kèm mỗi thành viên một sheet, kể cả người không có task
// nào trong tháng, và tên sheet phải hợp lệ dù tên người chứa ký tự Excel cấm
// hoặc trùng nhau.
func TestBuildTeamReportMemberSheets(t *testing.T) {
	trung1, trung2 := personalOf("Trùng tên"), personalOf("Trùng tên")
	rong := personalOf("Tên/có:ký*tự? cấm")
	rong.Tasks = nil
	rong.Metrics.DoneCount, rong.Metrics.Throughput, rong.Metrics.PI = 0, 0, 0

	d := sampleData()
	d.Members = []Data{personalOf("Sơn"), trung1, trung2, rong}

	b, err := BuildExcel(d)
	if err != nil {
		t.Fatalf("BuildExcel team: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("đọc lại .xlsx: %v", err)
	}
	got := f.GetSheetList()
	want := []string{"Báo cáo", "Task hoàn thành", "Sơn", "Trùng tên", "Trùng tên (2)", "Tên-có-ký-tự- cấm"}
	if len(got) != len(want) {
		t.Fatalf("danh sách sheet = %q, muốn %q", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("sheet[%d] = %q, muốn %q", i, got[i], w)
		}
	}

	// Sheet của một người phải chứa cả báo cáo lẫn bảng task của chính họ.
	if v, _ := f.GetCellValue("Sơn", "A1"); !strings.Contains(v, "Sơn") {
		t.Errorf("tiêu đề sheet cá nhân = %q, thiếu tên người", v)
	}
	rows, err := f.GetRows("Sơn")
	if err != nil {
		t.Fatalf("đọc sheet cá nhân: %v", err)
	}
	var coBangTask bool
	for _, r := range rows {
		if len(r) > 0 && strings.HasPrefix(r[0], "4. TASK HOÀN THÀNH") {
			coBangTask = true
		}
	}
	if !coBangTask {
		t.Error("sheet cá nhân thiếu mục 4 — bảng task hoàn thành")
	}

	if _, err := BuildPDF(d); err != nil {
		t.Fatalf("BuildPDF team: %v", err)
	}
}

// Tên sheet phải sống được với mọi username có thật trong DB. Ba ca dưới đây,
// nếu sheetName làm sai, đều hỏng ÂM THẦM hoặc hỏng cả file:
//   - Khác nhau chỉ ở hoa/thường: excelize so tên sheet không phân biệt hoa
//     thường, nên NewSheet trả về sheet CŨ kèm err = nil và báo cáo của người
//     trước bị ghi đè, file thiếu hẳn một người mà không có lỗi nào.
//   - Trùng (không phân biệt hoa thường) với sheet cố định của team: y như trên,
//     nhưng đè lên chính sheet "Báo cáo".
//   - Quá 31 đơn vị UTF-16, hoặc bắt đầu/kết thúc bằng dấu ': excelize trả lỗi
//     và BuildExcel dừng — CẢ TEAM không xuất được báo cáo vì một cái tên.
func TestSheetNameCacTenGayHong(t *testing.T) {
	// 30 chữ 'a' + 1 emoji = 31 rune nhưng 32 đơn vị UTF-16.
	emoji := strings.Repeat("a", 30) + "🚀"

	d := sampleData()
	d.Members = []Data{
		personalOf("nam"),
		personalOf("NAM"),
		personalOf("báo cáo"),
		personalOf(emoji),
		personalOf("'Sơn'"),
	}

	b, err := BuildExcel(d)
	if err != nil {
		t.Fatalf("BuildExcel: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("đọc lại .xlsx: %v", err)
	}

	// Đủ sheet: 2 sheet cố định + 5 thành viên, không ai bị đè.
	got := f.GetSheetList()
	if len(got) != 7 {
		t.Fatalf("có %d sheet (%q), muốn 7 — có người bị ghi đè", len(got), got)
	}
	seen := map[string]string{}
	for _, name := range got {
		if utf16Len(name) > maxSheetNameLen {
			t.Errorf("sheet %q dài %d đơn vị UTF-16, trần là %d", name, utf16Len(name), maxSheetNameLen)
		}
		if strings.HasPrefix(name, "'") || strings.HasSuffix(name, "'") {
			t.Errorf("sheet %q bắt đầu/kết thúc bằng dấu ' — excelize từ chối", name)
		}
		if first, dup := seen[strings.ToLower(name)]; dup {
			t.Errorf("sheet %q trùng %q khi bỏ qua hoa/thường", name, first)
		}
		seen[strings.ToLower(name)] = name
	}

	// Sheet "Báo cáo" của team phải còn nguyên, không bị thành viên "báo cáo" đè.
	if v, _ := f.GetCellValue("Báo cáo", "A1"); strings.Contains(v, "báo cáo") {
		t.Errorf("sheet team A1 = %q — đã bị sheet thành viên đè", v)
	}
	// Mỗi thành viên giữ đúng báo cáo của mình — đây là phần mà lỗi hoa/thường
	// phá âm thầm: sheet vẫn tồn tại nhưng nội dung là của người khác.
	for _, want := range []string{"nam", "NAM"} {
		found := ""
		for _, name := range got {
			if v, _ := f.GetCellValue(name, "A1"); strings.HasSuffix(v, "— "+want) {
				found = name
			}
		}
		if found == "" {
			t.Errorf("không còn sheet nào mang báo cáo của %q", want)
		}
	}

	if _, err := BuildPDF(d); err != nil {
		t.Fatalf("BuildPDF: %v", err)
	}
}

// Emoji trong dữ liệu người dùng nhập không được làm sập PDF: fpdf đánh chỉ số
// mảng 65536 phần tử thẳng bằng code point nên rune ngoài BMP gây panic, không
// phải trả lỗi — xem pdfText.
func TestPDFKhongPanicVoiEmoji(t *testing.T) {
	d := sampleData()
	d.Tasks[0].Title = "Sửa lỗi 🚀 khẩn"
	d.People = map[uint]string{1: "Sơn 🎯"}
	d.TaskTags = map[uint][]string{d.Tasks[0].ID: {"gấp 🔥"}}
	d.Members = []Data{personalOf("Nam 🚀")}

	if _, err := BuildPDF(d); err != nil {
		t.Fatalf("BuildPDF với emoji: %v", err)
	}
	// .xlsx không có giới hạn này — emoji phải giữ nguyên trong Excel.
	if _, err := BuildExcel(d); err != nil {
		t.Fatalf("BuildExcel với emoji: %v", err)
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
