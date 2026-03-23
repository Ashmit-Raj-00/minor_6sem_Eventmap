package config

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port               int
	PublicOrigin       string
	JWTSecret          []byte
	TokenTTL           time.Duration
	PasswordIterations int

	CSVDBDir string

	DefaultAdminUsername string
	DefaultAdminPassword string
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
	passwordIterations := envInt("PASSWORD_ITERATIONS", 120_000)
	csvDir := strings.TrimSpace(os.Getenv("CSV_DB_DIR"))
	if csvDir == "" {
		csvDir = "data"
	}

	defaultAdminUsername := strings.TrimSpace(os.Getenv("DEFAULT_ADMIN_USERNAME"))
	if defaultAdminUsername == "" {
		defaultAdminUsername = strings.TrimSpace(os.Getenv("DEFAULT_ADMIN_EMAIL"))
	}

	return Config{
		Port:                 port,
		PublicOrigin:         publicOrigin,
		JWTSecret:            jwtSecret,
		TokenTTL:             tokenTTL,
		PasswordIterations:   passwordIterations,
		CSVDBDir:             csvDir,
		DefaultAdminUsername: defaultAdminUsername,
		DefaultAdminPassword: strings.TrimSpace(os.Getenv("DEFAULT_ADMIN_PASSWORD")),
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

func randomSecret(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return []byte(base64.RawURLEncoding.EncodeToString(b))
}

func splitCSV(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
