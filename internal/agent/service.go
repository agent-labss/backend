package agent

import (
	"context"
	"errors"
	"time"

	"ai/backend/internal/toolcatalog"
)

var ErrAgentExecutionFailed = errors.New("agent execution failed")

var errAssistantMessageEmpty = errors.New("assistant message is empty")

const defaultFailedExecutionFinishTimeout = 5 * time.Second

type Catalog interface {
	ListEnabledTools(ctx context.Context) ([]toolcatalog.Tool, error)
	GetInstructions(ctx context.Context) (toolcatalog.Instructions, error)
}

type Executor interface {
	Execute(ctx context.Context, request ExecuteRequest) (Observation, error)
}

type ServiceConfig struct {
	Planner             Planner
	Catalog             Catalog
	Executor            Executor
	AgentExecutionStore AgentExecutionStore
	MaxSteps            int
	TotalTimeout        time.Duration
	EventBus            *EventBus
	Schedule            AgentExecutionScheduler
}

type AgentExecutionScheduler func(task func(context.Context))

type AgentExecutionStore struct {
	startExecution           func(ctx context.Context, record CreateAgentExecutionRecord) (AgentExecution, error)
	activeExecution          func(ctx context.Context, sessionID string) (*AgentExecution, error)
	getExecutionState        func(ctx context.Context, executionID string) (AgentExecutionStateRecord, error)
	finishExecution          func(ctx context.Context, execution AgentExecution) error
	saveStep                 func(ctx context.Context, step StepRecord) error
	createInterruption       func(ctx context.Context, interruption Interruption) (Interruption, error)
	markExecutionInterrupted func(ctx context.Context, execution AgentExecution, interruption Interruption) error
	resolveInterruption      func(ctx context.Context, interruptionID string, messageID string, status InterruptionStatus) error
	saveObservation          func(ctx context.Context, record ObservationRecord) error
	createChatSession        func(ctx context.Context, record CreateChatSessionRecord) (ChatSession, error)
	getChatSession           func(ctx context.Context, sessionID string) (ChatSession, error)
	listChatMessages         func(ctx context.Context, sessionID string) ([]ChatMessage, error)
	createChatMessage        func(ctx context.Context, record CreateChatMessageRecord) (ChatMessage, error)
	activeInterruption       func(ctx context.Context, sessionID string) (*Interruption, error)
}

func NewAgentExecutionStore(repository Repository) AgentExecutionStore {
	return AgentExecutionStore{
		startExecution:           repository.StartAgentExecution,
		activeExecution:          repository.ActiveAgentExecution,
		getExecutionState:        repository.GetAgentExecutionState,
		finishExecution:          repository.FinishAgentExecution,
		saveStep:                 repository.SaveStep,
		createInterruption:       repository.CreateInterruption,
		markExecutionInterrupted: repository.MarkAgentExecutionInterrupted,
		resolveInterruption:      repository.ResolveInterruption,
		saveObservation:          repository.SaveObservation,
		createChatSession:        repository.CreateChatSession,
		getChatSession:           repository.GetChatSession,
		listChatMessages:         repository.ListChatMessages,
		createChatMessage:        repository.CreateChatMessage,
		activeInterruption:       repository.ActiveInterruption,
	}
}

type Service struct {
	planner                      Planner
	catalog                      Catalog
	executor                     Executor
	executionStore               AgentExecutionStore
	maxSteps                     int
	totalTimeout                 time.Duration
	failedExecutionFinishTimeout time.Duration
	eventBus                     *EventBus
	schedule                     AgentExecutionScheduler
}

type executionState struct {
	execution           AgentExecution
	message             string
	attachments         []Attachment
	interruption        *Interruption
	instructions        toolcatalog.Instructions
	tools               []toolcatalog.Tool
	toolsByName         map[string]toolcatalog.Tool
	executionContext    *ExecutionContext
	observations        []Observation
	unknownToolCount    int
	businessErrorCounts map[string]int
}

func NewService(config ServiceConfig) Service {
	return Service{
		planner:                      config.Planner,
		catalog:                      config.Catalog,
		executor:                     config.Executor,
		executionStore:               config.AgentExecutionStore,
		maxSteps:                     config.MaxSteps,
		totalTimeout:                 config.TotalTimeout,
		failedExecutionFinishTimeout: defaultFailedExecutionFinishTimeout,
		eventBus:                     configuredEventBus(config.EventBus),
		schedule:                     configuredScheduler(config.Schedule),
	}
}

func defaultExecutionScheduler(task func(context.Context)) {
	go task(context.Background())
}

func configuredEventBus(bus *EventBus) *EventBus {
	if bus != nil {
		return bus
	}
	return NewEventBus()
}

func configuredScheduler(schedule AgentExecutionScheduler) AgentExecutionScheduler {
	if schedule != nil {
		return schedule
	}
	return defaultExecutionScheduler
}
