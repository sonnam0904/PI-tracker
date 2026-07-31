# Plugin `pi-tracker`

Plugin Claude Code đi kèm repo này, chứa các skill làm việc với dữ liệu PI Tracker.

## Skill

| Skill | Mục đích |
|---|---|
| [`bao-cao-thang`](skills/bao-cao-thang/SKILL.md) | Tạo báo cáo công việc hằng tháng, **gộp theo trường "Phân loại tag" của task** — sinh ra bản Excel ngắn gọn (mỗi tag một sheet) và bản Markdown đầy đủ. |

## Cài đặt

Repo này đồng thời là một plugin marketplace (khai báo ở [`.claude-plugin/marketplace.json`](../../.claude-plugin/marketplace.json)). Từ trong Claude Code:

```
/plugin marketplace add sonnam0904/PI-tracker
/plugin install pi-tracker@pi-tracker
```

Đang phát triển tại máy thì trỏ vào thư mục local để không phải push mỗi lần sửa:

Ví dụ:
```
/plugin marketplace add /home/sonnn/Work/task-manager
```

Sau khi cài, gọi skill bằng `/pi-tracker:bao-cao-thang` — tiền tố `pi-tracker` là tên plugin (hoặc để Claude tự chọn khi bạn yêu cầu "báo cáo tháng").

## Lưu ý khi phát triển

- Script Python chỉ dùng stdlib — `python3` trên máy là externally-managed (PEP 668) nên không cài được openpyxl. `scripts/xlsx_writer.py` tự ghi `.xlsx` bằng `zipfile` + XML.
- **Hạng mục công việc lấy từ trường "Phân loại tag" của task trong PI Tracker.** Không có danh sách hạng mục nào viết cứng trong script — thêm/bớt tag trong tracker là báo cáo đổi theo.
- `references/` giờ chỉ còn tài liệu cấu trúc báo cáo — **không còn file dữ liệu phân loại nào**. `overrides.json` và `nhom-cong-viec.json` đã xóa cùng lúc bỏ trục "giải pháp".
- Cơ chế đã bỏ, đừng dựng lại: bộ từ khóa `SOLUTIONS`, tiền tố `[Chatbot]` trong tiêu đề, nhánh phân loại mặc định.
- `.gitignore` ở repo root ignore `*.md`; đã có ngoại lệ `!plugins/**/*.md` để SKILL.md và các file reference được commit.
