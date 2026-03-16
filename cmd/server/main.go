package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eventmap/internal/api"
	"eventmap/internal/async"
	"eventmap/internal/config"
	"eventmap/internal/store"
)

func main() {
	cfg := config.FromEnv()

	jobRunner := async.NewRunner(async.RunnerConfig{
		NotificationsWorkers: 2,
		AnalyticsWorkers:     1,
	})
	defer jobRunner.Close()

	memStore := store.NewMemory(store.MemoryConfig{
		PasswordIterations: cfg.PasswordIterations,
	})
	store.SeedDefaultAdmin(memStore, cfg.DefaultAdminEmail, cfg.DefaultAdminPassword)

	handler := api.NewHandler(api.HandlerConfig{
		Config:   cfg,
		Store:    memStore,
		JobRunner: jobRunner,
	})

	server := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", cfg.Addr())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 2)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownCh := make(chan struct{})
	go func() {
		defer close(shutdownCh)
		ctx, cancel := config.ShutdownContext(10 * time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	select {
	case <-shutdownCh:
		log.Printf("shutdown complete")
	case <-time.After(12 * time.Second):
		log.Printf("shutdown timed out")
	}
}
