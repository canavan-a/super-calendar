package config

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	ListenAddr   string
	DatabasePath string
	// SMTPHost and SMTPPort pick the mail server; Gmail by default.
	SMTPHost        string
	SMTPPort        int
	SMTPEmail       string
	SMTPAppPassword string
	// TrustProxy believes X-Forwarded-For on loopback connections (a local
	// tunnel or reverse proxy) for rate-limit keys.
	TrustProxy bool
	// Dev enables debug-only behavior: logging sign-in codes when SMTP isn't
	// configured.
	// InviteCode, when set, is the code a new account must give before a
	// sign-in code is sent (people with a pending calendar invite needn't).
	InviteCode string
	Dev        bool
}

func Load() *Config {
	// .env is optional; real deployments set env vars directly.
	_ = godotenv.Load()

	dataDir := getEnv("SUPERCALENDAR_DATA_DIR", ".data")
	return &Config{
		ListenAddr:      getEnv("SUPERCALENDAR_LISTEN_ADDR", ":8090"),
		DatabasePath:    getEnv("SUPERCALENDAR_DB_PATH", filepath.Join(dataDir, "supercalendar.db")),
		SMTPHost:        getEnv("SUPERCALENDAR_SMTP_HOST", "smtp.gmail.com"),
		SMTPPort:        getEnvInt("SUPERCALENDAR_SMTP_PORT", 587),
		SMTPEmail:       os.Getenv("SUPERCALENDAR_SMTP_EMAIL"),
		SMTPAppPassword: os.Getenv("SUPERCALENDAR_SMTP_APP_PASSWORD"),
		InviteCode:      strings.TrimSpace(os.Getenv("SUPERCALENDAR_INVITE_CODE")),
		TrustProxy:      os.Getenv("SUPERCALENDAR_TRUST_PROXY") == "1",
		Dev:             os.Getenv("SUPERCALENDAR_DEV") == "1",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("%s: not a number: %q", key, v)
	}
	return n
}
