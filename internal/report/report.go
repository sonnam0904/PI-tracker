// Package report sinh báo cáo PI hàng tháng dạng Excel / PDF.
package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"taskmanager/internal/models"
	"taskmanager/internal/service"
)

// Data gom mọi thứ báo cáo cần cho một tháng.
type Data struct {
	Month        time.Time
	AsOf         time.Time // "ngày tính": chỉ số & phụ lục chỉ gồm task Done ≤ ngày này
	AssigneeName string    // rỗng = báo cáo toàn team; khác rỗng = báo cáo cá nhân
	Metrics      service.Metrics
	Advice       service.Advice
	Settings     models.Settings
	Tasks        []models.Task // task Done trong tháng (đã lọc theo AsOf + nhân sự)
	People       map[uint]string
	// taskID → ID các bug quy về task gốc đó (phân tích nguồn gốc qua
	// RelatedTaskID, bug mọi trạng thái, bất kể fix tháng nào). Giữ ID chứ không
	// chỉ số lượng để cột "Bug phát sinh" in được "#89" và .xlsx trỏ được sang
	// dòng bug tương ứng ở bảng dưới. nil = không có dữ liệu.
	OriginBugs map[uint][]uint
	// taskID → tên các tag phân loại. Tag là quan hệ nhiều-nhiều nên không nằm
	// trên models.Task; phải truyền vào đây để phụ lục in được cột Tag.
	TaskTags map[uint][]string

	// Members: báo cáo riêng của TỪNG thành viên, chỉ có ở bản toàn team
	// (AssigneeName == ""). Mỗi phần tử là một Data hoàn chỉnh — AssigneeName,
	// Metrics/Advice tính riêng với baseline 1 người, Tasks đã lọc theo người đó
	// — nên render bằng đúng code render báo cáo cá nhân: .xlsx cho mỗi người một
	// sheet, .pdf cho mỗi người một trang.
	//
	// Số liệu ở đây do tầng app tính (mỗi người một lượt MetricsService.Compute),
	// KHÔNG chia lại từ Metrics của team: T/CT/PI không cộng trừ tuyến tính được.
	// Phần tử con luôn để Members rỗng — chỉ lồng một cấp.
	Members []Data
}

func (d *Data) monthLabel() string {
	return fmt.Sprintf("%02d/%d", d.Month.Month(), d.Month.Year())
}

// title trả về tiêu đề báo cáo kèm tên nhân sự khi là báo cáo cá nhân.
func (d *Data) title() string {
	t := fmt.Sprintf("BÁO CÁO PERFORMANCE INDEX — THÁNG %s", d.monthLabel())
	if d.AssigneeName != "" {
		t += " — " + d.AssigneeName
	}
	return t
}

// scopeLabel mô tả phạm vi số liệu cho dòng header.
func (d *Data) scopeLabel() string {
	if d.AssigneeName != "" {
		return fmt.Sprintf("Nhân sự: %s (baseline 1 người)", d.AssigneeName)
	}
	return fmt.Sprintf("Team: %d người", d.Metrics.TeamSize)
}

// SortTasks sắp task theo Done date tăng dần cho phần phụ lục.
func (d *Data) SortTasks() {
	sort.SliceStable(d.Tasks, func(i, j int) bool {
		a, b := d.Tasks[i].DoneDate, d.Tasks[j].DoneDate
		if a == nil || b == nil {
			return b == nil
		}
		return a.Before(*b)
	})
}

func f1(v float64) string { return fmt.Sprintf("%.1f", v) }
func f2(v float64) string { return fmt.Sprintf("%.2f", v) }

func pct(v float64) string {
	if v >= 0 {
		return fmt.Sprintf("+%.0f%%", v)
	}
	return fmt.Sprintf("%.0f%%", v)
}

// ---- Bảng chỉ số vs baseline ----

type indicatorRow struct {
	Name, Cur, Base, Delta, Eval string
	Good                         bool
}

func indicatorRows(d Data) []indicatorRow {
	m, st := d.Metrics, d.Settings
	rows := []indicatorRow{}

	tPct := 0.0
	if m.TeamTBaseline > 0 {
		tPct = (m.Throughput - m.TeamTBaseline) / m.TeamTBaseline * 100
	}
	tEval := "Cao hơn baseline"
	if tPct < 0 {
		tEval = "Thấp hơn baseline"
	}
	rows = append(rows, indicatorRow{
		Name:  "Throughput (T) — task/tháng",
		Cur:   f2(m.Throughput),
		Base:  fmt.Sprintf("%s (= %s/người × %d người)", f2(m.TeamTBaseline), f2(st.TBaseline), m.TeamSize),
		Delta: pct(tPct),
		Eval:  tEval, Good: tPct >= 0,
	})

	// Điểm/tháng: sản lượng có trọng số size (S=1, M=3, L=6, XL=9) — bổ trợ
	// cho T vốn đếm task như nhau; không tham gia công thức PI.
	pRow := indicatorRow{
		Name: "Điểm/tháng (P) — điểm size: S=1, M=3, L=6, XL=9",
		Cur:  fmt.Sprintf("%s (tổng %s điểm / %d task)", f2(m.PointsPerMonth), f1(m.DonePoints), m.DoneCount),
		Base: "—", Delta: "—", Eval: "Tham khảo (chưa cấu hình baseline điểm)", Good: true,
	}
	if m.TeamPointBaseline > 0 {
		pPct := (m.PointsPerMonth - m.TeamPointBaseline) / m.TeamPointBaseline * 100
		pRow.Base = fmt.Sprintf("%s (= %s/người × %d người)", f2(m.TeamPointBaseline), f2(st.PointBaseline), m.TeamSize)
		pRow.Delta = pct(pPct)
		pRow.Eval = "Cao hơn baseline"
		if pPct < 0 {
			pRow.Eval = "Thấp hơn baseline"
		}
		pRow.Good = pPct >= 0
	}
	rows = append(rows, pRow)

	ctPct := 0.0
	if st.CTBaseline > 0 && m.CycleTime > 0 {
		ctPct = (m.CycleTime - st.CTBaseline) / st.CTBaseline * 100
	}
	ctEval := "Nhanh hơn baseline"
	if ctPct > 0 {
		ctEval = "Chậm hơn baseline"
	}
	rows = append(rows, indicatorRow{
		Name:  "Cycle Time (CT) — ngày/task",
		Cur:   f2(m.CycleTime),
		Base:  f2(st.CTBaseline),
		Delta: pct(ctPct),
		Eval:  ctEval, Good: ctPct <= 0,
	})

	rows = append(rows, indicatorRow{
		Name: "Lead Time (LT) — ngày (tạo → Done)",
		Cur:  f2(m.LeadTime), Base: "—", Delta: "—", Eval: "Tham khảo", Good: true,
	})
	rows = append(rows, indicatorRow{
		Name: "WIP — task đang In Progress/Blocked",
		Cur:  fmt.Sprintf("%d", m.WIP), Base: "—", Delta: "—", Eval: "Tham khảo", Good: true,
	})

	// Bug được bóc tách riêng — không tham gia T/CT/PI của task.
	rows = append(rows, indicatorRow{
		Name: "Throughput bug (T_bug) — bug/tháng",
		Cur:  f2(m.BugThroughput), Base: "—", Delta: "—",
		Eval: fmt.Sprintf("Tham khảo (%d bug Done, không tính vào PI)", m.BugDoneCount), Good: true,
	})
	rows = append(rows, indicatorRow{
		Name: "Cycle Time bug (CT_bug) — ngày/bug",
		Cur:  f2(m.BugCycleTime), Base: "—", Delta: "—",
		Eval: "Tham khảo (không tính vào PI)", Good: true,
	})

	// Chất lượng hoàn thành: bug phát sinh trên mỗi task Done — càng thấp càng tốt.
	ratioCur := "—"
	ratioEval := "Chưa có task Done để đánh giá"
	if m.DoneCount > 0 {
		ratioCur = fmt.Sprintf("%s (%.0f%%)", f2(m.BugRatio), m.BugRatio*100)
		ratioEval = fmt.Sprintf("%d bug / %d task — càng thấp càng tốt", m.BugDoneCount, m.DoneCount)
	}
	rows = append(rows, indicatorRow{
		Name: "Tỷ lệ bug/task — chất lượng hoàn thành",
		Cur:  ratioCur, Base: "—", Delta: "—",
		Eval: ratioEval, Good: m.DoneCount == 0 || m.BugRatio == 0,
	})

	// Chất lượng theo nguồn gốc: task Done tháng này đã SINH RA bao nhiêu bug
	// (bug quy về task gốc qua liên kết, bất kể bug được fix tháng nào).
	originCur := "—"
	originEval := "Chưa có task Done để đánh giá"
	if m.DoneCount > 0 {
		originCur = fmt.Sprintf("%s (%.0f%%)", f2(m.OriginBugRatio), m.OriginBugRatio*100)
		originEval = fmt.Sprintf("%d bug sinh ra từ %d task Done — tính mọi bug đã liên kết, kể cả chưa fix", m.OriginBugCount, m.DoneCount)
	}
	rows = append(rows, indicatorRow{
		Name: "Tỷ lệ bug theo nguồn gốc — bug sinh ra từ task Done tháng này",
		Cur:  originCur, Base: "—", Delta: "—",
		Eval: originEval, Good: m.DoneCount == 0 || m.OriginBugRatio == 0,
	})

	piEval := fmt.Sprintf("CHƯA ĐẠT mục tiêu %s (thiếu %s)", f2(d.Advice.TargetPI), f2(d.Advice.GapPI))
	if d.Advice.Achieved {
		piEval = fmt.Sprintf("ĐẠT mục tiêu %s (vượt %s)", f2(d.Advice.TargetPI), f2(m.PI-d.Advice.TargetPI))
	}
	capped := ""
	if m.PICapped {
		capped = fmt.Sprintf(" (chạm trần %s)", f2(st.Capacity))
	}
	rows = append(rows, indicatorRow{
		Name:  "Performance Index (PI)",
		Cur:   f2(m.PI) + capped,
		Base:  "1.00",
		Delta: pct((m.PI - 1) * 100),
		Eval:  piEval, Good: d.Advice.Achieved,
	})
	return rows
}

// ---- Kết luận & hành động ----

func conclusionLines(d Data) []string {
	m, a := d.Metrics, d.Advice
	if a.Achieved {
		return []string{fmt.Sprintf("KẾT LUẬN: ĐẠT — PI %s ≥ mục tiêu %s (vượt %s, tương đương +%.0f%%).",
			f2(m.PI), f2(a.TargetPI), f2(m.PI-a.TargetPI), (m.PI-a.TargetPI)/a.TargetPI*100)}
	}
	return []string{fmt.Sprintf("KẾT LUẬN: CHƯA ĐẠT — PI %s < mục tiêu %s (thiếu %s, tương đương %.0f%%).",
		f2(m.PI), f2(a.TargetPI), f2(a.GapPI), a.GapPI/a.TargetPI*100)}
}

// ---- Hiệu quả ứng dụng AI ----

type kv struct{ K, V string }

func aiImpactLines(d Data) []kv {
	m := d.Metrics
	var lines []kv

	// Mọi số ở đây lấy từ Metrics (đã bóc bug ra khỏi task thường), nên số nào
	// cùng xuất hiện ở App UI thì luôn khớp tuyệt đối với báo cáo.
	//
	// Báo cáo in ÍT hơn App UI, có chủ ý: không in estimate (số nội bộ lúc lập kế
	// hoạch — xem taskHeaders) và không in phần ROI so nhóm AI vs không AI. Card
	// "ROI ứng dụng AI" trên Dashboard vẫn còn; nó là công cụ nội bộ, không phải
	// nội dung gửi ra ngoài. Đừng "sửa" chỗ này cho khớp UI.
	lines = append(lines, kv{"Số task hoàn thành trong tháng", fmt.Sprintf("%d task", m.DoneCount)})
	if m.BugDoneCount > 0 {
		bugLine := fmt.Sprintf("%d bug — T_bug %s bug/tháng, CT_bug %s ngày/bug", m.BugDoneCount, f2(m.BugThroughput), f2(m.BugCycleTime))
		if m.DoneCount > 0 {
			bugLine += fmt.Sprintf(" — tỷ lệ %.0f%% bug/task", m.BugRatio*100)
		}
		lines = append(lines, kv{"Bug hoàn thành trong tháng (tách riêng, không tính PI)", bugLine})
	}
	if m.DoneCount > 0 {
		nonAI := m.DoneCount - m.AIUsedCount
		lines = append(lines, kv{"Tỷ lệ áp dụng AI",
			fmt.Sprintf("%d/%d task dùng AI (%.0f%%), %d task không AI",
				m.AIUsedCount, m.DoneCount, float64(m.AIUsedCount)/float64(m.DoneCount)*100, nonAI)})
	}
	lines = append(lines, kv{"Tổng thời gian làm thực tế (cycle)", f1(m.ActualCycleTotal) + " ngày"})

	return lines
}

// ---- Phụ lục task ----

// taskHeaders — KHÔNG in hai cột estimate (Est khách / Est AI). Estimate là số
// nội bộ lúc lập kế hoạch; phần đánh giá đã có Cycle (thời gian thực) và các chỉ
// số ở mục 1-3, nên bày estimate ra từng dòng chỉ làm bảng rộng thêm và mời gọi
// so sánh sai. Hai cột này vẫn còn trong models.Task và trong app — chỉ ẩn khỏi
// báo cáo xuất ra.
var taskHeaders = []string{"#ID", "Tiêu đề", "Phụ trách", "Loại", "Size", "AI", "Cycle (ngày)", "Start", "Done", "Bug phát sinh", "Tag"}

// bugHeaders — bảng bug tách riêng. Cột GIỮ ĐÚNG VỊ TRÍ của bảng task để hai
// bảng nằm cùng sheet vẫn thẳng cột (một sheet Excel chỉ có một bộ độ rộng):
// "Mức độ" đứng chỗ "Loại", "Task gốc" đứng chỗ "Bug phát sinh". "Cách đóng"
// không có cột tương ứng bên bảng task nên thêm vào cuối.
var bugHeaders = []string{"#ID", "Tiêu đề", "Phụ trách", "Mức độ", "Size", "AI", "Cycle (ngày)", "Start", "Done", "Task gốc", "Tag", "Cách đóng"}

// originColIdx — vị trí (1-based, tức cột J) của cặp cột liên kết hai bảng:
// "Bug phát sinh" bên bảng task và "Task gốc" bên bảng bug. Dùng chung một hằng
// vì hai bảng CỐ Ý xếp hai cột này cùng chỗ, và .xlsx đặt hyperlink theo nó.
const originColIdx = 10

// splitTasks tách danh sách thành task thường và bug — báo cáo in hai bảng
// riêng vì hai loại đọc bằng thước khác nhau: task thường tính vào T/CT/PI,
// bug là chi phí chất lượng bóc riêng.
func splitTasks(tasks []models.Task) (plain, bugs []models.Task) {
	for _, t := range tasks {
		if t.IsBug() {
			bugs = append(bugs, t)
		} else {
			plain = append(plain, t)
		}
	}
	return plain, bugs
}

// ---- ô dùng chung cho cả bảng task lẫn bảng bug ----

func (d Data) assigneeCell(t models.Task) string {
	if t.AssigneeID != nil {
		if n, ok := d.People[*t.AssigneeID]; ok {
			return n
		}
	}
	return "—"
}

func (d Data) tagCell(t models.Task) string {
	if names := d.TaskTags[t.ID]; len(names) > 0 {
		return strings.Join(names, ", ")
	}
	return "—"
}

func aiCell(t models.Task) string {
	if t.AIUsed {
		return "Có"
	}
	return "Không"
}

func cycleCell(t models.Task) string {
	if c, ok := t.CycleDays(); ok {
		return f1(c)
	}
	return "—"
}

func dateCell(p *time.Time) string {
	if p == nil {
		return "—"
	}
	return p.Format("02/01/2006")
}

// dash trả về "—" cho chuỗi rỗng, để ô trống không bị hiểu nhầm là lỗi xuất file.
func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// originBugsCell in danh sách ID bug mà task đã SINH RA, tra qua RelatedTaskID
// của các bug. In thẳng "#89, #91" thay vì "2 bug" để đối chiếu được với cột
// "#ID" của bảng bug — .xlsx còn biến ô này thành liên kết tới đúng dòng bug.
// Trống nghĩa là chưa bug nào được gán task gốc trong tracker — báo cáo không
// suy diễn hộ.
func (d Data) originBugsCell(t models.Task) string {
	ids := d.OriginBugs[t.ID]
	if len(ids) == 0 {
		return "—"
	}
	refs := make([]string, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, fmt.Sprintf("#%d", id))
	}
	return strings.Join(refs, ", ")
}

// taskRow dựng một dòng bảng task thường (không bug). Cột đầu là ID task trong
// DB (không phải số thứ tự) để khớp với tham chiếu "#ID" ở bảng bug.
func taskRow(d Data, t models.Task) []string {
	return []string{
		fmt.Sprintf("#%d", t.ID), t.Title, d.assigneeCell(t), t.Type.Label(), string(t.Size), aiCell(t),
		cycleCell(t), dateCell(t.StartDate), dateCell(t.DoneDate), d.originBugsCell(t), d.tagCell(t),
	}
}

// bugRow dựng một dòng bảng bug — thứ tự cột theo bugHeaders.
func bugRow(d Data, t models.Task) []string {
	origin := "—"
	if t.RelatedTaskID != nil {
		origin = fmt.Sprintf("#%d", *t.RelatedTaskID)
	}
	return []string{
		fmt.Sprintf("#%d", t.ID), t.Title, d.assigneeCell(t), dash(string(t.Severity)),
		string(t.Size), aiCell(t),
		cycleCell(t), dateCell(t.StartDate), dateCell(t.DoneDate), origin, d.tagCell(t),
		dash(string(t.Resolution)),
	}
}
