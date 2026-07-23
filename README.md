# Task Manager — PI Tracker

> Ứng dụng desktop quản lý task cho team dev và theo dõi **Performance Index (PI)** theo tháng.

---

## 🚀 Cài đặt

### Cách 1 — Tải bản build sẵn (khuyến nghị)

Tải file thực thi mới nhất cho hệ điều hành của bạn tại trang Release


Giải nén và chạy. Đặt file `.env` cùng thư mục với app để cấu hình DB / AI (xem [Cấu hình](#️-cấu-hình-env)). Mặc định dùng SQLite nên chạy được ngay không cần server DB.

### Cách 2 — Build từ source

**Yêu cầu môi trường:**

- Go ≥ 1.25 · Node.js ≥ 18 (npm)
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Linux (Ubuntu/Debian): `sudo apt install gcc libgtk-3-dev libwebkit2gtk-4.1-dev`

**Chạy & build:**

```bash
wails dev   -tags webkit2_41   # chạy dev, hot reload frontend
wails build -tags webkit2_41   # build → build/bin/task-manager
go test ./...                  # unit test metrics engine (PI, advice, cửa sổ tháng)
```

> Tag `webkit2_41` cần cho distro chỉ có WebKitGTK 4.1 (Ubuntu 24.04+). Distro còn WebKitGTK 4.0 thì bỏ tag này.

---



## ⚙️ Cấu hình (.env)

Sao chép `.env.example` → `.env`.

### Database

```env
DB_DRIVER=sqlite        # sqlite | postgres | mysql
DB_SQLITE_PATH=taskmanager.db

# Dùng cho postgres / mysql
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=secret
DB_NAME=taskmanager
DB_SCHEMA=              # (postgres) search_path; bỏ trống = public, tự tạo nếu chưa có
```

Mặc định dùng SQLite — không cần server DB. Schema tự migrate khi khởi động.

### Gợi ý estimate bằng AI (tùy chọn)

```env
# Bỏ trống = tắt. Bật bằng cách đặt provider + key.
AI_PROVIDER=openai      # anthropic | openai | qwen | deepseek | glm | gemini
AI_API_KEY=sk-...
AI_MODEL=               # bỏ trống = model mặc định của provider
AI_BASE_URL=            # chỉ đặt khi dùng proxy/endpoint riêng
```

Đa số provider (OpenAI, Qwen, Deepseek, GLM/z.ai, Gemini) dùng chung định dạng tương thích OpenAI; riêng Anthropic dùng Messages API — code tự chọn đúng đường đi theo `AI_PROVIDER`. Chưa cấu hình thì các nút AI tự ẩn.

---



## ✨ Tính năng



### 📊 Quản lý task — Gantt timeline

- Task hiển thị theo trục ngày trong tháng, có nút chuyển tháng (◀ / Tháng này / ▶).
- Thanh bar màu theo trạng thái: 🟩 Done · 🟦 In Progress · 🟥 Blocked · ⬜ Todo.
- Thanh **nét đứt** = task chưa xong, vẽ dự kiến từ Start date + estimate AI (tối thiểu 1 ngày).
- Nền sẫm cho Thứ 7/CN, vạch đỏ đánh dấu hôm nay, cột tên task ghim khi cuộn ngang.
- Bấm tên task hoặc thanh bar để mở modal sửa; nút **Xóa task** ở góc trên phải modal (bấm 2 lần để xác nhận).
- Task chưa có Start date vẫn hiện trên danh sách với ghi chú để xử lý.



### 💬 Checklist, bình luận & lịch sử hoạt động

- Mỗi task có **checklist** (thêm/tick/xóa, thanh tiến độ, badge ☑ x/y trên Kanban).
- **Bình luận** trong task, hiển thị chung một feed với **lịch sử hoạt động**: mọi thay đổi (tiêu đề, trạng thái, ngày, estimate, nhân sự…) được ghi lại dạng "Trường: cũ → mới" kèm **ai** thay đổi và **lúc nào**.
- "Ai" lấy từ **Người dùng hiện tại** chọn trong tab Cài đặt; tên được lưu cứng vào log nên lịch sử còn nguyên kể cả khi nhân sự bị xóa.



### 🤖 Phân tích task bằng AI (tùy chọn)

- Trong modal task, nút **✨ Phân tích bằng AI** gọi LLM để: (1) **viết lại yêu cầu thành mô tả chi tiết** (Mục tiêu / Phạm vi / Hạng mục / Tiêu chí hoàn thành) ghi thẳng vào ô **Mô tả**; (2) **tự tạo checklist todo** từ các hạng mục công việc; (3) đề xuất `Estimate AI`, `Estimate báo khách`, `Size` kèm lý do và độ tin cậy.
- Gợi ý estimate được **grounding** bằng các task Done gần đây của workspace (loại, size, estimate AI, effort thực, cycle) để bám lịch sử thật của team thay vì đoán.



### 📈 Dashboard & tư vấn đạt mục tiêu

- Stat cards T / CT / LT / WIP kèm diễn giải phép tính, PI hero với progress bar so với mục tiêu.
- Khi PI chưa đạt, tư vấn 2 hướng: **giữ CT** → cần thêm bao nhiêu task Done đến hết tháng, hoặc **giữ T** → cần giảm CT xuống bao nhiêu.
- Card **ROI ứng dụng AI**: tỉ lệ áp dụng AI, so cycle time trung bình nhóm dùng AI vs không AI, độ chính xác estimate theo từng nhóm.



### 📄 Xuất báo cáo (Excel / PDF)

- Nút **Xuất báo cáo** trên Dashboard, chọn `.xlsx` (excelize) hoặc `.pdf` (fpdf, nhúng font DejaVu hỗ trợ tiếng Việt).
- Nội dung theo tháng đang xem: bảng chỉ số vs baseline kèm chênh lệch %, kết luận mục tiêu PI + hành động cần làm, hiệu quả ứng dụng AI bằng số liệu, và phụ lục danh sách task hoàn thành.

---



## 📐 Chỉ số & Performance Index

Tính theo tháng dương lịch (mùng 1 → hết tháng), đơn vị **tháng chuẩn = 4 tuần = 28 ngày**:


| Chỉ số          | Công thức                                           | Ý nghĩa                       |
| --------------- | --------------------------------------------------- | ----------------------------- |
| Throughput (T)  | số task Done **tích lũy** ÷ (số ngày cả tháng ÷ 28) | task/tháng của team, cộng dồn |
| Cycle Time (CT) | trung bình (Done − Start − Blocked)                 | ngày/task                     |
| Lead Time (LT)  | trung bình (Done − Ngày tạo)                        | ngày, từ lúc tạo đến lúc xong |
| WIP             | số task In Progress / Blocked                       | trạng thái hiện tại           |


```
capacity = 2    # mức trần tối đa
PI = min( (T / (T_baseline × số người)) × (CT_baseline / CT), capacity )
```

