package async

import (
	"context"
	"log"
	"sync"
	"time"
)

type RunnerConfig struct {
	NotificationsWorkers int
	AnalyticsWorkers     int
}

type Runner struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	notifications chan NotificationJob
	analytics     chan AnalyticsJob
}

type NotificationJob struct {
	Kind    string
	Payload map[string]any
	At      time.Time
}

type AnalyticsJob struct {
	Event   string
	Payload map[string]any
	At      time.Time
}

func NewRunner(cfg RunnerConfig) *Runner {
	if cfg.NotificationsWorkers <= 0 {
		cfg.NotificationsWorkers = 1
	}
	if cfg.AnalyticsWorkers <= 0 {
		cfg.AnalyticsWorkers = 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{
		ctx:           ctx,
		cancel:        cancel,
		notifications: make(chan NotificationJob, 256),
		analytics:     make(chan AnalyticsJob, 256),
	}

	for i := 0; i < cfg.NotificationsWorkers; i++ {
		r.wg.Add(1)
		go r.notificationsWorker(i + 1)
	}
	for i := 0; i < cfg.AnalyticsWorkers; i++ {
		r.wg.Add(1)
		go r.analyticsWorker(i + 1)
	}

	return r
}

func (r *Runner) Close() {
	r.cancel()
	r.wg.Wait()
}

func (r *Runner) EnqueueNotification(kind string, payload map[string]any) {
	job := NotificationJob{Kind: kind, Payload: payload, At: time.Now()}
	select {
	case r.notifications <- job:
	default:
		log.Printf("notifications queue full; dropping job kind=%s", kind)
	}
}

func (r *Runner) EnqueueAnalytics(event string, payload map[string]any) {
	job := AnalyticsJob{Event: event, Payload: payload, At: time.Now()}
	select {
	case r.analytics <- job:
	default:
		log.Printf("analytics queue full; dropping event=%s", event)
	}
}

func (r *Runner) notificationsWorker(workerID int) {
	defer r.wg.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case job := <-r.notifications:
			log.Printf("[notify:%d] kind=%s payload=%v", workerID, job.Kind, job.Payload)
		}
	}
}

func (r *Runner) analyticsWorker(workerID int) {
	defer r.wg.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case job := <-r.analytics:
			log.Printf("[analytics:%d] event=%s payload=%v", workerID, job.Event, job.Payload)
		}
	}
}

