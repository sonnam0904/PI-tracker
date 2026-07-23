package report

import (
	"bytes"
	_ "embed"
	"fmt"
	"time"

	"github.com/go-pdf/fpdf"
)

//go:embed fonts/DejaVuSans.ttf
var fontRegular []byte

//go:embed fonts/DejaVuSans-Bold.ttf
var fontBold []byte

const (
	pdfMarginL = 10.0
	pdfPageW   = 190.0 // A4 210 − lề 2×10
	pdfBreakY  = 283.0 // 297 − lề dưới 14
)

// BuildPDF renders the report as an A4 PDF (font DejaVu hỗ trợ tiếng Việt).
func BuildPDF(d Data) ([]byte, error) {
	d.SortTasks()

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes("dejavu", "", fontRegular)
	pdf.AddUTF8FontFromBytes("dejavu", "B", fontBold)
	pdf.SetMargins(pdfMarginL, 12, 10)
	pdf.SetAutoPageBreak(true, 14)
	pdf.AddPage()

	// Tiêu đề
	pdf.SetFont("dejavu", "B", 15)
	pdf.CellFormat(pdfPageW, 9, d.title(), "", 1, "C", false, 0, "")
	pdf.SetFont("dejavu", "", 8.5)
	pdf.SetTextColor(110, 110, 110)
	pdf.CellFormat(pdfPageW, 5, fmt.Sprintf("Ngày xuất: %s  ·  %s  ·  Cửa sổ tính: %s → %s  ·  Số liệu tính đến hết ngày %s",
		time.Now().Format("02/01/2006 15:04"), d.scopeLabel(),
		d.Metrics.MonthStart.Format("02/01/2006"), d.Metrics.MonthEnd.AddDate(0, 0, -1).Format("02/01/2006"),
		d.AsOf.Format("02/01/2006")),
		"", 1, "C", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(4)

	sectionTitle := func(s string) {
		pdf.SetFont("dejavu", "B", 11.5)
		pdf.SetTextColor(31, 81, 160)
		pdf.CellFormat(pdfPageW, 8, s, "", 1, "L", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	}
	bodyFont := func(size float64) { pdf.SetFont("dejavu", "", size); pdf.SetTextColor(0, 0, 0) }
	headerRow := func(widths []float64, cells []string, size float64) {
		pdf.SetFont("dejavu", "B", size)
		pdf.SetFillColor(232, 235, 240)
		drawGridRow(pdf, widths, cells, size*0.62, "C", true, nil)
	}

	// 1. Chỉ số vs baseline
	sectionTitle("1. Chỉ số hiện tại so với baseline")
	indW := []float64{52, 38, 48, 18, 34}
	headerRow(indW, []string{"Chỉ số", "Hiện tại", "Baseline", "Chênh lệch", "Đánh giá"}, 8.5)
	bodyFont(8.5)
	for _, row := range indicatorRows(d) {
		row := row
		drawGridRow(pdf, indW, []string{row.Name, row.Cur, row.Base, row.Delta, row.Eval}, 5, "L", false,
			func(i int) {
				if i == 4 && row.Eval != "Tham khảo" {
					pdf.SetFont("dejavu", "B", 8.5)
					if row.Good {
						pdf.SetTextColor(26, 127, 55)
					} else {
						pdf.SetTextColor(207, 34, 46)
					}
				} else {
					bodyFont(8.5)
				}
			})
	}
	bodyFont(8.5)
	pdf.Ln(4)

	// 2. Kết luận
	sectionTitle("2. Đánh giá mục tiêu")
	bodyFont(9.5)
	for _, line := range conclusionLines(d) {
		pdf.MultiCell(pdfPageW, 5.6, "•  "+line, "", "L", false)
		pdf.Ln(0.8)
	}
	pdf.Ln(3)

	// 3. Hiệu quả AI
	sectionTitle("3. Hiệu quả ứng dụng AI (số liệu chi tiết)")
	aiW := []float64{78, 112}
	headerRow(aiW, []string{"Hạng mục", "Giá trị"}, 8.5)
	bodyFont(8.5)
	for _, l := range aiImpactLines(d) {
		drawGridRow(pdf, aiW, []string{l.K, l.V}, 5, "L", false, nil)
	}
	pdf.Ln(4)

	// 4. Phụ lục task
	sectionTitle(fmt.Sprintf("4. Phụ lục — danh sách %d task hoàn thành", len(d.Tasks)))
	tw := []float64{11, 35, 17, 19, 9, 11, 14, 12, 11, 18.5, 18.5, 14}
	pdfHeaders := []string{"#ID", "Tiêu đề", "Phụ trách", "Loại", "Size", "AI", "Est khách", "Est AI", "Cycle", "Start", "Done", "Bug PS"}
	headerRow(tw, pdfHeaders, 7.5)
	bodyFont(7.5)
	for _, t := range d.Tasks {
		drawGridRow(pdf, tw, taskRow(d, t), 4.2, "L", false, nil)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// drawGridRow vẽ một hàng bảng: mọi ô wrap text, chiều cao hàng = ô cao nhất,
// con trỏ luôn nhảy xuống ĐÚNG chiều cao hàng (tránh chữ đè lên nhau).
// cellStyle (nếu có) được gọi trước khi vẽ từng ô để đổi font/màu theo cột.
func drawGridRow(pdf *fpdf.Fpdf, widths []float64, cells []string, lineH float64, align string, fill bool, cellStyle func(i int)) {
	h := rowHeight(pdf, cells, widths, lineH)

	// Sang trang nếu hàng không còn chỗ (tự vẽ lại từ đầu trang mới).
	if pdf.GetY()+h > pdfBreakY {
		pdf.AddPage()
	}
	y := pdf.GetY()

	x := pdfMarginL
	mode := "D"
	if fill {
		mode = "FD"
	}
	for i, c := range cells {
		if cellStyle != nil {
			cellStyle(i)
		}
		pdf.Rect(x, y, widths[i], h, mode)
		// Căn giữa dọc khi ô ít dòng hơn hàng.
		cellH := float64(len(pdf.SplitText(c, widths[i]-2))) * lineH
		pdf.SetXY(x, y+(h-cellH)/2)
		pdf.MultiCell(widths[i], lineH, c, "", align, false)
		x += widths[i]
	}
	pdf.SetXY(pdfMarginL, y+h)
}

// rowHeight tính chiều cao hàng theo ô nhiều dòng nhất.
func rowHeight(pdf *fpdf.Fpdf, cells []string, widths []float64, lineH float64) float64 {
	max := 1
	for i, c := range cells {
		n := len(pdf.SplitText(c, widths[i]-2))
		if n > max {
			max = n
		}
	}
	return float64(max)*lineH + 1.6 // đệm trên/dưới nhẹ
}
