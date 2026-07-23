// Package machinecrypto mã hóa token phiên đăng nhập "theo máy": ciphertext
// chỉ giải mã được trên chính máy đã tạo ra nó. Khóa AES-256 dẫn xuất từ một
// bí mật ngẫu nhiên lưu cục bộ (file 0600) TRỘN với machine ID của HĐH — copy
// ciphertext (và cả file bí mật) sang máy khác vẫn không giải mã được vì
// machine ID khác. Không lưu username/mật khẩu ở đâu cả.
package machinecrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// cipherPrefix đánh dấu định dạng/phiên bản ciphertext để về sau đổi thuật
// toán mà không nhầm với token đời cũ.
const cipherPrefix = "v1:"

// secretFilePath — nơi lưu bí mật cục bộ. Cho phép override bằng env để test
// không đụng vào thư mục config thật của người dùng.
func secretFilePath() (string, error) {
	if p := os.Getenv("PI_SESSION_KEY_FILE"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pi-tracker", "session.key"), nil
}

// localSecret đọc bí mật 32 byte cục bộ; chưa có thì sinh ngẫu nhiên và lưu
// (thư mục 0700, file 0600). Bí mật này gắn với máy/cài đặt hiện tại.
func localSecret() ([]byte, error) {
	path, err := secretFilePath()
	if err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return b, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

// osMachineID trả về định danh máy ổn định của HĐH (best-effort). Không đọc
// được (container tối giản, thiếu quyền…) thì trả "" và chỉ dựa vào bí mật cục
// bộ — vẫn an toàn ở mức "không rời máy này".
func osMachineID() string {
	switch runtime.GOOS {
	case "linux":
		for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if b, err := os.ReadFile(p); err == nil {
				if s := strings.TrimSpace(string(b)); s != "" {
					return s
				}
			}
		}
	case "darwin":
		out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "IOPlatformUUID") {
					if i := strings.Index(line, "= \""); i >= 0 {
						return strings.Trim(line[i+3:], "\" ")
					}
				}
			}
		}
	case "windows":
		out, err := exec.Command("reg", "query",
			`HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid").Output()
		if err == nil {
			fields := strings.Fields(string(out))
			if len(fields) > 0 {
				return fields[len(fields)-1]
			}
		}
	}
	return ""
}

// machineKey = SHA-256(tag || localSecret || osMachineID) → khóa AES-256 ổn
// định theo máy, giống nhau giữa các lần chạy app.
func machineKey() ([]byte, error) {
	secret, err := localSecret()
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write([]byte("pi-tracker/session-key/v1"))
	h.Write(secret)
	if mid := osMachineID(); mid != "" {
		h.Write([]byte(mid))
	}
	return h.Sum(nil), nil
}

func gcmForMachine() (cipher.AEAD, error) {
	key, err := machineKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt mã hóa plaintext bằng AES-256-GCM với khóa theo máy; trả chuỗi
// "v1:<base64url(nonce||ciphertext)>" để client lưu local.
func Encrypt(plaintext string) (string, error) {
	gcm, err := gcmForMachine()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return cipherPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Decrypt giải mã chuỗi do Encrypt tạo. Sai định dạng, sai máy, hoặc bị chỉnh
// sửa → lỗi (GCM xác thực toàn vẹn).
func Decrypt(enc string) (string, error) {
	if !strings.HasPrefix(enc, cipherPrefix) {
		return "", errors.New("định dạng token không hợp lệ")
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc[len(cipherPrefix):])
	if err != nil {
		return "", err
	}
	gcm, err := gcmForMachine()
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext quá ngắn")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
