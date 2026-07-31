package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds database connection settings loaded from .env.
type Config struct {
	Driver     string // sqlite | postgres | mysql
	Host       string
	Port       string
	User       string
	Password   string
	Name       string
	Schema     string // postgres search_path; rỗng = public
	SQLitePath string
	// LogLevel điều khiển log của GORM: silent (mặc định) | error | warn | info.
	// info in MỌI câu SQL ra stdout — công tắc để soi truy vấn khi phát triển.
	LogLevel string
	// SlowQueryMS: câu SQL chạy lâu hơn mức này bị đánh dấu SLOW SQL. Chỉ có tác
	// dụng khi LogLevel từ warn trở lên.
	SlowQueryMS int
}

// Load reads .env (if present) then environment variables.
func Load() (*Config, error) {
	// Đọc .env vào MAP, KHÔNG ghi đè biến môi trường tiến trình. Nhờ vậy:
	//   - mỗi lần gọi (vd RetryDB sau khi người dùng sửa .env) đều nhận giá trị
	//     .env mới nhất — không bị kẹt giá trị đã nạp lần đầu;
	//   - biến môi trường THẬT vẫn được ưu tiên hơn .env (dùng cho deploy đặt
	//     cấu hình qua env). Thứ tự: env thật > .env > mặc định.
	fileEnv, _ := godotenv.Read() // rỗng nếu không có .env

	get := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v := fileEnv[key]; v != "" {
			return v
		}
		return fallback
	}

	cfg := &Config{
		Driver:     get("DB_DRIVER", "sqlite"),
		Host:       get("DB_HOST", "localhost"),
		Port:       get("DB_PORT", "5432"),
		User:       get("DB_USER", "postgres"),
		Password:   get("DB_PASSWORD", ""),
		Name:       get("DB_NAME", "taskmanager"),
		Schema:     get("DB_SCHEMA", ""),
		SQLitePath: get("DB_SQLITE_PATH", "taskmanager.db"),
		LogLevel:   get("DB_LOG", "silent"),
	}
	// Giá trị lạ (gõ sai, để trống) rơi về 200ms thay vì 0 — ngưỡng 0 sẽ khiến
	// GORM coi MỌI câu là slow query, làm log vô dụng đúng lúc cần đọc nó.
	cfg.SlowQueryMS = 200
	if v, err := strconv.Atoi(strings.TrimSpace(get("DB_SLOW_QUERY_MS", ""))); err == nil && v > 0 {
		cfg.SlowQueryMS = v
	}

	// Các biến NGOÀI nhóm DB_ được đọc bằng os.Getenv ở nơi khác. Nạp chúng từ
	// .env vào môi trường nếu CHƯA có (không đè biến thật) để các consumer đó
	// vẫn thấy cấu hình từ .env. KHÔNG nạp DB_* để phần trên luôn đọc .env
	// refresh mỗi lần Load.
	for k, v := range fileEnv {
		if strings.HasPrefix(k, "DB_") {
			continue
		}
		if _, ok := os.LookupEnv(k); !ok {
			_ = os.Setenv(k, v)
		}
	}

	return cfg, nil
}
