#!/usr/bin/env python3
"""Tinh va kiem tra ban estimate tu wbs-<slug>.json. Nguon duy nhat cho moi con so.

Khong bao gio tu cong tay cac tong trong ban estimate. Cong tay la loi AM THAM:
ban estimate van doc tron tru, khach van ky, va phan lech chi lo ra luc lam that.

Script lam 3 viec, theo thu tu:
  1. KIEM TRA (thoat 1 neu sai): dau viec thieu truong, internal_days > quote_days,
     comparable tro tro den task khong ton tai, contingency thap hon chi phi bug that.
  2. TINH: tong theo nhom, contingency, dai tin cay theo confidence, he so AI ngu y.
  3. IN: bang markdown dan thang vao ban estimate + estimate-<slug>.json.

KHONG sinh bao gia. Khong don gia, khong tien, khong tong tien — chi ngay cong.

Usage:
  python3 estimate_calc.py --wbs wbs-<slug>.json --tasks tasks.json \
      [--baseline baseline.json] [--out-md estimate-<slug>.md] \
      [--out-json estimate-<slug>.json] [--out-xlsx estimate-<slug>.xlsx]
"""
import argparse
import json
import re
import sys
from collections import OrderedDict
from pathlib import Path

# Dai tin cay theo do chac chan cua tung dau viec. Khong phai con so tuy y: neo
# vao so task lich su tuong duong tim duoc (xem SKILL.md Buoc 4).
BAND = {"high": 0.15, "med": 0.25, "low": 0.40}

# Cac loai viec AN — khong nam trong danh sach tinh nang khach doc ra, nhung luon
# ton chi phi that. Thieu nhom nao thi ban estimate dang thieu ngay cong o do.
HIDDEN = OrderedDict([
    ("phan tich / chot yeu cau", ("phan tich", "yeu cau", "khao sat", "lam ro", "requirement", "analysis")),
    ("thiet ke", ("thiet ke", "design", "kien truc", "wireframe", "ui", "ux")),
    ("kiem thu", ("kiem thu", "test", "qa", "qc")),
    ("nghiem thu / sua loi sau UAT", ("nghiem thu", "uat", "sua loi", "fix", "rework", "bug")),
    ("ban giao / tai lieu / trien khai", ("ban giao", "tai lieu", "doc", "deploy", "trien khai", "release")),
])

REQUIRED = ("id", "group", "task", "quote_days", "internal_days", "confidence")

# Cap bac mac dinh khi dau viec khong ghi `level`. De text tu do (khong ep danh
# sach co dinh) vi thang bac la quy uoc nhan su, doi theo thoi gian — ep cung o
# day se bien mot thay doi HR thanh loi script.
DEFAULT_LEVEL = "B2"


def no_accent(s):
    import unicodedata
    s = unicodedata.normalize("NFD", str(s).lower())
    return "".join(c for c in s if unicodedata.category(c) != "Mn").replace("đ", "d")


def has_keyword(hay, kw):
    """Tim tu khoa trong text, KHOP TU DAU TU chu khong khop chuoi con.

    Bat buoc phai chan dau tu: khop chuoi con thi "ui" trung trong "gui" (gui tin
    nhan), lam phep kiem "thieu dau viec thiet ke" im lang bo qua — dung kieu loi
    canh bao khong bao gio no nen khong ai biet la no da hong.

    Chan cuoi tu thi KHONG chan, de "doc" con khop "docs", "test" khop "testing",
    "deploy" khop "deployment".
    """
    if " " in kw:
        return kw in hay
    return re.search(r"\b" + re.escape(kw), hay) is not None


def fmt(v, nd=2):
    if v is None:
        return "—"
    return f"{v:.{nd}f}".rstrip("0").rstrip(".") or "0"


def die(errors):
    print("\n".join("LOI: " + e for e in errors), file=sys.stderr)
    sys.exit(1)


def validate(wbs, tasks_by_id, baseline):
    """Tra ve (errors, warnings). errors -> thoat 1; warnings -> in ra nhung van chay."""
    errors, warns = [], []
    items = wbs.get("items") or []
    if not items:
        errors.append("wbs khong co dau viec nao (`items` rong).")
        return errors, warns

    seen = set()
    for i, it in enumerate(items):
        where = f"dau viec #{i + 1} ({it.get('id') or 'khong co id'})"
        for f in REQUIRED:
            if it.get(f) in (None, ""):
                errors.append(f"{where}: thieu truong `{f}`.")
        if it.get("id") in seen:
            errors.append(f"{where}: id trung.")
        seen.add(it.get("id"))

        # Truong cua mau estimate cong ty. Deu tuy chon — WBS cu khong co chung
        # van chay duoc, chi nhan gia tri mac dinh.
        inherited = bool(it.get("inherited"))
        dev = it.get("dev_count", 1)
        if not isinstance(dev, int) or isinstance(dev, bool) or dev < 1:
            errors.append(f"{where}: dev_count = {dev!r}, phai la so nguyen >= 1.")
        if it.get("level") is not None and not str(it.get("level")).strip():
            errors.append(f"{where}: `level` de chuoi rong. Bo han truong nay de lay mac dinh "
                          f"{DEFAULT_LEVEL}, dung de rong.")
        if inherited and not (it.get("comparables") or []):
            warns.append(f"{where}: danh dau `inherited` nhung khong co comparable nao. "
                         "Ke thua tu dau? Ghi id task da lam de con so 0 ngay con phan bien duoc.")

        q, n = it.get("quote_days"), it.get("internal_days")
        if not isinstance(q, (int, float)) or not isinstance(n, (int, float)):
            continue
        # 0 ngay chi hop le voi dau viec ke thua hoan toan (dung lai nguyen code
        # da co). Khong co ngoai le nay thi khong dien duoc mau cong ty — dong
        # ET = 0 la cach mau the hien phan tai su dung.
        if q < 0 or (q == 0 and not inherited):
            errors.append(f"{where}: quote_days = {q}, phai > 0 (chi dau viec co "
                          "`inherited: true` moi duoc de 0).")
        if n < 0 or (n == 0 and not inherited):
            errors.append(f"{where}: internal_days = {n}, phai > 0 (chi dau viec co "
                          "`inherited: true` moi duoc de 0).")
        if n > q:
            # Dinh nghia dao nguoc: quote_days la effort KHI KHONG CO AI, internal_days
            # la ky vong KHI CO AI. internal > quote nghia la dang bao khach thap hon
            # chinh ky vong noi bo cua minh — cam ket lo ngay tu tren giay.
            errors.append(f"{where}: internal_days ({n}) > quote_days ({q}). "
                          "quote_days la effort KHONG co AI nen phai >= internal_days. "
                          "Kiem lai xem co dien nguoc hai cot khong.")
        if it.get("confidence") not in BAND:
            errors.append(f"{where}: confidence = {it.get('confidence')!r}, phai la high|med|low.")

        for cid in (it.get("comparables") or []):
            if cid not in tasks_by_id:
                errors.append(f"{where}: comparable #{cid} khong co trong danh sach task. "
                              "Dung bia id — chay lai estimate_baseline.py --like de lay id that.")
        if not (it.get("comparables") or []):
            if it.get("confidence") != "low":
                errors.append(f"{where}: khong co comparable nao thi confidence phai la 'low' "
                              f"(dang la '{it.get('confidence')}'). Khong co moc neo thi khong duoc tu tin.")
            else:
                warns.append(f"{where}: khong co task lich su tuong duong — con so nay la phong doan, "
                             "phai noi ro trong phan gia dinh.")
        if isinstance(q, (int, float)) and abs(q * 2 - round(q * 2)) > 1e-9:
            warns.append(f"{where}: quote_days = {q} khong phai boi cua 0.5. "
                         "Do chinh xac gia trong ban bao khach lam mat tin cay — lam tron ve nua ngay.")

    # Nhom viec an: kiem tra co mat, dua tren ten nhom + ten dau viec.
    hay = no_accent(" | ".join(str(it.get("group", "")) + " " + str(it.get("task", "")) for it in items))
    for label, keys in HIDDEN.items():
        if not any(has_keyword(hay, k) for k in keys):
            warns.append(f"Khong thay dau viec nao thuoc \"{label}\". Loai viec nay khong nam trong "
                         "danh sach tinh nang khach doc ra nhung luon ton ngay cong that — "
                         "bo sung, hoac ghi vao `exclusions` la co y khong bao.")

    ct = wbs.get("contingency_pct")
    if ct is None:
        errors.append("thieu `contingency_pct`. Ghi 0 kem ly do trong `assumptions` neu co y khong dat du phong.")
    elif baseline:
        hist = (baseline.get("overall") or {}).get("bug_overhead_pct")
        if hist and ct < hist:
            warns.append(f"contingency_pct = {ct}% thap hon chi phi bug/rework THUC TE trong lich su "
                         f"({hist}% ngay cong ke hoach). Lich su da tra tien cho phan nay roi — "
                         "ha xuong duoi muc do la tu an vao bien.")
    return errors, warns


def compute(wbs, baseline):
    items = wbs["items"]
    # Chuan hoa MOT lan o day de ban .md, .json va .xlsx khong the lech nhau —
    # neu moi ham render tu ap mac dinh rieng thi ba file se ke ba con so khac
    # nhau ma khong co phep kiem nao bat duoc.
    for it in items:
        it["dev_count"] = it.get("dev_count", 1)
        it["level"] = str(it.get("level") or DEFAULT_LEVEL)
        it["inherited"] = bool(it.get("inherited"))
        it["desc"] = it.get("desc") or ""
        # `desc` (mo ta cot 2 cua mau Excel) va `note` (vi sao lech khoi moc neo)
        # deu la chu gui ra ngoai. Ghep MOT chuoi dung cho ca .md lan .xlsx: neu
        # de moi ban lay mot truong khac nhau thi hai tai lieu cung mot estimate
        # se mo ta hai pham vi khac nhau, va khong ai doi chieu chung.
        it["detail"] = " — ".join(x for x in (it["desc"], it.get("note") or "") if x)

    groups = OrderedDict()
    for it in items:
        groups.setdefault(it["group"], []).append(it)

    def agg(rows):
        q = sum(r["quote_days"] for r in rows)
        n = sum(r["internal_days"] for r in rows)
        # Dai tin cay cong TUYEN TINH theo tung dau viec, khong lay can binh phuong:
        # gia dinh sai lech cua cac dau viec cung chieu (thuong dung — cung mot nguoi
        # hieu sai cung mot pham vi). Ket qua la dai RONG hon, tuc an ve phia an toan.
        lo = sum(r["quote_days"] * (1 - BAND[r["confidence"]]) for r in rows)
        hi = sum(r["quote_days"] * (1 + BAND[r["confidence"]]) for r in rows)
        return dict(n_items=len(rows), quote=round(q, 2), internal=round(n, 2),
                    low=round(lo, 2), high=round(hi, 2),
                    ai_factor=round(q / n, 2) if n else None)

    by_group = OrderedDict((g, agg(rows)) for g, rows in groups.items())
    base = agg(items)

    ct_pct = wbs["contingency_pct"]
    ct_quote = round(base["quote"] * ct_pct / 100, 2)
    ct_internal = round(base["internal"] * ct_pct / 100, 2)

    total = dict(
        quote=round(base["quote"] + ct_quote, 2),
        internal=round(base["internal"] + ct_internal, 2),
        low=round(base["low"] + ct_quote, 2),
        high=round(base["high"] + ct_quote, 2),
    )
    total["ai_factor"] = round(total["quote"] / total["internal"], 2) if total["internal"] else None

    checks = []
    hist_ratio = ((baseline or {}).get("overall") or {}).get("actual_vs_quote", {}).get("med_ratio")
    implied_ratio = round(total["internal"] / total["quote"], 3) if total["quote"] else None
    if hist_ratio and implied_ratio and implied_ratio < hist_ratio * 0.8:
        checks.append(
            f"He so AI ngu y trong ban nay ({implied_ratio}) LAC QUAN hon lich su ({hist_ratio}) qua 20%. "
            "Nghia la ke hoach noi bo dang ky vong AI giup nhieu hon muc tung dat duoc. "
            "Ha internal_days xuong qua muc lich su khong lam bao gia tot hon — no chi lam lich cam ket vo.")
    if hist_ratio and implied_ratio and implied_ratio > 0.95:
        checks.append(
            f"He so AI ngu y ({implied_ratio}) gan bang 1: quote_days va internal_days gan nhu trung nhau, "
            "tuc ban nay khong con bien nao cho AI. Kiem lai xem co lay actual lich su lam quote_days khong "
            "(day dung la bay 'ratchet' o SKILL.md).")

    return dict(
        feature=wbs.get("feature", ""), customer=wbs.get("customer", ""),
        prepared=wbs.get("prepared", ""),
        product=wbs.get("product", ""), support_staff=wbs.get("support_staff", ""),
        function_doc=wbs.get("function_doc", ""),
        by_group=by_group, subtotal=base,
        contingency=dict(pct=ct_pct, quote=ct_quote, internal=ct_internal),
        total=total, items=items,
        assumptions=wbs.get("assumptions") or [], exclusions=wbs.get("exclusions") or [],
        checks=checks,
    )


def render_md(r, warns):
    L = []
    a = L.append
    a(f"# Estimate khoi luong cong viec — {r['feature'] or '(chua dat ten tinh nang)'}\n")
    meta = [x for x in (f"Khach hang: {r['customer']}" if r["customer"] else "",
                        f"Lap ngay: {r['prepared']}" if r["prepared"] else "") if x]
    if meta:
        a(" · ".join(meta) + "\n")
    t = r["total"]
    a(f"**Tong khoi luong bao khach: {fmt(t['quote'])} ngay cong** "
      f"(dai {fmt(t['low'])}–{fmt(t['high'])} ngay cong).\n")
    a("> Day la **khoi luong cong viec (ngay cong)**, khong phai bao gia va khong phai thoi gian lich. "
      "Quy doi ra ngay lich can biet so nguoi tham gia va cac dau viec nao buoc phai lam tuan tu.\n")

    a("\n## Bang dau viec\n")
    a("| Ma | Dau viec | Ngay cong | Do chac chan | Can cu (task da lam) |")
    a("|---|---|---:|---|---|")
    for g, gv in r["by_group"].items():
        a(f"| | **{g}** | **{fmt(gv['quote'])}** | | |")
        for it in r["items"]:
            if it["group"] != g:
                continue
            cmp_ = ", ".join(f"#{c}" for c in (it.get("comparables") or [])) or "khong co moc neo"
            note = f" — {it['detail']}" if it.get("detail") else ""
            a(f"| {it['id']} | {it['task']}{note} | {fmt(it['quote_days'])} "
              f"| {it['confidence']} | {cmp_} |")
    s = r["subtotal"]
    a(f"| | **Tong cong viec ({s['n_items']} dau viec)** | **{fmt(s['quote'])}** | | |")
    c = r["contingency"]
    a(f"| | **Du phong sua loi / dieu chinh sau nghiem thu ({fmt(c['pct'], 1)}%)** "
      f"| **{fmt(c['quote'])}** | | |")
    a(f"| | **TONG** | **{fmt(t['quote'])}** | | |")

    a("\n## Gia dinh — con so tren chi dung khi cac dieu nay dung\n")
    for x in r["assumptions"] or ["(chua ghi gia dinh nao — day la thieu sot, bo sung truoc khi gui)"]:
        a(f"- {x}")
    a("\n## Khong bao gom\n")
    for x in r["exclusions"] or ["(chua ghi — moi thu khong liet ke o bang tren deu se bi hieu la co bao)"]:
        a(f"- {x}")

    a("\n---\n")
    a("## Phan noi bo — KHONG gui khach\n")
    a(f"- Ky vong noi bo (co AI ho tro): **{fmt(t['internal'])} ngay cong** "
      f"— he so {fmt(t['ai_factor'])}x so voi so bao khach.")
    a(f"- Bien du kien: {fmt(t['quote'] - t['internal'])} ngay cong.")
    a("- So bao khach la effort khi lam **khong co AI**; day moi la moc dung de bao. "
      "Bao bang so noi bo la trao het bien AI cho khach va khong con dem cho rework.")
    a("\n| Nhom | Dau viec | Bao khach | Noi bo | He so |")
    a("|---|---:|---:|---:|---:|")
    for g, gv in r["by_group"].items():
        a(f"| {g} | {gv['n_items']} | {fmt(gv['quote'])} | {fmt(gv['internal'])} | {fmt(gv['ai_factor'])}x |")
    a(f"| **Tong (chua du phong)** | **{s['n_items']}** | **{fmt(s['quote'])}** "
      f"| **{fmt(s['internal'])}** | **{fmt(s['ai_factor'])}x** |")

    if r["checks"] or warns:
        a("\n### Canh bao tu script\n")
        for x in r["checks"] + warns:
            a(f"- {x}")
    return "\n".join(L) + "\n"


def write_xlsx(r, path):
    """Ban .xlsx de gui/trinh bay. Tai dung xlsx_writer cua skill bao-cao-thang.

    Cung mot plugin nen duong dan tuong doi nay on dinh du plugin duoc cai bang
    marketplace local hay clone tu GitHub.
    """
    here = Path(__file__).resolve().parent
    sys.path.insert(0, str(here.parent.parent / "bao-cao-thang" / "scripts"))
    try:
        from xlsx_writer import Workbook
    except ImportError:
        sys.exit("Khong nap duoc xlsx_writer.py (o skills/bao-cao-thang/scripts/). "
                 "Bo --out-xlsx de chi sinh ban markdown.")

    NCOL = 7          # A..G — so cot cua mau
    LAST = "G"

    def full(cells, style):
        """Dien du NCOL o cho mot dong sap merge.

        Merge trong Excel chi lay dinh dang cua o goc de TO NEN, nhung VIEN thi
        ve theo tung o. Bo trong cac o bi merge se ra khoi mau thung day khong
        co vien phai/duoi — nen o nao cung phai duoc ghi kem style.
        """
        out = [(c, style) for c in cells]
        out += [("", style)] * (NCOL - len(out))
        return out

    wb = Workbook()
    sh = wb.sheet("Estimate")
    sh.widths({1: 6, 2: 34, 3: 58, 4: 13, 5: 11, 6: 12, 7: 13})

    # ---- Khoi meta (nhan nen vang, gia tri trai) --------------------------
    for label, value in (("Khách hàng", r["customer"]),
                         ("Nhân viên hỗ trợ", r["support_staff"]),
                         ("Sản phẩm", r["product"]),
                         ("Chức năng", r["function_doc"] or r["feature"])):
        sh.row([(label, "tpl_label"), ("", "tpl_label")]
               + [(value if i == 0 else "", "tpl_value") for i in range(NCOL - 2)])
        sh.merge(f"A{sh.cursor}:B{sh.cursor}")
        sh.merge(f"C{sh.cursor}:{LAST}{sh.cursor}")
    sh.blank()

    # ---- Header 2 tang ----------------------------------------------------
    sh.row([("STT", "tpl_h"), ("Tính năng", "tpl_h"), ("", "tpl_h"), ("ET", "tpl_h"),
            ("", "tpl_h"), ("", "tpl_h"), ("Có tính kế thừa", "tpl_h")])
    h1 = sh.cursor
    sh.row([("", "tpl_h"), ("", "tpl_h"), ("", "tpl_h"),
            ("Thời gian triển khai\n(Ngày)", "tpl_h"), ("Số lượng\n(Dev)", "tpl_h"),
            ("Trình độ\n(Cấp bậc)", "tpl_h"), ("", "tpl_h")], height=34)
    h2 = sh.cursor
    sh.merge(f"A{h1}:A{h2}")           # STT
    sh.merge(f"B{h1}:C{h2}")           # Tinh nang (ten + mo ta)
    sh.merge(f"D{h1}:F{h1}")           # ET
    sh.merge(f"{LAST}{h1}:{LAST}{h2}")  # Co tinh ke thua
    sh.freeze(row=h2)

    # ---- Cac dong dau viec ------------------------------------------------
    stt = 0
    for g, gv in r["by_group"].items():
        sh.row(full([g], "section")[:3] + [(gv["quote"], "tot_num")]
               + [("", "section")] * 3)
        sh.merge(f"A{sh.cursor}:C{sh.cursor}")
        for it in r["items"]:
            if it["group"] != g:
                continue
            stt += 1
            sh.row([(stt, "cell_c"), (it["task"], "cell_wrap"),
                    (it["detail"], "cell_wrap"),
                    (it["quote_days"], "num"), (it["dev_count"], "int"),
                    (it["level"], "cell_c"),
                    ("x" if it["inherited"] else "", "cell_c")])

    # ---- Ba dong tong -----------------------------------------------------
    s, c, t = r["subtotal"], r["contingency"], r["total"]
    for label, val in ((f"Tổng công việc ({s['n_items']} đầu việc)", s["quote"]),
                       (f"Dự phòng sửa lỗi / điều chỉnh sau nghiệm thu ({c['pct']}%)", c["quote"])):
        sh.row([("", "tot"), (label, "tot"), ("", "tot"), (val, "tot_num"),
                ("", "tot"), ("", "tot"), ("", "tot")])
        sh.merge(f"B{sh.cursor}:C{sh.cursor}")

    sh.row([("TỔNG", "tpl_tot"), ("(không tính T7/CN)", "tpl_tot_note"), ("", "tpl_tot"),
            (t["quote"], "tpl_tot_num"), ("", "tpl_tot"), ("", "tpl_tot"), ("", "tpl_tot")])
    sh.merge(f"B{sh.cursor}:C{sh.cursor}")

    sh.row(full([f"Dải tin cậy: {fmt(t['low'])}–{fmt(t['high'])} ngày công. "
                 "Đây là KHỐI LƯỢNG CÔNG VIỆC (ngày công), không phải báo giá và không phải "
                 "số ngày lịch — quy đổi ra lịch cần biết số người và thứ tự phụ thuộc giữa "
                 "các đầu việc. Tổng đã bao gồm thời gian kiểm thử.", ], "warn"))
    sh.merge(f"A{sh.cursor}:{LAST}{sh.cursor}")

    # ---- Gia dinh / Khong bao gom -----------------------------------------
    for title, rows in (("Giả định — con số trên chỉ đúng khi các điều này đúng", r["assumptions"]),
                        ("Không bao gồm", r["exclusions"])):
        sh.blank()
        sh.row(full([title], "section"))
        sh.merge(f"A{sh.cursor}:{LAST}{sh.cursor}")
        for x in rows or ["(chưa ghi)"]:
            sh.row([("", "cell"), (x, "cell_wrap")] + [("", "cell")] * (NCOL - 2))
            sh.merge(f"B{sh.cursor}:{LAST}{sh.cursor}")

    wb.save(path)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--wbs", required=True)
    ap.add_argument("--tasks", required=True,
                    help="Danh sach task lich su — de kiem comparable co that.")
    ap.add_argument("--baseline", default=None, help="baseline.json tu estimate_baseline.py")
    ap.add_argument("--out-md", default=None)
    ap.add_argument("--out-json", default=None)
    ap.add_argument("--out-xlsx", default=None)
    args = ap.parse_args()

    wbs = json.loads(Path(args.wbs).read_text())
    tasks_by_id = {t["id"]: t for t in json.loads(Path(args.tasks).read_text())}
    baseline = json.loads(Path(args.baseline).read_text()) if args.baseline else None

    errors, warns = validate(wbs, tasks_by_id, baseline)
    if errors:
        die(errors)

    r = compute(wbs, baseline)
    md = render_md(r, warns)

    if args.out_json:
        Path(args.out_json).write_text(json.dumps(r, ensure_ascii=False, indent=1))
    if args.out_md:
        Path(args.out_md).write_text(md)
    if args.out_xlsx:
        write_xlsx(r, args.out_xlsx)

    print(md)
    for x in warns:
        print(f"CANH BAO: {x}", file=sys.stderr)
    outs = [p for p in (args.out_md, args.out_json, args.out_xlsx) if p]
    if outs:
        print("[-> " + ", ".join(outs) + "]")


if __name__ == "__main__":
    main()
