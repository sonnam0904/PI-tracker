# Cấu trúc bản Markdown đầy đủ

Bám theo khung này. Số liệu lấy nguyên từ `metrics.json` / `tables.md` do `pi_report.py` sinh ra.

Đây là bản **đầy đủ**, đi kèm bản Excel ngắn gọn (`references/cau-truc-bao-cao-excel.md`).
Phân vai: Excel trả lời *bao nhiêu*, Markdown trả lời *điều đó có nghĩa gì*. Bản .md vẫn
phải có bảng số — người đọc .md không nên bị buộc mở Excel — nhưng giá trị của nó nằm ở
các mục "Đánh giá" và "Khuyến nghị", những thứ không đưa vào Excel.

---

## Header

```
# Báo cáo công việc tháng <MM>/<YYYY>

**Workspace:** <tên> · **Nguồn:** PI Tracker · **Kỳ:** <dd/mm> – <dd/mm> · **Phạm vi:** <N> task

> Lưu ý: hạng mục lấy từ trường "Phân loại tag" của task trong PI Tracker.
> <Nêu số task chưa gắn tag và số task gắn nhiều tag nếu có.>
```

## 1. Tổng quan

Bảng 1 từ script — **theo hạng mục (tag)**, không phải theo giải pháp (task / done / bug / est khách / est AI / thực tế / tiết kiệm / năng suất / tỷ trọng).

Kèm 3–5 dòng đọc bảng: hạng mục nào chiếm phần lớn nguồn lực, có lệch so với định hướng không, tỷ lệ hoàn thành ra sao. Nếu có task gắn nhiều tag thì nói rõ tỷ trọng cộng lại vượt 100%.

## 2. Năng suất nhờ AI

Phần này người đọc quan tâm nhất — đặt ngay sau tổng quan, đừng chôn xuống cuối.

Cần có:
- Tổng ngày công tiết kiệm được, quy đổi ra ý nghĩa (tương đương bao nhiêu nhân sự · tháng, lấy ~21 ngày công/người/tháng).
- Nêu **cả hai** cách phát biểu: giảm X% effort ⟺ tăng Y% năng suất (hệ số Z×).
- Bảng so sánh mức tăng năng suất **giữa các hạng mục** — chỉ ra mảng nào AI phát huy tốt nhất, mảng nào kém và tại sao. Bỏ qua hạng mục dưới 5 task khi kết luận (hệ số vô nghĩa về thống kê).
- Độ chính xác estimate AI: bao nhiêu task đúng / nhanh hơn / chậm hơn dự kiến, sai lệch tổng bao nhiêu %. Kết luận: estimate đã đủ tin để cam kết với khách chưa.
- Tỷ lệ áp dụng AI + **giới hạn của phép đo** khi nhóm đối chứng quá nhỏ.

## 3..N. Từng hạng mục

**Một mục cho MỖI hạng mục có task**, xếp theo effort giảm dần — số mục thay đổi theo số tag, không cố định 4. Lấy danh sách ở khóa `tags` trong `metrics.json`. Trong mỗi mục:

- Một dòng tóm tắt: `<N> task, <X> ngày công, hoàn thành <n>/<N>, nhân sự: <...>` (lấy từ `by_tag` và `by_tag_person`)
- **Công việc hoàn thành** — nhóm theo hạng mục nếu >8 task, mỗi dòng: `**#id** Mô tả ngắn (người, số ngày)`. Ưu tiên task effort lớn lên trước; task nhỏ gom một dòng. **Mô tả ngắn viết từ `description` của task, không copy nguyên tiêu đề** — tiêu đề thường thiếu phạm vi.
- **Khách hàng / phạm vi** — nêu khách hàng mà hạng mục này phục vụ và phạm vi hệ thống bị chạm. Lấy từ `description`, xem Bước 3b trong SKILL.md. Không có khách cụ thể thì ghi "việc nền tảng / dùng chung".
- **Năng suất:** tiết kiệm bao nhiêu ngày, hệ số bao nhiêu, so với mức trung bình toàn team thì cao hay thấp.
- **Ai gánh:** từ `by_tag_person` — một người chiếm gần hết một hạng mục là rủi ro phải nêu tên.
- **Bug:** số lượng, mức độ, effort. Ghi "không có bug" nếu không có — đó cũng là thông tin.
- **Tồn đọng:** task Blocked/chưa xong, đã bỏ bao nhiêu công, vướng gì.
- **Đánh giá:** 2–3 câu. Đây là phần có giá trị nhất của mục.

## Bug phát sinh toàn team

Bảng 4 từ script, xếp theo mức độ giảm dần.

Kèm nhận định: tổng effort xử lý bug và tỷ lệ trên tổng effort; bug tập trung ở tầng/hạng mục nào; thời gian xử lý bug Critical; có liên hệ nào giữa cụm bug và các task hạ tầng đã làm không.

## Chuyển tiếp sang tháng sau

Task Blocked cần gỡ + task đã lên lịch nhưng chưa bỏ công (mục `carryover` trong metrics.json).

## Khuyến nghị

3–5 mục, mỗi mục gắn với một con số cụ thể trong báo cáo. Không viết khuyến nghị chung chung.

---

## Lỗi hay mắc

- Nhầm "% giảm effort" với "% tăng năng suất" — hai con số khác nhau, xem SKILL.md.
- Cộng tay số liệu thay vì lấy từ script → sai lệch, và mỗi bảng ra một kết quả khác nhau.
- **Tự cộng tổng ngày công của từng nhóm task trong mục "Công việc hoàn thành" rồi ghi sai.** Lỗi này đã xảy ra thật hai lần; lần thứ hai các sai số triệt tiêu nhau nên **tổng chung vẫn đúng mà từng nhóm thì sai** — kiểm tổng chung không bắt được. Phải kiểm TỪNG nhóm bằng script theo Bước 6 trong SKILL.md, đối chiếu với `by_tag[...]['actual']`.
- Số ở bản .md lệch số ở bản .xlsx. Cả hai phải cùng đọc từ một `metrics.json`; nếu lệch thì gần như chắc chắn là do tự cộng tay ở bản .md.
- Tính cả task đã lên lịch nhưng chưa bỏ công nào vào tỷ lệ hoàn thành → kéo tụt số liệu sai. Script đã tách sẵn vào `carryover`.
- Trình bày mức tăng năng suất như số liệu đo được, trong khi nó là so với estimate của con người.
- **Viết tên khách hàng / phạm vi chỉ dựa vào tiêu đề task.** Tiêu đề hay viết tắt hoặc bỏ hẳn tên khách — ví dụ một task chỉ ghi "Workflow visa <tên khách>" trong khi mô tả nói rõ đó là chatbot tư vấn visa Trung Quốc. Phải đọc `description`; xem Bước 3b trong SKILL.md.
- Viết đều tay cho mọi hạng mục dù có mảng gần như trống — phải nêu rõ khoảng trống đó.
- Cộng dồn các hạng mục rồi gọi là tổng toàn team. Sai khi có task nhiều tag — tổng lấy ở dòng "Tổng (toàn team)" Bảng 1.
- Bỏ qua nhóm `(chua gan tag)`. Nếu nhóm này nặng ngày công thì báo cáo đang thiếu dữ liệu, phải nói ra.
