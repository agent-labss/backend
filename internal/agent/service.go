package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ai/backend/internal/toolcatalog"
)

var ErrAgentExecutionFailed = errors.New("agent execution failed")

var errAssistantMessageEmpty = errors.New("assistant message is empty")

const defaultFailedExecutionFinishTimeout = 5 * time.Second

const (
	resumePayloadResponseMessageID   = "response_message_id"
	resumePayloadResponseMessage     = "response_message"
	resumePayloadResponseAttachments = "response_attachments"
)

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

func (service Service) CreateChat(ctx context.Context, request CreateChatRequest) (ChatSession, error) {
	session, err := service.executionStore.createChatSession(ctx, CreateChatSessionRecord(request))
	if err != nil {
		return ChatSession{}, fmt.Errorf("create chat session: %w", err)
	}
	return session, nil
}

func (service Service) GetChat(ctx context.Context, sessionID string) (ChatSession, error) {
	session, err := service.executionStore.getChatSession(ctx, sessionID)
	if err != nil {
		return ChatSession{}, fmt.Errorf("get chat session: %w", err)
	}
	return session, nil
}

func (service Service) ListChatMessages(ctx context.Context, sessionID string) ([]ChatMessage, error) {
	messages, err := service.executionStore.listChatMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	return messages, nil
}

func (service Service) SubscribeChatEvents(ctx context.Context, chatID string) (<-chan ChatEvent, func(), error) {
	if _, err := service.executionStore.getChatSession(ctx, chatID); err != nil {
		return nil, nil, fmt.Errorf("get chat session: %w", err)
	}
	events, unsubscribe := service.eventBus.Subscribe(chatID)
	return events, unsubscribe, nil
}

func (service Service) CreateChatMessage(ctx context.Context, sessionID string, request CreateChatMessageRequest) (SubmitChatMessageResponse, error) {
	if _, err := service.executionStore.getChatSession(ctx, sessionID); err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("get chat session: %w", err)
	}
	active, err := service.executionStore.activeInterruption(ctx, sessionID)
	if errors.Is(err, ErrNoActiveInterruption) {
		active = nil
	} else if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("active interruption: %w", err)
	}
	if active == nil {
		if err := service.ensureNoActiveExecution(ctx, sessionID); err != nil {
			return SubmitChatMessageResponse{}, err
		}
	}

	userMessage, err := service.executionStore.createChatMessage(ctx, CreateChatMessageRecord{
		SessionID:   sessionID,
		Role:        ChatMessageRoleUser,
		Content:     request.Message,
		Attachments: request.Attachments,
	})
	if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("create user message: %w", err)
	}
	userMessage = chatMessageMetadata(userMessage)
	service.publishMessageCreated(userMessage)

	if active != nil {
		return service.submitResumeChatExecution(ctx, userMessage, *active, request)
	}
	return service.submitNewChatExecution(ctx, userMessage, request)
}

func (service Service) ensureNoActiveExecution(ctx context.Context, sessionID string) error {
	execution, err := service.executionStore.activeExecution(ctx, sessionID)
	if errors.Is(err, ErrAgentExecutionNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("active execution: %w", err)
	}
	if execution != nil {
		return ErrAgentExecutionActive
	}
	return nil
}

func (service Service) submitNewChatExecution(ctx context.Context, userMessage ChatMessage, request CreateChatMessageRequest) (SubmitChatMessageResponse, error) {
	execution, err := service.executionStore.startExecution(ctx, CreateAgentExecutionRecord{
		SessionID:        userMessage.SessionID,
		TriggerMessageID: userMessage.ID,
	})
	if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("start execution: %w", err)
	}
	service.eventBus.Publish(userMessage.SessionID, ChatEvent{Type: EventTypeExecutionStarted, ChatID: userMessage.SessionID, ExecutionID: execution.ID})
	service.schedule(func(ctx context.Context) {
		service.executeNewChatExecution(ctx, execution, userMessage.SessionID, executionRequest(request))
	})
	return SubmitChatMessageResponse{
		ChatID:      userMessage.SessionID,
		UserMessage: userMessage,
		ExecutionID: execution.ID,
		Status:      AgentExecutionStatusRunning,
	}, nil
}

func (service Service) submitResumeChatExecution(ctx context.Context, userMessage ChatMessage, interruption Interruption, request CreateChatMessageRequest) (SubmitChatMessageResponse, error) {
	stateRecord, err := service.executionStore.getExecutionState(ctx, interruption.ExecutionID)
	if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("get execution state: %w", err)
	}
	execution := stateRecord.AgentExecution
	execution.Status = AgentExecutionStatusRunning
	resumeRequest, err := service.resumedExecutionRequest(ctx, execution, userMessage, request)
	if err != nil {
		return SubmitChatMessageResponse{}, err
	}
	if err := service.executionStore.resolveInterruption(ctx, interruption.ID, userMessage.ID, InterruptionStatusResolved); err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("resolve interruption: %w", err)
	}
	resolved := resolvedInterruption(interruption, userMessage)

	service.eventBus.Publish(userMessage.SessionID, ChatEvent{
		Type:           EventTypeInterruptionResolved,
		ChatID:         userMessage.SessionID,
		ExecutionID:    execution.ID,
		InterruptionID: resolved.ID,
		Interruption:   &resolved,
	})
	service.eventBus.Publish(userMessage.SessionID, ChatEvent{Type: EventTypeExecutionResumed, ChatID: userMessage.SessionID, ExecutionID: execution.ID})
	service.schedule(func(ctx context.Context) {
		service.executeResumedChatExecution(ctx, execution, userMessage.SessionID, resumeRequest, &resolved, stateRecord.Observations)
	})
	return SubmitChatMessageResponse{
		ChatID:      userMessage.SessionID,
		UserMessage: userMessage,
		ExecutionID: execution.ID,
		Status:      AgentExecutionStatusRunning,
	}, nil
}

func (service Service) resumedExecutionRequest(ctx context.Context, execution AgentExecution, userMessage ChatMessage, request CreateChatMessageRequest) (executionRequest, error) {
	resumeRequest := executionRequest{Message: userMessage.Content, Attachments: request.Attachments}
	if execution.TriggerMessageID == "" {
		return resumeRequest, nil
	}

	messages, err := service.executionStore.listChatMessages(ctx, execution.SessionID)
	if err != nil {
		return executionRequest{}, fmt.Errorf("list chat messages: %w", err)
	}
	for _, message := range messages {
		if message.ID == execution.TriggerMessageID {
			resumeRequest.Message = message.Content
			return resumeRequest, nil
		}
	}

	return resumeRequest, nil
}

func resolvedInterruption(interruption Interruption, userMessage ChatMessage) Interruption {
	resolved := interruption
	resolved.Status = InterruptionStatusResolved
	resolved.RespondedAt = time.Now().UTC()
	resolved.Payload = resolvedInterruptionPayload(interruption.Payload, userMessage)
	return resolved
}

func resolvedInterruptionPayload(raw json.RawMessage, userMessage ChatMessage) json.RawMessage {
	payload := map[string]any{}
	if len(raw) > 0 {
		var existing map[string]any
		if err := json.Unmarshal(raw, &existing); err == nil && existing != nil {
			payload = existing
		}
	}
	payload[resumePayloadResponseMessageID] = userMessage.ID
	payload[resumePayloadResponseMessage] = userMessage.Content
	if len(userMessage.Attachments) > 0 {
		payload[resumePayloadResponseAttachments] = attachmentMetadataList(userMessage.Attachments)
	}
	return mustMarshalJSON(payload)
}

func (service Service) executeNewChatExecution(parent context.Context, execution AgentExecution, sessionID string, request executionRequest) {
	ctx, cancel := context.WithTimeout(parent, service.totalTimeout)
	defer cancel()

	response, executionErr := service.execute(ctx, execution, request)
	service.finishBackgroundExecution(ctx, execution, sessionID, response, executionErr)
}

func (service Service) executeResumedChatExecution(parent context.Context, execution AgentExecution, sessionID string, request executionRequest, interruption *Interruption, observations []Observation) {
	ctx, cancel := context.WithTimeout(parent, service.totalTimeout)
	defer cancel()

	response, executionErr := service.resumeExecution(ctx, execution, request, interruption, observations)
	service.finishBackgroundExecution(ctx, execution, sessionID, response, executionErr)
}

func (service Service) finishBackgroundExecution(ctx context.Context, execution AgentExecution, sessionID string, response AgentExecutionResponse, executionErr error) {
	if executionErr != nil {
		service.failAndPublishExecution(execution, sessionID, response, executionErr)
		return
	}
	assistantMessage, err := service.createAssistantMessage(ctx, sessionID, response)
	if err != nil {
		service.failAndPublishExecution(execution, sessionID, response, err)
		return
	}
	if response.Status == AgentExecutionStatusInterrupted && response.Interruption != nil {
		service.publishInterruptionCreated(execution, sessionID, response, assistantMessage)
		return
	}
	if err := service.completeExecution(ctx, execution, response); err != nil {
		failed := AgentExecutionResponse{ExecutionID: execution.ID, Status: AgentExecutionStatusFailed, Error: RedactText(err.Error())}
		service.eventBus.Publish(sessionID, ChatEvent{Type: EventTypeExecutionFailed, ChatID: sessionID, ExecutionID: execution.ID, AgentExecution: &failed, Error: failed.Error})
		return
	}
	if assistantMessage != nil {
		service.publishMessageCreated(*assistantMessage)
	}
	service.eventBus.Publish(sessionID, ChatEvent{Type: EventTypeExecutionCompleted, ChatID: sessionID, ExecutionID: execution.ID, AgentExecution: &response})
}

func (service Service) failAndPublishExecution(execution AgentExecution, sessionID string, response AgentExecutionResponse, err error) {
	failed, failErr := service.failExecution(execution, response, err)
	if failErr != nil {
		failed.Error = RedactText(failErr.Error())
	}
	service.eventBus.Publish(sessionID, ChatEvent{Type: EventTypeExecutionFailed, ChatID: sessionID, ExecutionID: execution.ID, AgentExecution: &failed, Error: failed.Error})
}

func (service Service) publishInterruptionCreated(execution AgentExecution, sessionID string, response AgentExecutionResponse, assistantMessage *ChatMessage) {
	service.eventBus.Publish(sessionID, ChatEvent{
		Type:           EventTypeInterruptionCreated,
		ChatID:         sessionID,
		ExecutionID:    execution.ID,
		InterruptionID: response.Interruption.ID,
		Interruption:   response.Interruption,
	})
	if assistantMessage != nil {
		service.publishMessageCreated(*assistantMessage)
	}
}

func (service Service) publishMessageCreated(message ChatMessage) {
	message = chatMessageMetadata(message)
	service.eventBus.Publish(message.SessionID, ChatEvent{
		Type:        EventTypeMessageCreated,
		ChatID:      message.SessionID,
		MessageID:   message.ID,
		ExecutionID: message.ExecutionID,
		Message:     &message,
	})
}

func (service Service) createAssistantMessage(ctx context.Context, sessionID string, response AgentExecutionResponse) (*ChatMessage, error) {
	content := response.Answer
	if response.Interruption != nil {
		content = response.Interruption.Message
	}
	if content == "" {
		return nil, errAssistantMessageEmpty
	}
	message, err := service.executionStore.createChatMessage(ctx, CreateChatMessageRecord{
		SessionID:   sessionID,
		ExecutionID: response.ExecutionID,
		Role:        ChatMessageRoleAssistant,
		Content:     content,
	})
	if err != nil {
		return nil, fmt.Errorf("create assistant message: %w", err)
	}
	return &message, nil
}

func (service Service) execute(ctx context.Context, execution AgentExecution, request executionRequest) (AgentExecutionResponse, error) {
	return service.executeFromStep(ctx, execution, request, nil, nil, 1)
}

func (service Service) resumeExecution(
	ctx context.Context,
	execution AgentExecution,
	request executionRequest,
	interruption *Interruption,
	observations []Observation,
) (AgentExecutionResponse, error) {
	return service.executeFromStep(ctx, execution, request, interruption, observations, nextStepOrder(observations))
}

func (service Service) executeFromStep(
	ctx context.Context,
	execution AgentExecution,
	request executionRequest,
	interruption *Interruption,
	observations []Observation,
	startStepOrder int,
) (AgentExecutionResponse, error) {
	state, err := service.newExecutionState(ctx, execution, request)
	if err != nil {
		return AgentExecutionResponse{}, err
	}
	state.interruption = interruption
	state.observations = append(state.observations, observations...)

	for stepOrder := startStepOrder; stepOrder < startStepOrder+service.maxSteps; stepOrder++ {
		response, done, err := service.executionStep(ctx, state, stepOrder)
		if err != nil || done {
			return response, err
		}
	}

	return AgentExecutionResponse{}, fmt.Errorf("%w: step limit exceeded", ErrAgentExecutionFailed)
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

func (service Service) newExecutionState(ctx context.Context, execution AgentExecution, request executionRequest) (*executionState, error) {
	instructions, err := service.catalog.GetInstructions(ctx)
	if err != nil {
		return nil, fmt.Errorf("get instructions: %w", err)
	}
	tools, err := service.catalog.ListEnabledTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	return &executionState{
		execution:           execution,
		message:             request.Message,
		attachments:         request.Attachments,
		instructions:        instructions,
		tools:               tools,
		toolsByName:         indexToolsByName(tools),
		executionContext:    NewExecutionContext(),
		businessErrorCounts: make(map[string]int),
	}, nil
}

func (service Service) executionStep(ctx context.Context, state *executionState, stepOrder int) (AgentExecutionResponse, bool, error) {
	action, err := service.planner.NextAction(ctx, PlanRequest{
		Instructions: state.instructions.Content,
		Message:      state.message,
		Attachments:  state.attachments,
		Interruption: state.interruption,
		Tools:        state.tools,
		Observations: state.observations,
	})
	if err != nil {
		return AgentExecutionResponse{}, false, fmt.Errorf("plan next action: %w", err)
	}

	switch action.Type {
	case ActionTypeFinalAnswer:
		return finalExecutionResponse(state.execution.ID, action), true, nil
	case ActionTypeAskUser:
		response, err := service.askUser(ctx, state, action)
		return response, true, err
	case ActionTypeCallTool:
		return AgentExecutionResponse{}, false, service.callTool(ctx, state, stepOrder, action)
	default:
		return AgentExecutionResponse{}, false, fmt.Errorf("%w: invalid action type %q", ErrAgentExecutionFailed, action.Type)
	}
}

func (service Service) askUser(ctx context.Context, state *executionState, action PlannerAction) (AgentExecutionResponse, error) {
	interruption, err := service.executionStore.createInterruption(ctx, Interruption{
		SessionID:   state.execution.SessionID,
		ExecutionID: state.execution.ID,
		Type:        InterruptionTypeInputRequest,
		Status:      InterruptionStatusAwaitingReview,
		Message:     action.Message,
		Payload:     action.Payload,
	})
	if err != nil {
		return AgentExecutionResponse{}, fmt.Errorf("create interruption: %w", err)
	}

	state.execution.Status = AgentExecutionStatusInterrupted
	if err := service.executionStore.markExecutionInterrupted(ctx, state.execution, interruption); err != nil {
		return AgentExecutionResponse{}, fmt.Errorf("mark execution interrupted: %w", err)
	}

	return AgentExecutionResponse{
		ExecutionID:  state.execution.ID,
		Status:       AgentExecutionStatusInterrupted,
		Interruption: &interruption,
	}, nil
}

func (service Service) callTool(ctx context.Context, state *executionState, stepOrder int, action PlannerAction) error {
	tool, ok := state.toolsByName[action.Tool]
	if !ok {
		return service.recordUnknownTool(ctx, state, stepOrder, action.Tool)
	}

	inputs, err := decodeInputs(action.Inputs)
	if err != nil {
		return err
	}

	service.eventBus.Publish(state.execution.SessionID, ChatEvent{
		Type:        EventTypeToolStarted,
		ChatID:      state.execution.SessionID,
		ExecutionID: state.execution.ID,
		ToolName:    tool.Name,
	})
	observation, step, err := service.executeTool(ctx, state, stepOrder, tool, inputs)
	if saveErr := service.saveStep(ctx, step); saveErr != nil {
		return saveErr
	}
	if err != nil {
		service.publishToolFinished(state.execution, tool.Name, observation)
		return err
	}

	state.observations = append(state.observations, observation)
	if err := service.executionStore.saveObservation(ctx, ObservationRecord{
		ExecutionID: state.execution.ID,
		StepOrder:   stepOrder,
		Observation: observation,
	}); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}
	service.publishToolFinished(state.execution, tool.Name, observation)
	if observation.Status == StepStatusFailed {
		return recordBusinessError(state, tool.Name, observation.Error)
	}

	return nil
}

func (service Service) executeTool(
	ctx context.Context,
	state *executionState,
	stepOrder int,
	tool toolcatalog.Tool,
	inputs map[string]any,
) (Observation, StepRecord, error) {
	started := time.Now()
	observation, err := service.executor.Execute(ctx, ExecuteRequest{
		ExecutionID:      state.execution.ID,
		StepID:           fmt.Sprintf("step_%d", stepOrder),
		StepOrder:        stepOrder,
		Tool:             tool,
		Inputs:           inputs,
		ExecutionContext: state.executionContext,
	})
	step := stepRecord(state.execution.ID, stepOrder, tool.ID, inputs, observation, time.Since(started).Milliseconds())
	if err != nil {
		step.Status = StepStatusFailed
		step.ErrorSummary = err.Error()
		return observation, step, fmt.Errorf("execute tool %q: %w", tool.Name, err)
	}

	return observation, step, nil
}

func (service Service) recordUnknownTool(ctx context.Context, state *executionState, stepOrder int, toolName string) error {
	state.unknownToolCount++
	observation := Observation{StepOrder: stepOrder, ToolName: toolName, Status: StepStatusFailed, Error: "unknown tool"}
	step := StepRecord{
		ExecutionID:   state.execution.ID,
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
		return fmt.Errorf("%w: unknown tool %q", ErrAgentExecutionFailed, toolName)
	}

	state.observations = append(state.observations, observation)
	if err := service.executionStore.saveObservation(ctx, ObservationRecord{
		ExecutionID: state.execution.ID,
		StepOrder:   stepOrder,
		Observation: observation,
	}); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}
	service.publishToolFinished(state.execution, toolName, observation)
	return nil
}

func (service Service) publishToolFinished(execution AgentExecution, toolName string, observation Observation) {
	savedObservation := observation
	service.eventBus.Publish(execution.SessionID, ChatEvent{
		Type:        EventTypeToolFinished,
		ChatID:      execution.SessionID,
		ExecutionID: execution.ID,
		ToolName:    toolName,
		Observation: &savedObservation,
	})
}

func (service Service) saveStep(ctx context.Context, step StepRecord) error {
	if err := service.executionStore.saveStep(ctx, step); err != nil {
		return fmt.Errorf("save step: %w", err)
	}

	return nil
}

func (service Service) completeExecution(ctx context.Context, execution AgentExecution, response AgentExecutionResponse) error {
	if response.Status == AgentExecutionStatusInterrupted {
		return nil
	}

	execution.Status = response.Status
	execution.ErrorSummary = response.Error
	if err := service.executionStore.finishExecution(ctx, execution); err != nil {
		return fmt.Errorf("finish execution: %w", err)
	}
	return nil
}

func (service Service) failExecution(execution AgentExecution, response AgentExecutionResponse, executionErr error) (AgentExecutionResponse, error) {
	execution.Status = AgentExecutionStatusFailed
	execution.ErrorSummary = executionErr.Error()
	ctx, cancel := context.WithTimeout(context.Background(), service.failedExecutionFinishTimeout)
	defer cancel()
	if err := service.executionStore.finishExecution(ctx, execution); err != nil {
		executionErr = errors.Join(executionErr, fmt.Errorf("finish failed execution: %w", err))
	}

	response.ExecutionID = execution.ID
	response.Status = AgentExecutionStatusFailed
	response.Error = RedactText(executionErr.Error())
	return response, executionErr
}

func finalExecutionResponse(executionID string, action PlannerAction) AgentExecutionResponse {
	return AgentExecutionResponse{
		ExecutionID: executionID,
		Status:      AgentExecutionStatusSucceeded,
		Answer:      RedactText(action.Answer),
		Outputs:     redactOutputs(action.Outputs),
	}
}

func stepRecord(
	executionID string,
	stepOrder int,
	toolID string,
	inputs map[string]any,
	observation Observation,
	durationMS int64,
) StepRecord {
	step := StepRecord{
		ExecutionID:   executionID,
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

func recordBusinessError(state *executionState, toolName string, errorSummary string) error {
	errorKey := toolName + "\x00" + errorSummary
	state.businessErrorCounts[errorKey]++
	if state.businessErrorCounts[errorKey] > 1 {
		return fmt.Errorf("%w: repeated tool error from %q: %s", ErrAgentExecutionFailed, toolName, errorSummary)
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
		return nil, fmt.Errorf("%w: decode tool inputs: %w", ErrAgentExecutionFailed, err)
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

func chatMessageMetadata(message ChatMessage) ChatMessage {
	message.Attachments = attachmentMetadataList(message.Attachments)
	return message
}
