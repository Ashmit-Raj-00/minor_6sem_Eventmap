package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"eventmap/internal/auth"
	"eventmap/internal/config"
	"eventmap/internal/store"
)

type middleware func(http.Handler) http.Handler

func chain(mw ...middleware) middleware {
	return func(next http.Handler) http.Handler {
		for i := len(mw) - 1; i >= 0; i-- {
			next = mw[i](next)
		}
		return next
	}
}

type ctxKey string

const (
	ctxKeyRequestID ctxKey = "request_id"
	ctxKeyUser      ctxKey = "user"
)

func withRequestID() middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b := make([]byte, 12)
			_, _ = rand.Read(b)
			id := base64.RawURLEncoding.EncodeToString(b)
			r = r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id))
			w.Header().Set("X-Request-Id", id)
			next.ServeHTTP(w, r)
		})
	}
}

func requestID(r *http.Request) string {
	v, _ := r.Context().Value(ctxKeyRequestID).(string)
	return v
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}

func withLogger(l *log.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, code: 200}
			start := time.Now()
			next.ServeHTTP(rec, r)
			l.Printf("%s %s %d %s rid=%s", r.Method, r.URL.Path, rec.code, time.Since(start).Truncate(time.Millisecond), requestID(r))
		})
	}
}

func withCORS(origin string) middleware {
	origin = strings.TrimSpace(origin)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type limiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func withRateLimit(limit int, window time.Duration) middleware {
	l := &limiter{
		hits:   map[string][]time.Time{},
		limit:  limit,
		window: window,
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			now := time.Now()
			if !l.allow(ip, now) {
				writeJSON(w, http.StatusTooManyRequests, map[string]any{
					"error": "rate_limited",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (l *limiter) allow(key string, now time.Time) bool {
	cutoff := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	h := l.hits[key]
	out := h[:0]
	for _, t := range h {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	if len(out) >= l.limit {
		l.hits[key] = out
		return false
	}
	out = append(out, now)
	l.hits[key] = out
	return true
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func withAuth(cfg config.Config, st *store.Memory) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" {
				next.ServeHTTP(w, r)
				return
			}
			parts := strings.SplitN(h, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_authorization"})
				return
			}
			token := strings.TrimSpace(parts[1])
			now := time.Now()

			claims, err := auth.VerifyHS256(cfg.JWTSecret, token, now)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_token"})
				return
			}

			u, err := st.GetUserByID(claims.Sub)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unknown_user"})
				return
			}

			r = r.WithContext(context.WithValue(r.Context(), ctxKeyUser, u))
			next.ServeHTTP(w, r)
		})
	}
}

func requireRoles(roles ...store.Role) middleware {
	allowed := map[store.Role]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := r.Context().Value(ctxKeyUser).(store.User)
			if !ok || u.ID == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
				return
			}
			if !allowed[u.Role] {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
