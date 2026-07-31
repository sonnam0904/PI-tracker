#!/usr/bin/env python3
"""Lấy dữ liệu task từ PI Tracker qua MCP (JSON-RPC over HTTP).

Đây là cách LẤY list_tasks, kể cả khi các tool MCP pi-tracker đã được nạp sẵn:
mỗi task mang đủ 27 field, riêng `description` chiếm ~77% payload, nên kết quả
vượt hạn mức token của một tool result và bị từ chối. Script gọi đúng tool đó qua
`tools/call` rồi ghi nguyên văn bytes nhận được ra file, chỉ in 3 dòng trạng thái.

`list_tasks` BẮT BUỘC phải giới hạn phạm vi — gọi không tham số sẽ báo lỗi:
  --month YYYY-MM   lấy task của một tháng (dùng cho báo cáo tháng)
  (mặc định)        không có --month thì script gửi all=true, tức CỐ Ý lấy toàn
                    bộ workspace. Chỉ nên dùng khi thật sự cần cả lịch sử.

Các tool payload nhỏ (list_people, list_tags, get_session, get_task) thì gọi thẳng,
không cần script này.

Cách tìm endpoint (theo thứ tự):
  1. Biến môi trường PI_TRACKER_URL + PI_TRACKER_TOKEN
  2. ./.mcp.json trong thư mục hiện tại
  3. ~/.claude.json  (mục projects[*].mcpServers["pi-tracker"])

Usage:
  python3 pi_fetch.py --out tasks.json [--people people.json] [--month YYYY-MM]
"""
import argparse
import json
import os
import re
import sys
import urllib.request
from pathlib import Path


def find_endpoint():
    url, token = os.environ.get("PI_TRACKER_URL"), os.environ.get("PI_TRACKER_TOKEN")
    if url:
        return url, token

    candidates = [Path.cwd() / ".mcp.json"]
    home_cfg = Path.home() / ".claude.json"

    for p in candidates:
        if p.is_file():
            cfg = json.loads(p.read_text()).get("mcpServers", {}).get("pi-tracker")
            if cfg:
                return cfg["url"], _bearer(cfg)

    if home_cfg.is_file():
        data = json.loads(home_cfg.read_text())
        for scope in [data] + list(data.get("projects", {}).values()):
            cfg = (scope.get("mcpServers") or {}).get("pi-tracker")
            if cfg:
                return cfg["url"], _bearer(cfg)

    sys.exit(
        "Khong tim thay cau hinh MCP 'pi-tracker'.\n"
        "Dat PI_TRACKER_URL / PI_TRACKER_TOKEN, hoac tao .mcp.json trong thu muc lam viec."
    )


def _bearer(cfg):
    auth = (cfg.get("headers") or {}).get("Authorization", "")
    return auth[7:] if auth.startswith("Bearer ") else auth or None


def call_tool(url, token, name, arguments=None):
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {"name": name, "arguments": arguments or {}},
    }
    req = urllib.request.Request(
        url,
        data=json.dumps(payload).encode(),
        headers={
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
            **({"Authorization": f"Bearer {token}"} if token else {}),
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            body = resp.read().decode()
    except Exception as e:
        sys.exit(f"Goi MCP that bai ({name}): {e}\nKiem tra app PI Tracker da chay chua.")

    # Endpoint co the tra ve SSE; lay dong data: dau tien
    if body.lstrip().startswith("event:") or body.lstrip().startswith("data:"):
        for line in body.splitlines():
            if line.startswith("data:"):
                body = line[5:].strip()
                break

    rpc = json.loads(body)
    if "error" in rpc:
        sys.exit(f"MCP tra ve loi ({name}): {rpc['error']}")
    result = rpc["result"]
    if result.get("isError"):
        sys.exit(f"Tool {name} loi: {result}")
    return json.loads(result["content"][0]["text"])


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default="tasks.json")
    ap.add_argument("--people", default=None, help="Ghi kem danh sach nhan su ra file nay")
    ap.add_argument("--month", default=None, metavar="YYYY-MM",
                    help="Chi lay task cua thang nay. Bo qua = lay TOAN BO workspace (all=true).")
    args = ap.parse_args()

    if args.month and not re.fullmatch(r"\d{4}-\d{2}", args.month):
        sys.exit(f"--month {args.month!r} sai dinh dang, can YYYY-MM (vd 2026-06)")

    # list_tasks bat buoc gioi han pham vi. Khong co --month thi phai noi ro
    # all=true — de viec keo ca lich su workspace ve luon la mot lua chon co y.
    scope = {"month": args.month} if args.month else {"all": True}
    url, token = find_endpoint()
    tasks = call_tool(url, token, "list_tasks", scope)
    Path(args.out).write_text(json.dumps(tasks, ensure_ascii=False, indent=1))
    print(f"OK: {len(tasks)} task ({args.month or 'toan bo workspace'}) -> {args.out}")

    if args.people:
        people = call_tool(url, token, "list_people")
        Path(args.people).write_text(json.dumps(people, ensure_ascii=False, indent=1))
        print(f"OK: {len(people)} nhan su -> {args.people}")

    session = call_tool(url, token, "get_session")
    print(f"Workspace: {session.get('workspaceName')} (user {session.get('username')})")


if __name__ == "__main__":
    main()
