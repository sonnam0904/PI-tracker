package service

import (
	"math"
	"time"

	"taskmanager/internal/models"
)

// Metrics are the core delivery indicators, computed per calendar month
// (mùng 1 → hết tháng). T tính bằng task/tháng (1 tháng chuẩn = 4 tuần),
// CT tính bằng ngày/task.
type Metrics struct {
	MonthStart time.Time // 00:00 ngày mùng 1
	MonthEnd   time.Time // 00:00 ngày mùng 1 tháng kế tiếp
	ElapsedEnd time.Time // mốc cuối của cửa sổ elapsed (now, hoặc Done date xa nhất nếu có task ghi ngày tương lai)

	ElapsedWeeks float64 // số tuần đã trôi qua trong tháng (tính đến hiện tại)
	FullWeeks    float64 // tổng số tuần của cả tháng

	TeamSize      int     // số người trong team (đếm từ danh sách nhân sự, tối thiểu 1)
	TeamTBaseline float64 // T_baseline × TeamSize: throughput baseline của cả team

	// Các chỉ số dưới đây CHỈ tính task thường (không phải bug):
	// bug được bóc tách riêng và không tham gia T/CT/LT/PI.
	DoneCount  int
	Throughput float64 // T TÍCH LŨY: task Done cộng dồn ÷ độ dài CẢ tháng (task/tháng)
	CycleTime  float64 // CT: trung bình (Done − Start − Blocked), ngày/task
	LeadTime   float64 // LT: trung bình (Done − Created), ngày
	WIP        int64   // task đang In Progress / Blocked (trạng thái hiện tại, gồm cả bug)

	// Điểm/tháng: sản lượng có trọng số theo size (models.SizePoints —
	// S=1, M=3, L=6, XL=9), cùng quy ước tích lũy như Throughput và cũng
	// không tính bug. Đo KHỐI LƯỢNG có xét độ lớn task, bổ trợ cho PI
	// (PI đếm task như nhau nên thiệt cho người nhận task lớn).
	DonePoints        float64 // tổng điểm task Done trong tháng
	PointsPerMonth    float64 // điểm tích lũy: DonePoints ÷ độ dài CẢ tháng (điểm/tháng)
	TeamPointBaseline float64 // PointBaseline × TeamSize

	// Chỉ số riêng cho bug (Type = Phát sinh (bug)) Done trong tháng.
	BugDoneCount  int
	BugThroughput float64 // bug Done cộng dồn ÷ độ dài CẢ tháng (bug/tháng)
	BugCycleTime  float64 // trung bình (Done − Start − Blocked) của bug, ngày/bug
	// Chất lượng hoàn thành: BugDoneCount ÷ DoneCount (bug/task, càng thấp
	// càng tốt). 0 khi tháng chưa có task Done nào — xem kèm BugDoneCount.
	BugRatio float64

	// Chất lượng theo NGUỒN GỐC: bug quy về task gốc qua RelatedTaskID —
	// đếm MỌI bug đã liên kết tới các task Done trong tháng này: kể cả chưa fix,
	// bất kể fix tháng nào và bất kể nhập vào tracker lúc nào.
	//
	// LƯU Ý hai chỉ số này CỐ Ý không bị chặn theo "ngày tính" (now), khác với
	// DoneCount và phần phụ lục: lý do ở TaskService.BugIDsByOrigin. Hệ quả là
	// xuất lại báo cáo một tháng đã chốt có thể ra số bug cao hơn lần xuất trước,
	// nếu trong lúc đó có bug mới được liên kết về task cũ.
	OriginBugCount int
	OriginBugRatio float64 // OriginBugCount ÷ DoneCount (bug sinh ra / task Done)

	PI       float64 // min((T/T_baseline) × (CT_baseline/CT), capacity)
	PICapped bool    // true nếu PI chạm trần capacity

	// So sánh estimate báo khách vs estimate làm bằng AI (task Done trong tháng)
	EstCustomerTotal float64
	EstAITotal       float64
	SavedDays        float64
	// Tổng cycle thực (ngày, đã trừ blocked) của task Done trong tháng —
	// để đối chiếu estimate AI có sát thực tế không.
	ActualCycleTotal float64

	// Effort thực tế nhập tay (Task.ActualDays > 0) của task Done trong tháng.
	// EstAIPairedTotal chỉ cộng est AI của CHÍNH các task đã nhập effort, để
	// so sánh estimate với effort trên cùng một tập task.
	ActualEffortTotal float64
	ActualEffortCount int
	EstAIPairedTotal  float64

	// Mức độ dùng AI: số task Done (không tính bug) có đánh dấu AIUsed.
	AIUsedCount int

	// ---- ROI ứng dụng AI: tách task thường (không bug) Done trong tháng theo
	// cờ AIUsed, để đo AI có giúp làm NHANH hơn không. ----
	// Cycle time trung bình mỗi nhóm (ngày/task, đã trừ blocked); *CycleCount =
	// số task đủ start/done góp vào trung bình đó. Card "ROI ứng dụng AI" trên
	// Dashboard đọc trực tiếp bốn field này; báo cáo xuất ra CỐ Ý không in phần
	// ROI (xem report.aiImpactLines).
	AICycleTime     float64
	AICycleCount    int
	NonAICycleTime  float64
	NonAICycleCount int
}

// Advice tells the dev what is needed to reach the target PI by month end.
type Advice struct {
	Achieved bool
	TargetPI float64
	GapPI    float64

	// Giữ CT hiện tại:
	RequiredThroughput float64 // task/tháng tối thiểu
	AdditionalInMonth  int     // số task Done cần thêm từ giờ đến hết tháng
	AdditionalPerWeek  float64 // nhịp cần duy trì trong các tuần còn lại
	// Giữ T hiện tại, cần giảm CT xuống tối đa bao nhiêu:
	RequiredCycleTime float64 // ngày/task
}

type MetricsService struct {
	tasks      *TaskService
	workspaces *WorkspaceService
	settings   *SettingsService
}

func NewMetricsService(tasks *TaskService, workspaces *WorkspaceService, settings *SettingsService) *MetricsService {
	return &MetricsService{tasks: tasks, workspaces: workspaces, settings: settings}
}

// MonthWindow returns [mùng 1, mùng 1 tháng sau) of the month containing t.
func MonthWindow(t time.Time) (time.Time, time.Time) {
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	return start, start.AddDate(0, 1, 0)
}

// dayAfter returns 00:00 of the day after t (mốc "hết ngày t").
func dayAfter(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, 1)
}

// DoneTasksAsOf returns task Done trong tháng với Done date ≤ hết ngày now.
// Dùng chung cho Compute và phần phụ lục báo cáo để hai nơi luôn khớp nhau.
func (s *MetricsService) DoneTasksAsOf(wsID uint, month, now time.Time, assigneeID uint) ([]models.Task, error) {
	start, end := MonthWindow(month)
	doneUpper := end
	if d := dayAfter(now); d.Before(end) {
		doneUpper = d
	}
	return s.tasks.DoneBetween(wsID, start, doneUpper, assigneeID)
}

// BugsByOrigin trả về map taskID → ID các bug quy về task gốc đó qua
// RelatedTaskID. Lấy MỌI trạng thái (bug chưa fix vẫn là lỗi task sinh ra), bất
// kể bug được fix tháng nào và nhập vào tracker lúc nào.
// tasks là danh sách task cần phân tích (phần tử loại bug được bỏ qua).
// Dùng chung cho Compute và phụ lục báo cáo để hai nơi luôn khớp nhau.
func (s *MetricsService) BugsByOrigin(wsID uint, tasks []models.Task) (map[uint][]uint, error) {
	ids := make([]uint, 0, len(tasks))
	for _, t := range tasks {
		if !t.IsBug() {
			ids = append(ids, t.ID)
		}
	}
	return s.tasks.BugIDsByOrigin(wsID, ids)
}

// Compute evaluates all indicators for the calendar month containing month.
// now là "ngày tính" (mặc định hôm nay, có thể chọn ngày khác để mô phỏng):
// chỉ task có Done date ≤ now được tính vào chỉ số — chọn now = cuối tháng
// để xem PI chốt sổ (bao gồm các task kế hoạch ghi Done date tương lai).
// assigneeID != 0: chỉ tính task của người đó và baseline theo 1 người.
func (s *MetricsService) Compute(wsID uint, month, now time.Time, assigneeID uint) (Metrics, models.Settings, error) {
	st, err := s.settings.Get(wsID)
	if err != nil {
		return Metrics{}, st, err
	}

	start, end := MonthWindow(month)

	done, err := s.DoneTasksAsOf(wsID, month, now, assigneeID)
	if err != nil {
		return Metrics{}, st, err
	}
	wip, err := s.tasks.CountWIP(wsID, assigneeID)
	if err != nil {
		return Metrics{}, st, err
	}

	// Số người trong team = số thành viên workspace (tối thiểu 1);
	// xem theo 1 người → baseline tính trên đúng 1 người.
	teamSize := 1
	if assigneeID == 0 {
		members, err := s.workspaces.Members(wsID)
		if err != nil {
			return Metrics{}, st, err
		}
		// Chỉ đếm thành viên THỰC SỰ làm việc: observer (người quan sát/quản lý)
		// không kể vào baseline để không làm loãng PI của team.
		counted := 0
		for _, mb := range members {
			if !mb.Observer {
				counted++
			}
		}
		if counted > 0 {
			teamSize = counted
		}
	}

	// ElapsedEnd/ElapsedWeeks chỉ dùng cho hiển thị tiến độ tháng —
	// KHÔNG ảnh hưởng tới T (T tính tích lũy trên cả tháng).
	elapsedEnd := end
	if now.Before(end) {
		elapsedEnd = now
	}
	if elapsedEnd.Before(start) {
		elapsedEnd = start
	}
	elapsedDays := elapsedEnd.Sub(start).Hours() / 24
	if elapsedDays < 1 {
		elapsedDays = 1
	}

	// Bóc bug ra khỏi task thường: bug có T/CT riêng, không tham gia
	// T/CT/LT/PI (bug là chi phí chất lượng, không phải sản lượng kế hoạch).
	var plain, bugs []models.Task
	for _, t := range done {
		if t.IsBug() {
			bugs = append(bugs, t)
		} else {
			plain = append(plain, t)
		}
	}

	m := Metrics{
		MonthStart:        start,
		MonthEnd:          end,
		ElapsedEnd:        elapsedEnd,
		ElapsedWeeks:      elapsedDays / 7,
		FullWeeks:         end.Sub(start).Hours() / 24 / 7,
		TeamSize:          teamSize,
		TeamTBaseline:     st.TBaseline * float64(teamSize),
		TeamPointBaseline: st.PointBaseline * float64(teamSize),
		DoneCount:         len(plain),
		BugDoneCount:      len(bugs),
		WIP:               wip,
	}

	var ctSum, ltSum float64
	var ctN, ltN int
	var aiCtSum, nonAICtSum float64
	var aiCtN, nonAICtN int
	for _, t := range plain {
		if d, ok := t.CycleDays(); ok {
			ctSum += d
			ctN++
			// Tách cycle theo nhóm AI/không-AI cho phân tích ROI.
			if t.AIUsed {
				aiCtSum += d
				aiCtN++
			} else {
				nonAICtSum += d
				nonAICtN++
			}
		}
		if d, ok := t.LeadDays(); ok {
			ltSum += d
			ltN++
		}
		m.DonePoints += models.SizePoints(t.Size)
		m.EstCustomerTotal += t.EstimateCustomerDays
		m.EstAITotal += t.EstimateAIDays
		if t.ActualDays > 0 {
			m.ActualEffortTotal += t.ActualDays
			m.ActualEffortCount++
			m.EstAIPairedTotal += t.EstimateAIDays
		}
		if t.AIUsed {
			m.AIUsedCount++
		}
	}
	m.SavedDays = m.EstCustomerTotal - m.EstAITotal
	m.ActualCycleTotal = ctSum
	m.AICycleCount, m.NonAICycleCount = aiCtN, nonAICtN
	if aiCtN > 0 {
		m.AICycleTime = aiCtSum / float64(aiCtN)
	}
	if nonAICtN > 0 {
		m.NonAICycleTime = nonAICtSum / float64(nonAICtN)
	}

	// 1 tháng chuẩn = 4 tuần. T tích lũy: task Done cộng dồn chia cho độ dài
	// CẢ tháng (không ngoại suy theo thời gian đã trôi qua) → PI cũng tích lũy,
	// đầu tháng thấp và tăng dần khi hoàn thành thêm task.
	m.Throughput = float64(len(plain)) / (m.FullWeeks / 4)
	m.PointsPerMonth = m.DonePoints / (m.FullWeeks / 4)
	if ctN > 0 {
		m.CycleTime = ctSum / float64(ctN)
	}
	if ltN > 0 {
		m.LeadTime = ltSum / float64(ltN)
	}

	// Chỉ số bug riêng, cùng quy ước tích lũy trên cả tháng.
	m.BugThroughput = float64(len(bugs)) / (m.FullWeeks / 4)
	var bugCtSum float64
	var bugCtN int
	for _, t := range bugs {
		if d, ok := t.CycleDays(); ok {
			bugCtSum += d
			bugCtN++
		}
	}
	if bugCtN > 0 {
		m.BugCycleTime = bugCtSum / float64(bugCtN)
	}
	if m.DoneCount > 0 {
		m.BugRatio = float64(m.BugDoneCount) / float64(m.DoneCount)
	}

	// Phân tích nguồn gốc: task Done tháng này đã sinh ra bao nhiêu bug
	// (bug quy về task gốc, bất kể bug được fix tháng nào).
	origin, err := s.BugsByOrigin(wsID, plain)
	if err != nil {
		return Metrics{}, st, err
	}
	for _, bugIDs := range origin {
		m.OriginBugCount += len(bugIDs)
	}
	if m.DoneCount > 0 {
		m.OriginBugRatio = float64(m.OriginBugCount) / float64(m.DoneCount)
	}

	// PI = min((T / T_baseline) × (CT_baseline / CT), capacity)
	// Throughput tăng hoặc cycle time giảm đều làm PI tăng.
	capacity := st.Capacity
	if capacity <= 0 {
		capacity = 2
	}
	if m.TeamTBaseline > 0 && st.CTBaseline > 0 && m.Throughput > 0 {
		if m.CycleTime > 0 {
			pi := (m.Throughput / m.TeamTBaseline) * (st.CTBaseline / m.CycleTime)
			if pi >= capacity {
				m.PI = capacity
				m.PICapped = true
			} else {
				m.PI = pi
			}
		} else {
			// CT = 0 (task xong ngay trong ngày bắt đầu) → PI vượt mọi ngưỡng, chạm trần.
			m.PI = capacity
			m.PICapped = true
		}
	}
	return m, st, nil
}

// Advise computes the thresholds needed to reach the target PI by month end.
// Required* luôn được tính kể cả khi đã đạt, để UI hiển thị biên độ an toàn
// (hiện tại cách ngưỡng tối thiểu bao xa).
func (s *MetricsService) Advise(m Metrics, st models.Settings) Advice {
	a := Advice{TargetPI: st.PITarget}
	if m.PI >= st.PITarget {
		a.Achieved = true
	} else {
		a.GapPI = st.PITarget - m.PI
	}

	if m.TeamTBaseline <= 0 || st.CTBaseline <= 0 || st.PITarget <= 0 {
		return a
	}

	// Baseline của team = T_baseline × TeamSize. Từ target = (T/T_team) × (CT_b/CT):
	//   giữ CT → T ≥ target × T_team × CT / CT_b
	//   giữ T  → CT ≤ T × CT_b / (target × T_team)
	if m.CycleTime > 0 {
		a.RequiredThroughput = st.PITarget * m.TeamTBaseline * m.CycleTime / st.CTBaseline
		// Chốt sổ cuối tháng: tổng task Done cả tháng phải đạt requiredT (task/tháng)
		// × độ dài tháng tính theo tháng chuẩn (số tuần / 4).
		needTotal := int(math.Ceil(a.RequiredThroughput * m.FullWeeks / 4))
		a.AdditionalInMonth = needTotal - m.DoneCount
		if a.AdditionalInMonth < 0 {
			a.AdditionalInMonth = 0
		}
		if remaining := m.FullWeeks - m.ElapsedWeeks; remaining > 0 {
			a.AdditionalPerWeek = float64(a.AdditionalInMonth) / remaining
		}
	}
	if m.Throughput > 0 {
		a.RequiredCycleTime = m.Throughput * st.CTBaseline / (st.PITarget * m.TeamTBaseline)
	}
	return a
}
