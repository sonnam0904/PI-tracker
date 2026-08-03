---
name: estimate-tinh-nang
description: Estimate khối lượng công việc (ngày công) cho một tính năng khách yêu cầu, để bộ phận kinh doanh có số liệu effort mà lập báo giá. Chia yêu cầu thành các đầu việc, neo từng đầu việc vào task tương đương ĐÃ LÀM THẬT trong PI Tracker, rồi sinh bảng đầu việc + ngày công kèm dải tin cậy, giả định và phần không bao gồm. Dùng khi người dùng nói "estimate tính năng này", "báo khách bao nhiêu ngày công", "khách hỏi làm mất bao lâu", "chia đầu việc để báo giá", "effort thực hiện tính năng X", "WBS", hoặc cần con số ngày công cho một yêu cầu chưa làm. KHÔNG tự lập báo giá — không đơn giá, không tiền.
---

# Estimate khối lượng công việc cho tính năng khách yêu cầu

Skill này trả lời đúng một câu hỏi: **tính năng khách yêu cầu tốn bao nhiêu ngày công, chia thành những đầu việc nào?**

Đầu ra phục vụ người lập báo giá. Nó **không phải** báo giá.

## Ranh giới — làm gì và tuyệt đối không làm gì

| Skill này làm | Skill này KHÔNG làm |
|---|---|
| Chia yêu cầu thành đầu việc (WBS) | Đơn giá, tổng tiền, chiết khấu, điều khoản thanh toán |
| Ngày công từng đầu việc + dải tin cậy | Ngày bàn giao / lịch dự án |
| Giả định và phần không bao gồm | Tạo task trong PI Tracker |
| Căn cứ: id task đã làm thật | Cam kết với khách |

⚠️ **Không quy ngày công ra tiền, kể cả khi người dùng cho biết đơn giá.** Ranh giới này là do người dùng đặt ra, không phải mặc định của công cụ. Đơn giá, biên lợi nhuận và điều khoản là việc của người lập báo giá — họ có thông tin thương mại mà skill này không có (quan hệ khách hàng, mức cạnh tranh, đang cần deal hay không). Người dùng đưa đơn giá thì trả lại ngày công và nói rõ phần nhân tiền để họ tự làm.

⚠️ **Không tạo task trong PI Tracker cho estimate này.** Tool MCP `create_task` đang nằm sẵn trong context nên nước đi "tự nhiên" là ghi luôn WBS vào tracker. Cấm, vì hai lý do:

1. Deal chưa thắng. Task chưa được duyệt sẽ chảy vào **báo cáo tháng** (`/pi-tracker:bao-cao-thang` gom theo tag), làm tổng `est khách` phồng lên bằng công việc chưa ai đặt hàng.
2. Nó làm hỏng chính moc neo của skill này. Baseline chỉ tính task **đã Done**, nhưng một tracker đầy task giả sẽ khiến người đọc `list_tasks` không phân biệt được việc thật.

Thắng deal rồi thì tạo task là đúng — nhưng đó là một yêu cầu riêng, người dùng phải nói ra.

---

## Ba trường effort — hiểu sai là mất tiền thật

PI Tracker có 3 trường effort. Skill này sinh ra hai trong số đó, và **việc lẫn chúng là lỗi đắt nhất của cả quy trình**:

| Trường | Nghĩa | Trong WBS gọi là | Vai trò |
|---|---|---|---|
| `estimateCustomerDays` | Effort nếu làm **không có AI** | `quote_days` | **Số đưa cho người báo giá** |
| `estimateAiDays` | Effort dự kiến **khi có AI hỗ trợ** | `internal_days` | Kỳ vọng nội bộ, không gửi khách |
| `actualDays` | Effort **thực tế** đã bỏ ra | — | Chỉ dùng làm mốc neo cho `internal_days` |

### ⚠️ Bẫy 1 — Báo khách bằng con số có AI

`internal_days` thấp hơn `quote_days` vì AI. Đưa `internal_days` cho người báo giá là:

- trao toàn bộ biên AI cho khách,
- không còn đệm nào cho rework và bug (xem Bẫy 4),
- và tháng nào AI không giúp được — tích hợp lạ, chờ đối tác, môi trường khách hàng kỳ dị — là lỗ ngay trên giấy.

Trên dữ liệu thật của workspace này hệ số năng suất khoảng **1,6×**. Báo bằng số AI tức tự bỏ ~37% khối lượng cho cùng một lượng việc.

`estimate_calc.py` in cả hai cột, nhưng cột nội bộ nằm dưới đường kẻ **"Phần nội bộ — KHÔNG gửi khách"**. Đừng copy phần đó vào tài liệu gửi ra ngoài.

### ⚠️ Bẫy 2 — "Ratchet": lấy `actualDays` làm mốc cho `quote_days`

Đây là bẫy khó thấy nhất, vì nó trông rất giống việc tốt: *"ta có dữ liệu thực tế rồi, sao còn estimate theo số cũ?"*

Nếu neo `quote_days` vào `actual` lịch sử thì mỗi vòng báo giá lại tự hạ xuống: actual thấp (nhờ AI) → hạ giá báo → kỳ sau actual lại thấp hơn nữa → hạ tiếp. Sau vài vòng `quote_days == actual`, biên AI bằng 0, và **mọi rủi ro ước lượng rơi hết về phía mình** trong khi khách không hề được nói là đã hưởng lợi.

Mốc neo đúng:

```
quote_days     <- est_customer LỊCH SỬ của task tương đương   (cột "quote med" của baseline)
internal_days  <- actual       LỊCH SỬ của task tương đương   (cột "internal med")
```

`estimate_calc.py` bắt hiện tượng này: hệ số AI ngụ ý tiến gần 1,0 thì nó cảnh báo.

### ⚠️ Bẫy 3 — `internal_days` phải cộng phần thực tế vượt dự kiến

Baseline in `actual/est_AI`. Số này thường **lớn hơn 1**, nghĩa là thực tế chậm hơn chính estimate AI của mình. Nhân nó vào `internal_days` trước khi dùng con số đó để hứa lịch — không thì lịch nội bộ vỡ ngay ở đầu việc thứ hai.

### ⚠️ Bẫy 4 — Bug và rework không tự xuất hiện trong WBS

Không ai viết đầu việc "sửa lỗi mình sẽ gây ra", nên WBS luôn thiếu phần này, trong khi lịch sử đã trả tiền cho nó rồi. `estimate_baseline.py` tính `chi phi bug` = ngày công bug ÷ ngày công kế hoạch, **theo từng hạng mục**. Dùng con số đó cho `contingency_pct` thay vì bốc một số tròn.

Để dự phòng thành **một dòng riêng**, đừng rải ẩn vào từng đầu việc: khách hỏi "sao việc này tới 5 ngày" thì phải giải thích được, và nếu khách cắt dự phòng thì đó là quyết định có ghi lại của họ.

---

## Vị trí file khi chạy

Skill nằm trong plugin `pi-tracker` nên gọi bằng `/pi-tracker:estimate-tinh-nang`. `${CLAUDE_PLUGIN_ROOT}` trỏ vào repo (nếu `marketplace add <đường dẫn local>`) hoặc bản clone trong `~/.claude/plugins/` (nếu add từ GitHub) — cả hai đều chạy được, skill không giữ file dữ liệu nào giữa các lần chạy.

Skill này **dùng lại `pi_fetch.py` và `xlsx_writer.py` của skill `bao-cao-thang`** thay vì nhân bản. Cùng một plugin nên đường dẫn tương đối ổn định; đừng copy hai file đó sang đây.

### Nơi ghi file kết quả

⚠️ **Mọi file sinh ra đều ghi vào thư mục làm việc hiện tại của phiên (`pwd`) — giống `bao-cao-thang`. Đừng tự nghĩ ra một thư mục khác.**

Thư mục làm việc là nơi người dùng mở phiên, và họ đã chọn nó có chủ đích: đó là chỗ họ biết để tìm file, và cũng là chỗ `pi_fetch.py` dò `.mcp.json`. Ghi ra một thư mục "gọn gàng hơn" do skill tự đặt (`~/Work/estimates/`, `~/Documents/`…) là để người dùng đi tìm file của chính họ — trên máy họ, ở chỗ họ không hề chỉ định.

Vào thư mục làm việc **một lần** ở Bước 2 rồi chạy mọi lệnh sau đó bằng đường dẫn tương đối; đừng truyền đường dẫn tuyệt đối cho `--out-*`. Bảy file của một lần chạy nằm cùng chỗ:

| File | Vai trò |
|---|---|
| `tasks.json`, `baseline.json` | Dữ liệu trung gian, dùng lại được cho lần estimate sau trong cùng thư mục |
| `wbs-<slug>.json` | Đầu vào soạn tay |
| `estimate-<slug>.md` / `.json` / `.xlsx` | Bản bàn giao |

Nếu thư mục làm việc là một **repo git**, kiểm `git check-ignore estimate-<slug>.json` trước khi ghi. Bản estimate mang phạm vi của khách và — dưới đường kẻ — hệ số AI cùng biên dự kiến; commit nhầm là đẩy dữ liệu thương mại nội bộ lên remote. Chưa được ignore thì **nói với người dùng và đề nghị thêm vào `.gitignore`**, đừng lặng lẽ đổi sang thư mục khác.

---

## Quy trình

### Bước 0 — Kiểm tra môi trường

Giống `bao-cao-thang`: cả hai script chạy bằng `python3` (chỉ stdlib, 3.6+ là đủ).

```bash
for c in python3 python "py -3"; do
  if $c -c 'import sys; assert sys.version_info >= (3, 6)' 2>/dev/null; then
    echo "PY_OK: dùng \"$c\" — $($c -V 2>&1)"; break
  fi
done
```

Không in gì → **dừng skill**, bảo người dùng chạy `/pi-tracker:setup`, rồi chạy lại. Đừng tự cộng tay thay cho script: mọi con số trong bản estimate đều do `estimate_calc.py` tính, và **một bản estimate cộng sai vẫn đọc trôi chảy, vẫn được ký, và chỉ lộ ra lúc làm thật**.

### Bước 1 — Chốt phạm vi TRƯỚC khi ước lượng

⚠️ **Phạm vi chưa rõ thì con số không phải "estimate thô" — nó là con số sai.** Đây là chỗ phải dừng lại hỏi người dùng, không phải chỗ để đoán rồi ghi chú "cần xác nhận".

Ước lượng một tính năng mà chưa biết những điều dưới đây thì sai số không phải ±30% mà là **gấp mấy lần**:

- **Kênh / nền tảng nào** — web, app, Zalo, Facebook, hay tất cả? Mỗi kênh là một lần tích hợp.
- **Tích hợp bên thứ ba nào**, và **ai chịu trách nhiệm lấy tài khoản/quota/duyệt nội dung**? Chờ đối tác là ngày công thật.
- **Có màn hình mới không**, hay chỉ backend? UI là đầu việc riêng, thường kèm một vòng sửa theo góp ý.
- **Dữ liệu / nội dung do ai chuẩn bị**? "Khách cung cấp" và "mình đi lấy" chênh nhau rất xa.
- **Nghiệm thu thế nào** — bao nhiêu vòng góp ý là hết trách nhiệm?
- **Chạy ở đâu** — hạ tầng mình hay môi trường khách? Môi trường khách luôn đắt hơn.

Người dùng chưa trả lời được thì vẫn estimate được, nhưng **mỗi câu chưa chốt phải thành một dòng trong `assumptions`**, viết ở dạng điều kiện kiểm chứng được ("Khách cung cấp Zalo OA đã xác thực"), chứ không phải "giả định phạm vi vừa phải".

Đồng thời gọi tool MCP `list_tags` để biết tính năng này rơi vào **hạng mục nào** — cần cho Bước 3, và cũng là tag sẽ gắn nếu sau này thắng deal (đóng vòng lặp: task thật → baseline tốt hơn cho lần estimate sau).

### Bước 2 — Lấy toàn bộ lịch sử

⚠️ **ĐỪNG gọi tool MCP `list_tasks` trực tiếp** — mỗi task mang đủ 27 field, riêng `description` chiếm ~77% payload; đo thật trên 70 task đã vỡ hạn mức token và bị harness từ chối. Lấy bằng script — nó gọi đúng `tools/call` → `list_tasks` trên cùng server MCP rồi ghi nguyên văn bytes ra file:

```bash
cd <thư mục làm việc của phiên — xem "Nơi ghi file kết quả" ở trên>
python3 "${CLAUDE_PLUGIN_ROOT}/skills/bao-cao-thang/scripts/pi_fetch.py" --out tasks.json
```

**Không truyền `--month`** — mặc định script gửi `all=true`, và estimate cần *toàn bộ* lịch sử: mốc neo tốt nhất cho một tính năng có thể là task làm từ 8 tháng trước. Lọc theo tháng ở đây chỉ làm mất mốc neo.

Báo lỗi kết nối → app PI Tracker chưa chạy. **Hỏi người dùng, đừng estimate không có mốc neo mà không nói.**

### Bước 3 — Lấy mốc neo từ lịch sử

Một lệnh, kèm `--like` cho **mỗi đầu việc** đang định chia (lặp lại được):

```bash
python3 "${CLAUDE_PLUGIN_ROOT}/skills/estimate-tinh-nang/scripts/estimate_baseline.py" \
  --tasks tasks.json --out baseline.json \
  --like "tich hop zalo oa gui tin nhan chu dong" \
  --like "webhook nhan trang thai doi soat" \
  --tag "Kênh Zalo"
```

- `--like` khớp **không dấu**, trên cả tiêu đề, mô tả và tag — gõ không dấu vẫn tìm ra task có dấu. Task phải khớp ≥1/3 số từ mới được coi là mốc neo, nên đừng nhồi cả câu dài vào: 3–6 từ khoá đúng nghĩa là tốt nhất.
- `--tag` chỉ để in gọn; `baseline.json` luôn ghi đủ mọi hạng mục.

Đọc ra ba thứ, theo thứ tự quan trọng:

1. **Bảng task tương đương** — id, `est khach`, `est AI`, `actual` của việc đã làm thật. Đây là mốc neo, không phải bảng tham khảo cho vui.
2. **`chi phi bug` của hạng mục** → `contingency_pct`.
3. **`actual/est_AI`** → hệ số nhân cho `internal_days` (Bẫy 3).

Nếu một truy vấn in **"KHÔNG tìm thấy task lịch sử nào khớp"**: đầu việc đó không có mốc neo. Để `comparables` rỗng và `confidence: "low"` — script sẽ tự nới dải và buộc bạn nói ra. **Đừng gán bừa id của task gần gần**: `estimate_calc.py` kiểm được id có thật, nhưng không thể biết nó có liên quan hay không, nên một id sai sẽ trông y như một căn cứ vững.

Baseline chỉ tính task **đã Done và có `actualDays` > 0** — task đang làm có `actual` dở dang, đưa vào sẽ kéo trung vị xuống và làm mọi estimate sau đó thấp một cách hệ thống. Dòng đầu output cho biết còn bao nhiêu task dùng được; dưới ~10 thì nói rõ với người dùng là mốc neo mỏng.

### Bước 4 — Chia đầu việc, ghi `wbs-<slug>.json`

Cấu trúc file và ý nghĩa từng trường: [`references/cau-truc-ban-estimate.md`](references/cau-truc-ban-estimate.md).

⚠️ **Danh sách đầu việc KHÔNG phải danh sách tính năng khách đọc ra.** Chép lại các gạch đầu dòng trong yêu cầu của khách rồi gán ngày công cho từng dòng là cách estimate thiếu một cách hệ thống — vì nó bỏ hết phần việc khách không nhìn thấy nhưng vẫn tốn ngày công thật:

| Loại việc ẩn | Vì sao luôn có |
|---|---|
| Phân tích & chốt yêu cầu | Yêu cầu khách viết ra chưa bao giờ đủ để code |
| Thiết kế (dữ liệu, luồng, UI) | Sửa thiết kế giữa lúc code đắt hơn nhiều |
| Kiểm thử | Không test thì chi phí chỉ chuyển sang cột bug |
| Sửa lỗi sau nghiệm thu | Khách luôn có góp ý ở vòng đầu |
| Tài liệu, bàn giao, triển khai | Đặc biệt đắt khi chạy trên môi trường khách |

`estimate_calc.py` quét WBS tìm 5 loại này và cảnh báo loại nào thiếu. **Cảnh báo không phải lỗi** — cố ý không bao thì ghi vào `exclusions` để nó thành thoả thuận rõ ràng, chứ đừng để trống rồi im lặng.

Quy tắc chọn số:

1. **Mỗi đầu việc phải có `comparables`** là id task đã làm thật, lấy từ Bước 3. Không có thì `confidence: "low"`.
2. `quote_days` neo vào `est khach` của comparables; `internal_days` neo vào `actual` của chúng (Bẫy 2). Chênh nhiều so với mốc neo thì viết lý do vào `note`.
3. `confidence`: `high` khi có ≥2 comparable sát nghĩa; `med` khi có 1 hoặc comparable chỉ gần gần; `low` khi không có. Nó quyết định dải: ±15% / ±25% / ±40%.
4. **Làm tròn về nửa ngày.** `2.37 ngày` không chính xác hơn `2.5 ngày`, chỉ làm người đọc tưởng có phép đo nào đó. Script cảnh báo nếu không phải bội của 0,5.
5. Đầu việc **1–8 ngày công**. Lớn hơn 8 nghĩa là chưa hiểu nó gồm những gì — chia nhỏ ra. Nhỏ hơn 1 thì gộp lại, kẻo bảng thành danh sách việc lặt vặt.

#### Các trường của mẫu Excel công ty

Bản `.xlsx` dựng theo đúng mẫu estimate của công ty, nên WBS có thêm mấy trường chỉ phục vụ mẫu đó. Tất cả **đều tuỳ chọn** — WBS không có chúng vẫn chạy, chỉ nhận giá trị mặc định:

| Trường | Cột trong mẫu | Mặc định |
|---|---|---|
| `product`, `support_staff`, `function_doc` | Khối meta 4 dòng đầu (cùng `customer`) | trống; `function_doc` rơi về `feature` |
| `items[].desc` | Cột mô tả, nằm dưới tiêu đề gộp "Tính năng" | trống |
| `items[].dev_count` | Số lượng (Dev) | 1 |
| `items[].level` | Trình độ (Cấp bậc) | `B2` |
| `items[].inherited` | Có tính kế thừa (dấu `x`) | `false` |

Hai chỗ dễ hiểu sai:

- **Cột ET chính là `quote_days`** — effort không có AI. `internal_days` không xuất hiện ở bất kỳ ô nào trong `.xlsx` (Bẫy 1).
- **`dev_count` không nhân vào ET.** ET là ngày công, `dev_count` là cần mấy người làm song song. Nhân hai cột với nhau là đếm khối lượng hai lần.

`inherited: true` là **trường hợp duy nhất** được để `quote_days` = 0 — mẫu công ty dùng dòng ET = 0 để thể hiện phần dùng lại nguyên code đã có. Đánh dấu kế thừa thì phải ghi `comparables` chỉ ra kế thừa từ đâu, không thì con số 0 không phản biện được và script sẽ cảnh báo.

### Bước 5 — Tính và kiểm

**Không bao giờ tự cộng tay.** Script là nguồn duy nhất cho mọi con số:

```bash
python3 "${CLAUDE_PLUGIN_ROOT}/skills/estimate-tinh-nang/scripts/estimate_calc.py" \
  --wbs wbs-<slug>.json --tasks tasks.json --baseline baseline.json \
  --out-md estimate-<slug>.md --out-json estimate-<slug>.json \
  --out-xlsx estimate-<slug>.xlsx
```

`--tasks` và `--baseline` không phải tuỳ chọn cho đẹp: `--tasks` để kiểm mọi `comparables` là id có thật, `--baseline` để đối chiếu `contingency_pct` với chi phí bug thật và bắt hiện tượng ratchet.

Script **thoát 1 và không ghi gì** khi: thiếu trường bắt buộc, `internal_days > quote_days` (điền ngược hai cột), comparable trỏ tới id không tồn tại, hoặc không có comparable mà `confidence` khác `low`. Sửa file WBS, **đừng sửa script và đừng bỏ cờ để lách**.

`--out-xlsx` tuỳ chọn, sinh bảng **theo mẫu estimate của công ty** (khối meta vàng, tiêu đề hai tầng hồng, dòng TỔNG cam) để dán thẳng vào hồ sơ báo giá. Không cần cài thư viện — dùng `xlsx_writer.py` viết `.xlsx` bằng stdlib, vì `python3` trên máy là externally-managed (PEP 668). Đừng thay bằng openpyxl/pandas.

`xlsx_writer.py` **dùng chung với `bao-cao-thang`**, và nó đánh số style theo thứ tự khai báo trong `FONTS`/`FILLS`/`XFS`. Cần thêm màu hay kiểu ô mới thì **thêm vào cuối danh sách** — chèn vào giữa sẽ dịch id của mọi style đứng sau và làm hỏng bản báo cáo tháng, mà báo cáo tháng vẫn chạy được nên lỗi chỉ lộ ra khi mở file.

### Bước 6 — Bàn giao

Bản `.md` script sinh ra đã đủ dùng. Trước khi đưa cho người dùng:

1. **Cắt phần dưới đường kẻ** nếu họ định chuyển thẳng cho khách — phần "Phần nội bộ" có hệ số AI và biên dự kiến (Bẫy 1).
2. **Xử lý hết cảnh báo của script**, hoặc nói rõ vì sao chấp nhận để nguyên.
3. **Nói rõ ngày công ≠ ngày lịch.** 20 ngày công không phải 20 ngày lịch, cũng không phải 10 ngày với 2 người: có đầu việc buộc tuần tự, và không ai làm 100% thời gian cho một việc. Muốn ra ngày bàn giao thì phải bàn riêng về số người và thứ tự phụ thuộc — **đừng tự chia rồi đưa ra một ngày**.

Thư mục đã có `estimate-<slug>.*` mà không do phiên này tạo → hỏi trước khi ghi đè.

Báo cho người dùng **đường dẫn đầy đủ** của từng file đã ghi. Họ vừa nhận một bản estimate sắp chuyển cho người báo giá — "đã tạo xong file" mà không nói ở đâu thì họ phải đi tìm.

---

## Nguyên tắc khi viết

**Trung thực về mốc neo.** Bao nhiêu đầu việc có comparable thật, bao nhiêu là phỏng đoán — nói ra con số. Một bản estimate ghi "3/7 đầu việc không có việc tương đương trong lịch sử" đáng tin hơn hẳn một bản trông đâu cũng chắc chắn.

**Dải tin cậy không phải để trang trí.** Nếu dải rộng (nhiều `low`), nói thẳng là phạm vi chưa đủ rõ để cam kết, và nêu cần chốt điều gì để thu hẹp lại. Người báo giá cần biết mình đang mua rủi ro nào.

**Giả định phải kiểm chứng được.** "Khách cung cấp Zalo OA đã xác thực trước ngày bắt đầu" là giả định; "phạm vi ở mức trung bình" không phải — không ai xác nhận hay phản đối được câu đó.

**Phần "không bao gồm" quan trọng ngang bảng số.** Mọi thứ không liệt kê đều sẽ bị hiểu là có bao. Cái đắt nhất thường là những thứ dễ quên: di trú dữ liệu cũ, đào tạo người dùng, hỗ trợ sau bàn giao, chi phí trả cho bên thứ ba.

**Xưng hô:** tiếng Việt, giọng trung tính, gọi "team". Không emoji trừ dấu cảnh báo.
