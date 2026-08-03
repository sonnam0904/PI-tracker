#!/usr/bin/env python3
"""Doc du lieu lich su PI Tracker -> moc neo (baseline) de estimate tinh nang moi.

Script nay KHONG estimate ho. No chi tra ve: task da lam that trong qua khu giong
voi viec dang can bao, va phan bo ngay cong cua chung. Viec chia dau viec va chon
con so la viec cua nguoi/agent doc ket qua nay.

HAI MOC NEO KHAC NHAU, DUNG LAN LA SAI TIEN THAT:

  quote_days   (-> estimateCustomerDays) neo bang `est_customer` LICH SU
  internal_days(-> estimateAiDays)       neo bang `actual` LICH SU

Dung neo quote_days bang `actual`. Actual thap la nho AI; lay actual lam gia bao
khach nghia la moi vong bao gia lai tu ha xuong (ratchet) cho den khi bien AI bang 0
va moi rui ro roi ve minh. Chi tiet o SKILL.md muc "Bay 2".

Chi task DA XONG duoc tinh vao moc neo: task dang lam co `actual` do dang nen se
keo trung vi xuong. Task chua ghi est_customer cung bi loai khoi thong ke est.

Usage:
  python3 estimate_baseline.py --tasks tasks.json [--out baseline.json]
      [--like "tich hop zalo oa gui tin nhan"] [--like "..."] [--top 6]
      [--tag "Ha tang"] [--full]
"""
import argparse
import json
import re
import sys
import unicodedata
from collections import defaultdict
from pathlib import Path

TYPE_PLAN = (1, 3)   # Theo plan | Phat sinh theo plan
TYPE_BUG = 2
NO_SIZE = "(chua ghi size)"
NO_TAG = "(chua gan tag)"

# Tu qua pho bien trong tieu de/mo ta task, khong mang thong tin phan biet khi do
# do giong nhau. Giu ngan — cat qua tay se lam mat tin hieu that.
STOP = set("""
va voi cho cua cac mot nhung khi tren duoi de la co khong bi tu den den ra vao
task tinh nang chuc nang he thong phan viec lam theo moi them sua fix update
the a an of for to in on and or new
""".split())


def no_accent(s):
    """Bo dau tieng Viet de 'ha tang' khop 'ha tang'/'Ha Tang'.

    So khop khong dau la CO Y: nguoi viet yeu cau tinh nang thuong go khong dau,
    trong khi task trong tracker co dau day du.
    """
    s = unicodedata.normalize("NFD", s.lower())
    s = "".join(c for c in s if unicodedata.category(c) != "Mn")
    return s.replace("đ", "d").replace("Đ", "d")


def tokens(s):
    return {w for w in re.split(r"[^0-9a-z]+", no_accent(s or "")) if len(w) > 2 and w not in STOP}


def pct_of(sorted_vals, q):
    """Phan vi theo noi suy tuyen tinh. Tu viet de chay duoc tren Python 3.6."""
    if not sorted_vals:
        return None
    if len(sorted_vals) == 1:
        return round(sorted_vals[0], 2)
    pos = (len(sorted_vals) - 1) * q
    lo = int(pos)
    hi = min(lo + 1, len(sorted_vals) - 1)
    frac = pos - lo
    return round(sorted_vals[lo] * (1 - frac) + sorted_vals[hi] * frac, 2)


def spread(vals):
    """Phan bo mot cot ngay cong: n / p25 / trung vi / p75 / min / max."""
    v = sorted(x for x in vals if x and x > 0)
    return dict(n=len(v), p25=pct_of(v, 0.25), med=pct_of(v, 0.5), p75=pct_of(v, 0.75),
                min=round(v[0], 2) if v else None, max=round(v[-1], 2) if v else None)


def is_calibratable(t):
    """Task dung duoc lam moc neo: da xong VA co ngay cong thuc te.

    Task dang lam bi loai vi `actual` con do dang — dua vao se keo trung vi xuong
    va lam moi estimate sau do thap he thong.
    """
    return t.get("status") == "Done" and (t.get("actualDays") or 0) > 0


def ratio_stats(pairs):
    """Ty le actual/est cua tung task roi lay trung vi — KHONG phai tong/tong.

    Trung vi cua ty le mo ta "mot task binh thuong lech bao nhieu"; tong/tong bi
    vai task XL nuot het cac task nho. Bao gia dung ca hai cho khac nhau nen tra ve
    ca `med_ratio` (moc neo cho 1 dau viec) va `agg_ratio` (kiem tra tong ca goi).
    """
    rs = sorted(a / e for e, a in pairs if e > 0 and a > 0)
    se, sa = sum(e for e, _ in pairs), sum(a for _, a in pairs)
    return dict(
        n=len(rs), med_ratio=pct_of(rs, 0.5),
        agg_ratio=round(sa / se, 3) if se else None,
        p25_ratio=pct_of(rs, 0.25), p75_ratio=pct_of(rs, 0.75),
    )


def bucket(tasks):
    """Moc neo cho mot nhom task (mot tag, mot size, hoac toan bo)."""
    cal = [t for t in tasks if is_calibratable(t)]
    plan = [t for t in cal if t.get("type") in TYPE_PLAN]
    bugs = [t for t in cal if t.get("type") == TYPE_BUG]

    plan_days = sum(t.get("actualDays") or 0 for t in plan)
    bug_days = sum(t.get("actualDays") or 0 for t in bugs)

    ec_ac = [(t.get("estimateCustomerDays") or 0, t.get("actualDays") or 0) for t in plan]
    ea_ac = [(t.get("estimateAiDays") or 0, t.get("actualDays") or 0) for t in plan]

    return dict(
        n_all=len(tasks), n_done=len(cal), n_plan=len(plan), n_bug=len(bugs),
        # Moc neo cho quote_days: est_customer cua task tuong duong da lam.
        quote_anchor=spread([t.get("estimateCustomerDays") or 0 for t in plan]),
        # Moc neo cho internal_days: ngay cong THUC TE.
        internal_anchor=spread([t.get("actualDays") or 0 for t in plan]),
        est_ai_spread=spread([t.get("estimateAiDays") or 0 for t in plan]),
        # actual / est_customer — luon < 1 khi AI giup that. KHONG dung so nay de
        # ha quote_days; no chi de biet bien hien tai rong bao nhieu.
        actual_vs_quote=ratio_stats(ec_ac),
        # actual / est_ai — > 1 nghia la thuc te CHAM hon du kien AI. Day moi la
        # so phai nhan vao internal_days khi cam ket lich.
        actual_vs_est_ai=ratio_stats(ea_ac),
        # Chi phi bug/rework thuc te, tinh theo % ngay cong ke hoach. Day la san
        # de chon contingency, khong phai con so bốc.
        bug_overhead_pct=round(bug_days / plan_days * 100, 1) if plan_days else None,
        plan_days=round(plan_days, 2), bug_days=round(bug_days, 2),
    )


def score(t, qtok):
    """Do giong nhau giua task lich su va cau truy van.

    Tieu de nang hon mo ta vi mo ta dai nen de trung tu ngau nhien; tag nang vua
    phai vi no la phan loai that do nguoi lam gan.
    """
    if not qtok:
        return 0.0
    ti, de = tokens(t.get("title")), tokens(t.get("description"))
    tg = tokens(" ".join(t.get("tags") or []))
    hit = len(qtok & ti) * 3 + len(qtok & tg) * 2 + len(qtok & de)
    return round(hit / (len(qtok) * 3), 3)


# Nguong lien quan: task phai khop it nhat 1/3 so tu cua cau truy van moi duoc coi
# la moc neo. Khong co nguong thi mot task trung dung tu "tich hop" cung nhay vao
# bang, va so ngay cong cua no bi dung lam can cu cho viec chang lien quan gi.
MIN_MATCH_RATIO = 1 / 3


def comparables(tasks, query, top):
    qtok = tokens(query)
    scored = []
    for t in tasks:
        if not is_calibratable(t):
            continue
        matched = qtok & (tokens(t.get("title")) | tokens(t.get("description"))
                          | tokens(" ".join(t.get("tags") or [])))
        if not qtok or len(matched) / len(qtok) < MIN_MATCH_RATIO:
            continue
        scored.append((score(t, qtok), sorted(matched), t))
    scored.sort(key=lambda x: (-x[0], -(x[2].get("actualDays") or 0)))
    return [
        dict(score=s, matched=m, n_query_tokens=len(qtok),
             id=t["id"], title=(t.get("title") or "").strip(),
             tags=t.get("tags") or [], size=t.get("size") or "",
             type=t.get("type"), ai_used=bool(t.get("aiUsed")),
             est_customer=t.get("estimateCustomerDays") or 0,
             est_ai=t.get("estimateAiDays") or 0,
             actual=t.get("actualDays") or 0,
             done=t.get("doneDate") or "")
        for s, m, t in scored[:top]
    ]


def fmt(v, nd=2):
    return "—" if v is None else (f"{v:.{nd}f}".rstrip("0").rstrip(".") or "0")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tasks", required=True)
    ap.add_argument("--out", default="baseline.json")
    ap.add_argument("--like", action="append", default=[], metavar="TU_KHOA",
                    help="Tim task lich su tuong duong. Lap lai duoc, moi dau viec mot lan.")
    ap.add_argument("--tag", action="append", default=[], metavar="TAG",
                    help="Chi in moc neo cua tag nay (van ghi day du ra --out).")
    ap.add_argument("--top", type=int, default=6, help="So task tuong duong moi truy van")
    ap.add_argument("--full", action="store_true", help="In het cac tag, khong cat 8 dong")
    args = ap.parse_args()

    tasks = json.loads(Path(args.tasks).read_text())
    if not isinstance(tasks, list) or not tasks:
        sys.exit(f"{args.tasks}: khong doc duoc danh sach task nao.")

    cal = [t for t in tasks if is_calibratable(t)]
    if not cal:
        sys.exit("Khong co task nao da Done kem ngay cong thuc te — chua co gi de neo. "
                 "Estimate luc nay la phong doan, phai noi ro voi nguoi dung.")

    by_tag, by_size = defaultdict(list), defaultdict(list)
    for t in tasks:
        for tg in ([x.strip() for x in (t.get("tags") or []) if x.strip()] or [NO_TAG]):
            by_tag[tg].append(t)
        by_size[t.get("size") or NO_SIZE].append(t)

    overall = bucket(tasks)
    no_ai = [t for t in cal if not t.get("aiUsed") and t.get("type") in TYPE_PLAN]

    baseline = dict(
        source=args.tasks,
        n_tasks=len(tasks), n_calibratable=len(cal),
        overall=overall,
        by_tag={k: bucket(v) for k, v in sorted(
            by_tag.items(), key=lambda kv: -sum(t.get("actualDays") or 0 for t in kv[1]))},
        by_size={k: bucket(by_size[k]) for k in ["S", "M", "L", "XL", NO_SIZE] if k in by_size},
        # Nhom doi chung: task KHONG dung AI. Day la bang chung truc tiep nhat cho
        # muc quote_days (effort khi khong co AI). n < 5 thi khong ket luan duoc.
        control_no_ai=dict(
            n=len(no_ai),
            est_customer=round(sum(t.get("estimateCustomerDays") or 0 for t in no_ai), 2),
            actual=round(sum(t.get("actualDays") or 0 for t in no_ai), 2),
            actual_spread=spread([t.get("actualDays") or 0 for t in no_ai]),
            note="" if len(no_ai) >= 5 else
                 "Nhom doi chung qua nho — muc 'effort khong co AI' chi la est cua nguoi, "
                 "khong phai do luong. Phai noi ro gioi han nay khi bao khach.",
        ),
        comparables={q: comparables(tasks, q, args.top) for q in args.like},
    )
    Path(args.out).write_text(json.dumps(baseline, ensure_ascii=False, indent=1))

    # ------------------------------------------------------------------ in ra
    o = overall
    print(f"Nguon: {args.tasks} — {len(tasks)} task, {len(cal)} task da Done dung lam moc neo "
          f"({o['n_plan']} theo plan, {o['n_bug']} bug).")
    print(f"Bien hien tai (toan bo): actual/est_khach trung vi {fmt(o['actual_vs_quote']['med_ratio'], 3)} "
          f"| actual/est_AI trung vi {fmt(o['actual_vs_est_ai']['med_ratio'], 3)} "
          f"| chi phi bug {fmt(o['bug_overhead_pct'], 1)}% ngay cong ke hoach")
    c = baseline["control_no_ai"]
    print(f"Nhom khong dung AI: {c['n']} task" + (f" — {c['note']}" if c["note"] else ""))

    print("\n## Moc neo theo SIZE (task theo plan da xong)\n")
    print("| Size | n | quote_days (est khach) p25/med/p75 | internal (actual) p25/med/p75 | actual/est_AI |")
    print("|---|---:|---|---|---:|")
    for sz, b in baseline["by_size"].items():
        q, i = b["quote_anchor"], b["internal_anchor"]
        print(f"| {sz} | {b['n_plan']} | {fmt(q['p25'])} / **{fmt(q['med'])}** / {fmt(q['p75'])} "
              f"| {fmt(i['p25'])} / **{fmt(i['med'])}** / {fmt(i['p75'])} "
              f"| {fmt(b['actual_vs_est_ai']['med_ratio'], 2)} |")

    rows = [(k, v) for k, v in baseline["by_tag"].items() if v["n_plan"]]
    if args.tag:
        want = {no_accent(x) for x in args.tag}
        rows = [r for r in rows if no_accent(r[0]) in want]
    cut = len(rows) if (args.full or args.tag) else 8
    print(f"\n## Moc neo theo HANG MUC (tag){'' if cut >= len(rows) else f' — {cut}/{len(rows)} dong nang nhat'}\n")
    print("| Hang muc | n plan | quote med | internal med | actual/est_khach | chi phi bug |")
    print("|---|---:|---:|---:|---:|---:|")
    for tg, b in rows[:cut]:
        print(f"| {tg} | {b['n_plan']} | {fmt(b['quote_anchor']['med'])} | {fmt(b['internal_anchor']['med'])} "
              f"| {fmt(b['actual_vs_quote']['med_ratio'], 3)} | {fmt(b['bug_overhead_pct'], 1)}% |")

    for q, hits in baseline["comparables"].items():
        print(f"\n## Task tuong duong — \"{q}\"\n")
        if not hits:
            print("KHONG tim thay task lich su nao khop. Dau viec nay khong co moc neo: "
                  "de `comparables` rong, danh confidence=low, va noi ro trong phan gia dinh. "
                  "Dung gan bua id cua task gan gan — estimate_calc.py kiem id co that nhung "
                  "khong the biet no co lien quan hay khong.")
            continue
        print("| id | Task | Tag | Size | est khach | est AI | actual | tu khop |")
        print("|---|---|---|---|---:|---:|---:|---|")
        for h in hits:
            title = h["title"][:58] + ("…" if len(h["title"]) > 58 else "")
            print(f"| #{h['id']} | {title} | {', '.join(h['tags']) or '—'} | {h['size'] or '—'} "
                  f"| {fmt(h['est_customer'])} | {fmt(h['est_ai'])} | {fmt(h['actual'])} "
                  f"| {len(h['matched'])}/{h['n_query_tokens']} |")

    print(f"\n[baseline -> {args.out}]")


if __name__ == "__main__":
    main()
