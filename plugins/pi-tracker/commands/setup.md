---
description: Kiểm tra môi trường cho skill bao-cao-thang — dò python3 và cài nếu thiếu
allowed-tools: Bash, AskUserQuestion
---

# Chuẩn bị môi trường cho pi-tracker

Mục đích: xác nhận máy này chạy được `/pi-tracker:bao-cao-thang`, và cài `python3` nếu thiếu.

Cả 3 script của skill (`pi_fetch.py`, `pi_report.py`, `pi_excel.py`) chạy bằng `python3`. Chúng chỉ dùng thư viện chuẩn — **không cần `pip install` gì** — nên thứ duy nhất phải có là bản thân interpreter.

⚠️ **`python3` không cần cho MCP.** Các tool MCP `pi-tracker` gọi trực tiếp vẫn chạy bình thường không có Python. Thứ cần Python là `pi_fetch.py` — đường duy nhất an toàn để lấy `list_tasks` mà không vỡ hạn mức token. Nên đây không phải tiện ích tùy chọn.

## Bước 1 — Dò interpreter

```bash
for c in python3 python "py -3"; do
  if $c -c 'import sys; assert sys.version_info >= (3, 6)' 2>/dev/null; then
    echo "PY_OK: dùng \"$c\" — $($c -V 2>&1)"; break
  fi
done
```

Script chỉ dùng f-string, không dùng API nào mới hơn, nên **Python 3.6+ là đủ**. Đừng đòi bản mới nhất.

- **In ra `PY_OK: …`** → môi trường đã sẵn sàng. Báo lại cho người dùng dòng đó kèm nhắc: nếu tên lệnh không phải `python3` thì khi chạy skill phải dùng đúng tên đó. Rồi **dừng ở đây**, không làm gì thêm.
- **Không in gì** → chưa có Python hợp lệ, đi tiếp Bước 2.

## Bước 2 — Dò trình quản lý gói

```bash
for m in brew winget apt-get dnf pacman apk; do command -v $m >/dev/null && echo "có: $m"; done
```

Chọn theo thứ tự ưu tiên dưới đây — **ưu tiên loại không cần sudo**:

| Có | Lệnh cài | Cần sudo | Ai chạy |
|---|---|---|---|
| `brew` (macOS) | `brew install python` | không | agent, sau khi xin phép |
| `winget` (Windows) | `winget install -e --id Python.Python.3.12` | không | agent, sau khi xin phép |
| `apt-get` | `sudo apt-get install -y python3` | **có** | **người dùng tự chạy** |
| `dnf` | `sudo dnf install -y python3` | **có** | **người dùng tự chạy** |
| `pacman` | `sudo pacman -S --noconfirm python` | **có** | **người dùng tự chạy** |
| `apk` | `sudo apk add python3` | **có** | **người dùng tự chạy** |

Hai trường hợp riêng:

- **macOS không có brew:** `xcode-select --install` cũng đem `/usr/bin/python3` về.
- **Không quyền sudo, cũng không brew/winget:** `curl -LsSf https://astral.sh/uv/install.sh | sh` rồi `uv python install` — cài vào home, không cần root. Chỉ dùng khi mọi cách trên bất khả, vì nó thêm một công cụ nữa vào máy người dùng.

## Bước 3 — Xin phép rồi cài

Dùng `AskUserQuestion` **đúng một lần**. Đưa option cài lên đầu, hậu tố `(Recommended)`, và ghi rõ lệnh sẽ chạy trong phần mô tả:

- `Cài python3 bằng <lệnh> (Recommended)`
- `Để tôi tự cài`

Sau khi người dùng chọn:

- **Lệnh không cần sudo** (`brew` / `winget`) → agent chạy trực tiếp.
- **Lệnh cần sudo** → **agent KHÔNG chạy.** In lệnh ra cho người dùng tự chạy ở terminal của họ, rồi chờ họ xác nhận đã xong. Cài gói cấp hệ thống bằng `sudo` không phải việc agent tự quyết, kể cả khi đã được đồng ý về mặt nguyên tắc.
- **Người dùng chọn tự cài** → in lệnh, dừng, không hỏi lại.

Không dò được trình quản lý gói nào: hướng người dùng tới <https://www.python.org/downloads/> và dừng.

## Bước 4 — Kiểm lại

Chạy lại nguyên lệnh ở Bước 1. Chưa thấy `PY_OK` thì **báo là chưa xong** — đừng kết luận đã sẵn sàng chỉ vì lệnh cài chạy không lỗi.

⚠️ **Windows đặt tên lệnh là `python` hoặc `py`, KHÔNG phải `python3`.** Đây là chỗ hay tưởng cài thất bại: gói đã cài xong mà gõ `python3` vẫn ra `command not found`. Nếu Bước 4 báo `py -3` thì nói rõ với người dùng rằng khi chạy skill phải dùng `py -3` thay cho `python3`.

## Quy tắc đầu ra

- Kết thúc bằng một dòng trạng thái rõ ràng: **sẵn sàng** hay **chưa sẵn sàng**, kèm tên lệnh Python phải dùng.
- Command này **chỉ** lo môi trường. Không chạy báo cáo, không gọi tool MCP nào.
- Nhắc một câu: skill còn cần **app PI Tracker đang chạy**. Không cần kiểm ở đây — `pi_fetch.py` đã báo rõ nếu app chưa chạy hoặc thiếu cấu hình MCP.
