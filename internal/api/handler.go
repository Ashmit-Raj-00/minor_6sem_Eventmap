package api

import (
	"log"
	"net/http"
	"time"

	"eventmap/internal/async"
	"eventmap/internal/config"
	"eventmap/internal/store"
)

type HandlerConfig struct {
	Config    config.Config
	Store     *store.Memory
	JobRunner *async.Runner
	Persist   func() error
}

func NewHandler(cfg HandlerConfig) http.Handler {
	if cfg.JobRunner == nil {
		cfg.JobRunner = async.NewRunner(async.RunnerConfig{})
	}

	mux := http.NewServeMux()
	h := &handlers{
		cfg:  cfg.Config,
		st:   cfg.Store,
		jobs: cfg.JobRunner,
		chat: newChatHub(),
	}
	if cfg.Persist != nil {
		h.persist = cfg.Persist
	}

	mux.HandleFunc("/api/health", h.health)
	mux.HandleFunc("/config.js", h.configJS)

	mux.HandleFunc("/api/auth/google", h.authGoogle)
	mux.HandleFunc("/api/auth/dev", h.authDev)
	mux.HandleFunc("/api/me", h.me)
	mux.HandleFunc("/api/me/role", h.meRole)
	mux.HandleFunc("/api/me/activity", h.meActivity)

	mux.HandleFunc("/api/events", h.events)
	mux.HandleFunc("/api/events/", h.eventSubroutes)
	mux.HandleFunc("/api/tasks/", h.taskSubroutes)

	mux.HandleFunc("/api/notifications", h.notifications)
	mux.HandleFunc("/api/leaderboard", h.leaderboard)

	// Local dev uploads (use S3/GCS in production).
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(cfg.Config.UploadsDir))))

	fs := http.FileServer(http.Dir("web"))
	mux.Handle("/", fs)

	return chain(
		withRequestID(),
		withLogger(log.Default()),
		withCORS(cfg.Config.PublicOrigin),
		withRateLimit(60, time.Minute),
	)(mux)
}
