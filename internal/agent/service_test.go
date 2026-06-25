package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"ai/backend/internal/toolcatalog"
)

var (
	errNoMoreActions       = errors.New("no more actions")
	errNoObservation       = errors.New("no observation")
	errToolCrashed         = errors.New("tool crashed")
	errInsertAssistantFail = errors.New("insert assistant failed")
)

type fakePlanner struct {
	actions  []PlannerAction
	requests []PlanRequest
	index    int
}

func (planner *fakePlanner) NextAction(_ context.Context, request PlanRequest) (PlannerAction, error) {
	planner.requests = append(planner.requests, request)
	if planner.index >= len(planner.actions) {
		return PlannerAction{}, errNoMoreActions
	}
	action := planner.actions[planner.index]
	planner.index++
	return action, nil
}

type fakeCatalog struct {
	tools        []toolcatalog.Tool
	instructions toolcatalog.Instructions
}

func (catalog fakeCatalog) ListEnabledTools(_ context.Context) ([]toolcatalog.Tool, error) {
	return catalog.tools, nil
}

func (catalog fakeCatalog) GetInstructions(_ context.Context) (toolcatalog.Instructions, error) {
	return catalog.instructions, nil
}

type fakeExecutor struct {
	observations []Observation
	err          error
}

func (executor *fakeExecutor) Execute(_ context.Context, request ExecuteRequest) (Observation, error) {
	if len(executor.observations) == 0 {
		return Observation{}, firstNonNilError(executor.err, errNoObservation)
	}
	observation := executor.observations[0]
	executor.observations = executor.observations[1:]
	observation.StepOrder = request.StepOrder
	observation.ToolName = request.Tool.Name
	return observation, executor.err
}

func firstNonNilError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

type memoryExecutionStore struct {
	record              CreateAgentExecutionRecord
	run                 AgentExecution
	steps               []StepRecord
	interruptions       []Interruption
	chats               []ChatSession
	messages            []ChatMessage
	observations        []Observation
	waitingExecution    AgentExecution
	finishedExecution   AgentExecution
	createMessageErrors map[ChatMessageRole]error
}

func (store *memoryExecutionStore) StartAgentExecution(_ context.Context, record CreateAgentExecutionRecord) (AgentExecution, error) {
	store.record = record
	store.run = AgentExecution{
		ID:               testExecutionID,
		SessionID:        record.SessionID,
		TriggerMessageID: record.TriggerMessageID,
		Status:           AgentExecutionStatusRunning,
		StartedAt:        time.Now(),
	}
	return store.run, nil
}

func (store *memoryExecutionStore) FinishAgentExecution(_ context.Context, run AgentExecution) error {
	store.finishedExecution = run
	store.run = run
	return nil
}

func (store *memoryExecutionStore) SaveStep(_ context.Context, step StepRecord) error {
	store.steps = append(store.steps, step)
	return nil
}

func (store *memoryExecutionStore) GetAgentExecutionState(_ context.Context, executionID string) (AgentExecutionStateRecord, error) {
	if store.run.ID != executionID {
		return AgentExecutionStateRecord{}, ErrAgentExecutionNotFound
	}
	active := store.activeInterruption()
	return AgentExecutionStateRecord{
		AgentExecution:     store.run,
		Interruptions:      append([]Interruption{}, store.interruptions...),
		ActiveInterruption: active,
		Observations:       append([]Observation{}, store.observations...),
	}, nil
}

func (store *memoryExecutionStore) CreateInterruption(_ context.Context, interruption Interruption) (Interruption, error) {
	if interruption.ID == "" {
		interruption.ID = "int_test"
	}
	if interruption.Type == "" {
		interruption.Type = InterruptionTypeInputRequest
	}
	if interruption.Status == "" {
		interruption.Status = InterruptionStatusAwaitingReview
	}
	store.interruptions = append(store.interruptions, interruption)
	return interruption, nil
}

func (store *memoryExecutionStore) MarkAgentExecutionInterrupted(_ context.Context, run AgentExecution, _ Interruption) error {
	run.Status = AgentExecutionStatusInterrupted
	store.run = run
	store.waitingExecution = run
	return nil
}

func (store *memoryExecutionStore) ResolveInterruption(_ context.Context, interruptionID string, messageID string, status InterruptionStatus) error {
	for index := range store.interruptions {
		if store.interruptions[index].ID == interruptionID {
			store.interruptions[index].Status = status
			store.interruptions[index].RespondedAt = time.Now()
			_ = messageID
		}
	}
	return nil
}

func (store *memoryExecutionStore) SaveObservation(_ context.Context, record ObservationRecord) error {
	store.observations = append(store.observations, record.Observation)
	return nil
}

func (store *memoryExecutionStore) CreateChatSession(_ context.Context, record CreateChatSessionRecord) (ChatSession, error) {
	session := ChatSession{ID: "chat_test", Title: record.Title, Status: ChatSessionStatusOpen, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store.chats = append(store.chats, session)
	return session, nil
}

func (store *memoryExecutionStore) GetChatSession(_ context.Context, sessionID string) (ChatSession, error) {
	for _, session := range store.chats {
		if session.ID == sessionID {
			return session, nil
		}
	}
	return ChatSession{}, ErrChatSessionNotFound
}

func (store *memoryExecutionStore) ListChatMessages(_ context.Context, sessionID string) ([]ChatMessage, error) {
	messages := []ChatMessage{}
	for _, message := range store.messages {
		if message.SessionID == sessionID {
			messages = append(messages, message)
		}
	}
	return messages, nil
}

func (store *memoryExecutionStore) CreateChatMessage(_ context.Context, record CreateChatMessageRecord) (ChatMessage, error) {
	if err := store.createMessageErrors[record.Role]; err != nil {
		return ChatMessage{}, err
	}
	message := ChatMessage{
		ID:          fmt.Sprintf("msg_%d", len(store.messages)+1),
		SessionID:   record.SessionID,
		ExecutionID: record.ExecutionID,
		Role:        record.Role,
		Content:     record.Content,
		Status:      ChatMessageStatusCompleted,
		Sequence:    len(store.messages) + 1,
		Attachments: record.Attachments,
		CreatedAt:   time.Now(),
	}
	store.messages = append(store.messages, message)
	return message, nil
}

func (store *memoryExecutionStore) ActiveInterruption(_ context.Context, sessionID string) (*Interruption, error) {
	for index := range store.interruptions {
		if store.interruptions[index].SessionID == sessionID && store.interruptions[index].Status == InterruptionStatusAwaitingReview {
			interruption := store.interruptions[index]
			return &interruption, nil
		}
	}
	return nil, ErrNoActiveInterruption
}

func (store *memoryExecutionStore) activeInterruption() *Interruption {
	for index := range store.interruptions {
		if store.interruptions[index].Status == InterruptionStatusAwaitingReview {
			interruption := store.interruptions[index]
			return &interruption
		}
	}
	return nil
}

func executionStores(store *memoryExecutionStore) AgentExecutionStore {
	return AgentExecutionStore{
		startExecution:           store.StartAgentExecution,
		getExecutionState:        store.GetAgentExecutionState,
		finishExecution:          store.FinishAgentExecution,
		saveStep:                 store.SaveStep,
		createInterruption:       store.CreateInterruption,
		markExecutionInterrupted: store.MarkAgentExecutionInterrupted,
		resolveInterruption:      store.ResolveInterruption,
		saveObservation:          store.SaveObservation,
		createChatSession:        store.CreateChatSession,
		getChatSession:           store.GetChatSession,
		listChatMessages:         store.ListChatMessages,
		createChatMessage:        store.CreateChatMessage,
		activeInterruption:       store.ActiveInterruption,
	}
}

type blockingFinishExecutionStore struct {
	finishStarted     chan struct{}
	finishContext     context.Context
	finishedExecution AgentExecution
	memoryExecutionStore
}

func newBlockingFinishExecutionStore() *blockingFinishExecutionStore {
	store := &blockingFinishExecutionStore{finishStarted: make(chan struct{})}
	store.chats = append(store.chats, ChatSession{ID: "chat_test", Status: ChatSessionStatusOpen})
	return store
}

func (store *blockingFinishExecutionStore) FinishAgentExecution(ctx context.Context, run AgentExecution) error {
	store.finishContext = ctx
	store.finishedExecution = run
	close(store.finishStarted)
	<-ctx.Done()
	return fmt.Errorf("finish wait: %w", ctx.Err())
}

type executionResult struct {
	response SubmitChatMessageResponse
	err      error
}

func immediateRunScheduler(task func(context.Context)) {
	task(context.Background())
}

func TestServiceCreateChatMessageSchedulesRunWithoutExecutingImmediately(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeFinalAnswer, Answer: "done"}}}
	bus := NewEventBusWithBuffer(8)
	events, unsubscribe := bus.Subscribe("chat_test")
	defer unsubscribe()

	var scheduled func(context.Context)
	service := NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{},
		Executor:            &fakeExecutor{},
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            3,
		TotalTimeout:        time.Second,
		EventBus:            bus,
		Schedule: func(task func(context.Context)) {
			scheduled = task
		},
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "export report"})
	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != AgentExecutionStatusRunning || response.ExecutionID == "" || response.UserMessage.Role != ChatMessageRoleUser {
		t.Fatalf("response = %#v, want accepted running response", response)
	}
	if scheduled == nil {
		t.Fatal("scheduled task is nil")
	}
	if len(planner.requests) != 0 {
		t.Fatalf("planner requests = %d, want 0 before scheduled task runs", len(planner.requests))
	}

	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeExecutionStarted)

	scheduled(context.Background())

	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeExecutionCompleted)
}

func TestServiceCreateChatMessageResolvesInterruptionAndSchedulesExistingRun(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	executionStore.run = AgentExecution{ID: "exec_existing", SessionID: "chat_test", Status: AgentExecutionStatusInterrupted}
	executionStore.interruptions = append(executionStore.interruptions, Interruption{
		ID:          "int_1",
		SessionID:   "chat_test",
		ExecutionID: "exec_existing",
		Type:        InterruptionTypeApproval,
		Status:      InterruptionStatusAwaitingReview,
		Message:     "Confirm import?",
	})
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeFinalAnswer, Answer: "import complete"}}}
	bus := NewEventBusWithBuffer(8)
	events, unsubscribe := bus.Subscribe("chat_test")
	defer unsubscribe()

	var scheduled func(context.Context)
	service := NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{},
		Executor:            &fakeExecutor{},
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            3,
		TotalTimeout:        time.Second,
		EventBus:            bus,
		Schedule: func(task func(context.Context)) {
			scheduled = task
		},
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "confirm"})
	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.ExecutionID != "exec_existing" || response.Status != AgentExecutionStatusRunning {
		t.Fatalf("response = %#v, want existing execution running", response)
	}
	if executionStore.interruptions[0].Status != InterruptionStatusResolved {
		t.Fatalf("interruption status = %q, want resolved", executionStore.interruptions[0].Status)
	}

	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeInterruptionResolved)
	assertNextEvent(t, events, EventTypeExecutionResumed)

	scheduled(context.Background())

	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeExecutionCompleted)
}

func assertNextEvent(t *testing.T, events <-chan ChatEvent, eventType ChatEventType) ChatEvent {
	t.Helper()

	select {
	case event := <-events:
		if event.Type != eventType {
			t.Fatalf("event.Type = %q, want %q in event %#v", event.Type, eventType, event)
		}
		return event
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("timed out waiting for event %q", eventType)
		return ChatEvent{}
	}
}

//nolint:cyclop // This test verifies chat-message orchestration, tool execution, persistence, and response shape together.
func TestServiceCreateChatMessageExecutesToolThenAssistantMessage(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "export_report", Inputs: json.RawMessage(`{"month":"2026-05"}`)},
		{Type: ActionTypeFinalAnswer, Answer: "done", Outputs: map[string]any{"report_file": "ctx://export_report/file"}},
	}}
	executor := &fakeExecutor{observations: []Observation{{Status: StepStatusSucceeded, Outputs: map[string]any{"report_file": "ctx://export_report/file"}}}}
	bus := NewEventBusWithBuffer(8)
	events, unsubscribe := bus.Subscribe("chat_test")
	defer unsubscribe()
	service := NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "export_report", TimeoutMS: 1000}}, instructions: toolcatalog.Instructions{Content: "Use tools."}},
		Executor:            executor,
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            8,
		TotalTimeout:        time.Minute,
		EventBus:            bus,
		Schedule:            immediateRunScheduler,
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "export report"})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != AgentExecutionStatusRunning || response.ExecutionID != testExecutionID {
		t.Fatalf("response = %#v, want accepted running response", response)
	}
	if executionStore.finishedExecution.Status != AgentExecutionStatusSucceeded {
		t.Fatalf("finished execution status = %q, want succeeded", executionStore.finishedExecution.Status)
	}
	if assistant := lastAssistantMessage(executionStore); assistant == nil || assistant.Content != "done" {
		t.Fatalf("assistant message = %#v, want done", assistant)
	}
	if len(executionStore.steps) != 1 {
		t.Fatalf("saved steps = %d, want 1", len(executionStore.steps))
	}
	if executionStore.record.SessionID != "chat_test" || executionStore.record.TriggerMessageID == "" {
		t.Fatalf("CreateAgentExecutionRecord = %#v, want chat session and trigger message", executionStore.record)
	}
	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeExecutionStarted)
	toolStarted := assertNextEvent(t, events, EventTypeToolStarted)
	if toolStarted.ToolName != "export_report" {
		t.Fatalf("toolStarted.ToolName = %q, want export_report", toolStarted.ToolName)
	}
	toolFinished := assertNextEvent(t, events, EventTypeToolFinished)
	if toolFinished.Observation == nil || toolFinished.Observation.Status != StepStatusSucceeded {
		t.Fatalf("toolFinished.Observation = %#v, want succeeded observation", toolFinished.Observation)
	}
	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeExecutionCompleted)
}

func TestServiceCreateChatMessagePublishesToolFinishedForExecutorError(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeCallTool, Tool: "export_report", Inputs: json.RawMessage(`{"month":"2026-05"}`)}}}
	executor := &fakeExecutor{
		observations: []Observation{{Status: StepStatusFailed, Error: "export failed"}},
		err:          errToolCrashed,
	}
	bus := NewEventBusWithBuffer(8)
	events, unsubscribe := bus.Subscribe("chat_test")
	defer unsubscribe()
	service := NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "export_report", TimeoutMS: 1000}}},
		Executor:            executor,
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            8,
		TotalTimeout:        time.Minute,
		EventBus:            bus,
		Schedule:            immediateRunScheduler,
	})

	_, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "export report"})
	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}

	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeExecutionStarted)
	assertNextEvent(t, events, EventTypeToolStarted)
	toolFinished := assertNextEvent(t, events, EventTypeToolFinished)
	if toolFinished.Observation == nil || toolFinished.Observation.Status != StepStatusFailed || toolFinished.Observation.Error != "export failed" {
		t.Fatalf("toolFinished.Observation = %#v, want failed observation", toolFinished.Observation)
	}
	assertNextEvent(t, events, EventTypeExecutionFailed)
}

func TestServiceCreateChatMessageFailsRunWhenAssistantMessageCannotBePersisted(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	executionStore.createMessageErrors = map[ChatMessageRole]error{ChatMessageRoleAssistant: errInsertAssistantFail}
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeFinalAnswer, Answer: "done"}}}
	service := NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{},
		Executor:            &fakeExecutor{},
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            8,
		TotalTimeout:        time.Minute,
		Schedule:            immediateRunScheduler,
	})

	_, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "export report"})
	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}

	if executionStore.finishedExecution.Status != AgentExecutionStatusFailed {
		t.Fatalf("finished execution status = %q, want failed", executionStore.finishedExecution.Status)
	}
	if executionStore.finishedExecution.ErrorSummary == "" {
		t.Fatal("finished execution error summary is empty, want assistant persistence error")
	}
}

func TestServiceCreateChatMessageFailsOnUnknownToolAfterRetry(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "missing", Inputs: json.RawMessage(`{}`)},
		{Type: ActionTypeCallTool, Tool: "missing", Inputs: json.RawMessage(`{}`)},
	}}
	service := newTestService(planner, executionStore)

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "run missing"})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != AgentExecutionStatusRunning {
		t.Fatalf("response status = %q, want running", response.Status)
	}
	if executionStore.finishedExecution.Status != AgentExecutionStatusFailed {
		t.Fatalf("finished execution status = %q, want failed", executionStore.finishedExecution.Status)
	}
}

func TestServiceCreateChatMessageBoundsFailedRunPersistence(t *testing.T) {
	const cleanupTimeout = 20 * time.Millisecond

	executionStore := newBlockingFinishExecutionStore()
	service := NewService(ServiceConfig{
		Planner:             &fakePlanner{},
		Catalog:             fakeCatalog{},
		Executor:            &fakeExecutor{},
		AgentExecutionStore: executionStores(&executionStore.memoryExecutionStore),
		MaxSteps:            8,
		TotalTimeout:        time.Minute,
		Schedule:            immediateRunScheduler,
	})
	service.executionStore.finishExecution = executionStore.FinishAgentExecution
	service.failedExecutionFinishTimeout = cleanupTimeout

	done := make(chan executionResult, 1)
	go func() {
		response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "run fails"})
		done <- executionResult{response: response, err: err}
	}()

	select {
	case <-executionStore.finishStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("FinishAgentExecution was not called for failed execution")
	}

	requireFailedExecutionFinishContext(t, executionStore)
	requireFailedExecutionReturned(t, done)
}

func requireFailedExecutionFinishContext(t *testing.T, executionStore *blockingFinishExecutionStore) {
	t.Helper()

	if _, ok := executionStore.finishContext.Deadline(); !ok {
		t.Fatal("FinishAgentExecution context has no deadline")
	}
	if executionStore.finishedExecution.Status != AgentExecutionStatusFailed {
		t.Fatalf("finished execution status = %q, want failed", executionStore.finishedExecution.Status)
	}
}

func requireFailedExecutionReturned(t *testing.T, done <-chan executionResult) {
	t.Helper()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("CreateChatMessage() error = %v, want nil submit error", result.err)
		}
		if result.response.Status != AgentExecutionStatusRunning {
			t.Fatalf("response status = %q, want running", result.response.Status)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("CreateChatMessage() did not return after failed-execution cleanup timeout")
	}
}

func TestServiceCreateChatMessagePassesAttachmentsToPlanner(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeFinalAnswer, Answer: "done"}}}
	service := newTestService(planner, executionStore)
	attachments := []Attachment{{
		ID:       "att_pdf",
		Filename: "merchant_catalog.pdf",
		MIMEType: "application/pdf",
		Kind:     AttachmentKindPDF,
		Size:     8,
		Data:     "JVBERi0xLjc=",
	}}

	_, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "update catalog", Attachments: attachments})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if len(planner.requests) != 1 {
		t.Fatalf("len(planner.requests) = %d, want 1", len(planner.requests))
	}
	if len(planner.requests[0].Attachments) != 1 {
		t.Fatalf("len(Attachments) = %d, want 1", len(planner.requests[0].Attachments))
	}
	if planner.requests[0].Attachments[0].Filename != "merchant_catalog.pdf" {
		t.Fatalf("Filename = %q, want merchant_catalog.pdf", planner.requests[0].Attachments[0].Filename)
	}
}

func TestServiceCreateChatMessageDoesNotExposeAttachmentData(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeFinalAnswer, Answer: "done"}}}
	bus := NewEventBusWithBuffer(8)
	events, unsubscribe := bus.Subscribe("chat_test")
	defer unsubscribe()
	service := NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{},
		Executor:            &fakeExecutor{},
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            8,
		TotalTimeout:        time.Minute,
		EventBus:            bus,
		Schedule:            immediateRunScheduler,
	})
	attachments := []Attachment{{
		ID:       "att_pdf",
		Filename: "merchant_catalog.pdf",
		MIMEType: "application/pdf",
		Kind:     AttachmentKindPDF,
		Size:     8,
		Data:     "JVBERi0xLjc=",
		FileID:   "file_internal",
	}}

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "update catalog", Attachments: attachments})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	requireAttachmentDataHidden(t, "response", response.UserMessage.Attachments[0])
	messageCreated := assertNextEvent(t, events, EventTypeMessageCreated)
	requireAttachmentDataHidden(t, "event", messageCreated.Message.Attachments[0])
	requirePlannerAttachmentData(t, planner, attachments[0])
}

func TestServiceCreateChatMessageCreatesInterruptionAndAssistantMessage(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	planner := &fakePlanner{actions: []PlannerAction{{
		Type:    ActionTypeAskUser,
		Message: "Which account should I use?",
		Payload: json.RawMessage(`{"kind":"choice"}`),
	}}}
	bus := NewEventBusWithBuffer(8)
	events, unsubscribe := bus.Subscribe("chat_test")
	defer unsubscribe()
	service := NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "export_report", TimeoutMS: 1000}}},
		Executor:            &fakeExecutor{},
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            8,
		TotalTimeout:        time.Minute,
		EventBus:            bus,
		Schedule:            immediateRunScheduler,
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "delete duplicate"})
	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != AgentExecutionStatusRunning {
		t.Fatalf("response status = %q, want running", response.Status)
	}
	if len(executionStore.interruptions) != 1 || executionStore.interruptions[0].Message != "Which account should I use?" {
		t.Fatalf("interruptions = %#v, want planner interruption", executionStore.interruptions)
	}
	if assistant := lastAssistantMessage(executionStore); assistant == nil || assistant.Content != "Which account should I use?" {
		t.Fatalf("assistant message = %#v, want interruption message", assistant)
	}
	if executionStore.waitingExecution.Status != AgentExecutionStatusInterrupted {
		t.Fatalf("waitingExecution.Status = %q, want interrupted", executionStore.waitingExecution.Status)
	}
	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeExecutionStarted)
	assertNextEvent(t, events, EventTypeInterruptionCreated)
	assertNextEvent(t, events, EventTypeMessageCreated)
}

func TestServiceCreateChatMessageResumesInterruptedRunWithAttachmentsAndObservations(t *testing.T) {
	executionStore := newInterruptedMemoryExecutionStore()
	executionStore.observations = []Observation{{
		StepOrder: 1,
		ToolName:  "lookup_account",
		Status:    StepStatusSucceeded,
		Outputs:   map[string]any{"account_id": "acct_123"},
	}}
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "export_report", Inputs: json.RawMessage(`{}`)},
		{Type: ActionTypeFinalAnswer, Answer: "continued"},
	}}
	executor := &fakeExecutor{observations: []Observation{{Status: StepStatusSucceeded}}}
	service := NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "export_report", TimeoutMS: 1000}}},
		Executor:            executor,
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            8,
		TotalTimeout:        time.Minute,
		Schedule:            immediateRunScheduler,
	})
	attachments := []Attachment{{ID: "att_turn", Filename: "accounts.csv", MIMEType: "text/csv", Kind: AttachmentKindCSV, Size: 7, Data: "YSxiCg=="}}

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "ok", Attachments: attachments})
	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.ExecutionID != testExecutionID || response.Status != AgentExecutionStatusRunning {
		t.Fatalf("response = %#v, want same execution accepted running", response)
	}
	if executionStore.finishedExecution.Status != AgentExecutionStatusSucceeded {
		t.Fatalf("finished execution status = %q, want succeeded", executionStore.finishedExecution.Status)
	}
	requirePlannerSawInterruptionResume(t, planner)
	if len(executionStore.steps) != 1 || executionStore.steps[0].StepOrder != 2 {
		t.Fatalf("saved steps = %#v, want resumed step order 2", executionStore.steps)
	}
	if executionStore.interruptions[0].Status != InterruptionStatusResolved {
		t.Fatalf("interruption status = %q, want resolved", executionStore.interruptions[0].Status)
	}
}

func requirePlannerSawInterruptionResume(t *testing.T, planner *fakePlanner) {
	t.Helper()

	if len(planner.requests) == 0 || planner.requests[0].Message != "ok" {
		t.Fatalf("planner requests = %#v, want resume message", planner.requests)
	}
	if planner.requests[0].Interruption == nil || planner.requests[0].Interruption.Message != "Which account should I use?" {
		t.Fatalf("planner interruption = %#v, want interruption context", planner.requests[0].Interruption)
	}
	if len(planner.requests[0].Observations) != 1 {
		t.Fatalf("planner observations = %#v, want prior observation", planner.requests[0].Observations)
	}
	if len(planner.requests[0].Attachments) != 1 {
		t.Fatalf("planner attachments = %d, want 1", len(planner.requests[0].Attachments))
	}
}

func requireAttachmentDataHidden(t *testing.T, source string, attachment Attachment) {
	t.Helper()

	if attachment.Data != "" {
		t.Fatalf("%s attachment Data = %q, want empty", source, attachment.Data)
	}
	if attachment.FileID != "" {
		t.Fatalf("%s attachment FileID = %q, want empty", source, attachment.FileID)
	}
}

func requirePlannerAttachmentData(t *testing.T, planner *fakePlanner, want Attachment) {
	t.Helper()

	if len(planner.requests) != 1 {
		t.Fatalf("planner requests = %#v, want one request", planner.requests)
	}
	got := planner.requests[0].Attachments[0]
	if got.Data != want.Data {
		t.Fatalf("planner attachment Data = %q, want %q", got.Data, want.Data)
	}
	if got.FileID != want.FileID {
		t.Fatalf("planner attachment FileID = %q, want %q", got.FileID, want.FileID)
	}
}

func TestServiceCreateChatMessageFailsAtStepLimit(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "export_report", Inputs: json.RawMessage(`{}`)},
		{Type: ActionTypeCallTool, Tool: "export_report", Inputs: json.RawMessage(`{}`)},
	}}
	executor := &fakeExecutor{observations: []Observation{
		{Status: StepStatusSucceeded},
		{Status: StepStatusSucceeded},
	}}
	service := NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "export_report", TimeoutMS: 1000}}},
		Executor:            executor,
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            1,
		TotalTimeout:        time.Minute,
		Schedule:            immediateRunScheduler,
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "too many"})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != AgentExecutionStatusRunning {
		t.Fatalf("response status = %q, want running", response.Status)
	}
	if executionStore.finishedExecution.Status != AgentExecutionStatusFailed {
		t.Fatalf("finished execution status = %q, want failed", executionStore.finishedExecution.Status)
	}
}

func TestServiceCreateChatMessageAllowsOneBusinessErrorFollowUp(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "find_partner", Inputs: json.RawMessage(`{"partner_name":"missing"}`)},
		{Type: ActionTypeFinalAnswer, Answer: "partner not found"},
	}}
	executor := &fakeExecutor{observations: []Observation{{Status: StepStatusFailed, Error: "partner_not_found"}}}
	service := NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "find_partner", TimeoutMS: 1000}}},
		Executor:            executor,
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            8,
		TotalTimeout:        time.Minute,
		Schedule:            immediateRunScheduler,
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "export missing partner"})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != AgentExecutionStatusRunning {
		t.Fatalf("response status = %q, want running", response.Status)
	}
	if assistant := lastAssistantMessage(executionStore); assistant == nil || assistant.Content != "partner not found" {
		t.Fatalf("assistant message = %#v, want partner not found", assistant)
	}
	if len(executionStore.steps) != 1 || executionStore.steps[0].Status != StepStatusFailed {
		t.Fatalf("saved steps = %#v, want one failed step", executionStore.steps)
	}
}

func TestServiceCreateChatMessageFailsRepeatedBusinessError(t *testing.T) {
	executionStore := newChatMemoryExecutionStore()
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "find_partner", Inputs: json.RawMessage(`{"partner_name":"missing"}`)},
		{Type: ActionTypeCallTool, Tool: "find_partner", Inputs: json.RawMessage(`{"partner_name":"missing"}`)},
	}}
	executor := &fakeExecutor{observations: []Observation{
		{Status: StepStatusFailed, Error: "partner_not_found"},
		{Status: StepStatusFailed, Error: "partner_not_found"},
	}}
	service := NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "find_partner", TimeoutMS: 1000}}},
		Executor:            executor,
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            8,
		TotalTimeout:        time.Minute,
		Schedule:            immediateRunScheduler,
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "export missing partner"})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != AgentExecutionStatusRunning {
		t.Fatalf("response status = %q, want running", response.Status)
	}
	if executionStore.finishedExecution.Status != AgentExecutionStatusFailed {
		t.Fatalf("finished execution status = %q, want failed", executionStore.finishedExecution.Status)
	}
}

func newTestService(planner Planner, executionStore *memoryExecutionStore) Service {
	return NewService(ServiceConfig{
		Planner:             planner,
		Catalog:             fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "export_report", TimeoutMS: 1000}}},
		Executor:            &fakeExecutor{},
		AgentExecutionStore: executionStores(executionStore),
		MaxSteps:            8,
		TotalTimeout:        time.Minute,
		Schedule:            immediateRunScheduler,
	})
}

func lastAssistantMessage(store *memoryExecutionStore) *ChatMessage {
	for index := len(store.messages) - 1; index >= 0; index-- {
		if store.messages[index].Role == ChatMessageRoleAssistant {
			return &store.messages[index]
		}
	}
	return nil
}

func newChatMemoryExecutionStore() *memoryExecutionStore {
	return &memoryExecutionStore{
		chats: []ChatSession{{ID: "chat_test", Status: ChatSessionStatusOpen, CreatedAt: time.Now(), UpdatedAt: time.Now()}},
	}
}

func newInterruptedMemoryExecutionStore() *memoryExecutionStore {
	store := newChatMemoryExecutionStore()
	store.run = AgentExecution{ID: testExecutionID, SessionID: "chat_test", Status: AgentExecutionStatusInterrupted, StartedAt: time.Now()}
	store.interruptions = []Interruption{{
		ID:          "int_test",
		SessionID:   "chat_test",
		ExecutionID: testExecutionID,
		Type:        InterruptionTypeInputRequest,
		Status:      InterruptionStatusAwaitingReview,
		Message:     "Which account should I use?",
	}}
	return store
}
