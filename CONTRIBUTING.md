# Hướng dẫn đóng góp

Cảm ơn bạn đã quan tâm đến PI Tracker! Tài liệu này mô tả quy trình đóng góp cho dự án.

Mọi tương tác trong dự án tuân theo [Quy tắc ứng xử](CODE_OF_CONDUCT.md).

## Chuẩn bị môi trường

- Go ≥ 1.25, Node.js ≥ 18 (npm)
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Linux (Ubuntu/Debian): `sudo apt install gcc libgtk-3-dev libwebkit2gtk-4.1-dev`

```bash
cp .env.example .env     # cấu hình (mặc định SQLite, chạy được ngay)
wails dev                # chạy dev, hot reload frontend
```

> Trên distro chỉ có WebKitGTK 4.1 (Ubuntu 24.04+): thêm `-tags webkit2_41` vào `wails dev`/`wails build`.

## Quy trình đóng góp

1. **Fork & branch**: tạo nhánh từ `master`, đặt tên theo dạng `feat/…`, `fix/…`, `docs/…`.
2. **Code**: bám theo phong cách và quy ước có sẵn trong repo. Comment bằng tiếng Việt cho nhất quán với codebase.
3. **Test**: đảm bảo xanh trước khi mở PR.
   ```bash
   go test ./...                    # test backend Go
   cd frontend && npm run build     # kiểm tra frontend build
   ```
4. **Commit**: dùng [Conventional Commits](https://www.conventionalcommits.org/) — dự án dùng semantic-release để tự phát hành, nên tiền tố commit ảnh hưởng số phiên bản:
   - `feat: …` → tăng minor
   - `fix: …` → tăng patch
   - `feat!: …` hoặc có `BREAKING CHANGE:` → tăng major
   - `docs: …`, `chore: …`, `refactor: …`, `test: …` → không phát hành
5. **Pull Request**: mô tả rõ *cái gì* và *tại sao*, kèm cách kiểm thử. Liên kết issue liên quan nếu có.

## Báo lỗi & đề xuất

- **Bug/tính năng**: mở [issue](https://github.com/sonnam0904/PI-tracker/issues) với các bước tái hiện, hành vi mong đợi và thực tế, môi trường (OS, DB driver).
- **Lỗ hổng bảo mật**: **không** mở issue công khai — làm theo [SECURITY.md](SECURITY.md).

