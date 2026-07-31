# Cấu trúc bản Excel ngắn gọn — TỔ CHỨC THEO HẠNG MỤC

Sinh bằng `scripts/pi_excel.py`, đọc **duy nhất** `metrics.json`. Không sửa file .xlsx
bằng tay — muốn đổi số thì sửa tag trong PI Tracker rồi fetch lại và chạy lại.

**Nguyên tắc tổ chức: mỗi hạng mục một sheet.** Người mở file phải trả lời được ngay
hai câu cho từng hạng mục: *hạng mục này làm được những việc gì*, và *AI giúp giảm
effort / tăng năng suất bao nhiêu*. Không được quay lại kiểu tổ chức theo loại số liệu
(một sheet năng suất, một sheet task) — mở ra sẽ không thấy được từng hạng mục làm gì.

Phân vai với bản .md: **Excel = số theo hạng mục, Markdown = nhận định.** Excel chỉ chứa
ba loại chữ — tiêu đề, ghi chú về cách đọc số, và tên task. Mọi phân tích xu hướng, đánh
giá nút thắt, khuyến nghị đều nằm ở bản .md.

---

## Số sheet

`1 (Tổng quan) + số hạng mục có task + 2 (Bug toàn team, Tồn đọng)`

Workspace có N tag → `N + 3` sheet. Thứ tự: Tổng quan · một sheet cho từng tag (xếp
theo ngày công giảm dần) · Bug toàn team · Tồn đọng.

**Không có trần số sheet.** Tag tăng thì sheet tăng theo — đó là chủ ý: hạng mục do người
dùng tự đặt trong PI Tracker, skill không giới hạn. Sheet sinh **động theo dữ liệu**: chỉ
tag có `n > 0`, xếp theo effort thực tế giảm dần, nhóm `(chua gan tag)` luôn xuống cuối vì
nó không phải một hạng mục thật mà là dữ liệu còn thiếu.

---

## Sheet 1 — Tổng quan

- **CHỈ SỐ CHÍNH TOÀN TEAM** — 12 dòng. Ba dòng effort xếp theo thứ tự
  `báo khách → dự kiến có AI → thực tế`, rồi bốn dòng dẫn xuất đánh dấu `→`
  (tiết kiệm, giảm effort, tăng năng suất, hệ số). Thứ tự này để người đọc thấy được
  con số tiết kiệm đến từ đâu, không phải một số rơi từ trên trời.
- Ghi chú quy đổi ra **nhân sự · tháng** (mốc 21 ngày công/người/tháng).
- **TỪNG HẠNG MỤC LÀM ĐƯỢC BAO NHIÊU — AI GIẢM EFFORT BAO NHIÊU** — 12 cột, có dòng
  TỔNG. Kèm ghi chú tự động chỉ ra hạng mục AI phát huy tốt nhất / kém nhất theo hệ số.

  ⚠️ Câu kết luận này **chỉ xét hạng mục có từ `MAU_TOI_THIEU` = 5 task trở lên**, và
  ghi rõ đã loại hạng mục nào. Bản đầu tiên từng kết luận "AI phát huy tốt nhất ở <một
  hạng mục> (4.0×)" trong khi hạng mục đó chỉ có **1 task** — hệ số vô nghĩa. Đừng bỏ
  ngưỡng này. Nhóm `(chua gan tag)` cũng bị loại khỏi kết luận.

- **TỔNG QUAN CÔNG VIỆC THEO HẠNG MỤC** — bảng để **đọc một lần là biết mỗi hạng mục
  làm gì, phục vụ ai, phạm vi đến đâu**. Một dòng chính cho mỗi tag + một dòng phụ ghi
  phổ loại việc:

Bố cục (giá trị trong khung là **placeholder**, không phải dữ liệu mẫu cần khớp):

  ```
  Hạng mục   Task Bug Thực tế Giảm effort  Đầu việc chính                        Nhân sự
  <tag>       N    B    X       P%        • <DD/MM> — <việc đã làm> cho          <người 1>,
                                            <khách>: <phạm vi> (#<id>, <n> ngày) <người 2>,
                                          • <DD/MM–DD/MM> — <đợt việc gộp>       <người 3>
                                            (#<id>, #<id> — <n> ngày)
                                          • Còn lại <N> task nhỏ (<X> ngày): …
                                            Loại việc: <n> theo plan · <n> bug
  ```

  ### Cột "Đầu việc chính" — script KHÔNG sinh được cột này

  ⚠️ **Đây là văn bản tổng hợp do người viết soạn, nạp qua `--highlights highlights-<MM>-<YYYY>.json`.**
  Một câu như *"bàn giao workflow X cho khách Y: chuẩn hoá Z"* đòi hiểu nội dung task rồi
  diễn đạt lại — Python không làm được bằng regex trên `title`/`description`.
  Quy tắc soạn nằm ở **Bước 3c trong SKILL.md**.

  Tên file **gắn tháng**, vì tra cứu chỉ theo tên tag mà tag tồn tại xuyên tháng — file
  không gắn tháng còn sót lại sẽ bị tháng sau dùng lại. `check_highlights()` trong
  `pi_excel.py` đối chiếu id/tag với `metrics.json` và dừng (exit 1) nếu lệch.

  Định dạng file:

  ```json
  { "<tên tag>": ["DD/MM — việc đã làm ... (#id, #id — X ngày)", "Còn lại N task nhỏ (Y ngày): …"] }
  ```

  **Thiếu `--highlights` thì cột này rớt về liệt kê tiêu đề** (dạng `#<id> <tiêu đề>… · #<id> <tiêu đề>…`)
  — vẫn chạy, không lỗi, nhưng mất hẳn phần tổng hợp. Đây là **fallback có chủ ý**: nếu mở
  Excel thấy cột chỉ là chuỗi tiêu đề nối nhau thì nghĩa là quên truyền file, không phải
  script hỏng.

  Chiều cao dòng **tự tính theo số dòng** trong ô (≈15pt/dòng, chặn trên 320pt) — bản
  tổng hợp dài không bị cắt mất. Đừng đặt lại chiều cao cứng.

  ### Khách hàng / phạm vi

  Tên khách hàng nằm trong **mô tả** task, thường không có ở tiêu đề — nên nó vào bảng này
  qua bản tổng hợp, không phải qua việc script đọc tiêu đề. Ví dụ tháng 07/2026: Phúc Anh
  (#4), Sohagame (#32), Vietravel (#8), visa Trung Quốc (#34) đều **chỉ xuất hiện trong mô
  tả**. Chi tiết đầy đủ hơn nằm ở cột `Mục tiêu / phạm vi` của sheet từng hạng mục.

  Bảng cũ ở vị trí này từng có cột `Khách / phạm vi` nhập tay trong `nhom-cong-viec.json`
  — file đó đã xóa. **Đừng dựng lại file map thủ công**; nguồn đúng là mô tả task.

- **MỖI HẠNG MỤC AI LÀM GÌ** — cấp gộp thứ hai, trả lời "ai đang gánh mảng nào".
  Mỗi hạng mục một dòng in đậm, dưới đó là **từng người** thụt lề, xếp theo ngày công
  giảm dần. Một người chiếm gần hết một hạng mục là rủi ro single point of failure —
  bảng này để lộ ra điều đó.

  Số liệu lấy từ `by_tag_person` — **tổng tính trong `pi_report.py`**, không tính trong
  Excel, để bản .md dùng lại được đúng con số đó. Nhóm `(chua gan tag)` hiện bằng style
  `minor` (chữ mờ) để phân biệt với hạng mục thật.

  Trong một hạng mục thì các dòng người **cộng lại đúng** bằng dòng in đậm. Nhưng cộng
  dồn CÁC HẠNG MỤC với nhau thì không bằng tổng toàn team nếu có task gắn nhiều tag —
  phần đếm trùng in ở ghi chú dưới bảng (`tag_overlap` trong `metrics.json`).

- **THEO NHÂN SỰ** — cùng bộ cột, để lộ ai đang gánh phần lớn effort.
- **⚠ GIỚI HẠN CỦA PHÉP ĐO** — khối cảnh báo bắt buộc (xem bên dưới).
- Ghi chú: hạng mục lấy trực tiếp từ trường "Phân loại tag" — dữ liệu thật do người làm task gán, không phải suy luận.

## Sheet 2..n — Một sheet cho mỗi hạng mục

Đây là phần cốt lõi. Thứ tự các mục có chủ ý:

1. **Dòng tóm tắt ở header** — `<N>/<M> task hoàn thành · X ngày công (Y% effort toàn
   team) · tiết kiệm Z ngày (hệ số H×) · B bug`.

2. **AI GIÚP GIẢM EFFORT & TĂNG NĂNG SUẤT BAO NHIÊU** — **bảng NGANG**, mỗi người một
   dòng, dòng cuối `TỔNG CẢ HẠNG MỤC` chính là số liệu của cả hạng mục:

   ```
   Người        Task Bug Est khách Est AI Thực tế Tiết kiệm Giảm effort Tăng NS Hệ số
   <người 1>      n   b     …       …      …        …         …%        +…%     …×
   <người 2>      n   b     …       …      …        …         …%        +…%     …×
   TỔNG CẢ HM     N   B     …       …      …        …         …%        +…%     …×
   ```

   ⚠️ **Phải là bảng ngang, không phải danh sách dọc label/value.** Bản đầu tiên xếp dọc
   và bị bó chữ không đọc được: cột A của sheet này rộng 6 (dành cho ID task) nên nhãn
   `"Effort báo khách (không AI)"` bị xuống 4–5 dòng, block 9 chỉ số chiếm gần hết màn hình.

   Vì cột A hẹp và cột B rộng 50 (tiêu đề task), bảng này **gộp A:B làm cột nhãn**, số liệu
   từ cột C trở đi. Đừng để nhãn ở riêng cột A.

   Gộp luôn bảng theo nhân sự vào đây thay vì tách hai block — dòng TỔNG đã là số của hạng
   mục nên không cần lặp lại. Đây cũng là chỗ lộ ra nguyên nhân khi một hạng mục có hệ số
   thấp: thường không phải do mảng đó "khó AI hoá" mà do một người chưa áp dụng.

   Hạng mục (tag) chỉ có **1 người** thì bỏ dòng người (trùng dòng TỔNG), chỉ ghi
   `TỔNG CẢ HẠNG MỤC (tên người)`.

   Dòng chú thích dưới bảng gánh phần chữ dài: ý nghĩa 3 trường effort, công thức giảm
   effort, tỷ lệ áp dụng AI, và độ chính xác estimate AI (sai lệch % + bao nhiêu task
   đúng/nhanh/chậm) của riêng hạng mục đó.

4. **CÔNG VIỆC HOÀN THÀNH** — trả lời "làm được những việc gì". Xếp theo ngày công giảm
   dần, có AutoFilter, dòng TỔNG.

   Cột **`Mục tiêu / phạm vi (từ mô tả task)`** rút từ `description` qua `mo_ta_ngan()`:
   bỏ ký tự markdown (`##`, bullet, `**`) rồi cắt ~200 ký tự theo ranh giới từ. Đây là
   chỗ người đọc thấy được **khách hàng nào, chạm vào module nào** — thông tin mà tiêu đề
   task thường không có. Đừng bỏ cột này để tiết kiệm chỗ. Mỗi task có đủ `est khách / est AI / thực tế / tiết
   kiệm / hệ số / dùng AI` → thấy được AI tác động ở **mức từng task**, không chỉ mức
   tổng. Cột `Dùng AI` ghi **"KHÔNG"** tô vàng khi task không dùng AI, để không bị lẫn
   vào đám "có".

   Không gom task theo hạng mục nghiệp vụ (workflow / hạ tầng / bảo mật) trong Excel —
   việc gom đó là suy luận của người viết, script không làm được đáng tin. Cột `Loại`
   (Theo plan / Phát sinh theo plan / Phát sinh (bug)) là dữ liệu thật, dùng nó.

5. **BUG PHÁT SINH** — bug của riêng hạng mục đó, kèm % effort của hạng mục. Nếu không
   có bug thì **vẫn hiện mục này** với ghi chú nhắc rằng 0 bug có thể chỉ nghĩa là chưa
   lên production / chưa có tải thật, không phải đã ổn định.

6. **TỒN ĐỌNG** — chỉ gọi là "đang treo" khi thật sự có task chưa xong. Nếu chỉ có task
   chuyển tiếp thì tiêu đề đổi thành "CHUYỂN TIẾP THÁNG SAU — không có task tồn đọng".
   Đừng ghi "0 ngày công đang treo", đọc lên gây hiểu sai.

## Sheet Bug toàn team

Mức độ (Critical/Major/Minor kèm ngày công) → **bug theo hạng mục** → danh sách đầy đủ
có AutoFilter, xếp Critical → Major → Minor rồi effort giảm dần. Hai cột
**Phát hiện / Đóng** để đọc được thời gian xử lý.

## Sheet Tồn đọng

Bản tổng hợp xuyên hạng mục. Blocker trống hiển thị **"KHÔNG GHI BLOCKER"** — cố ý viết
hoa để lộ ra task treo mà không ai theo.

---

## Khối cảnh báo bắt buộc

Khối **⚠ GIỚI HẠN CỦA PHÉP ĐO** ở sheet Tổng quan **không được bỏ**, kể cả khi con số
đẹp. Nội dung sinh tự động từ `control_group_no_ai`, chốt bằng câu về cách phát biểu ra
ngoài team: nói *"làm nhanh hơn mốc báo khách N×"*, không nói *"AI tăng năng suất N%"*.

---

## Định dạng

Xử lý trong `scripts/xlsx_writer.py`, khai báo tập trung ở `NUM_FORMATS` / `FONTS` /
`FILLS` / `XFS`. Thêm style mới thì thêm vào `XFS`, đừng hard-code trong `pi_excel.py`.

| Loại số | Style | Hiển thị |
|---|---|---|
| Ngày công | `num` | `125.75` (2 chữ số thập phân) |
| Phần trăm | `pct` | `37.4%` |
| % tăng năng suất | `gain` | `+59.7%` (có dấu) |
| Hệ số | `mult` | `1.60×` |
| Số nguyên | `int` | `67` |

### Chỉ một cột phần trăm effort — đừng thêm cột thứ hai

Mọi bảng chỉ có **một** cột phần trăm về effort: **`Giảm effort`** = (est khách − thực tế) ÷
est khách của chính dòng đó. Định nghĩa này giống nhau ở mọi bảng, mọi cấp (toàn team,
hạng mục, nhân sự) nên số ở các bảng khác nhau đối chiếu được với nhau.

**Đã từng có thêm cột `Tỷ trọng effort`** (= thực tế của hạng mục ÷ thực tế toàn team) đặt
ngay cạnh — và bị đọc lẫn ngay: cùng một hạng mục ra `giảm effort 40%` ở bảng trên nhưng
`tỷ trọng 45%` ở bảng dưới, người đọc tưởng script tính sai. Hai số có **mẫu số khác nhau**
(est khách của chính hạng mục đó vs thực tế toàn team) nên không bao giờ trùng được.

Đã bỏ cột tỷ trọng khỏi toàn bộ Excel. Nếu cần nói mảng nào chiếm bao nhiêu nguồn lực thì
**viết bằng lời ở bản .md**, đừng đưa lại thành cột phần trăm trong cùng bảng.

Giá trị phần trăm **lưu dạng 37.4, không phải 0.374** — number format tự thêm ký tự `%`.
Ô đó không dùng trực tiếp trong công thức nhân được; đây là đánh đổi có chủ ý để số liệu
khớp đúng `metrics.json`.

Dòng TỔNG: nền xanh nhạt + in đậm, style suy ra từ style cột qua `TOTAL_STYLE`.

Tên sheet **là tên tag nguyên văn** — tag do người dùng đặt trong PI Tracker nên đã có
dấu sẵn, không cần map. Riêng ký tự `/` không hợp lệ trong tên sheet Excel nên
`xlsx_writer` tự thay bằng `-` (tag `A/B` → sheet `A-B`). Bảng `SHEET_NAME` trong
`pi_excel.py` hiện **rỗng**, chỉ giữ lại làm chỗ sửa vài trường hợp tên đặc biệt nếu cần.

---

## Kiểm tra sau khi sinh

`python3` mặc định không có openpyxl, nhưng **python3.11 trên máy này có** — dùng làm bộ
kiểm tra độc lập. Kiểm tra quan trọng nhất là **đối chiếu dòng TỔNG của từng sheet giải
pháp với `metrics.json`** (tổng công việc hoàn thành = `actual` trừ phần task Blocked):

```bash
python3.11 -c "
import openpyxl, json
m=json.load(open('metrics.json'))
wb=openpyxl.load_workbook('bao-cao-thang-<MM>-<YYYY>.xlsx')
for tag,v in m['by_tag'].items():
    if not v['n']: continue
    w=wb[tag.replace('/','-')]
    for r in w.iter_rows():
        if isinstance(r[1].value,str) and 'task hoàn thành' in str(r[1].value):
            act=r[6].value; break
    blocked=sum(x['actual'] for x in m['unfinished'] if tag in x['tags'])
    print('OK ' if abs(act-(v['actual']-blocked))<1e-9 else 'SAI', tag, act)
"
```

Kiểm tra thêm bằng LibreOffice nếu cần:

```bash
libreoffice --headless --convert-to csv bao-cao-thang-<MM>-<YYYY>.xlsx --outdir /tmp
```
