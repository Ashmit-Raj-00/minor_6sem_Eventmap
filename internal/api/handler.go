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
}

func NewHandler(cfg HandlerConfig) http.Handler {
	if cfg.JobRunner == nil {
		cfg.JobRunner = async.NewRunner(async.RunnerConfig{})
	}

	mux := http.NewServeMux()
	h := &handlers{
		cfg: cfg.Config,
		st:  cfg.Store,
		jobs: cfg.JobRunner,
	}

	mux.HandleFunc("/api/health", h.health)
	mux.HandleFunc("/api/auth/register", h.register)
	mux.HandleFunc("/api/auth/login", h.login)
	mux.HandleFunc("/api/me", h.me)
	mux.HandleFunc("/api/events", h.events)
	mux.HandleFunc("/api/events/", h.eventSubroutes)

	fs := http.FileServer(http.Dir("web"))
	mux.Handle("/", fs)

	return chain(
		withRequestID(),
		withLogger(log.Default()),
		withCORS(cfg.Config.PublicOrigin),
		withRateLimit(60, time.Minute),
	)(mux)
}

