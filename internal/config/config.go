package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port         int
	PublicOrigin string
	JWTSecret    []byte
	TokenTTL     time.Duration

	CSVDBDir string

	GoogleClientID string
	DevAuthEnabled bool
	UnsafeSkipGoogleVerify bool

	UploadsDir      string
	MaxUploadBytes  int64
}

func FromEnv() Config {
	port := envInt("PORT", 8080)

	publicOrigin := strings.TrimSpace(os.Getenv("PUBLIC_ORIGIN"))
	if publicOrigin == "" {
		publicOrigin = "http://localhost:" + strconv.Itoa(port)
	}

	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	var jwtSecret []byte
	if secret != "" {
		jwtSecret = []byte(secret)
	} else {
		jwtSecret = randomSecret(32)
	}

	tokenTTL := envDuration("TOKEN_TTL", 12*time.Hour)
	csvDir := strings.TrimSpace(os.Getenv("CSV_DB_DIR"))
	if csvDir == "" {
		csvDir = "data"
		if err := os.MkdirAll(csvDir, 0o755); err != nil {
			csvDir = filepath.Join(os.TempDir(), "eventmap-data")
		}
	}

	uploadsDir := strings.TrimSpace(os.Getenv("UPLOADS_DIR"))
	if uploadsDir == "" {
		uploadsDir = filepath.Join(csvDir, "uploads")
	}
	_ = os.MkdirAll(uploadsDir, 0o755)

	return Config{
		Port:            port,
		PublicOrigin:    publicOrigin,
		JWTSecret:       jwtSecret,
		TokenTTL:        tokenTTL,
		CSVDBDir:        csvDir,
		GoogleClientID:  strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		DevAuthEnabled:  envBool("DEV_AUTH_ENABLED", true),
		UnsafeSkipGoogleVerify: envBool("UNSAFE_SKIP_GOOGLE_VERIFY", false),
		UploadsDir:      uploadsDir,
		MaxUploadBytes:  envInt64("MAX_UPLOAD_BYTES", 8<<20), // 8 MiB
	}
}

func (c Config) Addr() string { return ":" + strconv.Itoa(c.Port) }

func ShutdownContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "y":
		return true
	case "0", "false", "no", "n":
		return false
	default:
		return def
	}
}

func envInt64(key string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func randomSecret(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return []byte(base64.RawURLEncoding.EncodeToString(b))
}
