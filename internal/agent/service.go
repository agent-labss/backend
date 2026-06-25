package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ai/backend/internal/toolcatalog"
)

var ErrRunFailed = errors.New("agent run failed")

var errAssistantMessageEmpty = errors.New("assistant message is empty")

const defaultFailedRunFinishTimeout = 5 * time.Second

type Catalog interface {
	ListEnabledTools(ctx context.Context) ([]toolcatalog.Tool, error)
	GetInstructions(ctx context.Context) (toolcatalog.Instructions, error)
}

type Executor interface {
	Execute(ctx context.Context, request ExecuteRequest) (Observation, error)
}

type ServiceConfig struct {
	Planner      Planner
	Catalog      Catalog
	Executor     Executor
	RunStore     RunStore
	MaxSteps     int
	TotalTimeout time.Duration
	EventBus     *EventBus
	Schedule     RunScheduler
}

type RunScheduler func(task func())

type RunStore struct {
	startRun            func(ctx context.Context, record CreateRunRecord) (Run, error)
	getRunState         func(ctx context.Context, runID string) (RunStateRecord, error)
	finishRun           func(ctx context.Context, run Run) error
	saveStep            func(ctx context.Context, step StepRecord) error
	createInterruption  func(ctx context.Context, interruption Interruption) (Interruption, error)
	markRunInterrupted  func(ctx context.Context, run Run, interruption Interruption) error
	resolveInterruption func(ctx context.Context, interruptionID string, messageID string, status InterruptionStatus) error
	saveObservation     func(ctx context.Context, record ObservationRecord) error
	createChatSession   func(ctx context.Context, record CreateChatSessionRecord) (ChatSession, error)
	getChatSession      func(ctx context.Context, sessionID string) (ChatSession, error)
	listChatMessages    func(ctx context.Context, sessionID string) ([]ChatMessage, error)
	createChatMessage   func(ctx context.Context, record CreateChatMessageRecord) (ChatMessage, error)
	activeInterruption  func(ctx context.Context, sessionID string) (*Interruption, error)
}

func NewRunStore(repository Repository) RunStore {
	return RunStore{
		startRun:            repository.StartRun,
		getRunState:         repository.GetRunState,
		finishRun:           repository.FinishRun,
		saveStep:            repository.SaveStep,
		createInterruption:  repository.CreateInterruption,
		markRunInterrupted:  repository.MarkRunInterrupted,
		resolveInterruption: repository.ResolveInterruption,
		saveObservation:     repository.SaveObservation,
		createChatSession:   repository.CreateChatSession,
		getChatSession:      repository.GetChatSession,
		listChatMessages:    repository.ListChatMessages,
		createChatMessage:   repository.CreateChatMessage,
		activeInterruption:  repository.ActiveInterruption,
	}
}

type Service struct {
	planner                Planner
	catalog                Catalog
	executor               Executor
	runStore               RunStore
	maxSteps               int
	totalTimeout           time.Duration
	failedRunFinishTimeout time.Duration
	eventBus               *EventBus
	schedule               RunScheduler
}

type runState struct {
	run                 Run
	message             string
	attachments         []Attachment
	interruption        *Interruption
	instructions        toolcatalog.Instructions
	tools               []toolcatalog.Tool
	toolsByName         map[string]toolcatalog.Tool
	runContext          *RunContext
	observations        []Observation
	unknownToolCount    int
	businessErrorCounts map[string]int
}

func NewService(config ServiceConfig) Service {
	return Service{
		planner:                config.Planner,
		catalog:                config.Catalog,
		executor:               config.Executor,
		runStore:               config.RunStore,
		maxSteps:               config.MaxSteps,
		totalTimeout:           config.TotalTimeout,
		failedRunFinishTimeout: defaultFailedRunFinishTimeout,
		eventBus:               configuredEventBus(config.EventBus),
		schedule:               configuredScheduler(config.Schedule),
	}
}

func defaultRunScheduler(task func()) {
	go task()
}

func configuredEventBus(bus *EventBus) *EventBus {
	if bus != nil {
		return bus
	}
	return NewEventBus()
}

func configuredScheduler(schedule RunScheduler) RunScheduler {
	if schedule != nil {
		return schedule
	}
	return defaultRunScheduler
}

func (service Service) CreateChat(ctx context.Context, request CreateChatRequest) (ChatSession, error) {
	session, err := service.runStore.createChatSession(ctx, CreateChatSessionRecord(request))
	if err != nil {
		return ChatSession{}, fmt.Errorf("create chat session: %w", err)
	}
	return session, nil
}

func (service Service) GetChat(ctx context.Context, sessionID string) (ChatSession, error) {
	session, err := service.runStore.getChatSession(ctx, sessionID)
	if err != nil {
		return ChatSession{}, fmt.Errorf("get chat session: %w", err)
	}
	return session, nil
}

func (service Service) ListChatMessages(ctx context.Context, sessionID string) ([]ChatMessage, error) {
	messages, err := service.runStore.listChatMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	return messages, nil
}

func (service Service) SubscribeChatEvents(ctx context.Context, chatID string) (<-chan ChatEvent, func(), error) {
	if _, err := service.runStore.getChatSession(ctx, chatID); err != nil {
		return nil, nil, fmt.Errorf("get chat session: %w", err)
	}
	events, unsubscribe := service.eventBus.Subscribe(chatID)
	return events, unsubscribe, nil
}

func (service Service) CreateChatMessage(ctx context.Context, sessionID string, request CreateChatMessageRequest) (SubmitChatMessageResponse, error) {
	if _, err := service.runStore.getChatSession(ctx, sessionID); err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("get chat session: %w", err)
	}
	userMessage, err := service.runStore.createChatMessage(ctx, CreateChatMessageRecord{
		SessionID:   sessionID,
		Role:        ChatMessageRoleUser,
		Content:     request.Message,
		Attachments: request.Attachments,
	})
	if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("create user message: %w", err)
	}
	service.publishMessageCreated(userMessage)

	active, err := service.runStore.activeInterruption(ctx, sessionID)
	if errors.Is(err, ErrNoActiveInterruption) {
		active = nil
	} else if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("active interruption: %w", err)
	}
	if active != nil {
		return service.submitResumeChatRun(ctx, userMessage, *active, request)
	}
	return service.submitNewChatRun(ctx, userMessage, request)
}

func (service Service) submitNewChatRun(ctx context.Context, userMessage ChatMessage, request CreateChatMessageRequest) (SubmitChatMessageResponse, error) {
	run, err := service.runStore.startRun(ctx, CreateRunRecord{
		SessionID:        userMessage.SessionID,
		TriggerMessageID: userMessage.ID,
	})
	if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("start run: %w", err)
	}
	service.eventBus.Publish(userMessage.SessionID, ChatEvent{Type: EventTypeRunStarted, ChatID: userMessage.SessionID, RunID: run.ID})
	service.schedule(func() {
		service.executeNewChatRun(run, userMessage.SessionID, runRequest(request))
	})
	return SubmitChatMessageResponse{
		ChatID:      userMessage.SessionID,
		UserMessage: userMessage,
		RunID:       run.ID,
		Status:      RunStatusRunning,
	}, nil
}

func (service Service) submitResumeChatRun(ctx context.Context, userMessage ChatMessage, interruption Interruption, request CreateChatMessageRequest) (SubmitChatMessageResponse, error) {
	if err := service.runStore.resolveInterruption(ctx, interruption.ID, userMessage.ID, InterruptionStatusResolved); err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("resolve interruption: %w", err)
	}
	stateRecord, err := service.runStore.getRunState(ctx, interruption.RunID)
	if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("get run state: %w", err)
	}
	run := stateRecord.Run
	run.Status = RunStatusRunning
	resolved := interruption
	resolved.Status = InterruptionStatusResolved
	resolved.RespondedAt = time.Now().UTC()

	service.eventBus.Publish(userMessage.SessionID, ChatEvent{
		Type:           EventTypeInterruptionResolved,
		ChatID:         userMessage.SessionID,
		RunID:          run.ID,
		InterruptionID: resolved.ID,
		Interruption:   &resolved,
	})
	service.eventBus.Publish(userMessage.SessionID, ChatEvent{Type: EventTypeRunResumed, ChatID: userMessage.SessionID, RunID: run.ID})
	service.schedule(func() {
		service.executeResumedChatRun(run, userMessage.SessionID, runRequest(request), &resolved, stateRecord.Observations)
	})
	return SubmitChatMessageResponse{
		ChatID:      userMessage.SessionID,
		UserMessage: userMessage,
		RunID:       run.ID,
		Status:      RunStatusRunning,
	}, nil
}

func (service Service) executeNewChatRun(run Run, sessionID string, request runRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), service.totalTimeout)
	defer cancel()

	response, runErr := service.run(ctx, run, request)
	service.finishBackgroundRun(ctx, run, sessionID, response, runErr)
}

func (service Service) executeResumedChatRun(run Run, sessionID string, request runRequest, interruption *Interruption, observations []Observation) {
	ctx, cancel := context.WithTimeout(context.Background(), service.totalTimeout)
	defer cancel()

	response, runErr := service.resumeRun(ctx, run, request, interruption, observations)
	service.finishBackgroundRun(ctx, run, sessionID, response, runErr)
}

func (service Service) finishBackgroundRun(ctx context.Context, run Run, sessionID string, response RunResponse, runErr error) {
	if runErr != nil {
		failed, failErr := service.failRun(run, response, runErr)
		if failErr != nil {
			failed.Error = RedactText(failErr.Error())
		}
		service.eventBus.Publish(sessionID, ChatEvent{Type: EventTypeRunFailed, ChatID: sessionID, RunID: run.ID, Run: &failed, Error: failed.Error})
		return
	}
	if err := service.completeRun(ctx, run, response); err != nil {
		failed := RunResponse{RunID: run.ID, Status: RunStatusFailed, Error: RedactText(err.Error())}
		service.eventBus.Publish(sessionID, ChatEvent{Type: EventTypeRunFailed, ChatID: sessionID, RunID: run.ID, Run: &failed, Error: failed.Error})
		return
	}
	assistantMessage, err := service.createAssistantMessage(ctx, sessionID, response)
	if err != nil {
		failed := RunResponse{RunID: run.ID, Status: RunStatusFailed, Error: RedactText(err.Error())}
		service.eventBus.Publish(sessionID, ChatEvent{Type: EventTypeRunFailed, ChatID: sessionID, RunID: run.ID, Run: &failed, Error: failed.Error})
		return
	}
	if assistantMessage != nil {
		service.publishMessageCreated(*assistantMessage)
	}
	if response.Status == RunStatusInterrupted && response.Interruption != nil {
		service.eventBus.Publish(sessionID, ChatEvent{
			Type:           EventTypeInterruptionCreated,
			ChatID:         sessionID,
			RunID:          run.ID,
			InterruptionID: response.Interruption.ID,
			Interruption:   response.Interruption,
		})
		return
	}
	service.eventBus.Publish(sessionID, ChatEvent{Type: EventTypeRunCompleted, ChatID: sessionID, RunID: run.ID, Run: &response})
}

func (service Service) publishMessageCreated(message ChatMessage) {
	service.eventBus.Publish(message.SessionID, ChatEvent{
		Type:      EventTypeMessageCreated,
		ChatID:    message.SessionID,
		MessageID: message.ID,
		RunID:     message.RunID,
		Message:   &message,
	})
}

func (service Service) createAssistantMessage(ctx context.Context, sessionID string, response RunResponse) (*ChatMessage, error) {
	content := response.Answer
	if response.Interruption != nil {
		content = response.Interruption.Message
	}
	if content == "" {
		return nil, errAssistantMessageEmpty
	}
	message, err := service.runStore.createChatMessage(ctx, CreateChatMessageRecord{
		SessionID: sessionID,
		RunID:     response.RunID,
		Role:      ChatMessageRoleAssistant,
		Content:   content,
	})
	if err != nil {
		return nil, fmt.Errorf("create assistant message: %w", err)
	}
	return &message, nil
}

func (service Service) run(ctx context.Context, run Run, request runRequest) (RunResponse, error) {
	return service.runFromStep(ctx, run, request, nil, nil, 1)
}

func (service Service) resumeRun(
	ctx context.Context,
	run Run,
	request runRequest,
	interruption *Interruption,
	observations []Observation,
) (RunResponse, error) {
	return service.runFromStep(ctx, run, request, interruption, observations, nextStepOrder(observations))
}

func (service Service) runFromStep(
	ctx context.Context,
	run Run,
	request runRequest,
	interruption *Interruption,
	observations []Observation,
	startStepOrder int,
) (RunResponse, error) {
	state, err := service.newRunState(ctx, run, request)
	if err != nil {
		return RunResponse{}, err
	}
	state.interruption = interruption
	state.observations = append(state.observations, observations...)

	for stepOrder := startStepOrder; stepOrder < startStepOrder+service.maxSteps; stepOrder++ {
		response, done, err := service.runStep(ctx, state, stepOrder)
		if err != nil || done {
			return response, err
		}
	}

	return RunResponse{}, fmt.Errorf("%w: step limit exceeded", ErrRunFailed)
}

func nextStepOrder(observations []Observation) int {
	maxStepOrder := 0
	for _, observation := range observations {
		if observation.StepOrder > maxStepOrder {
			maxStepOrder = observation.StepOrder
		}
	}
	return maxStepOrder + 1
}

func (service Service) newRunState(ctx context.Context, run Run, request runRequest) (*runState, error) {
	instructions, err := service.catalog.GetInstructions(ctx)
	if err != nil {
		return nil, fmt.Errorf("get instructions: %w", err)
	}
	tools, err := service.catalog.ListEnabledTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	return &runState{
		run:                 run,
		message:             request.Message,
		attachments:         request.Attachments,
		instructions:        instructions,
		tools:               tools,
		toolsByName:         indexToolsByName(tools),
		runContext:          NewRunContext(),
		businessErrorCounts: make(map[string]int),
	}, nil
}

func (service Service) runStep(ctx context.Context, state *runState, stepOrder int) (RunResponse, bool, error) {
	action, err := service.planner.NextAction(ctx, PlanRequest{
		Instructions: state.instructions.Content,
		Message:      state.message,
		Attachments:  state.attachments,
		Interruption: state.interruption,
		Tools:        state.tools,
		Observations: state.observations,
	})
	if err != nil {
		return RunResponse{}, false, fmt.Errorf("plan next action: %w", err)
	}

	switch action.Type {
	case ActionTypeFinalAnswer:
		return finalResponse(state.run.ID, action), true, nil
	case ActionTypeAskUser:
		response, err := service.askUser(ctx, state, action)
		return response, true, err
	case ActionTypeCallTool:
		return RunResponse{}, false, service.callTool(ctx, state, stepOrder, action)
	default:
		return RunResponse{}, false, fmt.Errorf("%w: invalid action type %q", ErrRunFailed, action.Type)
	}
}

func (service Service) askUser(ctx context.Context, state *runState, action PlannerAction) (RunResponse, error) {
	interruption, err := service.runStore.createInterruption(ctx, Interruption{
		SessionID: state.run.SessionID,
		RunID:     state.run.ID,
		Type:      InterruptionTypeInputRequest,
		Status:    InterruptionStatusAwaitingReview,
		Message:   action.Message,
		Payload:   action.Payload,
	})
	if err != nil {
		return RunResponse{}, fmt.Errorf("create interruption: %w", err)
	}

	state.run.Status = RunStatusInterrupted
	if err := service.runStore.markRunInterrupted(ctx, state.run, interruption); err != nil {
		return RunResponse{}, fmt.Errorf("mark run interrupted: %w", err)
	}

	return RunResponse{
		RunID:        state.run.ID,
		Status:       RunStatusInterrupted,
		Interruption: &interruption,
	}, nil
}

func (service Service) callTool(ctx context.Context, state *runState, stepOrder int, action PlannerAction) error {
	tool, ok := state.toolsByName[action.Tool]
	if !ok {
		return service.recordUnknownTool(ctx, state, stepOrder, action.Tool)
	}

	inputs, err := decodeInputs(action.Inputs)
	if err != nil {
		return err
	}

	service.eventBus.Publish(state.run.SessionID, ChatEvent{
		Type:     EventTypeToolStarted,
		ChatID:   state.run.SessionID,
		RunID:    state.run.ID,
		ToolName: tool.Name,
	})
	observation, step, err := service.executeTool(ctx, state, stepOrder, tool, inputs)
	if saveErr := service.saveStep(ctx, step); saveErr != nil {
		return saveErr
	}
	if err != nil {
		return err
	}

	state.observations = append(state.observations, observation)
	if err := service.runStore.saveObservation(ctx, ObservationRecord{
		RunID:       state.run.ID,
		StepOrder:   stepOrder,
		Observation: observation,
	}); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}
	savedObservation := observation
	service.eventBus.Publish(state.run.SessionID, ChatEvent{
		Type:        EventTypeToolFinished,
		ChatID:      state.run.SessionID,
		RunID:       state.run.ID,
		ToolName:    tool.Name,
		Observation: &savedObservation,
	})
	if observation.Status == StepStatusFailed {
		return recordBusinessError(state, tool.Name, observation.Error)
	}

	return nil
}

func (service Service) executeTool(
	ctx context.Context,
	state *runState,
	stepOrder int,
	tool toolcatalog.Tool,
	inputs map[string]any,
) (Observation, StepRecord, error) {
	started := time.Now()
	observation, err := service.executor.Execute(ctx, ExecuteRequest{
		RunID:      state.run.ID,
		StepID:     fmt.Sprintf("step_%d", stepOrder),
		StepOrder:  stepOrder,
		Tool:       tool,
		Inputs:     inputs,
		RunContext: state.runContext,
	})
	step := stepRecord(state.run.ID, stepOrder, tool.ID, inputs, observation, time.Since(started).Milliseconds())
	if err != nil {
		step.Status = StepStatusFailed
		step.ErrorSummary = err.Error()
		return observation, step, fmt.Errorf("execute tool %q: %w", tool.Name, err)
	}

	return observation, step, nil
}

func (service Service) recordUnknownTool(ctx context.Context, state *runState, stepOrder int, toolName string) error {
	state.unknownToolCount++
	observation := Observation{StepOrder: stepOrder, ToolName: toolName, Status: StepStatusFailed, Error: "unknown tool"}
	step := StepRecord{
		RunID:         state.run.ID,
		StepOrder:     stepOrder,
		ToolID:        "",
		InputSummary:  mustMarshalJSON(map[string]any{}),
		OutputSummary: mustMarshalJSON(map[string]any{errorField: observation.Error}),
		Status:        StepStatusFailed,
		ErrorSummary:  observation.Error,
	}
	if err := service.saveStep(ctx, step); err != nil {
		return err
	}
	if state.unknownToolCount > 1 {
		return fmt.Errorf("%w: unknown tool %q", ErrRunFailed, toolName)
	}

	state.observations = append(state.observations, observation)
	if err := service.runStore.saveObservation(ctx, ObservationRecord{
		RunID:       state.run.ID,
		StepOrder:   stepOrder,
		Observation: observation,
	}); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}
	savedObservation := observation
	service.eventBus.Publish(state.run.SessionID, ChatEvent{
		Type:        EventTypeToolFinished,
		ChatID:      state.run.SessionID,
		RunID:       state.run.ID,
		ToolName:    toolName,
		Observation: &savedObservation,
	})
	return nil
}

func (service Service) saveStep(ctx context.Context, step StepRecord) error {
	if err := service.runStore.saveStep(ctx, step); err != nil {
		return fmt.Errorf("save step: %w", err)
	}

	return nil
}

func (service Service) completeRun(ctx context.Context, run Run, response RunResponse) error {
	if response.Status == RunStatusInterrupted {
		return nil
	}

	run.Status = response.Status
	run.ErrorSummary = response.Error
	if err := service.runStore.finishRun(ctx, run); err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	return nil
}

func (service Service) failRun(run Run, response RunResponse, runErr error) (RunResponse, error) {
	run.Status = RunStatusFailed
	run.ErrorSummary = runErr.Error()
	ctx, cancel := context.WithTimeout(context.Background(), service.failedRunFinishTimeout)
	defer cancel()
	if err := service.runStore.finishRun(ctx, run); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("finish failed run: %w", err))
	}

	response.RunID = run.ID
	response.Status = RunStatusFailed
	response.Error = RedactText(runErr.Error())
	return response, runErr
}

func finalResponse(runID string, action PlannerAction) RunResponse {
	return RunResponse{
		RunID:   runID,
		Status:  RunStatusSucceeded,
		Answer:  RedactText(action.Answer),
		Outputs: redactOutputs(action.Outputs),
	}
}

func stepRecord(
	runID string,
	stepOrder int,
	toolID string,
	inputs map[string]any,
	observation Observation,
	durationMS int64,
) StepRecord {
	step := StepRecord{
		RunID:         runID,
		StepOrder:     stepOrder,
		ToolID:        toolID,
		InputSummary:  mustMarshalJSON(RedactJSONValue(inputs)),
		OutputSummary: mustMarshalJSON(RedactJSONValue(nonNilMap(observation.Outputs))),
		DurationMS:    durationMS,
		Status:        StepStatusSucceeded,
	}
	if observation.Status == StepStatusFailed {
		step.Status = StepStatusFailed
		step.ErrorSummary = observation.Error
		step.OutputSummary = mustMarshalJSON(map[string]any{"error": observation.Error})
	}

	return step
}

func recordBusinessError(state *runState, toolName string, errorSummary string) error {
	errorKey := toolName + "\x00" + errorSummary
	state.businessErrorCounts[errorKey]++
	if state.businessErrorCounts[errorKey] > 1 {
		return fmt.Errorf("%w: repeated tool error from %q: %s", ErrRunFailed, toolName, errorSummary)
	}

	return nil
}

func indexToolsByName(tools []toolcatalog.Tool) map[string]toolcatalog.Tool {
	indexed := make(map[string]toolcatalog.Tool, len(tools))
	for _, tool := range tools {
		indexed[tool.Name] = tool
	}
	return indexed
}

func decodeInputs(raw json.RawMessage) (map[string]any, error) {
	var inputs map[string]any
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return nil, fmt.Errorf("%w: decode tool inputs: %w", ErrRunFailed, err)
	}
	return nonNilMap(inputs), nil
}

func mustMarshalJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func redactOutputs(outputs map[string]any) map[string]any {
	return RedactJSONValue(nonNilMap(outputs))
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}

	return value
}
