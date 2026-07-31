#!/usr/bin/env python3
"""Sinh bao cao thang ban NGAN GON dang .xlsx tu metrics.json — TO CHUC THEO HANG MUC.

Hang muc = TAG cua task trong PI Tracker. Khong co danh sach hang muc co dinh.

Cau truc: 1 sheet tong quan + 1 sheet cho MOI tag + sheet bug + sheet ton dong.
Moi sheet tag tra loi 2 cau: hang muc do LAM DUOC NHUNG VIEC GI, va AI giup
GIAM EFFORT / TANG NANG SUAT bao nhieu.

Moi con so lay tu metrics.json do pi_report.py sinh ra. Script nay chi dan xep lai,
khong tu tinh lai chi so nao ngoai: dem task nhanh/dung/cham so voi estimate AI, va
tiet kiem/he so o muc TUNG TASK (est_customer - actual).

Usage:
  python3 pi_excel.py --metrics metrics.json --out bao-cao-thang-<MM>-<YYYY>.xlsx
"""
import argparse
import json
import re
from collections import Counter
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from xlsx_writer import Workbook, col_letter  # noqa: E402

SEV_STYLE = {"Critical": "crit", "Major": "major", "Minor": "minor"}
SEV_ORDER = {"Critical": 0, "Major": 1, "Minor": 2, "": 3}
NGAY_CONG_THANG = 21  # ngay cong / nguoi / thang, dung de quy doi nhan su

# Phai khop NO_GROUP trong pi_report.py — nhom cho task chua gan tag.
NO_TAG = "(chua gan tag)"

# metrics.json luu ten loai task khong dau (TYPE_NAME trong pi_report.py). File .xlsx
# la ban de gui/trinh bay nen hien co dau.
LOAI_VIEC = {
    "Theo plan": "Theo plan",
    "Phat sinh (bug)": "Phát sinh (bug)",
    "Phat sinh theo plan": "Phát sinh theo plan",
}


def loai(v):
    return LOAI_VIEC.get(v, v)

# Ten hang muc hien thi. Tag do nguoi dung tu dat trong PI Tracker nen thuong da
# dung dang muon hien; bang nay chi de sua vai truong hop dac biet.
# Ky tu "/" khong hop le trong ten sheet Excel -> xlsx_writer tu thay bang "-".
SHEET_NAME = {}


def ten(tag):
    """Ten hang muc de hien thi. Dung o MOI cho in ten ra file, khong chi ten sheet."""
    return SHEET_NAME.get(tag, tag)

# Style cua o tuong ung tren dong TONG. Style nao khong co trong bang -> "tot".
TOTAL_STYLE = {
    "int": "tot_int", "num": "tot_num", "pct": "tot_pct",
    "gain": "tot_gain", "mult": "tot_x",
}


# ------------------------------------------------------------------- helpers
def pct(part, whole, nd=1):
    return round(part / whole * 100, nd) if whole else 0.0


def he_so(est, actual):
    return round(est / actual, 2) if actual else 0.0


def tags_by_effort(m):
    """Cac hang muc (tag) co task, xep theo effort thuc te giam dan.

    Task chua gan tag dồn vao nhom "(chua gan tag)" — day xuong cuoi vi no khong
    phai mot hang muc thuc su ma la viec con thieu du lieu.
    """
    items = [(k, v) for k, v in m["by_tag"].items() if v["n"]]
    return sorted(items, key=lambda kv: (kv[0] == NO_TAG, -kv[1]["actual"]))


def people_of(m, tag):
    """Nhan su trong mot hang muc, xep theo effort giam dan."""
    out = [(k.split("|", 1)[1], v) for k, v in m["by_tag_person"].items()
           if k.split("|", 1)[0] == tag]
    return sorted(out, key=lambda kv: -kv[1]["actual"])


def est_accuracy(tasks):
    """Dem task nhanh hon / dung / cham hon estimateAiDays."""
    fast = on = 0
    slower = []
    for t in tasks:
        ea, ac = t.get("est_ai"), t.get("actual")
        if not ea:
            continue
        if ac < ea:
            fast += 1
        elif ac == ea:
            on += 1
        else:
            slower.append(t)
    return fast, on, slower


def header(sh, month, workspace, ncols, subtitle):
    yyyy, mm = month.split("-")[0], month.split("-")[1]
    sh.row([(f"Báo cáo công việc tháng {mm}/{yyyy} — {workspace}", "title")], height=24)
    sh.merge(f"A1:{col_letter(ncols)}1")
    sh.row([(subtitle, "meta")], height=14)
    sh.merge(f"A2:{col_letter(ncols)}2")
    sh.blank()


def section(sh, title, ncols):
    r = sh.row([(title, "section")], height=19)
    sh.merge(f"A{r}:{col_letter(ncols)}{r}")
    return r


def note(sh, text, ncols, height=26, style="note"):
    r = sh.row([(text, style)], height=height)
    sh.merge(f"A{r}:{col_letter(ncols)}{r}")
    return r


def thead(sh, cols, title_col=0):
    """Ghi dong tieu de bang. cols: list (tieu de, style_cot)."""
    return sh.row([(c[0], "h_left" if i == title_col else "h")
                   for i, c in enumerate(cols)], height=30)


def table(sh, cols, rows, total=None, title_col=0):
    """cols: list (tieu de, style_cot). rows: list[list[value]] hoac list[list[(v, style)]].

    Tra ve (dong header, dong cuoi phan du lieu — chua tinh dong TONG).
    """
    r_head = thead(sh, cols, title_col)
    for row in rows:
        sh.row([v if isinstance(v, tuple) else (v, cols[i][1])
                for i, v in enumerate(row)])
    r_last = sh.cursor
    if total:
        sh.row([(v, TOTAL_STYLE.get(cols[i][1], "tot")) for i, v in enumerate(total)])
    return r_head, r_last


# So dau viec chinh liet ke trong o "Dau viec chinh" cua bang tong quan. Nhieu hon
# nay thi o bi cao qua, doc khong con la "overview" nua.
TOP_VIEC = 4


def overview_of(m, tag):
    """Tom tat mot hang muc tu chinh danh sach task cua no.

    Tra ve (dau_viec, nhan_su, loai_viec) — TAT CA lay tu du lieu that:
      dau_viec : neu co highlights.json cho tag nay -> dung nguyen cac dong tong hop
                 do NGUOI VIET soan (dang "DD/MM — viec da lam cho khach X"). Khong co
                 thi rot ve liet ke tieu de {TOP_VIEC} task nang nhat — doc duoc nhung
                 khong phai tong hop.
      nhan_su  : nguoi tham gia, xep theo ngay cong giam dan.
      loai_viec: pho theo truong `type` (Theo plan / bug / Phat sinh theo plan).

    KHONG suy luan hang muc con hay ten khach tu tieu de — chi trich nguyen van.
    """
    rows = sorted([x for x in m["tasks"] if tag in x["tags"]],
                  key=lambda x: -x["actual"])
    hl = (m.get("_highlights") or {}).get(tag)
    if hl:
        # Ban tong hop do nguoi viet — moi dong mot dau viec, giu nguyen thu tu da soan.
        dau_viec = "\n".join(f"• {line}" for line in hl)
    else:
        # Fallback co y: chi liet ke tieu de + so ngay. Doc duoc nhung KHONG phai tong hop
        # — neu thay cot nay chi la danh sach tieu de thi nghia la thieu highlights.json.
        top, rest = rows[:TOP_VIEC], rows[TOP_VIEC:]
        parts = [f"#{x['id']} {x['title']} ({fmt_ngay(x['actual'])}n)" for x in top]
        if rest:
            parts.append(f"… và {len(rest)} task khác "
                         f"({fmt_ngay(sum(x['actual'] for x in rest))}n)")
        dau_viec = " · ".join(parts)
    nguoi = [w for w, _ in people_of(m, tag)]
    dem = Counter(loai(x["type"]) for x in rows)
    loai_txt = " · ".join(f"{n} {k.lower()}" for k, n in dem.most_common())
    return dau_viec, ", ".join(nguoi), loai_txt


def mo_ta_ngan(desc, n=200):
    """Rut mo ta task thanh mot doan ngan doc duoc trong o Excel.

    Mo ta viet bang markdown ("## Muc tieu", gach dau dong, **bold") nen phai lam sach
    truoc khi cat, neu khong o Excel se day ky tu ## va *. Cat theo RANH GIOI TU, khong
    cat giua tu.
    """
    if not desc:
        return ""
    s = re.sub(r"^#{1,6}\s*", "", desc, flags=re.M)      # bo tieu de markdown
    s = re.sub(r"^[-*+]\s*", "", s, flags=re.M)           # bo bullet
    s = re.sub(r"[*_`]+", "", s)                          # bo bold/italic/code
    s = re.sub(r"\s+", " ", s).strip()
    if len(s) <= n:
        return s
    cut = s[:n].rsplit(" ", 1)[0]
    return cut + "…"


def fmt_ngay(v):
    """0.5 -> '0.5', 6.0 -> '6' — bo '.0' cho o chu do dai."""
    return f"{v:g}"


# Duoi nguong nay thi khong dua vao ket luan "AI phat huy tot nhat / kem nhat":
# 1-2 task cho he so rat cao ma khong co y nghia thong ke.
MAU_TOI_THIEU = 5


def dang_ke(m):
    """Cac hang muc co du task de ket luan."""
    return [(k, v) for k, v in m["by_tag"].items()
            if v["n"] >= MAU_TOI_THIEU and k != NO_TAG]


def kpi(sh, label, value, style="kpi_text", suffix=None, merge_to="C"):
    r = sh.row([(label, "kpi_label"), (value, style),
                (suffix, "small") if suffix else ("", "cell")])
    if suffix is None and merge_to:
        sh.merge(f"B{r}:{merge_to}{r}")
    return r


# ================================================================== sheet 1
def sheet_tong_quan(wb, m, workspace):
    t = m["total"]
    sh = wb.sheet("Tổng quan")
    sh.widths({1: 26, 2: 11, 3: 9, 4: 8, 5: 11, 6: 11, 7: 11, 8: 12, 9: 12, 10: 13,
               11: 10, 12: 11})
    N = 12

    header(sh, m["month"], workspace, N,
           f"Nguồn: PI Tracker · Phạm vi: {m['scope_task_count']} task khởi động và/hoặc "
           f"hoàn thành trong tháng · Không tính {len(m['carryover'])} task đã lên lịch "
           f"cho tháng sau · Chi tiết từng hạng mục ở các sheet tiếp theo")

    # ---- chi so chinh
    section(sh, "CHỈ SỐ CHÍNH TOÀN TEAM", N)
    for label, value, style in [
        ("Task hoàn thành", f"{t['done']}/{t['n']}", "kpi_text"),
        ("Tỷ lệ hoàn thành", pct(t["done"], t["n"]), "kpi_pct"),
        ("Effort báo khách (không AI)", t["est_customer"], "kpi_num"),
        ("Effort dự kiến khi có AI", t["est_ai"], "kpi_num"),
        ("Effort thực tế", t["actual"], "kpi_num"),
        ("→ Ngày công tiết kiệm", t["saved_days"], "kpi_num"),
        ("→ Giảm effort", t["effort_cut_pct"], "kpi_pct"),
        ("→ Tăng năng suất", t["productivity_gain_pct"], "kpi_gain"),
        ("→ Hệ số năng suất", t["productivity_x"], "kpi_x"),
        ("Tỷ lệ áp dụng AI", t["ai_adoption_pct"], "kpi_pct"),
        ("Bug phát sinh", f"{t['bug']} ({m['severity_breakdown'].get('Critical', 0)} Critical)", "kpi_text"),
        ("Effort xử lý bug", t["bug_days"], "kpi_num"),
    ]:
        kpi(sh, label, value, style)

    note(sh, f"{t['saved_days']} ngày công tiết kiệm ≈ "
             f"{round(t['saved_days'] / NGAY_CONG_THANG, 1)} nhân sự · tháng "
             f"(mốc {NGAY_CONG_THANG} ngày công/người/tháng). "
             f"Giảm {t['effort_cut_pct']}% effort ⟺ tăng {t['productivity_gain_pct']}% "
             f"năng suất (hệ số {t['productivity_x']}×) — cùng một sự thật, hai cách "
             f"phát biểu, đừng dùng lẫn.", N, 28)
    sh.blank()

    # ---- theo giai phap
    section(sh, "TỪNG HẠNG MỤC LÀM ĐƯỢC BAO NHIÊU — AI GIẢM EFFORT BAO NHIÊU", N)
    cols = [("Hạng mục (tag)", "cell"), ("Task", "int"), ("Done", "int"), ("Bug", "int"),
            ("Est khách\n(ngày)", "num"), ("Est AI\n(ngày)", "num"),
            ("Thực tế\n(ngày)", "num"), ("Tiết kiệm\n(ngày)", "num"),
            ("Giảm effort", "pct"), ("Tăng\nnăng suất", "gain"), ("Hệ số", "mult")]
    rows = [[ten(name), v["n"], v["done"], v["bug"],
             v["est_customer"], v["est_ai"], v["actual"], v["saved_days"],
             v["effort_cut_pct"], v["productivity_gain_pct"], v["productivity_x"]]
            for name, v in tags_by_effort(m)]
    total = ["TỔNG", t["n"], t["done"], t["bug"], t["est_customer"], t["est_ai"],
             t["actual"], t["saved_days"], t["effort_cut_pct"],
             t["productivity_gain_pct"], t["productivity_x"]]
    table(sh, cols, rows, total)

    # Chi ket luan tren cac giai phap co du mau — 1 task cho he so 4x la vo nghia.
    du_mau = dang_ke(m)
    if du_mau:
        best = max(du_mau, key=lambda kv: kv[1]["productivity_x"])
        worst = min(du_mau, key=lambda kv: kv[1]["productivity_x"])
        bo_qua = [f"{ten(k)} ({v['n']} task)"
                  for k, v in m["by_tag"].items()
                  if v["n"] and v["n"] < MAU_TOI_THIEU]
        txt = (f"Trong các hạng mục có từ {MAU_TOI_THIEU} task trở lên: AI phát huy tốt "
               f"nhất ở {ten(best[0])} "
               f"({best[1]['productivity_x']}×, "
               f"{best[1]['ai_adoption_pct']}% task dùng AI), kém nhất ở "
               f"{ten(worst[0])} "
               f"({worst[1]['productivity_x']}×, {worst[1]['ai_adoption_pct']}% task dùng "
               f"AI). Xem sheet riêng của từng hạng mục để biết vì sao.")
        if bo_qua:
            txt += (f" Đã loại khỏi kết luận: {', '.join(bo_qua)} — quá ít task, hệ số "
                    f"cao/thấp không có ý nghĩa thống kê.")
        note(sh, txt, N, 32)
    sh.blank()

    # ---- overview cong viec theo hang muc
    # Muc dich: doc mot bang la biet moi hang muc LAM GI, PHUC VU AI, PHAM VI DEN DAU
    # — khong phai liet ke het task (viec do o sheet rieng cua tung tag).
    section(sh, "TỔNG QUAN CÔNG VIỆC THEO HẠNG MỤC", N)
    # Ten hang muc A:B; so lieu C..F; "Dau viec chinh" gop G..K (rong, wrap); nhan su L..N.
    r = sh.row([("Hạng mục (tag)", "h_left"), ("", "h_left"),
                ("Task", "h"), ("Bug", "h"), ("Thực tế\n(ngày)", "h"), ("Giảm\neffort", "h"),
                ("Đầu việc chính — khách hàng / phạm vi hiện trong tiêu đề task", "h_left"),
                ("", "h_left"), ("", "h_left"), ("", "h_left"), ("", "h_left"),
                ("Nhân sự", "h_left"), ("", "h_left"), ("", "h_left")], height=30)
    sh.merge(f"A{r}:B{r}")
    sh.merge(f"G{r}:K{r}")
    sh.merge(f"L{r}:{col_letter(N)}{r}")

    for tag, tv in tags_by_effort(m):
        dau_viec, nhan_su, loai_viec = overview_of(m, tag)
        style_ten = "tot" if tag != NO_TAG else "minor"
        # Chieu cao theo SO DONG thuc te trong o "Dau viec chinh" (moi dong ~14pt) —
        # de cung height thi ban tong hop nhieu dong se bi cat mat.
        n_dong = dau_viec.count("\n") + 1
        cao = max(46, min(n_dong * 15 + 8, 320))
        r = sh.row([(ten(tag), style_ten), ("", "cell"),
                    (tv["n"], "int"), (tv["bug"], "int"),
                    (tv["actual"], "num"), (tv["effort_cut_pct"], "pct"),
                    (dau_viec, "small_wrap"), ("", "cell"), ("", "cell"), ("", "cell"),
                    ("", "cell"),
                    (nhan_su, "small_wrap"), ("", "cell"), ("", "cell")], height=cao)
        sh.merge(f"A{r}:B{r}")
        sh.merge(f"G{r}:K{r}")
        sh.merge(f"L{r}:{col_letter(N)}{r}")
        # Dong phu: pho loai viec — cho biet hang muc nay la viec ke hoach hay chua chay.
        r = sh.row([("", "cell"), ("", "cell"), ("", "cell"), ("", "cell"),
                    ("", "cell"), ("", "cell"),
                    (f"Loại việc: {loai_viec}", "minor"), ("", "cell"), ("", "cell"),
                    ("", "cell"), ("", "cell"),
                    ("", "cell"), ("", "cell"), ("", "cell")])
        sh.merge(f"A{r}:B{r}")
        sh.merge(f"G{r}:K{r}")
        sh.merge(f"L{r}:{col_letter(N)}{r}")

    note(sh, f"Cột \"Đầu việc chính\" liệt kê {TOP_VIEC} task nặng ngày công nhất của hạng "
             f"mục, phần còn lại gộp một dòng — đây là bảng ĐỌC NHANH, danh sách task đầy đủ "
             f"nằm ở sheet riêng của từng hạng mục. Tên khách hàng và phạm vi hiện lên qua "
             f"chính tiêu đề task (script không suy diễn thêm): nếu tiêu đề không ghi tên "
             f"khách thì bảng này cũng không biết — muốn thấy khách hàng rõ ràng thì ghi vào "
             f"tiêu đề task, hoặc tạo thêm tag riêng cho khách hàng.", N, 46)
    sh.blank()

    # ---- moi hang muc ai lam gi (cap gop thu hai)
    section(sh, "MỖI HẠNG MỤC AI LÀM GÌ", N)
    gcols = [("Hạng mục / Người", "h_left"), ("Task", "h"), ("Bug", "h"),
             ("Est khách\n(ngày)", "h"), ("Thực tế\n(ngày)", "h"),
             ("Tiết kiệm\n(ngày)", "h"), ("Giảm effort", "h")]
    r = sh.row([(gcols[0][0], "h_left"), ("", "h_left")]
               + [(c[0], "h") for c in gcols[1:]],
               height=30)
    sh.merge(f"A{r}:B{r}")

    for tag, tv in tags_by_effort(m):
        style_ten = "tot" if tag != NO_TAG else "minor"
        r = sh.row([(ten(tag), style_ten), ("", "tot"),
                    (tv["n"], "tot_int"), (tv["bug"], "tot_int"),
                    (tv["est_customer"], "tot_num"), (tv["actual"], "tot_num"),
                    (tv["saved_days"], "tot_num"), (tv["effort_cut_pct"], "tot_pct")])
        sh.merge(f"A{r}:B{r}")
        for who, pv in people_of(m, tag):
            r = sh.row([(f"    {who}", "cell"), ("", "cell"),
                        (pv["n"], "int"), (pv["bug"], "int"),
                        (pv["est_customer"], "num"), (pv["actual"], "num"),
                        (pv["saved_days"], "num"), (pv["effort_cut_pct"], "pct")])
            sh.merge(f"A{r}:B{r}")

    note(sh, "Bảng này cho biết ai đang gánh hạng mục nào — một người chiếm gần hết một "
             "hạng mục là rủi ro single point of failure, cần nêu trong báo cáo. Danh sách "
             "task chi tiết nằm ở sheet riêng của từng hạng mục.", N, 32)
    sh.blank()

    # ---- theo nhan su
    section(sh, "THEO NHÂN SỰ", N)
    pcols = [("Người", "cell"), ("Task", "int"), ("Done", "int"), ("Bug", "int"),
             ("Est khách\n(ngày)", "num"), ("Est AI\n(ngày)", "num"),
             ("Thực tế\n(ngày)", "num"), ("Tiết kiệm\n(ngày)", "num"),
             ("Giảm effort", "pct"), ("Tăng\nnăng suất", "gain"), ("Hệ số", "mult")]
    people = sorted(m["by_person"].items(), key=lambda kv: -kv[1]["actual"])
    prows = [[name, v["n"], v["done"], v["bug"],
              v["est_customer"], v["est_ai"], v["actual"], v["saved_days"],
              v["effort_cut_pct"], v["productivity_gain_pct"], v["productivity_x"]]
             for name, v in people]
    table(sh, pcols, prows)
    sh.blank()

    # ---- gioi han phep do
    cg = m["control_group_no_ai"]
    section(sh, "⚠ GIỚI HẠN CỦA PHÉP ĐO — đọc trước khi trích dẫn hệ số năng suất", N)
    note(sh,
         f"{t['ai_used']}/{t['n']} task dùng AI ({t['ai_adoption_pct']}%). Nhóm đối "
         f"chứng không dùng AI chỉ có {cg['n']} task ({cg['est_customer']} ngày est "
         f"khách → {cg['actual']} ngày thực tế) — quá nhỏ để kết luận.\n\n"
         f"Nghĩa là: mức tăng {t['productivity_gain_pct']}% ({t['productivity_x']}×) là "
         f"so với ESTIMATE BÁO KHÁCH — một con số do con người ước lượng, không phải kết "
         f"quả đo có kiểm soát. Nó cho biết team làm nhanh hơn mốc cam kết bao nhiêu, "
         f"nhưng KHÔNG tách được phần nào do AI, phần nào do estimate vốn có đệm, phần "
         f"nào do kinh nghiệm tích lũy.\n\n"
         f"Khi trình bày ra ngoài team: nói \"làm nhanh hơn mốc báo khách "
         f"{t['productivity_x']}×\", KHÔNG nói \"AI tăng năng suất "
         f"{t['productivity_gain_pct']}%\". Muốn có số liệu thật về đóng góp của AI, "
         f"phải chủ động giữ nhóm đối chứng tối thiểu 5 task cùng loại.",
         N, 108, style="warn")
    sh.blank()

    note(sh, "Hạng mục công việc lấy trực tiếp từ trường \"Phân loại tag\" của task trong "
             "PI Tracker — dữ liệu thật do người làm task gán, không phải suy luận từ tiêu đề.",
         N, 15)
    sh.freeze(row=2)
    return sh


# ================================================================== sheet 2..n
def sheet_hang_muc(wb, m, workspace, name, v):
    """Mot sheet cho mot hang muc (tag): lam duoc gi + AI giup giam effort bao nhieu."""
    t = m["total"]
    sh = wb.sheet(ten(name))
    # Cot A hep (ID task) va cot B rong (tieu de task) -> bang chi so PHAI dung cot
    # A:B gop lai lam cot nhan, so lieu tu C tro di. Neu de nhan o cot A rieng thi
    # chu bi bo lai xuong 4-5 dong, khong doc duoc.
    # Cot C = "Muc tieu / pham vi" (rut tu mo ta task) nen rong; cac cot so day sang phai.
    sh.widths({1: 6, 2: 40, 3: 52, 4: 11, 5: 16, 6: 11, 7: 10, 8: 11, 9: 11, 10: 10,
               11: 11, 12: 11})
    N = 12

    header(sh, m["month"], workspace, N,
           f"{ten(name)} — {v['done']}/{v['n']} task hoàn thành · {v['actual']} ngày công "
           f"· tiết kiệm {v['saved_days']} ngày · giảm {v['effort_cut_pct']}% effort "
           f"(hệ số {v['productivity_x']}×) · {v['bug']} bug")

    tasks = [x for x in m["tasks"] if name in x["tags"]]
    ppl = people_of(m, name)

    # ---- AI giup gi: bang NGANG, dong TONG chinh la so lieu cua ca giai phap
    section(sh, "AI GIÚP GIẢM EFFORT & TĂNG NĂNG SUẤT BAO NHIÊU", N)
    kcols = ["Task", "Bug", "Est khách\n(ngày)", "Est AI\n(ngày)", "Thực tế\n(ngày)",
             "Tiết kiệm\n(ngày)", "Giảm\neffort", "Tăng\nnăng suất", "Hệ số"]
    r = sh.row([("Người", "h_left"), ("", "h_left")] + [(c, "h") for c in kcols],
               height=30)
    sh.merge(f"A{r}:B{r}")

    def dong(nhan, b, st):
        """Mot dong cua bang chi so. st='' -> dong thuong, st='tot_' -> dong TONG."""
        r = sh.row([(nhan, st or "cell"), ("", st or "cell"),
                    (b["n"], st + "int" if st else "int"),
                    (b["bug"], st + "int" if st else "int"),
                    (b["est_customer"], st + "num" if st else "num"),
                    (b["est_ai"], st + "num" if st else "num"),
                    (b["actual"], st + "num" if st else "num"),
                    (b["saved_days"], st + "num" if st else "num"),
                    (b["effort_cut_pct"], st + "pct" if st else "pct"),
                    (b["productivity_gain_pct"], st + "gain" if st else "gain"),
                    (b["productivity_x"], "tot_x" if st else "mult")])
        sh.merge(f"A{r}:B{r}")

    if len(ppl) > 1:
        for pn, pv in ppl:
            dong(pn, pv, "")
        dong("TỔNG CẢ HẠNG MỤC", v, "tot_")
    else:
        # Chi 1 nguoi lam -> dong nguoi va dong TONG trung nhau, chi ghi 1 dong.
        chi_minh = f" ({ppl[0][0]})" if ppl else ""
        dong(f"TỔNG CẢ HẠNG MỤC{chi_minh}", v, "tot_")

    fast, on, slower = est_accuracy(tasks)
    note(sh, f"Est khách = mốc chuẩn nếu làm không có AI · Est AI = estimate khi có AI hỗ "
             f"trợ · Thực tế = ngày công đã bỏ ra. Giảm effort = (est khách − thực tế) ÷ "
             f"est khách. Tăng năng suất = hệ số − 1, cùng một sự thật với giảm effort, "
             f"khác cách phát biểu.\n"
             f"Áp dụng AI: {v['ai_used']}/{v['n']} task ({v['ai_adoption_pct']}%). "
             f"Estimate AI lệch {v['est_ai_deviation_pct']}% (dương = chậm hơn dự kiến) — "
             f"{on} task đúng dự kiến / {fast} nhanh hơn / {len(slower)} chậm hơn "
             f"trên {fast + on + len(slower)} task có estimate AI.", N, 46)
    sh.blank()

    # ---- cong viec hoan thanh
    # Chi "Done" moi la hoan thanh. Liet ke cac trang thai CHUA xong de loai tru la
    # cach de sai am tham: app dung "In Progress" (co dau cach), va them mot trang
    # thai moi vao model se lam no roi thang vao muc HOAN THANH ma khong ai thay.
    # Doi chieu duong cung khop voi v['done'] o header va muc TON DONG (status != Done).
    done = sorted([x for x in tasks if x["status"] == "Done"], key=lambda x: -x["actual"])
    section(sh, f"CÔNG VIỆC HOÀN THÀNH ({len(done)} task) — xếp theo ngày công giảm dần", N)
    cols = [("ID", "cell_c"), ("Công việc", "cell_wrap"),
            ("Mục tiêu / phạm vi (từ mô tả task)", "small_wrap"), ("Người", "cell"),
            ("Loại", "cell_wrap"), ("Est khách\n(ngày)", "num"), ("Est AI\n(ngày)", "num"),
            ("Thực tế\n(ngày)", "num"), ("Tiết kiệm\n(ngày)", "num"),
            ("Hệ số", "mult"), ("Dùng AI", "cell_c")]
    rows = []
    for x in done:
        saved = round(x["est_customer"] - x["actual"], 2)
        rows.append([x["id"], x["title"], mo_ta_ngan(x.get("description")),
                     x["assignee"], loai(x["type"]), x["est_customer"],
                     x["est_ai"], x["actual"], saved, he_so(x["est_customer"], x["actual"]),
                     ("có", "cell_c") if x["ai_used"] else ("KHÔNG", "minor")])
    total = ["", f"TỔNG {len(done)} task hoàn thành", "", "", "",
             round(sum(x["est_customer"] for x in done), 2),
             round(sum(x["est_ai"] for x in done), 2),
             round(sum(x["actual"] for x in done), 2),
             round(sum(x["est_customer"] - x["actual"] for x in done), 2), "", ""]
    r_head, r_last = table(sh, cols, rows, total, title_col=1)
    sh.autofilter(f"A{r_head}:{col_letter(N)}{r_last}")
    sh.blank()

    # ---- bug cua giai phap
    bugs = sorted([b for b in m["bugs"] if name in b["tags"]],
                  key=lambda b: (SEV_ORDER.get(b["severity"], 3), -b["actual"]))
    if bugs:
        section(sh, f"BUG PHÁT SINH ({len(bugs)} bug · {v['bug_days']} ngày công · "
                    f"{pct(v['bug_days'], v['actual'])}% effort của hạng mục)", N)
        bcols = [("ID", "cell_c"), ("Bug", "cell_wrap"), ("Người", "cell"),
                 ("Mức độ", "cell_c"), ("Xử lý", "cell_c"), ("Effort\n(ngày)", "num"),
                 ("Phát hiện", "cell_c"), ("Đóng", "cell_c"), ("", "cell"), ("", "cell")]
        brows = [[b["id"], b["title"], b["assignee"],
                  (b["severity"], SEV_STYLE.get(b["severity"], "cell_c")),
                  b["resolution"] or b["status"], b["actual"],
                  b["start"] or "—", b["done"] or "—", "", ""] for b in bugs]
        table(sh, bcols, brows, title_col=1)
        sh.blank()
    else:
        section(sh, "BUG PHÁT SINH", N)
        note(sh, "Không có bug nào trong tháng. Đây cũng là thông tin — nhưng cần đọc "
                 "đúng: sản phẩm chưa lên production hoặc chưa có tải thật thì con số 0 "
                 "phản ánh chưa bị thử thách, không phải đã ổn định.", N, 26)
        sh.blank()

    # ---- ton dong
    unfin = [x for x in m["unfinished"] if name in x["tags"]]
    carry = [x for x in m["carryover"] if name in x["tags"]]
    if unfin or carry:
        blocked_days = sum(x["actual"] for x in unfin)
        # Chi goi la "dang treo" khi that su co task chua xong; task chuyen tiep
        # chua bo cong nao nen khong phai ton dong.
        if unfin:
            tieu_de = (f"⚠ TỒN ĐỌNG — {len(unfin)} task chưa hoàn thành, "
                       f"{blocked_days} ngày công đã bỏ ra đang treo")
        else:
            tieu_de = (f"CHUYỂN TIẾP THÁNG SAU ({len(carry)} task, chưa bỏ công nào) "
                       f"— không có task tồn đọng")
        section(sh, tieu_de, N)
        ucols = [("ID", "cell_c"), ("Task", "cell_wrap"), ("Người", "cell"),
                 ("Trạng thái", "cell_c"), ("Đã bỏ công\n(ngày)", "num"),
                 ("Checklist", "cell_c"), ("Blocker / Hạn", "cell_wrap"),
                 ("", "cell"), ("", "cell"), ("", "cell")]
        urows = []
        for x in sorted(unfin, key=lambda x: -x["actual"]):
            urows.append([x["id"], x["title"], x["assignee"], x["status"], x["actual"],
                          f"{x['todo_done']}/{x['todo_total']}" if x["todo_total"] else "—",
                          x["blocker"] or "KHÔNG GHI BLOCKER", "", "", ""])
        for x in sorted(carry, key=lambda x: x["due"] or "9999"):
            urows.append([x["id"], x["title"], x["assignee"], "Chuyển tiếp", 0, "—",
                          f"hạn {x['due'] or '—'} · est AI {x['est_ai']} ngày", "", "", ""])
        table(sh, ucols, urows, title_col=1)
        note(sh, "Task \"Chuyển tiếp\" chưa bỏ công nào nên không tính vào tỷ lệ hoàn "
                 "thành của tháng.", N, 15)

    sh.freeze(row=2)
    return sh


# ================================================================== sheet bug
def sheet_bug(wb, m, workspace):
    t = m["total"]
    sh = wb.sheet("Bug toàn team")
    sh.widths({1: 6, 2: 52, 3: 18, 4: 10, 5: 11, 6: 11, 7: 10, 8: 12, 9: 12})
    N = 9

    header(sh, m["month"], workspace, N,
           f"{t['bug']} bug phát sinh · {t['bug_done']} đã xử lý · {t['bug_days']} ngày "
           f"công ({pct(t['bug_days'], t['actual'])}% tổng effort) · "
           f"{t['bug_ratio_pct']}% số task")

    section(sh, "MỨC ĐỘ", N)
    sev = m["severity_breakdown"]
    for nm in ("Critical", "Major", "Minor"):
        if sev.get(nm):
            days = round(sum(b["actual"] for b in m["bugs"] if b["severity"] == nm), 2)
            sh.row([(nm, "kpi_label"), (sev[nm], SEV_STYLE[nm]), (days, "kpi_num"),
                    ("ngày công", "small")])
    sh.row([("Tổng", "kpi_label"), (t["bug"], "kpi_text"), (t["bug_days"], "kpi_num"),
            ("ngày công", "small")])
    sh.blank()

    section(sh, "BUG THEO HẠNG MỤC", N)
    scols = [("Hạng mục", "cell"), ("Bug", "int"), ("Đã xử lý", "int"),
             ("Effort\n(ngày)", "num"), ("% effort\nhạng mục", "pct"),
             ("% số task\nhạng mục", "pct"), ("", "cell"), ("", "cell"), ("", "cell")]
    srows = [[ten(nm), sv["bug"], sv["bug_done"], sv["bug_days"],
              pct(sv["bug_days"], sv["actual"]), sv["bug_ratio_pct"], "", "", ""]
             for nm, sv in tags_by_effort(m) if sv["bug"]]
    stotal = ["TỔNG", t["bug"], t["bug_done"], t["bug_days"],
              pct(t["bug_days"], t["actual"]), t["bug_ratio_pct"], "", "", ""]
    table(sh, scols, srows, stotal)
    sh.blank()

    section(sh, "DANH SÁCH BUG — xếp theo mức độ rồi effort giảm dần", N)
    cols = [("ID", "cell_c"), ("Bug", "cell_wrap"), ("Hạng mục", "cell"),
            ("Người", "cell"), ("Mức độ", "cell_c"), ("Xử lý", "cell_c"),
            ("Effort\n(ngày)", "num"), ("Phát hiện", "cell_c"), ("Đóng", "cell_c")]
    bugs = sorted(m["bugs"], key=lambda b: (SEV_ORDER.get(b["severity"], 3), -b["actual"]))
    rows = [[b["id"], b["title"], ", ".join(b["tags"]), b["assignee"],
             (b["severity"], SEV_STYLE.get(b["severity"], "cell_c")),
             b["resolution"] or b["status"], b["actual"], b["start"] or "—",
             b["done"] or "—"] for b in bugs]
    total = ["", "TỔNG", "", "", "", "", t["bug_days"], "", ""]
    r_head, r_last = table(sh, cols, rows, total, title_col=1)
    sh.autofilter(f"A{r_head}:{col_letter(N)}{r_last}")
    sh.blank()

    note(sh, f"Bug chiếm {t['bug_ratio_pct']}% số task nhưng chỉ "
             f"{pct(t['bug_days'], t['actual'])}% effort — nhiều về số lượng, nhỏ về công. "
             f"Cột Phát hiện/Đóng cho biết thời gian xử lý; phân tích cụm bug theo thời "
             f"gian và liên hệ với task hạ tầng nằm ở bản .md đầy đủ.", N, 26)
    sh.freeze(row=2)
    return sh


# ============================================================ sheet ton dong
def sheet_ton_dong(wb, m, workspace):
    sh = wb.sheet("Tồn đọng")
    sh.widths({1: 6, 2: 50, 3: 18, 4: 11, 5: 11, 6: 11, 7: 11, 8: 34})
    N = 8

    blocked_days = sum(x["actual"] for x in m["unfinished"])
    header(sh, m["month"], workspace, N,
           f"{len(m['unfinished'])} task chưa hoàn thành ({blocked_days} ngày công đã bỏ "
           f"ra đang treo) · {len(m['carryover'])} task chuyển tiếp tháng sau")

    if m["unfinished"]:
        section(sh, f"⚠ TASK CHƯA HOÀN THÀNH — {blocked_days} NGÀY CÔNG ĐANG TREO", N)
        cols = [("ID", "cell_c"), ("Task", "cell_wrap"), ("Hạng mục", "cell"),
                ("Người", "cell"), ("Trạng thái", "cell_c"),
                ("Đã bỏ công\n(ngày)", "num"), ("Checklist", "cell_c"),
                ("Blocker", "cell_wrap")]
        rows = [[x["id"], x["title"], ", ".join(x["tags"]), x["assignee"], x["status"],
                 x["actual"],
                 f"{x['todo_done']}/{x['todo_total']}" if x["todo_total"] else "—",
                 x["blocker"] or "KHÔNG GHI BLOCKER"]
                for x in sorted(m["unfinished"], key=lambda x: -x["actual"])]
        total = ["", "TỔNG", "", "", "", blocked_days, "", ""]
        table(sh, cols, rows, total, title_col=1)
        sh.blank()

    if m["carryover"]:
        section(sh, f"CHUYỂN TIẾP THÁNG SAU ({len(m['carryover'])} task, chưa bỏ công nào)", N)
        cols = [("ID", "cell_c"), ("Task", "cell_wrap"), ("Hạng mục", "cell"),
                ("Người", "cell"), ("Est AI\n(ngày)", "num"), ("Bắt đầu", "cell_c"),
                ("Hạn", "cell_c"), ("", "cell")]
        rows = [[x["id"], x["title"], ", ".join(x["tags"]), x["assignee"], x["est_ai"],
                 x["start"] or "—", x["due"] or "—", ""]
                for x in sorted(m["carryover"], key=lambda x: x["due"] or "9999")]
        table(sh, cols, rows, title_col=1)
        sh.blank()

    note(sh, "Task chuyển tiếp KHÔNG được tính vào tỷ lệ hoàn thành của tháng — chưa bỏ "
             "công nào nên tính vào sẽ kéo tụt số liệu sai.", N, 15)
    sh.freeze(row=2)
    return sh


def check_highlights(m, hl, path):
    """Doi chieu highlights voi metrics — chan file cua THANG KHAC bi dung lai.

    Ly do ton tai: cot "Dau viec chinh" nap tu file rieng, tra cuu chi theo TEN TAG.
    Tag la cap workspace (BizChat, BizAI...) nen ton tai xuyen thang -> highlights
    thang 07 khop khoa y het thang 08. Truoc khi co ham nay, chay bao cao thang 08
    voi file cu con sot lai trong thu muc se dua nguyen van bay viec cua thang 07
    (kem ngay 07/07 va id task cu) vao Excel, KHONG bao loi gi.

    Bat luon ca id go sai trong cung thang — cung mot phep kiem.

    Va bat ca id DUNG ky nhung SAI hang muc: chi kiem "id co ton tai" thi mot task
    cua tag khac dat nham duoi tag nay van lot, roi sheet cua tag nay khoe viec cua
    tag kia. Moi id phai thuc su mang tag dang xet.
    """
    ids = {x["id"] for x in m["tasks"]}
    tags_cua = {x["id"]: set(x["tags"]) for x in m["tasks"]}
    tags = set(m["by_tag"])
    loi = []

    for tag in hl:
        if tag not in tags:
            loi.append(f"  - tag {tag!r} khong co trong metrics.json "
                       f"(hang muc ky nay: {', '.join(sorted(tags))})")

    for tag, lines in hl.items():
        for line in lines:
            la = {int(i) for i in re.findall(r"#(\d+)", line)}
            if la - ids:
                thieu = ", ".join(f"#{i}" for i in sorted(la - ids))
                loi.append(f"  - {tag}: task {thieu} khong co trong ky nay "
                           f"— {line[:60]}...")
            # Tag khong hop le da bao o tren roi; kiem tiep chi de ra nhieu
            # dong loi noi cung mot chuyen.
            if tag not in tags:
                continue
            lac = sorted(i for i in la & ids if tag not in tags_cua[i])
            if lac:
                chi_tiet = ", ".join(
                    f"#{i} (thuoc {', '.join(sorted(tags_cua[i])) or 'khong hang muc nao'})"
                    for i in lac)
                loi.append(f"  - {tag}: task {chi_tiet} khong thuoc hang muc nay "
                           f"— {line[:60]}...")

    if loi:
        sys.exit(
            f"highlights.json KHONG khop metrics.json (ky {m['month']}):\n"
            + "\n".join(loi)
            + f"\n\nFile: {path}\n"
            "Gan nhu chac chan day la highlights cua THANG KHAC bi dung lai, id go sai,\n"
            "hoac dau viec bi xep nham sang hang muc khac.\n"
            "Soan lai highlights cho ky nay (Buoc 3c trong SKILL.md) — dung sua script."
        )


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--metrics", required=True, help="metrics.json do pi_report.py sinh")
    ap.add_argument("--out", required=True, help="duong dan file .xlsx")
    ap.add_argument("--workspace", default="BizchatAI")
    ap.add_argument("--highlights", default=None,
                    help="highlights-<MM>-<YYYY>.json — dau viec chinh do NGUOI VIET tong hop "
                         "cho tung hang muc. Khong co thi cot 'Dau viec chinh' rot ve liet ke "
                         "tieu de. Doi chieu voi metrics truoc khi dung (xem check_highlights).")
    args = ap.parse_args()

    m = json.loads(Path(args.metrics).read_text())
    # Dau viec chinh la VAN BAN TONG HOP, script khong sinh duoc tu title/description.
    # Nap tu file rieng neu co; thieu thi fallback liet ke tieu de (xem overview_of).
    m["_highlights"] = json.loads(Path(args.highlights).read_text()) if args.highlights else {}
    if m["_highlights"]:
        check_highlights(m, m["_highlights"], args.highlights)

    wb = Workbook()
    sheet_tong_quan(wb, m, args.workspace)
    tags = tags_by_effort(m)
    for name, v in tags:
        sheet_hang_muc(wb, m, args.workspace, name, v)
    sheet_bug(wb, m, args.workspace)
    sheet_ton_dong(wb, m, args.workspace)
    wb.save(args.out)

    t = m["total"]
    print(f"OK: {args.out}")
    print(f"  {len(wb.sheets)} sheet: " + " · ".join(s.name for s in wb.sheets))
    print(f"  {t['n']} task · {t['done']} done · {t['bug']} bug · "
          f"{t['actual']} ngày thực tế · tiết kiệm {t['saved_days']} ngày "
          f"({t['productivity_x']}×)")
    for name, v in tags:
        print(f"    {name:20s} {v['done']}/{v['n']} task · {v['actual']:6.2f} ngày · "
              f"tiết kiệm {v['saved_days']:5.2f} · {v['productivity_x']}× · "
              f"{v['bug']} bug")
    if m.get("needs_tag"):
        print(f"  ⚠ {len(m['needs_tag'])} task chưa gắn tag — gắn trong PI Tracker rồi chạy lại")
    ov = m.get("tag_overlap") or {}
    if ov.get("days"):
        print(f"  ⚠ {ov['tasks']} task gắn nhiều tag: cộng dồn các hạng mục ra "
              f"{ov['tag_actual_sum']} ngày, thực tế {ov['real_actual']} ngày "
              f"(đếm trùng {ov['days']} ngày)")


if __name__ == "__main__":
    main()
