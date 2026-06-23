package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/gorm"

	"ai/backend/internal/agent"
	"ai/backend/internal/config"
	"ai/backend/internal/httpapi"
	"ai/backend/internal/platform/datastore"
	"ai/backend/internal/status"
	"ai/backend/internal/toolcatalog"
)

func Run(cfg config.Config) error {
	ctx := context.Background()
	database, err := datastore.Connect(ctx, datastore.Config{
		Driver: cfg.DatabaseDriver,
		URL:    cfg.DatabaseURL,
	})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer datastore.Close(database)

	routerConfig, err := newRouterConfig(cfg, database)
	if err != nil {
		return err
	}
	router := httpapi.NewRouter(routerConfig)

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

func newRouterConfig(cfg config.Config, database *gorm.DB) (httpapi.RouterConfig, error) {
	toolRepository := toolcatalog.NewRepository(database)
	toolService := toolcatalog.NewService(toolRepository, cfg.TrustedToolDir)
	toolHandler := toolcatalog.NewHandler(toolService)

	agentRepository := agent.NewRepository(database)
	agentHandler := newAgentHandler(cfg, agentRepository, toolService)

	statusService := status.NewService(datastore.Pinger{DB: database})
	statusHandler := status.NewHandler(statusService, cfg.AppEnv)

	return httpapi.RouterConfig{
		StatusHandler: statusHandler,
		ToolHandler:   toolHandler,
		AgentHandler:  agentHandler,
	}, nil
}

func newAgentHandler(cfg config.Config, repository agent.Repository, catalog agent.Catalog) agent.Handler {
	planner := agent.NewOpenAIPlanner(cfg.OpenAIAPIKey, cfg.OpenAIModel)
	executor := agent.NewCLIExecutor(agent.ServiceAccount{
		Profile:  "internal_report_service",
		Username: cfg.InternalReportUsername,
		Password: cfg.InternalReportPassword,
	})
	service := agent.NewService(agent.ServiceConfig{
		Planner:      planner,
		Catalog:      catalog,
		Executor:     executor,
		RunStore:     repository,
		MaxSteps:     cfg.AgentMaxSteps,
		TotalTimeout: time.Duration(cfg.AgentTotalTimeoutMS) * time.Millisecond,
	})
	return agent.NewHandler(service)
}
