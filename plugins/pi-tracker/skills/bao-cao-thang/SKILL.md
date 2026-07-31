---
name: bao-cao-thang
description: Tạo báo cáo công việc hằng tháng từ PI Tracker, gộp theo trường "Phân loại tag" của task — mỗi tag là một hạng mục công việc, công việc hoàn thành, năng suất tăng nhờ AI và số ngày công tiết kiệm, bug phát sinh. Sinh ra 2 file: bản Excel ngắn gọn để gửi/trình bày và bản Markdown đầy đủ kèm nhận định. Dùng khi người dùng yêu cầu "báo cáo tháng", "tổng hợp công việc tháng", "báo cáo theo hạng mục / theo tag", "report PI tracker", "xuất báo cáo excel", hoặc hỏi về năng suất AI / ngày công tiết kiệm của team.
---

# Báo cáo tháng theo hạng mục (PI Tracker)

Báo cáo này trả lời đúng 3 câu hỏi, theo thứ tự ưu tiên:

1. **Mỗi hạng mục công việc hoàn thành được những gì trong tháng?**
2. **AI giúp tăng bao nhiêu năng suất và tiết kiệm bao nhiêu ngày công?**
3. **Phát sinh bao nhiêu bug, nghiêm trọng đến đâu, tốn bao nhiêu công xử lý?**

Mọi thứ khác là phụ. Không biến báo cáo thành bản liệt kê task.

## Hạng mục = tag của task, không có danh sách cố định

⚠️ **Không có danh sách hạng mục nào viết cứng trong skill hay trong script.** Hạng mục lấy nguyên từ trường **"Phân loại tag"** của task trong PI Tracker. Thêm/bớt/đổi tên tag trong tracker thì báo cáo đổi theo — không phải sửa script.

Hệ quả về cách làm việc: **muốn báo cáo chia mảng khác đi thì sửa tag trong PI Tracker, không sửa skill.** Đây là dữ liệu thật do người làm task gán, nên skill không được đoán thay — kể cả khi thấy task chưa gắn tag, đừng suy hạng mục từ tiêu đề.

**Mỗi lần chạy skill này phải sinh ra ĐỦ HAI file** trong thư mục làm việc:

| File | Vai trò |
|---|---|
| `bao-cao-thang-<MM>-<YYYY>.xlsx` | **Bản ngắn gọn để gửi/trình bày.** Số liệu thuần, **tổ chức theo hạng mục — mỗi tag một sheet**, sinh tự động bằng script, không viết tay. |
| `bao-cao-thang-<MM>-<YYYY>.md` | **Bản đầy đủ.** Toàn bộ phần nhận định, phân tích xu hướng, rủi ro, khuyến nghị. |

Excel trả lời *bao nhiêu*; Markdown trả lời *điều đó có nghĩa gì*. Đừng nhét nhận định dài vào Excel, cũng đừng bỏ bảng số khỏi Markdown.

⚠️ **Bản Excel phải tổ chức theo hạng mục, không theo loại số liệu.** Mỗi sheet hạng mục trả lời đúng hai câu: hạng mục đó **làm được những việc gì**, và **AI giúp giảm effort / tăng năng suất bao nhiêu** (cả ở mức tổng và mức từng task). Kiểu tổ chức cũ — một sheet năng suất, một sheet danh sách task phẳng — đã bị bác vì mở ra không thấy được từng mảng làm gì.

---

## Vị trí file khi chạy

Skill này được đóng gói trong plugin `pi-tracker` (marketplace `pi-tracker`, repo `PI-tracker`), nên được gọi bằng `/pi-tracker:bao-cao-thang`. `${CLAUDE_PLUGIN_ROOT}` trong các lệnh dưới đây là thư mục plugin đã cài, và **trỏ vào đâu thì tùy cách cài**:

| Cách cài | `${CLAUDE_PLUGIN_ROOT}` trỏ tới |
|---|---|
| `marketplace add <đường dẫn local>` | **chính repo** — sửa file là có hiệu lực ngay |
| `marketplace add owner/repo` (GitHub) | bản clone/cache trong `~/.claude/plugins/` |

Skill này **không còn file dữ liệu nào cần giữ giữa các tháng**. Toàn bộ việc phân loại nằm ở tag trong PI Tracker, nên `${CLAUDE_PLUGIN_ROOT}` trỏ vào repo hay cache đều không ảnh hưởng số liệu — chỉ là đường dẫn tới script.

---

## Quy trình

### Bước 0 — Kiểm tra môi trường (làm TRƯỚC mọi thứ)

Cả 3 script của skill chạy bằng `python3`. Thiếu nó thì skill không chạy được.

⚠️ **Đừng nhầm: `python3` KHÔNG cần cho MCP.** Các tool MCP `pi-tracker` (`list_people`, `list_tags`, `get_session`, `get_task`) gọi trực tiếp vẫn chạy bình thường mà không có Python. Thứ cần Python là `pi_fetch.py` — **đường duy nhất an toàn để lấy `list_tasks`** mà không vỡ hạn mức token (lý do đầy đủ ở Bước 1). Nên thiếu Python không phải mất một tiện ích, mà là **mất đường lấy dữ liệu gốc**. Thấy tool MCP vẫn gọi được rồi kết luận "khỏi cần Python" là đi đúng vào đường sai bị cấm ở cuối bước này.

**Chạy lệnh này trước tiên.** Nó vừa kiểm có Python chưa, vừa cho biết phải gọi bằng tên nào:

```bash
for c in python3 python "py -3"; do
  if $c -c 'import sys; assert sys.version_info >= (3, 6)' 2>/dev/null; then
    echo "PY_OK: dùng \"$c\" — $($c -V 2>&1)"; break
  fi
done
```

Script chỉ dùng f-string, không dùng API nào mới hơn, nên **Python 3.6+ là đủ**.

- In ra `PY_OK: …` → sang Bước 1, và dùng **đúng chuỗi lệnh nó báo** thay cho `python3` ở mọi bước sau (Windows đặt tên là `python` hoặc `py -3`, không phải `python3`).
- **Không in gì** → **dừng skill**, bảo người dùng chạy **`/pi-tracker:setup`** để cài. Command đó lo toàn bộ việc dò trình quản lý gói, xin phép và cài — đừng tự làm lại ở đây. Xong thì chạy lại skill.

#### Chưa có Python thì DỪNG — và tuyệt đối không đi đường vòng

Đây là chỗ dễ sai nhất của cả skill. Các tool MCP đang nằm sẵn trong context, nên nước đi "tự nhiên" là gọi `list_tasks` trực tiếp rồi tự gõ `tasks.json`. **Cấm.** Bốn việc tuyệt đối không làm:

- **KHÔNG** gọi `list_tasks` trực tiếp rồi tự dựng `tasks.json` — xem Bước 1: tốn ~1600 lần context, và một `actualDays` chép sai chảy thẳng vào báo cáo mà **không phép kiểm nào bắt được**.
- **KHÔNG** đọc file tạm harness đổ ra để dựng lại dữ liệu.
- **KHÔNG** viết lại script sang ngôn ngữ khác giữa lúc chạy.
- **KHÔNG** bỏ bước Excel để "ít nhất có được bản Markdown".

**Không có báo cáo còn tốt hơn một báo cáo sai mà trông như đúng** — vì `metrics.json` luôn tự nhất quán với đầu vào, kể cả đầu vào đã sai.

### Bước 1 — Lấy dữ liệu

⚠️ **ĐỪNG gọi tool MCP `list_tasks` trực tiếp — nó chắc chắn vỡ hạn mức token.** Mỗi task mang đủ 27 field, kể cả `description` (chiếm ~77% payload). Đo thực tế trên workspace 70 task: **81,796 ký tự → harness từ chối** và đổ ra file tạm, tái hiện 2/2 lần. Workspace lớn hơn thì càng vỡ.

`list_tasks` **bắt buộc phải giới hạn phạm vi** — gọi không tham số sẽ báo lỗi. Hai cách: `month="YYYY-MM"` (hoặc `from`/`to`), hoặc `all=true` để cố ý lấy trọn workspace. Ràng buộc này chặn việc vô tình kéo cả lịch sử về, nhưng **không** giải quyết vấn đề token ở trên: một tháng bận vẫn thừa sức vỡ hạn mức.

Lấy dữ liệu task bằng script — **nó cũng chính là MCP**, gọi đúng `tools/call` → `list_tasks` trên đúng server đó, rồi ghi nguyên văn bytes nhận được ra file (đã kiểm: hash trùng khớp từng byte với kết quả tool MCP):

```bash
cd <thư mục làm việc>
python3 "${CLAUDE_PLUGIN_ROOT}/skills/bao-cao-thang/scripts/pi_fetch.py" --out tasks.json --people people.json
```

Script tự dò endpoint từ `.mcp.json` hoặc `~/.claude.json`, in ra 3 dòng trạng thái (~26 token). Nếu báo lỗi kết nối → app PI Tracker chưa chạy, hỏi người dùng thay vì đoán số liệu.

**Mặc định script gửi `all=true`** (lấy trọn workspace) vì báo cáo cần cả task gác kỳ và dữ liệu tháng trước để so sánh xu hướng. Thêm `--month YYYY-MM` nếu chỉ cần đúng một tháng và workspace đã lớn — nhưng nhớ rằng `pi_report.py` tự lọc theo `--month` rồi, nên lọc sớm ở đây chỉ để giảm tải, không đổi số liệu của tháng đó.

**Các tool MCP còn lại thì gọi trực tiếp bình thường** — payload nhỏ: `list_people` (map assigneeId → tên), `list_tags` (xem workspace có hạng mục nào), `get_session` (xác nhận workspace), `get_task` (đọc sâu một task cụ thể).

Đừng tìm cách lách hạn mức bằng cách đọc lại file tạm mà harness đổ ra, hay bằng phân trang qua context. Cả hai đều dẫn tới việc **tự gõ lại JSON để dựng `tasks.json`** — tốn ~1600 lần context, và một `actualDays` chép sai sẽ chảy thẳng vào `metrics.json` rồi vào báo cáo mà không phép kiểm nào bắt được, vì `metrics.json` vẫn tự nhất quán với đầu vào đã sai. Dữ liệu gốc phải đi từ MCP ra file không qua tay bạn.

### Bước 2 — Tính chỉ số

**Không bao giờ tự cộng tay các con số này.** Luôn chạy script — nó là nguồn duy nhất cho mọi số trong báo cáo:

```bash
python3 "${CLAUDE_PLUGIN_ROOT}/skills/bao-cao-thang/scripts/pi_report.py" \
  --tasks tasks.json --people people.json --month <YYYY-MM> \
  --out-json metrics.json --out-md tables.md
```

⚠️ **`<YYYY-MM>` là chỗ phải thay, đừng để lệnh nào trong skill này mang tháng cụ thể.** Chép nguyên một tháng cũ là lỗi **âm thầm**: chạy vào tháng 08 với `--month 2026-07` thì script sinh ra báo cáo tháng 7 hoàn chỉnh, không cảnh báo gì, vì nó không thể biết bạn định làm tháng nào. Còn để nguyên placeholder thì script dừng ngay với `Khong co task nao trong thang <YYYY-MM>` (exit 1) — sai kiểu ồn ào, sửa được. Mọi tên file dưới đây cũng vậy: `<MM>-<YYYY>` phải thay theo kỳ đang làm.

Script in ra 6 bảng dựng sẵn + ghi `metrics.json` chứa toàn bộ số liệu chi tiết. Đọc `metrics.json` để lấy dữ liệu viết phần nhận định.

Mỗi task trong `metrics.json` có **cả `title` và `description`** (mô tả nguyên văn, giữ markdown). Xem Bước 3b bên dưới — đây là chỗ hay bị làm sai.

**Hạng mục lấy trực tiếp từ trường `tags` của task** — script không có danh sách hạng mục nào. Xem Bước 3.

### Bước 3 — Rà soát tag trước khi viết

Hạng mục lấy từ tag nên **không còn bước suy luận phân loại nào**. Thay vào đó phải đọc hai mục cảnh báo trong output:

- **"Cần gán tag"** — task chưa gắn tag nào, bị gom vào `(chua gan tag)` và tô xám trong Excel. Gắn tag cho chúng **trong PI Tracker**, rồi fetch lại dữ liệu và chạy lại script. Đừng viết báo cáo khi mục này còn task nặng ngày công — một hạng mục "(chưa gắn tag)" chiếm 20% effort thì báo cáo mất nghĩa.
- **"Task gắn nhiều tag"** — xem mục ⚠ ngay dưới.

⚠️ **Task gắn nhiều tag được đếm vào MỌI tag của nó, nên tổng các hạng mục LỚN HƠN tổng thật.** Đây là hệ quả có chủ ý (một task có thể vừa là `Hạ tầng` vừa là `Concurrency`), nhưng nó phá vỡ phép cộng. Script tính phần đếm trùng tường minh ở `tag_overlap` trong `metrics.json`:

```
sum(by_tag[*].actual) == total.actual + tag_overlap.days
```

Khi viết báo cáo: **số tổng toàn team lấy ở dòng "Tổng (toàn team)" của Bảng 1**, không bao giờ cộng dồn các dòng hạng mục lại. Cột "Tỷ trọng" vì vậy cũng có thể cộng lại vượt 100% — nói rõ điều này nếu tháng đó có task nhiều tag.

Về việc đặt tên tag (làm trong PI Tracker, không phải ở đây):

- **Đặt theo hạng mục nghiệp vụ**, không theo trạng thái hay loại task. Ví dụ tốt: `Kênh Zalo`, `Hạ tầng & hiệu năng chat`, `Cổng thanh toán`.
- **Bug gắn đúng tag chức năng của nó**, không tạo tag "Xử lý bug" riêng. Số bug đã có ở chiều khác (trường `type`), nên gộp lại mới thấy được điều đáng chú ý — ví dụ hạng mục hạ tầng có 9 task trong đó 5 là bug, tức đợt refactor làm lộ ra một loạt lỗi.
- **Tránh tag quá thô và tag quá vụn.** Tag trùng đúng tên sản phẩm/nhóm lớn (kiểu `BizChat`, `BizAI`) làm Bảng 1 và Bảng 3 gần như trùng nhau, không thấy được bên trong mỗi mảng làm gì; tag chỉ dùng cho 1 task thì bảng tổng hợp lại thành bản liệt kê task. Nếu thấy hiện tượng này, **nói ra trong báo cáo** kèm đề xuất chia tag lại.
- Tên tag không phân biệt chữ hoa/thường: `Hạ tầng` và `hạ tầng` là cùng một tag.

### Bước 3b — Xác định khách hàng & phạm vi: đọc CẢ mô tả, không chỉ tiêu đề

⚠️ **Không bao giờ suy tên khách hàng hay phạm vi công việc chỉ từ tiêu đề task.** Tiêu đề thường viết tắt hoặc bỏ hẳn thông tin này; mô tả mới là chỗ ghi đủ.

> **Bảng dưới là VÍ DỤ MINH HOẠ** lấy từ dữ liệu tháng 07/2026 để cho thấy vấn đề, **không phải danh sách cần tra cứu**. Task id và tên khách sẽ khác ở mỗi tháng — đừng dùng lại chúng, hãy đọc `description` của tháng đang làm.

| Task | Tiêu đề nói | Mô tả nói thêm |
|---|---|---|
| #34 | "Workflow visa minh quang" | chatbot tư vấn **visa Trung Quốc** trên LangGraph Studio, mục tiêu chốt lead |
| #4 | "Refactor workflow tư vấn bán laptop" | **phạm vi**: Knowledge Hub, tối ưu logic tìm kiếm, nâng chất lượng kiểm thử |
| #12 | "Thêm tính năng cho khách Trống Đồng" | phạm vi: copy tin nhắn, gửi file qua livechat + Zalo cá nhân |

Trong ví dụ trên, nếu chỉ đọc tiêu đề thì task đầu sẽ bị ghi thành "workflow cho Minh Quang" — mất hẳn việc đây là chatbot visa Trung Quốc.

Cách làm:

1. Với mỗi hạng mục, đọc `description` của các task nặng ngày công nhất trong đó.
2. Rút ra: **khách hàng nào** (tên riêng), **phạm vi gì** (module/kênh/hệ thống nào bị chạm).
3. Task không nêu khách → là việc nền tảng / dùng chung, ghi thẳng như vậy, đừng gán bừa một khách.

Mô tả viết bằng markdown (`## Mục tiêu`, `## Phạm vi`) nên đọc trực tiếp được cấu trúc — dùng đúng phần "Phạm vi" nếu task có ghi.

### Bước 3c — Tổng hợp "đầu việc chính" cho bảng Tổng quan (`highlights-<MM>-<YYYY>.json`)

Bảng **TỔNG QUAN CÔNG VIỆC THEO HẠNG MỤC** ở sheet Tổng quan có cột "Đầu việc chính". Cột này **không phải danh sách tiêu đề task** — nó là **văn bản tổng hợp do bạn viết**, vì script không thể gộp nhiều task thành một câu có nghĩa.

Sau khi đọc mô tả ở Bước 3b, ghi ra `highlights-<MM>-<YYYY>.json` theo kỳ đang làm.

⚠️ **Tên file phải gắn tháng — đừng đặt là `highlights.json`.** Tra cứu trong Excel chỉ theo **tên tag**, mà tag là cấp workspace (`BizChat`, `BizAI`…) nên tồn tại xuyên tháng và khớp khóa y hệt tháng sau. Một file không gắn tháng còn sót trong thư mục sẽ được tháng sau dùng lại, đưa nguyên bầy việc cũ (kèm ngày `07/07` và id task cũ) vào báo cáo mới. `pi_excel.py` nay đối chiếu id/tag với `metrics.json` và dừng với exit code 1 nếu lệch, nhưng đặt tên đúng thì không phải dựa vào lưới an toàn đó.

**Cấu trúc file** — một khóa cho mỗi tag có task, lấy tên tag từ khóa `tags` trong `metrics.json`:

```json
{
  "<tên tag lấy từ metrics.json>": [
    "<DD/MM> — <việc đã làm xong> cho <khách nếu mô tả có nêu>: <phạm vi> (#<id>, <N> ngày)",
    "<DD/MM–DD/MM> — <đợt việc gộp nhiều task> (#<id>, #<id>, #<id> — <N> ngày)",
    "Còn lại <N> task nhỏ (<X> ngày): <liệt kê ngắn các mảng việc>"
  ]
}
```

<details>
<summary><b>Ví dụ minh hoạ</b> — dữ liệu tháng 07/2026, KHÔNG phải mẫu cố định, đừng dùng lại các dòng này</summary>

```json
{
  "BizChat": [
    "07/07 — Bàn giao workflow tư vấn bán laptop cho khách Phúc Anh: chuẩn hoá Knowledge Hub + tài liệu Agent (#4, 6 ngày)",
    "16–23/07 — Đợt refactor hạ tầng chat hướng mục tiêu C10k: shard pool, Kafka workers 1000→10000, gộp 3 consumer (#72, #70, #68, #66 — 4.5 ngày)",
    "Còn lại 16 task nhỏ (17.0 ngày): giao diện & trải nghiệm chat, sửa lỗi kênh Facebook/bot.ai"
  ]
}
```

</details>

Quy tắc viết mỗi dòng:

1. **Bắt đầu bằng ngày** — lấy từ `done` (doneDate) của task, dạng `DD/MM`. Gộp nhiều task thì ghi khoảng `DD/MM–DD/MM`.
2. **Nói việc đã làm xong, không nhắc lại tiêu đề.** Sai: copy nguyên tiêu đề task. Đúng: nêu kết quả + khách + phạm vi, ví dụ "Bàn giao workflow tư vấn bán laptop cho khách Phúc Anh: chuẩn hoá Knowledge Hub + tài liệu Agent".
3. **Nêu tên khách hàng khi mô tả task có** — đây là lý do phải làm Bước 3b trước. Không có khách thì đừng bịa.
4. **Gộp task liên quan thành một dòng** kèm danh sách id và tổng ngày, dạng `(#id, #id, #id — N ngày)`. Đây chính là phần "tổng hợp"; liệt kê từng task một là làm sai mục đích bảng.
5. **4–9 dòng mỗi hạng mục.** Ít hơn thì mất thông tin, nhiều hơn thì ô Excel cao quá không còn là "đọc nhanh".
6. **Dòng cuối phải là "Còn lại N task nhỏ (X ngày): …"** để phần không nêu tên không bị ẩn đi. Bắt buộc — nếu thiếu, người đọc tưởng bảng đã kể hết.
7. Task đang Blocked thì ghi một dòng `⚠ Còn treo: …` để nó không lẫn vào việc đã xong.

⚠️ **Kiểm số trước khi đưa vào Excel.** Mọi con số ngày trong file highlights là do bạn cộng tay nên phải verify — đây là chỗ đã sai thật (ghi 5 ngày trong khi thực tế 4.5). `pi_excel.py` chỉ kiểm id/tag có tồn tại, **không kiểm số ngày** — phép kiểm đó là ở đây:

```bash
python3 -c "
import json, re
m = json.load(open('metrics.json')); hl = json.load(open('highlights-<MM>-<YYYY>.json'))
t = {x['id']: x for x in m['tasks']}
for tag, lines in hl.items():
    named = set()
    for line in lines:
        ids = [int(i) for i in re.findall(r'#(\d+)', line)]
        named |= set(ids)
        mm = re.search(r'—\s*([\d.]+)\s*ngày\)', line)   # dong co TONG nhieu task
        if mm and len(ids) > 1:
            real = round(sum(t[i]['actual'] for i in ids), 2)
            if abs(float(mm.group(1)) - real) > 1e-9:
                print('SAI', tag, '| ghi', mm.group(1), '| thuc', real, '|', line[:60])
    allids = {x['id'] for x in m['tasks'] if tag in x['tags']}
    rest = allids - named
    d = round(sum(t[i]['actual'] for i in named), 2)
    dr = round(sum(t[i]['actual'] for i in rest), 2)
    ok = abs(d + dr - m['by_tag'][tag]['actual']) < 1e-9
    print(f'{tag}: neu ten {len(named)}/{len(allids)} = {d}n, con lai {len(rest)} = {dr}n, phu du: {ok}')
"
```

Dòng nào in `SAI` thì sửa file highlights, đừng sửa script. `phu du: False` nghĩa là có task bị đếm hai lần hoặc bỏ sót.

### Bước 4 — Sinh bản Excel ngắn gọn

Chạy sau khi đã rà soát tag ở Bước 3 (để Excel không mang nhóm "(chưa gắn tag)" nặng ngày công):

```bash
python3 "${CLAUDE_PLUGIN_ROOT}/skills/bao-cao-thang/scripts/pi_excel.py" \
  --metrics metrics.json --highlights highlights-<MM>-<YYYY>.json \
  --out bao-cao-thang-<MM>-<YYYY>.xlsx --workspace BizchatAI
```

⚠️ **Thiếu `--highlights` thì cột "Đầu việc chính" rớt về liệt kê tiêu đề task** — vẫn chạy, không lỗi, nhưng mất hẳn phần tổng hợp. Nếu mở Excel thấy cột đó chỉ là chuỗi `#4 Refactor… · #34 Workflow…` thì nghĩa là quên truyền file.

Nếu script dừng với `highlights... KHONG khop metrics.json` thì đang truyền file của tháng khác (hoặc gõ sai id) — soạn lại cho đúng kỳ, đừng bỏ `--highlights` để lách.

Script đọc **duy nhất** `metrics.json` nên số liệu trong Excel và trong Markdown luôn khớp nhau. Không sửa Excel bằng tay — sửa tag trong PI Tracker rồi fetch lại và chạy lại.

Sinh ra `1 + <số tag có task> + 2` sheet: Tổng quan · một sheet cho **mỗi tag** · Bug toàn team · Tồn đọng. Số tag tăng thì số sheet tăng theo, không có trần. Chi tiết từng sheet ở `references/cau-truc-bao-cao-excel.md`.

Kiểm tra sau khi sinh — đối chiếu dòng TỔNG của từng sheet hạng mục với `metrics.json` (script kiểm tra có trong file reference trên). Sai lệch nghĩa là script hỏng, không phải làm tròn.

**Không cần cài thư viện.** `scripts/xlsx_writer.py` tự ghi định dạng .xlsx bằng stdlib (`zipfile` + XML), vì `python3` mặc định trên máy là externally-managed (PEP 668) nên không cài được openpyxl. Đừng thay bằng openpyxl/pandas.

### Bước 5 — Viết bản Markdown đầy đủ

Theo cấu trúc ở `references/cau-truc-bao-cao.md`. Ghi ra file `bao-cao-thang-<MM>-<YYYY>.md` trong thư mục làm việc.

Trước khi ghi: nếu thư mục đã có file báo cáo cùng tháng mà **không phải do phiên này tạo ra**, hỏi người dùng muốn ghi đè hay tạo file mới — đừng tự ghi đè công việc của phiên trước.

### Bước 6 — Không tự cộng tay tổng theo hạng mục

Trong bản Markdown, phần "Công việc hoàn thành" gom task thành hạng mục kèm tổng ngày công. **Đừng tự cộng — lấy nguyên số từ Bảng 1 / Bảng 3 (`by_tag`, `by_tag_person` trong `metrics.json`).** Đây là chỗ đã sai thật hai lần: một lần lệch 4 tổng nhóm (nhóm nặng nhất lệch 5 ngày), một lần các sai số triệt tiêu nhau nên tổng vẫn đúng mà từng nhóm thì sai — **kiểm tổng chung không bắt được lỗi này, phải kiểm từng nhóm**.

⚠️ **Tổng các hạng mục KHÔNG bằng tổng toàn team** khi có task gắn nhiều tag. Bất biến đúng là:

```
sum(by_tag[*].actual) == total.actual + tag_overlap.days
```

Kiểm lại bằng script khi thấy số đáng ngờ:

```bash
python3 -c "
import json; m=json.load(open('metrics.json'))
s = sum(v['actual'] for v in m['by_tag'].values())
ov = m['tag_overlap']
print('tong cac hang muc :', round(s, 2))
print('tong toan team    :', ov['real_actual'])
print('dem trung         :', ov['days'], f\"({ov['tasks']} task nhieu tag)\")
print('khop?             :', round(s, 2) == round(ov['real_actual'] + ov['days'], 2))
"
```

Nếu dòng cuối in `False` thì script hỏng — báo ra, đừng tự chỉnh số trong Markdown cho khớp.

Và khi tự gom nhóm nhỏ trong phần "Công việc hoàn thành", kiểm từng nhóm bằng chính id đã dùng:

```bash
python3 -c "
import json; m=json.load(open('metrics.json')); t={x['id']:x for x in m['tasks']}
def s(*ids): return round(sum(t[i]['actual'] for i in ids), 2)
TAG = m['tags'][0]                       # doi thanh ten hang muc dang kiem
print('nhom A', s(*[]))                  # dien id thuc te cua tung nhom vao day
print('tong hang muc', TAG, m['by_tag'][TAG]['actual'])
"
```

Trong báo cáo, mỗi khi nêu tổng theo hạng mục mà tháng đó có task nhiều tag, **phải nói kèm phần đếm trùng**. Tuyệt đối không cộng dồn các hạng mục rồi gọi đó là tổng toàn team — số đó lấy ở dòng "Tổng (toàn team)" Bảng 1.

---

## Định nghĩa chỉ số — không được nhầm lẫn

PI Tracker có 3 trường effort. Hiểu đúng ý nghĩa là điều kiện tiên quyết:

| Trường | Ý nghĩa |
|---|---|
| `estimateCustomerDays` | Effort báo khách — mốc chuẩn nếu làm **không có AI** |
| `estimateAiDays` | Effort dự kiến khi **có AI hỗ trợ** |
| `actualDays` | Effort **thực tế** đã bỏ ra |

Từ đó:

- **Ngày công tiết kiệm** = `estimateCustomerDays − actualDays`
- **% giảm effort** = tiết kiệm / `estimateCustomerDays`
- **% tăng năng suất** = (`estimateCustomerDays` / `actualDays` − 1) × 100

⚠️ **CHỈ DÙNG MỘT chỉ số phần trăm về effort trong bảng: "giảm effort".** Không đặt cột "tỷ trọng effort" (= thực tế của hạng mục ÷ thực tế toàn team) cạnh cột "giảm effort" — hai cột đều có chữ "effort" nhưng **mẫu số khác nhau** nên không bao giờ trùng, và đã bị đọc lẫn thật (một hạng mục: giảm effort 40.5% vs tỷ trọng 44.5% → tưởng script tính sai). Bảng 1 do script sinh có cả hai cột này (Giảm effort và Tỷ trọng) — khi trích vào bản .md thì **chỉ mang một cột**, và nếu mang cột Tỷ trọng thì phải nói rõ nó cộng lại có thể vượt 100% do task nhiều tag.

⚠️ **Hai chỉ số cuối KHÁC NHAU và hay bị dùng lẫn.** Ví dụ số học: 200 ngày báo khách làm hết 125 ngày thực tế → **giảm 37.5% effort** ⟺ **tăng 60% năng suất** (hệ số 1.6×). Cùng một sự thật, hai cách phát biểu. Trong báo cáo hãy nêu **cả hai** kèm hệ số nhân, đừng chọn con số to hơn rồi gọi nhầm tên.

- **Độ chính xác estimate AI** = (`actualDays` − `estimateAiDays`) / `estimateAiDays`. Dương = chậm hơn dự kiến. Đây là chỉ số cho biết có nên tin estimate để cam kết với khách hay không.
- **Tỷ lệ áp dụng AI** = số task `aiUsed` / tổng task.

**Cảnh báo về nhóm đối chứng:** khi gần như 100% task đều dùng AI, không có nhóm đối chứng thật — mức "tăng năng suất" là so với *estimate báo khách*, tức một con số ước lượng của con người, không phải đo lường có kiểm soát. Script trả về `control_group_no_ai`; nếu `n < 5` thì phải nói rõ giới hạn này trong báo cáo thay vì trình bày con số như một kết quả đo được.

---

## Hạng mục có những gì — tra lúc chạy, không viết cứng

**Skill này không giữ danh sách hạng mục.** Muốn biết tháng này có những hạng mục nào:

- Gọi tool MCP `list_tags` (nếu session đã nạp) — trả về toàn bộ tag của workspace.
- Hoặc đọc `metrics.json`: khóa `tags` là danh sách tag xuất hiện trong kỳ, đã xếp theo ngày công giảm dần. Đây chính là mục lục của báo cáo.

Khi viết phần nhận định cho một hạng mục, **suy nghĩa của nó từ danh sách task bên trong** (Bảng 5 trong `tables.md`), đừng dựa vào giả định về tên tag. Tag `Hạ tầng` ở workspace này có thể mang nghĩa khác hẳn ở workspace khác.

---

## Nguyên tắc khi viết

**Trung thực về khoảng trống dữ liệu.** Nếu một hạng mục gần như không có task (ví dụ 1.5 ngày công trên tổng 120), đừng viết cho đủ mục. Nêu thẳng con số và đặt câu hỏi: mảng này bị tạm gác, công việc có chạy nhưng không ghi vào tracker, hay chỉ là task không được gắn tag? Một báo cáo bỏ sót công việc thật còn tệ hơn một báo cáo nói rõ là mình không chắc.

**Mỗi hạng mục cần một câu kết luận, không chỉ bảng số.** Bảng cho biết *bao nhiêu*; phần viết phải trả lời *điều đó có nghĩa gì* — đang tăng tốc hay chững lại, nút thắt nằm ở đâu, ai đang gánh.

**Bug phải đọc được thành xu hướng.** Không dừng ở đếm số. Cụm bug tập trung ở đâu (tầng nào, nửa đầu hay nửa cuối tháng), có liên quan tới task hạ tầng nào không, thời gian xử lý trung bình bao lâu.

**Nêu tên rủi ro nhân sự khi thấy.** Một người gánh phần lớn task hoặc toàn bộ bug Critical là single point of failure — phải viết ra, kể cả khi tháng đó mọi việc vẫn xong.

**Khách hàng và phạm vi phải lấy từ mô tả task, không đoán từ tiêu đề.** Xem Bước 3b. Viết "phục vụ khách X" khi mô tả có nêu; không có thì ghi là việc nền tảng, đừng suy diễn.

**Đề xuất phải gắn với số liệu trong báo cáo.** Không viết khuyến nghị chung chung kiểu "cần cải thiện quy trình".

**Xưng hô:** báo cáo viết bằng tiếng Việt, giọng trung tính, dùng "team" thay vì "anh/chị em". Không dùng emoji trừ dấu cảnh báo cho mục Blocked.
