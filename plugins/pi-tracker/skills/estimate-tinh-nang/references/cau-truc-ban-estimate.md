# Cấu trúc `wbs-<slug>.json` và bản estimate

## `wbs-<slug>.json` — file duy nhất cần soạn tay

⚠️ **Tên file phải gắn slug của tính năng, đừng đặt là `wbs.json`.** Một file không gắn tên còn sót trong thư mục sẽ bị lần estimate sau dùng lại, đưa nguyên bầy đầu việc của tính năng khác vào bản mới. `estimate_calc.py` kiểm được id comparable có thật nhưng **không thể biết WBS này thuộc tính năng nào**, nên lưới an toàn đó không đỡ được lỗi này.

```json
{
  "feature": "<tên tính năng, ngắn, đúng cách khách gọi nó>",
  "customer": "<tên khách, để trống nếu là việc nền tảng>",
  "support_staff": "<nhân viên hỗ trợ — khối meta của mẫu Excel>",
  "product": "<sản phẩm, ví dụ BizChat>",
  "function_doc": "<chức năng, dòng meta cuối; bỏ trống thì lấy `feature`>",
  "prepared": "<YYYY-MM-DD>",
  "contingency_pct": 26,
  "assumptions": ["<điều kiện kiểm chứng được>"],
  "exclusions": ["<việc cố ý không bao>"],
  "items": [
    {
      "id": "B1",
      "group": "Xây dựng",
      "task": "<đầu việc, nói việc phải làm chứ không nhắc lại tên tính năng>",
      "desc": "<mô tả cho khách đọc — cột 'Tính năng' thứ hai của mẫu Excel>",
      "quote_days": 5,
      "internal_days": 3.5,
      "dev_count": 1,
      "level": "B2",
      "inherited": false,
      "comparables": [1, 4],
      "confidence": "high",
      "note": "<vì sao lệch khỏi mốc neo — bỏ được nếu không lệch>"
    }
  ]
}
```

### Từng trường

| Trường | Bắt buộc | Ghi chú |
|---|---|---|
| `feature` | nên có | Vào tiêu đề bản estimate. |
| `customer`, `prepared` | không | Vào dòng meta. |
| `support_staff`, `product`, `function_doc` | không | Chỉ dùng cho khối meta 4 dòng của bản `.xlsx` (xem phần mẫu Excel bên dưới). Bỏ trống thì ô để trắng — trừ `function_doc`, rơi về `feature`. |
| `contingency_pct` | **có** | Lấy từ `chi phi bug` của hạng mục trong baseline. Cố ý để 0 thì phải ghi lý do vào `assumptions`. |
| `assumptions` | nên có | Trống thì bản estimate tự in dòng "đây là thiếu sót". |
| `exclusions` | nên có | Chỗ ghi các loại việc ẩn cố ý không bao. |
| `items[].id` | **có** | Mã ngắn, duy nhất (`A1`, `B2`…). Hiện ở cột đầu bảng. |
| `items[].group` | **có** | Nhóm đầu việc. Thứ tự nhóm trong bảng theo lần xuất hiện đầu tiên. |
| `items[].task` | **có** | Một câu, nói việc phải làm. |
| `items[].desc` | không | Mô tả chi tiết cho khách đọc. Trong `.xlsx` là cột thứ hai dưới tiêu đề gộp "Tính năng"; trong `.md` nối vào sau tên đầu việc. |
| `items[].quote_days` | **có** | Effort **không có AI** → số cho người báo giá, và là số điền vào cột **ET** của mẫu Excel. Bội của 0,5. Phải > 0 trừ khi `inherited: true`. |
| `items[].internal_days` | **có** | Effort **có AI**, kỳ vọng nội bộ. Phải ≤ `quote_days`, nếu không script thoát 1. |
| `items[].dev_count` | không (mặc định 1) | Cột "Số lượng (Dev)". Số nguyên ≥ 1. **Không nhân vào ngày công** — ngày công vẫn là `quote_days`; cột này chỉ nói cần mấy người, phục vụ xếp lịch. |
| `items[].level` | không (mặc định `B2`) | Cột "Trình độ (Cấp bậc)". Text tự do — thang bậc là quy ước nhân sự, đổi theo thời gian, nên script không ép danh sách cố định. Để chuỗi rỗng là lỗi; bỏ hẳn trường này để lấy mặc định. |
| `items[].inherited` | không (mặc định `false`) | Cột "Có tính kế thừa" (dấu `x`). Đánh dấu đầu việc dùng lại nguyên phần đã làm. **Đây là trường hợp duy nhất được để `quote_days`/`internal_days` = 0.** Không có comparable thì script cảnh báo — kế thừa từ đâu phải chỉ ra được. |
| `items[].comparables` | **có** (mảng, được rỗng) | id task đã Done thật. Rỗng → `confidence` buộc phải là `low`. |
| `items[].confidence` | **có** | `high` (±15%) / `med` (±25%) / `low` (±40%). |
| `items[].note` | không | Vì sao lệch khỏi mốc neo. Nối sau `desc` trong **cả** `.md` và `.xlsx` — viết cho khách đọc được. |

### Nhóm đầu việc — gợi ý khung

Không phải khung cố định, nhưng thiếu nhóm nào thì phải trả lời được vì sao:

| Nhóm | Gồm gì |
|---|---|
| Phân tích & thiết kế | Chốt yêu cầu, thiết kế dữ liệu/luồng, wireframe |
| Xây dựng | Backend, tích hợp bên thứ ba, UI — mỗi kênh/hệ thống một đầu việc |
| Kiểm thử & bàn giao | Kiểm thử tích hợp, tài liệu, triển khai, hỗ trợ nghiệm thu |

`estimate_calc.py` quét cả `group` và `task` để tìm 5 loại việc ẩn (phân tích, thiết kế, kiểm thử, nghiệm thu/sửa lỗi, bàn giao/tài liệu/triển khai). Đặt tên nhóm bằng từ ngữ thông thường là quét ra; đặt tên sáng tạo ("Giai đoạn 1") thì cảnh báo nổ dù thực chất có đủ — lúc đó đọc lại xem có đúng là đủ không, rồi bỏ qua cảnh báo.

---

## Bản estimate `.md` — script sinh, không sửa tay

Sinh bằng `estimate_calc.py --out-md`. Sửa số trong file này là làm nó lệch khỏi `wbs-<slug>.json`; sửa WBS rồi chạy lại.

Thứ tự các phần đã được chọn có lý do:

1. **Tiêu đề + tổng + dải** — người đọc cần con số ở dòng đầu, không phải sau ba bảng.
2. **Ghi chú "ngày công, không phải báo giá, không phải ngày lịch"** — đặt ngay dưới tổng vì đây chính là chỗ bị đọc nhầm.
3. **Bảng đầu việc** — nhóm in đậm kèm tổng nhóm, đầu việc thụt vào, cột "Căn cứ" là id task đã làm. Cột căn cứ khiến bản estimate phản biện được: khách hỏi "sao 5 ngày" thì mở task #1 ra xem.
4. **Tổng công việc → Dự phòng → TỔNG** ba dòng riêng. Dự phòng là dòng nhìn thấy được để khách cắt được một cách có ý thức (xem Bẫy 4 trong SKILL.md).
5. **Giả định** rồi **Không bao gồm**.
6. `---` **rồi phần nội bộ.** Đường kẻ là chỗ cắt khi chuyển tài liệu cho khách.

### Phần nội bộ — cắt trước khi gửi khách

Gồm: kỳ vọng nội bộ (có AI), hệ số AI, biên dự kiến, bảng bao khách/nội bộ theo nhóm, và cảnh báo của script. Gửi phần này ra ngoài là trao toàn bộ biên AI cho khách.

### `estimate-<slug>.json`

Cùng dữ liệu ở dạng máy đọc — `by_group`, `subtotal`, `contingency`, `total`, `items`, `checks`. Dùng khi cần đối chiếu số hoặc so hai phương án phạm vi với nhau.

---

## Bản `.xlsx` — theo mẫu estimate của công ty

`--out-xlsx` sinh một sheet `Estimate` dựng theo đúng mẫu Excel công ty đang dùng, để bản này dán thẳng vào hồ sơ báo giá mà không phải gõ lại:

| Dòng | Nội dung |
|---|---|
| 1–4 | Khối meta nền vàng: **Khách hàng** / **Nhân viên hỗ trợ** / **Sản phẩm** / **Chức năng** (nhãn gộp `A:B`, giá trị gộp `C:G`) |
| 6–7 | Tiêu đề bảng hai tầng nền hồng: `STT` · `Tính năng` (gộp tên + mô tả) · `ET` (gộp *Thời gian triển khai (Ngày)* / *Số lượng (Dev)* / *Trình độ (Cấp bậc)*) · `Có tính kế thừa`. Đóng băng ở dòng 7. |
| 8… | Dòng nhóm (gộp `A:C`, tổng nhóm ở cột ET) rồi các đầu việc; `STT` đánh số chạy suốt qua các nhóm |
| cuối bảng | **Tổng công việc** → **Dự phòng** → **TỔNG** nền cam kèm chú thích *(không tính T7/CN)* |
| sau đó | Dòng dải tin cậy, rồi Giả định / Không bao gồm |

Ba điểm cố ý khác mẫu gốc, biết trước để khỏi tưởng là lỗi:

1. **Có dòng nhóm.** Mẫu gốc là bảng phẳng. Giữ nhóm vì WBS ở đây thường 10–15 đầu việc, phẳng hết thì không đọc ra được phần nào ăn bao nhiêu ngày. Xoá dòng nhóm trong Excel là ra đúng mẫu gốc.
2. **Dự phòng là một dòng riêng trước TỔNG.** Mẫu gốc chỉ có TỔNG. Xem Bẫy 4 trong SKILL.md: rải dự phòng ẩn vào từng đầu việc thì khách hỏi "sao việc này 5 ngày" là không giải thích được.
3. **Chú thích chỉ ghi "(không tính T7/CN)".** Mẫu gốc ghi thêm "và thời gian kiểm thử" — bản này thì kiểm thử **nằm trong** TỔNG (nó là một đầu việc trong WBS), nên giữ nguyên câu đó là nói sai về chính con số bên cạnh.

Cột **ET = `quote_days`** — effort không có AI. Đây là chỗ Bẫy 1 dễ vào nhất: `.xlsx` là bản dễ bị gửi tiếp nhất nên **sheet này không có phần nội bộ**, và `internal_days` không xuất hiện ở bất kỳ ô nào.

`dev_count` **không nhân vào** ET. Hai cột trả lời hai câu khác nhau: ET là ngày công, `dev_count` là cần mấy người — nhân chúng với nhau là đếm khối lượng hai lần.
