// Package updater kiểm tra phiên bản mới trên GitHub Releases và tự cập nhật:
// tải asset đúng hệ điều hành, lấy binary bên trong (frontend đã nhúng sẵn vào
// binary Wails), thay thế file thực thi đang chạy rồi khởi động lại tiến trình.
//
// Không dùng thư viện ngoài — chỉ stdlib. Việc thay file thực thi an toàn theo
// từng HĐH: Unix rename đè trực tiếp (tiến trình đang chạy giữ inode cũ),
// Windows đổi tên file đang chạy sang .old trước rồi mới chuyển file mới vào.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo GitHub chứa release (owner/name). Đổi ở đây nếu fork sang repo khác.
const Repo = "sonnam0904/PI-tracker"

// Release là thông tin bản phát hành mới nhất, rút gọn theo nhu cầu app.
type Release struct {
	Version   string `json:"version"`   // đã bỏ tiền tố "v", ví dụ "1.2.3"
	Notes     string `json:"notes"`     // mô tả release (markdown)
	AssetURL  string `json:"assetUrl"`  // link tải asset khớp HĐH hiện tại
	AssetName string `json:"assetName"` // tên file asset
}

// Status là kết quả CheckUpdate trả cho frontend.
type Status struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Notes     string `json:"notes"`
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// ghRelease phản chiếu phần cần dùng trong payload GitHub API.
type ghRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest gọi GitHub API lấy release mới nhất và chọn asset khớp HĐH hiện tại.
func Latest(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github api: %s", resp.Status)
	}
	var gr ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return Release{}, err
	}
	rel := Release{
		Version: strings.TrimPrefix(gr.TagName, "v"),
		Notes:   gr.Body,
	}
	for _, a := range gr.Assets {
		if matchesOS(a.Name) {
			rel.AssetURL = a.URL
			rel.AssetName = a.Name
			break
		}
	}
	return rel, nil
}

// matchesOS báo asset có dành cho HĐH đang chạy không (theo tên file trong
// workflow release: *-linux-*, *-windows-*, *-macos-*).
func matchesOS(name string) bool {
	n := strings.ToLower(name)
	switch runtime.GOOS {
	case "linux":
		return strings.Contains(n, "linux")
	case "windows":
		return strings.Contains(n, "windows")
	case "darwin":
		return strings.Contains(n, "macos") || strings.Contains(n, "darwin")
	}
	return false
}

// CheckUpdate so phiên bản hiện tại với release mới nhất. current là chuỗi
// version nhúng lúc build (ví dụ "1.2.3"); "dev"/rỗng nghĩa là bản dev, luôn coi
// như đã mới nhất để không làm phiền lúc phát triển.
func CheckUpdate(ctx context.Context, current string) (Status, error) {
	st := Status{Current: current}
	if current == "" || current == "dev" {
		return st, nil
	}
	rel, err := Latest(ctx)
	if err != nil {
		return st, err
	}
	st.Latest = rel.Version
	st.Notes = rel.Notes
	st.Available = rel.AssetURL != "" && isNewer(current, rel.Version)
	return st, nil
}

// isNewer báo latest > current theo semver (chỉ so major.minor.patch).
func isNewer(current, latest string) bool {
	c := parseSemver(current)
	l := parseSemver(latest)
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 { // bỏ phần prerelease/build
		v = v[:i]
	}
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		out[i], _ = strconv.Atoi(part)
	}
	return out
}

// DownloadAndApply tải asset mới nhất, lấy binary bên trong và thay thế file
// thực thi đang chạy. Trả về đường dẫn file thực thi (đã cập nhật) để Restart.
func DownloadAndApply(ctx context.Context) (string, error) {
	rel, err := Latest(ctx)
	if err != nil {
		return "", err
	}
	if rel.AssetURL == "" {
		return "", fmt.Errorf("không tìm thấy bản build cho %s", runtime.GOOS)
	}

	archive, err := download(ctx, rel.AssetURL)
	if err != nil {
		return "", fmt.Errorf("tải bản mới: %w", err)
	}
	bin, err := extractBinary(archive, rel.AssetName)
	if err != nil {
		return "", fmt.Errorf("giải nén: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if err := replaceExecutable(exe, bin); err != nil {
		return "", fmt.Errorf("thay file thực thi: %w", err)
	}
	return exe, nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// extractBinary rút file thực thi từ archive tùy loại:
//   - .tar.gz (Linux): entry "task-manager"
//   - .zip (Windows): entry "task-manager.exe"
//   - .zip (macOS): binary trong bundle "*.app/Contents/MacOS/<tên>"
func extractBinary(archive []byte, assetName string) ([]byte, error) {
	name := strings.ToLower(assetName)
	switch {
	case strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz"):
		return fromTarGz(archive)
	case strings.HasSuffix(name, ".zip"):
		return fromZip(archive)
	default:
		return nil, fmt.Errorf("định dạng asset không hỗ trợ: %s", assetName)
	}
}

func fromTarGz(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	// Archive có thể kèm file phụ (vd .env.example) — ưu tiên file có bit thực
	// thi. Nếu không thấy, quay lại file thường đầu tiên.
	var fallback []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		if hdr.FileInfo().Mode()&0o111 != 0 {
			return data, nil // file thực thi
		}
		if fallback == nil {
			fallback = data
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("không có file thực thi trong archive")
}

func fromZip(archive []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	// macOS: ưu tiên binary trong bundle Contents/MacOS/. Windows: file .exe.
	// Chọn ứng viên "nặng" nhất trong Contents/MacOS để tránh nhầm file phụ.
	var macBin *zip.File
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		lname := strings.ToLower(f.Name)
		if strings.HasSuffix(lname, ".exe") {
			return readZipFile(f)
		}
		if strings.Contains(f.Name, ".app/Contents/MacOS/") {
			if macBin == nil || f.UncompressedSize64 > macBin.UncompressedSize64 {
				macBin = f
			}
		}
	}
	if macBin != nil {
		return readZipFile(macBin)
	}
	// Fallback: một file đơn trong zip.
	if len(zr.File) == 1 && !zr.File[0].FileInfo().IsDir() {
		return readZipFile(zr.File[0])
	}
	return nil, fmt.Errorf("không tìm thấy file thực thi trong zip")
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// replaceExecutable ghi binary mới vào cùng thư mục với exe rồi rename đè —
// cùng filesystem nên rename là thao tác nguyên tử. Windows không cho ghi đè
// file đang chạy, nên đổi tên file hiện tại sang .old trước.
func replaceExecutable(exe string, newBin []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(newBin); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		os.Remove(tmpName)
		return err
	}

	if runtime.GOOS == "windows" {
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			os.Remove(tmpName)
			return err
		}
		if err := os.Rename(tmpName, exe); err != nil {
			os.Rename(old, exe) // rollback
			os.Remove(tmpName)
			return err
		}
		return nil
	}

	if err := os.Rename(tmpName, exe); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// Restart khởi động lại ứng dụng: chạy file thực thi mới rồi để caller thoát
// tiến trình hiện tại. Trên macOS mở lại qua bundle .app để app kích hoạt đúng.
func Restart(exe string) error {
	if runtime.GOOS == "darwin" {
		if app := appBundle(exe); app != "" {
			return exec.Command("open", "-n", app).Start()
		}
	}
	cmd := exec.Command(exe)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Start()
}

// appBundle trả về đường dẫn "*.app" bao ngoài file thực thi (macOS), hoặc "".
func appBundle(exe string) string {
	if i := strings.Index(exe, ".app/"); i >= 0 {
		return exe[:i+len(".app")]
	}
	return ""
}
