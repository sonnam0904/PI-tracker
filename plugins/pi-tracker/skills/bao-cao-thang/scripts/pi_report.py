#!/usr/bin/env python3
"""Tong hop chi so bao cao thang tu du lieu PI Tracker, GOP THEO TAG.

Moi phep cong/chia deu lam o day de so lieu trong bao cao luon nhat quan.
Khong bao gio tu cong tay cac con so nay.

Usage:
  python3 pi_report.py --tasks tasks.json --month <YYYY-MM> \
      [--people people.json] [--out-json metrics.json] [--out-md tables.md]

HANG MUC CONG VIEC = truong `tags` cua task trong PI Tracker. Khong co danh sach
hang muc co dinh trong script: them/bot tag trong tracker la bao cao doi theo.
Task chua gan tag vao nhom "(chua gan tag)" kem canh bao.

Task gan NHIEU tag duoc dem vao TAT CA tag do, nen tong ngay cong cua cac tag
LON HON tong thuc te toan team. Phan dem trung tinh tuong minh o `tag_overlap`:

    sum(by_tag[*].actual) == total.actual + tag_overlap.days

Dung cong don cac dong tag roi coi la tong toan team — lay dong Tong o Bang 1.

Moi task trong `tasks` co ca `title` VA `description` (nguyen van). Khi can xac dinh
ten khach hang / pham vi cong viec thi phai doc CA HAI — nhieu task chi ghi ten khach
trong mo ta, khong co o tieu de.
"""
import argparse
import json
import re
import sys
from collections import Counter, defaultdict
from pathlib import Path

# ---------------------------------------------------------------- hang muc
# Bao cao KHONG con truc "giai phap" co dinh. Hang muc cong viec = TAG cua task
# trong PI Tracker, nen tap hang muc la dong: them/bot tag trong tracker la bao
# cao doi theo, khong phai sua script.
#
# Da bo (dung tim lai): bo tu khoa SOLUTIONS, tien to "[Chatbot]", overrides.json,
# nhanh mac dinh. Tat ca chi ton tai de SUY LUAN ra giai phap — gio khong can nua
# vi tag la du lieu that do nguoi lam task gan.

TYPE_NAME = {1: "Theo plan", 2: "Phat sinh (bug)", 3: "Phat sinh theo plan"}

# Task chua gan tag nao trong PI Tracker
NO_GROUP = "(chua gan tag)"


def task_tags(t):
    """Hang muc cong viec cua task = danh sach tag trong PI Tracker.

    Tra ve [NO_GROUP] neu task chua gan tag, de no van hien thanh mot nhom rieng
    trong bang tong hop (kem canh bao) chu khong bi bo im lang khoi bao cao.
    """
    tags = [str(x).strip() for x in (t.get("tags") or []) if str(x).strip()]
    return tags or [NO_GROUP]


def clean_title(title):
    """Bo tien to '[...]' o dau tieu de cho bang doc gon.

    Tien to khong con y nghia phan loai (hang muc lay tu tag), nhung du lieu cu
    van con "[Chatbot] ..." nen van cat cho sach.
    """
    return re.sub(r"^\s*\[[^\]]+\]\s*", "", title or "").strip()


# ---------------------------------------------------------------- pham vi thang
def in_month(task, month):
    """Task cham vao ky bao cao: bat dau HOAC hoan thanh trong thang."""
    return (task.get("startDate") or "")[:7] == month or (task.get("doneDate") or "")[:7] == month


def is_carryover(task):
    """Task da len lich nhung chua bo cong nao — thuoc ke hoach thang sau.

    Tach rieng khoi so lieu chinh, neu khong se lam loang ty le hoan thanh
    va keo tut he so nang suat (dong gop 0 ngay thuc te nhung van cong estimate).
    """
    return (task.get("actualDays") or 0) == 0 and task.get("status") == "Todo"


# ---------------------------------------------------------------- tong hop
def blank():
    return dict(
        n=0, done=0, blocked=0, todo=0, inprogress=0,
        bug=0, bug_done=0, bug_days=0.0,
        est_customer=0.0, est_ai=0.0, actual=0.0,
        ai_used=0, blocked_days=0.0, ids=[],
    )


def accumulate(bucket, t):
    bucket["n"] += 1
    st = t.get("status")
    bucket["done"] += st == "Done"
    bucket["blocked"] += st == "Blocked"
    bucket["todo"] += st == "Todo"
    bucket["inprogress"] += st == "In Progress"
    bucket["est_customer"] += t.get("estimateCustomerDays") or 0
    bucket["est_ai"] += t.get("estimateAiDays") or 0
    bucket["actual"] += t.get("actualDays") or 0
    bucket["ai_used"] += 1 if t.get("aiUsed") else 0
    bucket["blocked_days"] += t.get("blockedDays") or 0
    if t.get("type") == 2:
        bucket["bug"] += 1
        bucket["bug_done"] += st == "Done"
        bucket["bug_days"] += t.get("actualDays") or 0
    bucket["ids"].append(t["id"])


def derive(b):
    """Chi so nang suat AI. Dinh nghia phai giu nguyen giua cac thang."""
    ec, ac, ea = b["est_customer"], b["actual"], b["est_ai"]
    b["saved_days"] = round(ec - ac, 2)
    # % giam effort: tiet kiem bao nhieu phan tram so voi bao khach
    b["effort_cut_pct"] = round((ec - ac) / ec * 100, 1) if ec else 0.0
    # He so nang suat: 1 ngay cong thuc te lam duoc bao nhieu ngay cong bao khach
    b["productivity_x"] = round(ec / ac, 2) if ac else None
    # % tang nang suat = (he so - 1) * 100  — KHAC voi % giam effort
    b["productivity_gain_pct"] = round((ec / ac - 1) * 100, 1) if ac else None
    # Sai lech thuc te so voi estimate AI: duong = cham hon du kien
    b["est_ai_deviation_pct"] = round((ac - ea) / ea * 100, 1) if ea else None
    b["ai_adoption_pct"] = round(b["ai_used"] / b["n"] * 100, 1) if b["n"] else 0.0
    b["bug_ratio_pct"] = round(b["bug"] / b["n"] * 100, 1) if b["n"] else 0.0
    for k in ("est_customer", "est_ai", "actual", "bug_days", "blocked_days"):
        b[k] = round(b[k], 2)
    return b


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tasks", required=True)
    ap.add_argument("--month", required=True, help="YYYY-MM")
    ap.add_argument("--people", default=None)
    ap.add_argument("--out-json", default=None)
    ap.add_argument("--out-md", default=None)
    args = ap.parse_args()

    tasks = json.loads(Path(args.tasks).read_text())

    names = {0: "(chua gan)"}
    if args.people:
        for p in json.loads(Path(args.people).read_text()):
            names[p["ID"]] = p["Name"]

    touched = [t for t in tasks if in_month(t, args.month)]
    scope = [t for t in touched if not is_carryover(t)]
    carryover = [t for t in touched if is_carryover(t)]
    if not scope:
        sys.exit(f"Khong co task nao trong thang {args.month}.")

    by_tag = defaultdict(blank)
    by_person = defaultdict(blank)
    by_tag_person = defaultdict(blank)
    total = blank()
    listing = []
    needs_tag = []
    multi_tag = []      # task gan >=2 tag -> bi dem vao nhieu hang muc
    overlap_days = 0.0  # so ngay cong bi dem lai vi task gan nhieu tag

    for t in scope:
        who = names.get(t.get("assigneeId"), str(t.get("assigneeId")))
        # Hang muc = tag. Task nhieu tag duoc dem vao TAT CA tag do, nen tong cac
        # tag LON HON tong thuc te — theo doi rieng phan dem trung o overlap_days.
        tg = task_tags(t)
        accumulate(by_person[who], t)
        for tag in tg:
            accumulate(by_tag[tag], t)
            accumulate(by_tag_person[(tag, who)], t)
        accumulate(total, t)
        if len(tg) > 1:
            overlap_days += (t.get("actualDays") or 0) * (len(tg) - 1)
        row = dict(
            id=t["id"], title=clean_title(t.get("title", "")),
            # Mo ta giu NGUYEN VAN (ke ca markdown "## Muc tieu"): ten khach hang va
            # pham vi thuong chi nam trong mo ta chu khong co o tieu de. Ban .md PHAI
            # doc truong nay khi viet phan khach hang / pham vi, dung doan tu tieu de.
            description=(t.get("description") or "").strip(),
            tags=tg,
            assignee=who, status=t.get("status"), type=TYPE_NAME.get(t.get("type"), "?"),
            severity=t.get("severity") or "", resolution=t.get("resolution") or "",
            priority=t.get("priority"), size=t.get("size"),
            est_customer=t.get("estimateCustomerDays") or 0,
            est_ai=t.get("estimateAiDays") or 0,
            actual=t.get("actualDays") or 0,
            ai_used=bool(t.get("aiUsed")),
            start=t.get("startDate") or "", done=t.get("doneDate") or "",
            due=t.get("dueDate") or "", blocker=t.get("blocker") or "",
            todo_done=t.get("todoDone"), todo_total=t.get("todoTotal"),
        )
        listing.append(row)
        if tg == [NO_GROUP]:
            needs_tag.append(row)
        elif len(tg) > 1:
            multi_tag.append(row)

    for b in (list(by_tag.values()) + list(by_person.values())
              + list(by_tag_person.values())):
        derive(b)
    derive(total)

    # Nhom doi chung: task khong dung AI, de doi chieu muc tang nang suat
    no_ai = [r for r in listing if not r["ai_used"]]
    control = dict(
        n=len(no_ai),
        est_customer=round(sum(r["est_customer"] for r in no_ai), 2),
        actual=round(sum(r["actual"] for r in no_ai), 2),
        note="Nhom doi chung qua nho de ket luan" if len(no_ai) < 5 else "",
    )

    bugs = sorted(
        [r for r in listing if r["type"] == "Phat sinh (bug)"],
        key=lambda r: ({"Critical": 0, "Major": 1, "Minor": 2}.get(r["severity"], 3), -r["actual"]),
    )
    unfinished = [r for r in listing if r["status"] != "Done"]

    metrics = dict(
        month=args.month,
        scope_task_count=len(scope),
        # Danh sach tag xuat hien trong ky, xep theo ngay cong giam dan. Day la
        # "muc luc" cua bao cao: moi tag mot muc trong ban .md, mot sheet trong Excel.
        tags=[k for k, _ in sorted(by_tag.items(), key=lambda kv: -kv[1]["actual"])],
        carryover=[
            dict(id=t["id"], title=clean_title(t.get("title", "")), assignee=names.get(t.get("assigneeId")),
                 description=(t.get("description") or "").strip(),
                 tags=task_tags(t), est_ai=t.get("estimateAiDays") or 0,
                 start=t.get("startDate") or "", due=t.get("dueDate") or "")
            for t in carryover
        ],
        total=total,
        # Truc gop CHINH cua bao cao. CANH BAO: task gan nhieu tag duoc dem vao moi
        # tag cua no, nen sum(by_tag[*].actual) >= total.actual — xem tag_overlap.
        by_tag={k: v for k, v in sorted(by_tag.items(), key=lambda kv: -kv[1]["actual"])},
        by_person={k: v for k, v in sorted(by_person.items(), key=lambda kv: -kv[1]["actual"])},
        # Cap gop thu hai: moi tag ai lam gi. Key = "<tag>|<nguoi>".
        by_tag_person={
            f"{tg}|{p}": v for (tg, p), v in
            sorted(by_tag_person.items(),
                   key=lambda kv: (-by_tag[kv[0][0]]["actual"], -kv[1]["actual"]))
        },
        # Phan ngay cong bi dem lai vi task gan nhieu tag. Bat bien de kiem tra:
        #   sum(by_tag[*].actual) == total.actual + tag_overlap.days
        tag_overlap=dict(
            days=round(overlap_days, 2),
            tasks=len(multi_tag),
            tag_actual_sum=round(sum(v["actual"] for v in by_tag.values()), 2),
            real_actual=total["actual"],
        ),
        severity_breakdown=dict(Counter(b["severity"] or "(trong)" for b in bugs)),
        control_group_no_ai=control,
        bugs=bugs,
        unfinished=unfinished,
        needs_tag=needs_tag,
        multi_tag=multi_tag,
        tasks=listing,
    )

    out_json = args.out_json or "metrics.json"
    Path(out_json).write_text(json.dumps(metrics, ensure_ascii=False, indent=1))

    lines = []
    a = lines.append
    tag_sum = round(sum(v["actual"] for v in by_tag.values()), 2)
    tags_by_effort = sorted(by_tag, key=lambda x: -by_tag[x]["actual"])

    a(f"## Bang 1 — Tong hop theo hang muc / tag ({args.month})\n")
    if overlap_days > 0:
        a(f"> CANH BAO: {len(multi_tag)} task gan nhieu tag nen duoc dem vao moi tag cua no. "
          f"Cong don cot Thuc te ra {tag_sum} ngay, thuc te toan team chi {total['actual']} ngay "
          f"(dem trung {round(overlap_days, 2)} ngay). Dong **Tong** la so DUNG; dung cong cac "
          f"dong tag lai. Cot 'Ty trong' vi vay co the cong lai > 100%.\n")
    a("| Hang muc (tag) | Task | Done | Bug | Est khach | Est AI | Thuc te | Tiet kiem | Nang suat | Ty trong |")
    a("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
    for tg in tags_by_effort:
        b = by_tag[tg]
        share = b["actual"] / total["actual"] * 100 if total["actual"] else 0
        gain = f"+{b['productivity_gain_pct']}%" if b["productivity_gain_pct"] is not None else "—"
        a(f"| {tg} | {b['n']} | {b['done']} | {b['bug']} | {b['est_customer']} | {b['est_ai']} "
          f"| {b['actual']} | {b['saved_days']} | {gain} | {share:.0f}% |")
    t = total
    gain_t = f"+{t['productivity_gain_pct']}%" if t["productivity_gain_pct"] is not None else "—"
    a(f"| **Tong (toan team)** | **{t['n']}** | **{t['done']}** | **{t['bug']}** | **{t['est_customer']}** "
      f"| **{t['est_ai']}** | **{t['actual']}** | **{t['saved_days']}** | **{gain_t}** | 100% |")

    a(f"\n## Bang 2 — Theo nhan su\n")
    a("| Nguoi | Task | Done | Bug | Est khach | Thuc te | Tiet kiem | Nang suat |")
    a("|---|---:|---:|---:|---:|---:|---:|---:|")
    for who in sorted(by_person, key=lambda p: -by_person[p]["actual"]):
        b = by_person[who]
        gain = f"+{b['productivity_gain_pct']}%" if b["productivity_gain_pct"] is not None else "—"
        a(f"| {who} | {b['n']} | {b['done']} | {b['bug']} | {b['est_customer']} "
          f"| {b['actual']} | {b['saved_days']} | {gain} |")

    a("\n## Bang 3 — Moi hang muc ai lam gi (tag -> nhan su)\n")
    a("Dung nguyen tong o bang nay cho phan \"Cong viec hoan thanh\" trong ban .md. "
      "KHONG cong tay lai. Dong in dam = tong cua tag do.\n")
    a("| Hang muc / Nguoi | Task | Bug | Ngay cong | Tiet kiem | He so |")
    a("|---|---:|---:|---:|---:|---:|")
    for tg in tags_by_effort:
        tb = by_tag[tg]
        a(f"| **{tg}** | **{tb['n']}** | **{tb['bug']}** | **{tb['actual']}** "
          f"| **{tb['saved_days']}** | **{tb['productivity_x']}x** |")
        rows = [(p, v) for (g, p), v in by_tag_person.items() if g == tg]
        for who, v in sorted(rows, key=lambda kv: -kv[1]["actual"]):
            a(f"|   {who} | {v['n']} | {v['bug']} | {v['actual']} "
              f"| {v['saved_days']} | {v['productivity_x']}x |")

    a(f"\n## Bang 4 — Bug phat sinh ({len(bugs)})\n")
    a("| ID | Bug | Hang muc | Nguoi | Muc do | Xu ly | Effort |")
    a("|---|---|---|---|---|---|---:|")
    for b in bugs:
        a(f"| #{b['id']} | {b['title']} | {', '.join(b['tags'])} | {b['assignee']} "
          f"| {b['severity'] or '—'} | {b['resolution'] or b['status']} | {b['actual']} |")

    a(f"\n## Bang 5 — Cong viec hoan thanh, theo hang muc\n")
    a("Task gan nhieu tag se xuat hien o nhieu muc duoi day — do la co y.\n")
    for tg in tags_by_effort:
        b = by_tag[tg]
        done_rows = sorted(
            [r for r in listing if tg in r["tags"] and r["status"] == "Done"],
            key=lambda r: -r["actual"],
        )
        a(f"\n### {tg} — {len(done_rows)}/{b['n']} task hoan thanh, {b['actual']} ngay cong")
        a("\n| ID | Task | Nguoi | Loai | Est khach | Thuc te |")
        a("|---|---|---|---|---:|---:|")
        for r in done_rows:
            a(f"| #{r['id']} | {r['title']} | {r['assignee']} | {r['type']} "
              f"| {r['est_customer']} | {r['actual']} |")

    if unfinished:
        a(f"\n## Bang 6 — Task chua hoan thanh ({len(unfinished)})\n")
        a("| ID | Task | Hang muc | Nguoi | Trang thai | Da bo cong | Blocker |")
        a("|---|---|---|---|---|---:|---|")
        for r in unfinished:
            a(f"| #{r['id']} | {r['title']} | {', '.join(r['tags'])} | {r['assignee']} "
              f"| {r['status']} | {r['actual']} | {r['blocker'] or '(khong ghi)'} |")

    if metrics["carryover"]:
        a(f"\n## Chuyen tiep sang thang sau ({len(metrics['carryover'])} task, chua bo cong nao)\n")
        a("| ID | Task | Hang muc | Nguoi | Est AI | Han |")
        a("|---|---|---|---|---:|---|")
        for r in metrics["carryover"]:
            a(f"| #{r['id']} | {r['title']} | {', '.join(r['tags'])} | {r['assignee']} "
              f"| {r['est_ai']} | {r['due'] or '—'} |")

    if needs_tag:
        a(f"\n## Can gan tag ({len(needs_tag)} task chua gan tag nao)\n")
        a("Hang muc cong viec lay TU TAG trong PI Tracker, khong con suy luan tu tieu de. "
          "Gan tag cho cac task nay NGAY TRONG PI TRACKER (truong \"Phan loai tag\") roi fetch "
          "lai va chay lai script. Chung dang bi gom vao nhom \"" + NO_GROUP + "\".\n")
        a("| ID | Task | Nguoi | Ngay cong |")
        a("|---|---|---|---:|")
        for r in needs_tag:
            a(f"| #{r['id']} | {r['title']} | {r['assignee']} | {r['actual']} |")

    if multi_tag:
        a(f"\n## Task gan nhieu tag ({len(multi_tag)} task, dem trung {round(overlap_days, 2)} ngay)\n")
        a("Cac task nay duoc dem vao MOI tag cua no. Khong phai loi — chi la khi viet bao cao "
          "thi dung cong don cac dong tag lai; tong dung nam o dong **Tong (toan team)** Bang 1.\n")
        a("| ID | Task | Nguoi | Tag | Ngay cong |")
        a("|---|---|---|---|---:|")
        for r in multi_tag:
            a(f"| #{r['id']} | {r['title']} | {r['assignee']} | {', '.join(r['tags'])} | {r['actual']} |")

    md = "\n".join(lines) + "\n"
    if args.out_md:
        Path(args.out_md).write_text(md)
    print(md)
    print(f"\n[metrics -> {out_json}]")


if __name__ == "__main__":
    main()
