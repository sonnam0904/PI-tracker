package report

import (
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"
)

// BuildExcel renders the report as an .xlsx file.
func BuildExcel(d Data) ([]byte, error) {
	d.SortTasks()
	f := excelize.NewFile()
	const S = "Báo cáo"
	f.SetSheetName("Sheet1", S)

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

	widths := map[string]float64{"A": 38, "B": 30, "C": 34, "D": 12, "E": 30}
	for col, w := range widths {
		f.SetColWidth(S, col, col, w)
	}

	r := 1
	set := func(row int, style int, vals ...interface{}) {
		f.SetSheetRow(S, fmt.Sprintf("A%d", row), &vals)
		if style != 0 {
			f.SetCellStyle(S, fmt.Sprintf("A%d", row), fmt.Sprintf("%c%d", 'A'+len(vals)-1, row), style)
		}
	}

	set(r, title, d.title())
	r++
	set(r, 0, fmt.Sprintf("Ngày xuất: %s · %s · Cửa sổ tính: %s → %s · Số liệu tính đến hết ngày %s",
		time.Now().Format("02/01/2006 15:04"), d.scopeLabel(),
		d.Metrics.MonthStart.Format("02/01/2006"), d.Metrics.MonthEnd.AddDate(0, 0, -1).Format("02/01/2006"),
		d.AsOf.Format("02/01/2006")))
	r += 2

	// 1. Chỉ số vs baseline
	set(r, section, "1. CHỈ SỐ HIỆN TẠI SO VỚI BASELINE")
	r++
	set(r, hdr, "Chỉ số", "Hiện tại", "Baseline", "Chênh lệch", "Đánh giá")
	r++
	for _, row := range indicatorRows(d) {
		set(r, cell, row.Name, row.Cur, row.Base, row.Delta, row.Eval)
		evalStyle := bad
		if row.Good {
			evalStyle = good
		}
		if row.Eval != "Tham khảo" {
			f.SetCellStyle(S, fmt.Sprintf("E%d", r), fmt.Sprintf("E%d", r), evalStyle)
		}
		r++
	}
	r++

	// 2. Kết luận
	set(r, section, "2. ĐÁNH GIÁ MỤC TIÊU")
	r++
	for _, line := range conclusionLines(d) {
		set(r, 0, line)
		r++
	}
	r++

	// 3. Hiệu quả AI
	set(r, section, "3. HIỆU QUẢ ỨNG DỤNG AI (SỐ LIỆU CHI TIẾT)")
	r++
	set(r, hdr, "Hạng mục", "Giá trị")
	r++
	for _, l := range aiImpactLines(d) {
		set(r, cell, l.K, l.V)
		r++
	}

	// Sheet phụ lục task
	const T = "Task hoàn thành"
	f.NewSheet(T)
	tw := []float64{5, 40, 16, 18, 8, 8, 15, 13, 12, 12, 12}
	for i, w := range tw {
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(T, col, col, w)
	}
	hvals := make([]interface{}, len(taskHeaders))
	for i, h := range taskHeaders {
		hvals[i] = h
	}
	f.SetSheetRow(T, "A1", &hvals)
	lastCol, _ := excelize.ColumnNumberToName(len(taskHeaders))
	f.SetCellStyle(T, "A1", fmt.Sprintf("%s1", lastCol), hdr)
	for i, t := range d.Tasks {
		vals := make([]interface{}, 0, len(taskHeaders))
		for _, v := range taskRow(d, t) {
			vals = append(vals, v)
		}
		f.SetSheetRow(T, fmt.Sprintf("A%d", i+2), &vals)
		f.SetCellStyle(T, fmt.Sprintf("A%d", i+2), fmt.Sprintf("%s%d", lastCol, i+2), cell)
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func borders() []excelize.Border {
	b := []excelize.Border{}
	for _, t := range []string{"left", "right", "top", "bottom"} {
		b = append(b, excelize.Border{Type: t, Style: 1, Color: "999999"})
	}
	return b
}
