package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"taskmanager/internal/models"
)

// xlStyles gom style dùng chung cho mọi sheet — tạo một lần rồi truyền đi, vì
// mỗi lần NewStyle là một style mới trong file (tạo lại theo sheet sẽ phình file).
type xlStyles struct{ title, section, hdr, cell, good, bad, link int }

func newXLStyles(f *excelize.File) xlStyles {
	title, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 15}})
	section, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 12, Color: "2F81F7"}})
	hdr, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E8EBF0"}},
		Border:    borders(),
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	cell, _ := f.NewStyle(&excelize.Style{Border: borders(), Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	good, _ := f.NewStyle(&excelize.Style{Border: borders(), Font: &excelize.Font{Bold: true, Color: "1A7F37"}, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	bad, _ := f.NewStyle(&excelize.Style{Border: borders(), Font: &excelize.Font{Bold: true, Color: "CF222E"}, Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	// link — ô có hyperlink nội bộ: xanh + gạch chân, quy ước ai cũng hiểu là
	// bấm được. Không có style này thì liên kết tồn tại mà không ai biết để bấm.
	link, _ := f.NewStyle(&excelize.Style{
		Border:    borders(),
		Font:      &excelize.Font{Color: "2F81F7", Underline: "single"},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	return xlStyles{title: title, section: section, hdr: hdr, cell: cell, good: good, bad: bad, link: link}
}

// setRow ghi một dòng bắt đầu từ cột A và tô style cho đúng số ô đã ghi.
func setRow(f *excelize.File, sheet string, row, style int, vals ...interface{}) {
	f.SetSheetRow(sheet, fmt.Sprintf("A%d", row), &vals)
	if style != 0 {
		last, _ := excelize.ColumnNumberToName(len(vals))
		f.SetCellStyle(sheet, fmt.Sprintf("A%d", row), fmt.Sprintf("%s%d", last, row), style)
	}
}

// BuildExcel renders the report as an .xlsx file.
//
// Bản toàn team (d.AssigneeName == "") kèm thêm MỖI THÀNH VIÊN MỘT SHEET: báo
// cáo cá nhân đầy đủ (mục 1-3, baseline 1 người) rồi tới danh sách task hoàn
// thành của chính người đó — xem d.Members.
func BuildExcel(d Data) ([]byte, error) {
	d.SortTasks()
	f := excelize.NewFile()
	const S = "Báo cáo"
	const T = "Task hoàn thành"
	f.SetSheetName("Sheet1", S)
	s := newXLStyles(f)

	for col, w := range map[string]float64{"A": 38, "B": 30, "C": 34, "D": 12, "E": 30} {
		f.SetColWidth(S, col, col, w)
	}
	writeReportBody(f, S, d, s)

	// Sheet phụ lục: toàn bộ phạm vi của báo cáo (team = mọi người).
	f.NewSheet(T)
	for i, w := range taskColWidths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(T, col, col, w)
	}
	writeTaskAndBugTables(f, T, 1, d, s, false)

	// Mỗi thành viên một sheet. Tên sheet Excel bị giới hạn ký tự và độ dài nên
	// phải làm sạch + chống trùng, xem sheetName. Khoá map đã hạ chữ vì excelize
	// so tên sheet không phân biệt hoa thường — một thành viên tên "báo cáo" mà
	// khoá theo nguyên văn thì sẽ đè lên chính sheet báo cáo của team.
	used := map[string]bool{strings.ToLower(S): true, strings.ToLower(T): true}
	for _, mem := range d.Members {
		mem.SortTasks()
		name := sheetName(mem.AssigneeName, used)
		if _, err := f.NewSheet(name); err != nil {
			return nil, fmt.Errorf("tạo sheet %q: %w", name, err)
		}
		// Một sheet chỉ có MỘT bộ độ rộng cột, mà sheet này chứa cả bảng chỉ số
		// (5 cột) lẫn bảng task (13 cột) — dùng bộ dung hoà cho cả hai.
		for i, w := range memberColWidths {
			col, _ := excelize.ColumnNumberToName(i + 1)
			f.SetColWidth(name, col, col, w)
		}
		r := writeReportBody(f, name, mem, s)
		r++
		writeTaskAndBugTables(f, name, r, mem, s, true)
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeReportBody ghi tiêu đề + mục 1/2/3 của d vào sheet, trả về dòng trống kế tiếp.
func writeReportBody(f *excelize.File, sheet string, d Data, s xlStyles) int {
	r := 1
	setRow(f, sheet, r, s.title, d.title())
	r++
	setRow(f, sheet, r, 0, fmt.Sprintf("Ngày xuất: %s · %s · Cửa sổ tính: %s → %s · Số liệu tính đến hết ngày %s",
		time.Now().Format("02/01/2006 15:04"), d.scopeLabel(),
		d.Metrics.MonthStart.Format("02/01/2006"), d.Metrics.MonthEnd.AddDate(0, 0, -1).Format("02/01/2006"),
		d.AsOf.Format("02/01/2006")))
	r += 2

	// 1. Chỉ số vs baseline
	setRow(f, sheet, r, s.section, "1. CHỈ SỐ HIỆN TẠI SO VỚI BASELINE")
	r++
	setRow(f, sheet, r, s.hdr, "Chỉ số", "Hiện tại", "Baseline", "Chênh lệch", "Đánh giá")
	r++
	for _, row := range indicatorRows(d) {
		setRow(f, sheet, r, s.cell, row.Name, row.Cur, row.Base, row.Delta, row.Eval)
		evalStyle := s.bad
		if row.Good {
			evalStyle = s.good
		}
		if row.Eval != "Tham khảo" {
			f.SetCellStyle(sheet, fmt.Sprintf("E%d", r), fmt.Sprintf("E%d", r), evalStyle)
		}
		r++
	}
	r++

	// 2. Kết luận
	setRow(f, sheet, r, s.section, "2. ĐÁNH GIÁ MỤC TIÊU")
	r++
	for _, line := range conclusionLines(d) {
		setRow(f, sheet, r, 0, line)
		r++
	}
	r++

	// 3. Hiệu quả AI
	setRow(f, sheet, r, s.section, "3. HIỆU QUẢ ỨNG DỤNG AI (SỐ LIỆU CHI TIẾT)")
	r++
	setRow(f, sheet, r, s.hdr, "Hạng mục", "Giá trị")
	r++
	for _, l := range aiImpactLines(d) {
		setRow(f, sheet, r, s.cell, l.K, l.V)
		r++
	}
	return r
}

// writeTaskAndBugTables ghi phần phụ lục từ dòng row: bảng task thường, cách
// một dòng, rồi bảng bug. Hai bảng RIÊNG nhau vì đọc bằng hai thước khác nhau —
// task thường tính vào T/CT/PI, bug là chi phí chất lượng bóc riêng — và bảng
// bug có cột riêng (mức độ, cách đóng, task gốc).
//
// numbered = true: đánh số mục tiếp theo mục 1-3 của báo cáo (dùng cho sheet
// từng người, nơi hai bảng nằm ngay dưới báo cáo cá nhân).
func writeTaskAndBugTables(f *excelize.File, sheet string, row int, d Data, s xlStyles, numbered bool) int {
	plain, bugs := splitTasks(d.Tasks)
	title := func(n int, text string) string {
		if numbered {
			return fmt.Sprintf("%d. %s", n, text)
		}
		return text
	}

	setRow(f, sheet, row, s.section, title(4, fmt.Sprintf("TASK HOÀN THÀNH (%d task)", len(plain))))
	// Dòng dữ liệu đầu của bảng: sau dòng tiêu đề mục và dòng header cột.
	taskFirst := row + 2
	row = writeTable(f, sheet, row+1, taskHeaders, rowsOf(d, plain, taskRow), s)
	row++

	setRow(f, sheet, row, s.section,
		title(5, fmt.Sprintf("BUG PHÁT SINH ĐÃ XỬ LÝ (%d bug — không tính vào T/CT/PI)", len(bugs))))
	row++
	if len(bugs) == 0 {
		setRow(f, sheet, row, 0, "Không có bug nào hoàn thành trong kỳ.")
		return row + 1
	}
	bugFirst := row + 1
	next := writeTable(f, sheet, row, bugHeaders, rowsOf(d, bugs, bugRow), s)
	linkOriginColumns(f, sheet, d, plain, taskFirst, bugs, bugFirst, s)
	return next
}

// linkOriginColumns nối hai bảng lại với nhau: ô "Bug phát sinh" của task nhảy
// xuống dòng bug tương ứng, ô "Task gốc" của bug nhảy lên dòng task đã sinh ra
// nó. Hai bảng cùng sheet nên dùng hyperlink dạng Location (tham chiếu ô), giữ
// đúng hành vi khi người dùng đọc file ở Excel/LibreOffice/Google Sheets.
//
// Chỉ đặt link khi ĐÍCH thực sự có trong file: bug fix tháng khác (hoặc chưa
// fix) vẫn hiện ID ở cột "Bug phát sinh" nhưng không có dòng nào để trỏ tới —
// link chết còn tệ hơn không link.
func linkOriginColumns(f *excelize.File, sheet string, d Data, plain []models.Task, taskFirst int, bugs []models.Task, bugFirst int, s xlStyles) {
	bugRowByID := make(map[uint]int, len(bugs))
	for i, b := range bugs {
		bugRowByID[b.ID] = bugFirst + i
	}
	taskRowByID := make(map[uint]int, len(plain))
	for i, t := range plain {
		taskRowByID[t.ID] = taskFirst + i
	}

	// Task → bug. Một ô Excel chỉ mang được MỘT hyperlink, nên khi task sinh
	// nhiều bug thì ô in đủ ID còn link trỏ tới bug đầu tiên có mặt ở bảng dưới;
	// tooltip nói rõ đang nhảy tới bug nào để không ai tưởng bấm sai.
	for i, t := range plain {
		for _, id := range d.OriginBugs[t.ID] {
			if r, ok := bugRowByID[id]; ok {
				setLocationLink(f, sheet, taskFirst+i, r, fmt.Sprintf("Xem bug #%d ở mục bug phát sinh", id), s)
				break
			}
		}
	}

	// Bug → task gốc.
	for i, b := range bugs {
		if b.RelatedTaskID == nil {
			continue
		}
		if r, ok := taskRowByID[*b.RelatedTaskID]; ok {
			setLocationLink(f, sheet, bugFirst+i, r, fmt.Sprintf("Xem task gốc #%d ở mục task hoàn thành", *b.RelatedTaskID), s)
		}
	}
}

// setLocationLink biến ô cột originColIdx của dòng row thành liên kết nhảy tới
// dòng target trong CÙNG sheet (neo ở cột A để cả dòng đích lọt vào khung nhìn).
func setLocationLink(f *excelize.File, sheet string, row, target int, tooltip string, s xlStyles) {
	cell, err := excelize.CoordinatesToCellName(originColIdx, row)
	if err != nil {
		return
	}
	anchor, err := excelize.CoordinatesToCellName(1, target)
	if err != nil {
		return
	}
	ref := fmt.Sprintf("'%s'!%s", strings.ReplaceAll(sheet, "'", "''"), anchor)
	if err := f.SetCellHyperLink(sheet, cell, ref, "Location", excelize.HyperlinkOpts{Tooltip: &tooltip}); err != nil {
		return
	}
	f.SetCellStyle(sheet, cell, cell, s.link)
}

// rowsOf dựng các dòng dữ liệu bằng hàm dựng dòng tương ứng (taskRow / bugRow).
func rowsOf(d Data, tasks []models.Task, build func(Data, models.Task) []string) [][]string {
	out := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, build(d, t))
	}
	return out
}

// writeTable ghi header + các dòng từ dòng row, trả về dòng kế tiếp.
func writeTable(f *excelize.File, sheet string, row int, headers []string, rows [][]string, s xlStyles) int {
	hvals := make([]interface{}, len(headers))
	for i, h := range headers {
		hvals[i] = h
	}
	setRow(f, sheet, row, s.hdr, hvals...)
	row++
	for _, r := range rows {
		vals := make([]interface{}, 0, len(r))
		for _, v := range r {
			vals = append(vals, v)
		}
		setRow(f, sheet, row, s.cell, vals...)
		row++
	}
	return row
}

// Độ rộng cột sheet phụ lục, khớp thứ tự taskHeaders + cột "Cách đóng" (L) mà
// chỉ bảng bug dùng tới. Cột J rộng hơn các cột số vì nó chứa danh sách ID bug
// ("#89, #91"), không phải một con số.
var taskColWidths = []float64{5, 48, 16, 18, 8, 8, 12, 12, 12, 17, 26, 16}

// Độ rộng cột sheet của từng thành viên: A-E vừa là bảng chỉ số (Chỉ số / Hiện
// tại / Baseline / Chênh lệch / Đánh giá) vừa là 5 cột đầu bảng task, nên rộng
// hơn mức cần cho #ID và Size một chút — đổi lại bảng chỉ số đọc được.
var memberColWidths = []float64{30, 30, 30, 13, 26, 9, 12, 12, 12, 17, 26, 16}

// maxSheetNameLen — trần độ dài tên sheet của Excel. Đo bằng ĐƠN VỊ UTF-16, xem
// utf16Len.
const maxSheetNameLen = 31

// utf16Len đếm độ dài s theo đơn vị UTF-16: ký tự ngoài BMP (emoji…) tính 2 đơn
// vị, không phải 1. Đây là đúng thước excelize.checkSheetName dùng để kiểm trần
// 31 ký tự, nên cắt theo rune là chưa đủ.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

// cutUTF16 cắt s về tối đa n đơn vị UTF-16, không bao giờ cắt giữa một rune.
func cutUTF16(s string, n int) string {
	if utf16Len(s) <= n {
		return s
	}
	w := 0
	for i, r := range s {
		u := 1
		if r > 0xFFFF {
			u = 2
		}
		if w+u > n {
			return s[:i]
		}
		w += u
	}
	return s
}

// sheetName làm sạch tên người thành tên sheet Excel hợp lệ: bỏ ký tự cấm
// []:*?/\, tối đa 31 ký tự, không trùng sheet đã có (thêm hậu tố " (2)", " (3)"…).
// Tên rỗng — thành viên chưa có username — vẫn phải ra một tên dùng được, nếu
// không cả file hỏng vì một dòng dữ liệu thiếu.
//
// Ba luật của excelize dễ bỏ sót, mỗi luật hỏng theo một kiểu riêng:
//   - Độ dài đo bằng đơn vị UTF-16 (xem utf16Len): một username 31 rune có kèm
//     emoji là 32 đơn vị → NewSheet trả ErrSheetNameLength → BuildExcel dừng,
//     CẢ TEAM không xuất được báo cáo vì một cái tên.
//   - Tên không được bắt đầu/kết thúc bằng dấu ' (ErrSheetNameSingleQuote).
//   - excelize so tên sheet KHÔNG phân biệt hoa thường (strings.EqualFold trong
//     GetSheetIndex). "nam" và "NAM" là cùng một sheet: NewSheet("NAM") trả về
//     index của sheet "nam" kèm err = nil — không tạo sheet mới, không báo lỗi —
//     rồi mọi lệnh ghi sau đó ĐÈ LÊN báo cáo của người trước. Vì vậy used phải
//     khoá theo dạng đã hạ chữ, nếu không việc chống trùng ở đây vô nghĩa.
func sheetName(name string, used map[string]bool) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	// Cắt trước rồi mới bỏ ' và khoảng trắng ở hai đầu: cắt có thể tạo ra dấu '
	// cuối chuỗi mà bản gốc không có.
	clean = trimSheetEdges(cutUTF16(clean, maxSheetNameLen))
	if clean == "" {
		clean = "Nhân sự"
	}
	out := clean
	for i := 2; used[strings.ToLower(out)]; i++ {
		suffix := fmt.Sprintf(" (%d)", i)
		// Hậu tố kết thúc bằng ')' nên phần cắt không cần bỏ ' ở cuối nữa.
		out = trimSheetEdges(cutUTF16(clean, maxSheetNameLen-utf16Len(suffix))) + suffix
	}
	used[strings.ToLower(out)] = true
	return out
}

// trimSheetEdges bỏ khoảng trắng và dấu ' ở hai đầu — excelize từ chối tên sheet
// có ký tự đầu hoặc cuối là '.
func trimSheetEdges(s string) string {
	return strings.Trim(s, " '")
}

func borders() []excelize.Border {
	b := []excelize.Border{}
	for _, t := range []string{"left", "right", "top", "bottom"} {
		b = append(b, excelize.Border{Type: t, Style: 1, Color: "999999"})
	}
	return b
}
