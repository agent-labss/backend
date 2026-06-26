package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"ai/backend/internal/config"
)

func TestRunWrapsSQLiteConnectionErrors(t *testing.T) {
	err := Run(config.Config{
		AppEnv:         config.DefaultAppEnv,
		HTTPAddr:       config.DefaultHTTPAddr,
		DatabaseDriver: config.DefaultDatabaseDriver,
		DatabaseURL:    "/dev/null/ai.db",
	})

	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "connect database: connect sqlite database:") {
		t.Fatalf("Run() error = %q, want sqlite context", err)
	}
}

func TestRunSchedulerCancelsAndWaitsForScheduledRuns(t *testing.T) {
	scheduler := newExecutionScheduler(context.Background())
	runStarted := make(chan struct{})
	runReleased := make(chan struct{})
	scheduler.Schedule(func(ctx context.Context) {
		close(runStarted)
		<-ctx.Done()
		close(runReleased)
	})

	select {
	case <-runStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("scheduled execution did not start")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	select {
	case <-runReleased:
	default:
		t.Fatal("scheduled execution was not released before Shutdown returned")
	}
}
