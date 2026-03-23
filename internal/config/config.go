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

	AuthProvider string // local | supabase | either

	SupabaseURL       string
	SupabaseAnonKey   string
	SupabaseJWTSecret []byte

	AdminEmails     []string
	OrganizerEmails []string

	DefaultAdminEmail    string
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

	authProvider := strings.TrimSpace(strings.ToLower(os.Getenv("AUTH_PROVIDER")))

	supabaseURL := strings.TrimSpace(os.Getenv("SUPABASE_URL"))
	supabaseAnonKey := strings.TrimSpace(os.Getenv("SUPABASE_ANON_KEY"))

	supabaseJWTSecretEnv := strings.TrimSpace(os.Getenv("SUPABASE_JWT_SECRET"))
	var supabaseJWTSecret []byte
	if supabaseJWTSecretEnv != "" {
		supabaseJWTSecret = []byte(supabaseJWTSecretEnv)
	}

	if authProvider == "" {
		if len(supabaseJWTSecret) > 0 {
			authProvider = "supabase"
		} else {
			authProvider = "local"
		}
	}

	return Config{
		Port:               port,
		PublicOrigin:       publicOrigin,
		JWTSecret:          jwtSecret,
		TokenTTL:           tokenTTL,
		PasswordIterations: passwordIterations,
		AuthProvider:       authProvider,
		SupabaseURL:        supabaseURL,
		SupabaseAnonKey:    supabaseAnonKey,
		SupabaseJWTSecret:  supabaseJWTSecret,
		AdminEmails:        splitCSV(os.Getenv("ADMIN_EMAILS")),
		OrganizerEmails:    splitCSV(os.Getenv("ORGANIZER_EMAILS")),
		DefaultAdminEmail:  strings.TrimSpace(os.Getenv("DEFAULT_ADMIN_EMAIL")),
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
