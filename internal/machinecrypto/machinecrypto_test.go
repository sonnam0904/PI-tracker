package machinecrypto

import (
	"path/filepath"
	"testing"
)

// dùng file bí mật trong thư mục tạm để không đụng config thật của người dùng.
func useTempSecret(t *testing.T) {
	t.Helper()
	t.Setenv("PI_SESSION_KEY_FILE", filepath.Join(t.TempDir(), "session.key"))
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	useTempSecret(t)
	const token = "abc123_secret-token"

	enc, err := Encrypt(token)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == token {
		t.Fatal("ciphertext không được trùng plaintext")
	}
	got, err := Decrypt(enc)
	if err != nil || got != token {
		t.Fatalf("decrypt: got=%q err=%v", got, err)
	}
}

func TestDecryptTamperedFails(t *testing.T) {
	useTempSecret(t)
	enc, err := Encrypt("hello")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Đổi 1 ký tự cuối → GCM phát hiện, giải mã lỗi.
	tampered := enc[:len(enc)-1] + string(enc[len(enc)-1]^1)
	if _, err := Decrypt(tampered); err == nil {
		t.Fatal("ciphertext bị sửa phải lỗi")
	}
	if _, err := Decrypt("khong-co-prefix"); err == nil {
		t.Fatal("sai định dạng phải lỗi")
	}
}

func TestDecryptWrongMachineFails(t *testing.T) {
	// Máy A tạo ciphertext.
	t.Setenv("PI_SESSION_KEY_FILE", filepath.Join(t.TempDir(), "a.key"))
	enc, err := Encrypt("token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Máy B: bí mật cục bộ khác → không giải mã được.
	t.Setenv("PI_SESSION_KEY_FILE", filepath.Join(t.TempDir(), "b.key"))
	if _, err := Decrypt(enc); err == nil {
		t.Fatal("token của máy khác phải giải mã lỗi")
	}
}
