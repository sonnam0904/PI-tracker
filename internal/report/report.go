// Package report sinh báo cáo PI hàng tháng dạng Excel / PDF.
package report

import (
	"fmt"
	"sort"
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
	// taskID → số bug quy về task gốc đó (phân tích nguồn gốc qua RelatedTaskID,
	// bug mọi trạng thái, bất kể fix tháng nào). nil = không có dữ liệu.
	OriginBugs map[uint]int
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

	// Mọi chỉ số AI/estimate lấy từ Metrics (đã bóc bug ra khỏi task thường)
	// để App UI và báo cáo luôn khớp nhau tuyệt đối.
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
	lines = append(lines, kv{"Tổng estimate báo khách hàng", f1(m.EstCustomerTotal) + " ngày"})
	lines = append(lines, kv{"Tổng estimate làm bằng AI", f1(m.EstAITotal) + " ngày"})
	lines = append(lines, kv{"Tổng thời gian làm thực tế (cycle)", f1(m.ActualCycleTotal) + " ngày"})

	if m.EstCustomerTotal > 0 {
		saved := m.EstCustomerTotal - m.EstAITotal
		lines = append(lines, kv{"Tiết kiệm kế hoạch (est. khách − est. AI)",
			fmt.Sprintf("%s ngày (%.0f%% so với báo khách)", f1(saved), saved/m.EstCustomerTotal*100)})
		actualSaved := m.EstCustomerTotal - m.ActualCycleTotal
		if actualSaved >= 0 {
			lines = append(lines, kv{"Tiết kiệm thực tế (est. khách − cycle thực)",
				fmt.Sprintf("%s ngày — hoàn thành nhanh hơn cam kết %.0f%%", f1(actualSaved), actualSaved/m.EstCustomerTotal*100)})
		} else {
			lines = append(lines, kv{"Tiết kiệm thực tế (est. khách − cycle thực)",
				fmt.Sprintf("%s ngày — CHẬM hơn cam kết %.0f%%", f1(actualSaved), -actualSaved/m.EstCustomerTotal*100)})
		}
	}
	if m.EstAITotal > 0 && m.ActualCycleTotal > 0 {
		diff := (m.ActualCycleTotal - m.EstAITotal) / m.EstAITotal * 100
		lines = append(lines, kv{"Độ sát estimate AI so với thực tế",
			fmt.Sprintf("lệch %s (thực %s / est %s ngày)", pct(diff), f1(m.ActualCycleTotal), f1(m.EstAITotal))})
	}

	// ---- ROI ứng dụng AI: so nhóm dùng AI vs không AI ----
	if sp, ok := m.AISpeedupPct(); ok {
		cmp, v := "nhanh hơn", sp
		if v < 0 {
			cmp, v = "chậm hơn", -v
		}
		lines = append(lines, kv{"ROI tốc độ — cycle time AI vs không AI",
			fmt.Sprintf("%s vs %s ngày/task (%d/%d task) — task dùng AI %s %.0f%%",
				f1(m.AICycleTime), f1(m.NonAICycleTime), m.AICycleCount, m.NonAICycleCount, cmp, v)})
	}
	if m.AIEffortCount > 0 && m.AIEstPairedTotal > 0 {
		dev := (m.AIEffortTotal - m.AIEstPairedTotal) / m.AIEstPairedTotal * 100
		lines = append(lines, kv{"ROI ước lượng — độ lệch est AI ↔ effort thực (nhóm AI)",
			fmt.Sprintf("lệch %s (effort %s / est %s ngày, %d task)", pct(dev), f1(m.AIEffortTotal), f1(m.AIEstPairedTotal), m.AIEffortCount)})
	}
	if m.NonAIEffortCount > 0 && m.NonAIEstPairedTotal > 0 {
		dev := (m.NonAIEffortTotal - m.NonAIEstPairedTotal) / m.NonAIEstPairedTotal * 100
		lines = append(lines, kv{"ROI ước lượng — độ lệch est AI ↔ effort thực (nhóm không AI)",
			fmt.Sprintf("lệch %s (effort %s / est %s ngày, %d task)", pct(dev), f1(m.NonAIEffortTotal), f1(m.NonAIEstPairedTotal), m.NonAIEffortCount)})
	}

	lines = append(lines, kv{"PI so với baseline (1.00)", fmt.Sprintf("%s (%s so với trước khi áp dụng AI)", f2(m.PI), pct((m.PI-1)*100))})
	return lines
}

// ---- Phụ lục task ----

var taskHeaders = []string{"#ID", "Tiêu đề", "Phụ trách", "Loại", "Size", "AI", "Est khách (ngày)", "Est AI (ngày)", "Cycle (ngày)", "Start", "Done", "Bug phát sinh"}

// taskRow dựng một dòng phụ lục. Cột đầu là ID task trong DB (không phải số
// thứ tự) để khớp với tham chiếu "← #ID" ở cột Bug phát sinh.
func taskRow(d Data, t models.Task) []string {
	assignee := "—"
	if t.AssigneeID != nil {
		if n, ok := d.People[*t.AssigneeID]; ok {
			assignee = n
		}
	}
	ai := "Không"
	if t.AIUsed {
		ai = "Có"
	}
	cycle := "—"
	if c, ok := t.CycleDays(); ok {
		cycle = f1(c)
	}
	date := func(p *time.Time) string {
		if p == nil {
			return "—"
		}
		return p.Format("02/01/2006")
	}
	// Cột nguồn gốc: task thường hiện số bug nó sinh ra;
	// dòng bug hiện liên kết ngược về task gốc.
	originCol := "—"
	if t.IsBug() {
		if t.RelatedTaskID != nil {
			originCol = fmt.Sprintf("#%d", *t.RelatedTaskID)
		}
	} else if n := d.OriginBugs[t.ID]; n > 0 {
		originCol = fmt.Sprintf("%d bug", n)
	}
	return []string{
		fmt.Sprintf("#%d", t.ID), t.Title, assignee, t.Type.Label(), string(t.Size), ai,
		f1(t.EstimateCustomerDays), f1(t.EstimateAIDays), cycle, date(t.StartDate), date(t.DoneDate),
		originCol,
	}
}
