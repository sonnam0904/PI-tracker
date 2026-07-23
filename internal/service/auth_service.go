package service

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"

	"taskmanager/internal/models"
)

// Tham số Argon2id theo khuyến nghị OWASP (m=64MB, t=3, p=2).
const (
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)

type AuthService struct {
	db *gorm.DB
}

func NewAuthService(db *gorm.DB) *AuthService {
	return &AuthService{db: db}
}

// Register tạo tài khoản mới; mật khẩu băm bằng Argon2id + salt ngẫu nhiên.
func (s *AuthService) Register(username, password string) (models.User, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	if !usernameRe.MatchString(username) {
		return models.User{}, fmt.Errorf("username 3–32 ký tự, chỉ gồm chữ, số, '_' '.' '-'")
	}
	if len(password) < 6 {
		return models.User{}, fmt.Errorf("mật khẩu tối thiểu 6 ký tự")
	}

	var count int64
	if err := s.db.Model(&models.User{}).Where("username = ?", username).Count(&count).Error; err != nil {
		return models.User{}, err
	}
	if count > 0 {
		return models.User{}, fmt.Errorf("username %q đã tồn tại", username)
	}

	hash, err := hashPassword(password)
	if err != nil {
		return models.User{}, err
	}
	u := models.User{Username: username, PasswordHash: hash}
	if err := s.db.Create(&u).Error; err != nil {
		return models.User{}, err
	}
	return u, nil
}

// Login xác thực username + password.
func (s *AuthService) Login(username, password string) (models.User, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	var u models.User
	err := s.db.Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.User{}, fmt.Errorf("sai username hoặc mật khẩu")
	}
	if err != nil {
		return models.User{}, err
	}
	ok, err := verifyPassword(password, u.PasswordHash)
	if err != nil || !ok {
		return models.User{}, fmt.Errorf("sai username hoặc mật khẩu")
	}
	return u, nil
}

func (s *AuthService) Get(id uint) (models.User, error) {
	var u models.User
	err := s.db.First(&u, id).Error
	return u, err
}

// sessionTTL — thời hạn của một phiên "ghi nhớ đăng nhập". Mỗi lần khôi phục
// thành công sẽ gia hạn (sliding window) nên người dùng thường xuyên không bị
// đăng xuất.
const sessionTTL = 30 * 24 * time.Hour

// CreateSession sinh token ngẫu nhiên cho user và lưu vào DB. Trả về token để
// client lưu ở local máy (KHÔNG lưu username/mật khẩu).
func (s *AuthService) CreateSession(userID uint) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now()
	sess := models.Session{Token: token, UserID: userID, CreatedAt: now, ExpiresAt: now.Add(sessionTTL)}
	if err := s.db.Create(&sess).Error; err != nil {
		return "", err
	}
	return token, nil
}

// ResolveSession đổi token thành user. Token không tồn tại/đã hết hạn → lỗi
// (đồng thời xóa bản ghi hết hạn). Token hợp lệ thì gia hạn thêm sessionTTL.
func (s *AuthService) ResolveSession(token string) (models.User, error) {
	if token == "" {
		return models.User{}, fmt.Errorf("thiếu token phiên")
	}
	var sess models.Session
	err := s.db.Where("token = ?", token).First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.User{}, fmt.Errorf("phiên không hợp lệ")
	}
	if err != nil {
		return models.User{}, err
	}
	if time.Now().After(sess.ExpiresAt) {
		s.db.Where("token = ?", token).Delete(&models.Session{})
		return models.User{}, fmt.Errorf("phiên đã hết hạn")
	}
	var u models.User
	if err := s.db.First(&u, sess.UserID).Error; err != nil {
		return models.User{}, fmt.Errorf("phiên không hợp lệ")
	}
	// Gia hạn sliding window; lỗi cập nhật không chặn đăng nhập.
	s.db.Model(&models.Session{}).Where("token = ?", token).
		Update("expires_at", time.Now().Add(sessionTTL))
	return u, nil
}

// DeleteSession xóa phiên đã lưu (khi người dùng đăng xuất).
func (s *AuthService) DeleteSession(token string) {
	if token == "" {
		return
	}
	s.db.Where("token = ?", token).Delete(&models.Session{})
}

// hashPassword băm Argon2id, mã hóa theo chuẩn PHC string.
func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// verifyPassword so khớp theo constant-time; đọc tham số từ chuỗi PHC
// nên đổi tham số mặc định sau này không làm hỏng hash cũ.
func verifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("hash không đúng định dạng")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
