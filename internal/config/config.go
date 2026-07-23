package config

import (
	"os"

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
}

// Load reads .env (if present) then environment variables.
func Load() (*Config, error) {
	// .env is optional: real env vars take precedence when the file is absent.
	_ = godotenv.Load()

	cfg := &Config{
		Driver:     getEnv("DB_DRIVER", "sqlite"),
		Host:       getEnv("DB_HOST", "localhost"),
		Port:       getEnv("DB_PORT", "5432"),
		User:       getEnv("DB_USER", "postgres"),
		Password:   getEnv("DB_PASSWORD", ""),
		Name:       getEnv("DB_NAME", "taskmanager"),
		Schema:     getEnv("DB_SCHEMA", ""),
		SQLitePath: getEnv("DB_SQLITE_PATH", "taskmanager.db"),
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
