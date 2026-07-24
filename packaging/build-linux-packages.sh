#!/usr/bin/env bash
# Dựng gói cài đặt Linux (.deb + .rpm) cho PI Tracker.
#
# Yêu cầu: wails CLI, Go, Node; nfpm (tự cài qua `go install` nếu thiếu).
# Cách dùng (từ thư mục gốc repo):
#   VERSION=1.2.3 packaging/build-linux-packages.sh
#   packaging/build-linux-packages.sh            # version tự lấy từ git, hoặc 0.0.0
#
# Kết quả nằm ở dist/: task-manager_<ver>_amd64.deb và task-manager-<ver>.x86_64.rpm
set -euo pipefail

cd "$(dirname "$0")/.."

# Version: ưu tiên biến VERSION, rồi git describe (bỏ tiền tố v), cuối cùng 0.0.0.
VERSION="${VERSION:-$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || true)}"
VERSION="${VERSION:-0.0.0}"
export APP_VERSION="$VERSION"

echo "▶ Build app (wails) — version $APP_VERSION"
wails build -platform linux/amd64 -tags webkit2_41 \
  -ldflags "-X main.Version=$APP_VERSION"

# nfpm: cài nếu chưa có.
if ! command -v nfpm >/dev/null 2>&1; then
  echo "▶ Cài nfpm…"
  go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
  export PATH="$PATH:$(go env GOPATH)/bin"
fi

mkdir -p dist
echo "▶ Đóng gói .deb"
nfpm package -f packaging/nfpm.yaml -p deb -t dist/
echo "▶ Đóng gói .rpm"
nfpm package -f packaging/nfpm.yaml -p rpm -t dist/

echo "✓ Xong:"
ls -1 dist/*.deb dist/*.rpm
