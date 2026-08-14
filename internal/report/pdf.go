package report

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
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
//
// Bản toàn team (d.AssigneeName == "") in tiếp báo cáo riêng của TỪNG thành
// viên, mỗi người bắt đầu ở một trang mới — xem Data.Members.
func BuildPDF(d Data) ([]byte, error) {
	d.SortTasks()

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes("dejavu", "", fontRegular)
	pdf.AddUTF8FontFromBytes("dejavu", "B", fontBold)
	pdf.SetMargins(pdfMarginL, 12, 10)
	pdf.SetAutoPageBreak(true, 14)
	pdf.AddPage()
	writePDFReport(pdf, d)

	for _, mem := range d.Members {
		mem.SortTasks()
		pdf.AddPage()
		writePDFReport(pdf, mem)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writePDFReport vẽ trọn một báo cáo (tiêu đề + mục 1-4) vào trang hiện tại của
// pdf. Dùng chung cho bản team và bản của từng thành viên.
func writePDFReport(pdf *fpdf.Fpdf, d Data) {
	// Tiêu đề
	pdf.SetFont("dejavu", "B", 15)
	// Tiêu đề và dòng phạm vi có chứa tên người → phải qua pdfText như ô bảng.
	pdf.CellFormat(pdfPageW, 9, pdfText(d.title()), "", 1, "C", false, 0, "")
	pdf.SetFont("dejavu", "", 8.5)
	pdf.SetTextColor(110, 110, 110)
	pdf.CellFormat(pdfPageW, 5, pdfText(fmt.Sprintf("Ngày xuất: %s  ·  %s  ·  Cửa sổ tính: %s → %s  ·  Số liệu tính đến hết ngày %s",
		time.Now().Format("02/01/2006 15:04"), d.scopeLabel(),
		d.Metrics.MonthStart.Format("02/01/2006"), d.Metrics.MonthEnd.AddDate(0, 0, -1).Format("02/01/2006"),
		d.AsOf.Format("02/01/2006"))),
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
		pdf.MultiCell(pdfPageW, 5.6, pdfText("•  "+line), "", "L", false)
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

	// 4. Phụ lục task thường — bug tách xuống mục 5.
	plain, bugs := splitTasks(d.Tasks)
	sectionTitle(fmt.Sprintf("4. Phụ lục — danh sách %d task hoàn thành", len(plain)))
	headerRow(pdfTaskColWidths, pdfTaskHeaders, 7.5)
	bodyFont(7.5)
	for _, t := range plain {
		drawGridRow(pdf, pdfTaskColWidths, taskRow(d, t), 4.2, "L", false, nil)
	}
	pdf.Ln(4)

	// 5. Bug: bảng riêng, cột riêng (mức độ / cách đóng / task gốc).
	sectionTitle(fmt.Sprintf("5. Bug phát sinh đã xử lý (%d bug — không tính vào T/CT/PI)", len(bugs)))
	if len(bugs) == 0 {
		bodyFont(9)
		pdf.MultiCell(pdfPageW, 5.6, "Không có bug nào hoàn thành trong kỳ.", "", "L", false)
		return
	}
	headerRow(pdfBugColWidths, pdfBugHeaders, 7.5)
	bodyFont(7.5)
	for _, t := range bugs {
		drawGridRow(pdf, pdfBugColWidths, bugRow(d, t), 4.2, "L", false, nil)
	}
}

// Header + độ rộng cột của hai bảng phụ lục trong .pdf. Nhãn viết tắt hơn bản
// .xlsx vì khổ A4 ngang chỉ có pdfPageW mm cho cả bảng, nhưng THỨ TỰ và SỐ cột
// phải khớp taskHeaders / bugHeaders — drawGridRow ghép cells[i] với widths[i]
// nên lệch một cột là cả bảng lệch mà không có lỗi nào báo ra.
//
// Tổng mỗi bộ độ rộng phải đúng pdfPageW; chỗ dư sau khi bỏ hai cột estimate
// (xem taskHeaders) dồn cho Tiêu đề / Phụ trách / Tag — các cột wrap nhiều dòng
// nhất. Cột "Bug PS" / "Task gốc" rộng hơn các cột số vì chứa danh sách ID
// ("#89, #91"). TestPDFColWidths giữ hai bất biến này.
var (
	pdfTaskHeaders   = []string{"#ID", "Tiêu đề", "Phụ trách", "Loại", "Size", "AI", "Cycle", "Start", "Done", "Bug PS", "Tag"}
	pdfTaskColWidths = []float64{11, 43, 20, 18, 8, 10, 10, 15, 15, 18, 22}

	pdfBugHeaders   = []string{"#ID", "Tiêu đề", "Phụ trách", "Mức độ", "Size", "AI", "Cycle", "Start", "Done", "Task gốc", "Tag", "Cách đóng"}
	pdfBugColWidths = []float64{10, 40, 18, 16, 8, 9, 10, 13, 13, 16, 20, 17}
)

// pdfText làm sạch chuỗi trước khi đưa vào fpdf. BẮT BUỘC với mọi chuỗi do người
// dùng nhập (tiêu đề task, tên người, tên tag…).
//
// fpdf tra bảng độ rộng ký tự bằng một mảng 65536 phần tử, đánh chỉ số THẲNG
// bằng code point (fpdf.SplitText, fpdf.generateCIDFontMap). Mọi rune ngoài BMP
// — emoji là ca thường gặp nhất — làm nó PANIC "index out of range", chứ không
// phải trả lỗi: một task tên "Sửa lỗi 🚀" là đủ để sập app lúc xuất PDF.
//
// Font DejaVu nhúng kèm cũng không có glyph cho emoji, nên thay bằng "□" không
// mất thông tin nào vốn in được.
func pdfText(s string) string {
	return strings.Map(func(r rune) rune {
		if r > 0xFFFF {
			return '□'
		}
		return r
	}, s)
}

// drawGridRow vẽ một hàng bảng: mọi ô wrap text, chiều cao hàng = ô cao nhất,
// con trỏ luôn nhảy xuống ĐÚNG chiều cao hàng (tránh chữ đè lên nhau).
// cellStyle (nếu có) được gọi trước khi vẽ từng ô để đổi font/màu theo cột.
func drawGridRow(pdf *fpdf.Fpdf, widths []float64, cells []string, lineH float64, align string, fill bool, cellStyle func(i int)) {
	// Mọi ô của mọi bảng đều đi qua đây, nên đây là chỗ duy nhất cần làm sạch cho
	// phần bảng. Ghi ra slice mới, không sửa slice của người gọi.
	safe := make([]string, len(cells))
	for i, c := range cells {
		safe[i] = pdfText(c)
	}
	cells = safe

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
