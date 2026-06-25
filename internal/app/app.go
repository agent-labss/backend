package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
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

	executionScheduler := newExecutionScheduler(ctx)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := executionScheduler.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown agent executions: %v", err)
		}
	}()

	routerConfig, err := newRouterConfig(cfg, database, executionScheduler.Schedule)
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
	if err := executionScheduler.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown agent executions: %w", err)
	}

	return nil
}

type executionScheduler struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	once   sync.Once
	wg     sync.WaitGroup
	closed bool
}

func newExecutionScheduler(parent context.Context) *executionScheduler {
	ctx, cancel := context.WithCancel(parent)
	return &executionScheduler{ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

func (scheduler *executionScheduler) Schedule(task func(context.Context)) {
	scheduler.mu.Lock()
	if scheduler.closed {
		scheduler.mu.Unlock()
		return
	}
	scheduler.wg.Add(1)
	scheduler.mu.Unlock()

	go func() {
		defer scheduler.wg.Done()
		task(scheduler.ctx)
	}()
}

func (scheduler *executionScheduler) Shutdown(ctx context.Context) error {
	scheduler.once.Do(func() {
		scheduler.mu.Lock()
		scheduler.closed = true
		scheduler.cancel()
		scheduler.mu.Unlock()
		go func() {
			scheduler.wg.Wait()
			close(scheduler.done)
		}()
	})

	select {
	case <-scheduler.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for agent executions: %w", ctx.Err())
	}
}

func newRouterConfig(cfg config.Config, database *gorm.DB, schedule agent.AgentExecutionScheduler) (httpapi.RouterConfig, error) {
	toolRepository := toolcatalog.NewRepository(database)
	toolService := toolcatalog.NewService(toolRepository, cfg.TrustedToolDir)
	toolHandler := toolcatalog.NewHandler(toolService)

	agentRepository := agent.NewRepository(database)
	agentHandler := newAgentHandler(cfg, agentRepository, toolService, schedule)

	statusService := status.NewService()
	statusHandler := status.NewHandler(statusService, cfg.AppEnv)

	return httpapi.RouterConfig{
		StatusHandler:      statusHandler,
		ToolHandler:        toolHandler,
		ChatSessionHandler: agentHandler,
		ChatMessageHandler: agentHandler,
	}, nil
}

func newAgentHandler(cfg config.Config, repository agent.Repository, catalog agent.Catalog, schedule agent.AgentExecutionScheduler) agent.Handler {
	planner := agent.NewOpenAIPlanner(cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIBaseURL)
	executor := agent.NewCLIExecutor()
	eventBus := agent.NewEventBus()
	service := agent.NewService(agent.ServiceConfig{
		Planner:             planner,
		Catalog:             catalog,
		Executor:            executor,
		AgentExecutionStore: agent.NewAgentExecutionStore(repository),
		MaxSteps:            cfg.AgentMaxSteps,
		TotalTimeout:        time.Duration(cfg.AgentTotalTimeoutMS) * time.Millisecond,
		EventBus:            eventBus,
		Schedule:            schedule,
	})
	return agent.NewHandler(service, service, service, agent.UploadConfig{
		MaxFiles:      cfg.AgentMaxFilesPerRun,
		MaxFileBytes:  cfg.AgentMaxFileBytes,
		MaxTotalBytes: cfg.AgentMaxTotalFileBytes,
	})
}
