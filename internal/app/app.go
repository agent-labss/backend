package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"orderbuddy-ai/backend/internal/config"
	"orderbuddy-ai/backend/internal/httpapi"
	"orderbuddy-ai/backend/internal/platform/postgres"
	"orderbuddy-ai/backend/internal/status"
)

func Run(cfg config.Config) error {
	pool, err := postgres.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	statusService := status.NewService(pool)
	statusHandler := status.NewHandler(statusService, cfg.AppEnv)
	router := httpapi.NewRouter(httpapi.RouterConfig{StatusHandler: statusHandler})

	listenErr := make(chan error, 1)
	go func() {
		log.Printf("ai backend listening on %s", cfg.HTTPAddr)
		listenErr <- router.Listen(cfg.HTTPAddr)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	select {
	case <-stop:
	case err := <-listenErr:
		return fmt.Errorf("listen http: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := router.ShutdownWithContext(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http: %w", err)
	}

	return nil
}
