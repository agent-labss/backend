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
	errNoMoreActions = errors.New("no more actions")
	errNoObservation = errors.New("no observation")
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
}

func (executor *fakeExecutor) Execute(_ context.Context, request ExecuteRequest) (Observation, error) {
	if len(executor.observations) == 0 {
		return Observation{}, errNoObservation
	}
	observation := executor.observations[0]
	executor.observations = executor.observations[1:]
	observation.StepOrder = request.StepOrder
	observation.ToolName = request.Tool.Name
	return observation, nil
}

type memoryRunStore struct {
	record        CreateRunRecord
	run           Run
	steps         []StepRecord
	interruptions []Interruption
	chats         []ChatSession
	messages      []ChatMessage
	observations  []Observation
	waitingRun    Run
	finishedRun   Run
}

func (store *memoryRunStore) StartRun(_ context.Context, record CreateRunRecord) (Run, error) {
	store.record = record
	store.run = Run{
		ID:               testRunID,
		SessionID:        record.SessionID,
		TriggerMessageID: record.TriggerMessageID,
		Status:           RunStatusRunning,
		StartedAt:        time.Now(),
	}
	return store.run, nil
}

func (store *memoryRunStore) FinishRun(_ context.Context, run Run) error {
	store.finishedRun = run
	store.run = run
	return nil
}

func (store *memoryRunStore) SaveStep(_ context.Context, step StepRecord) error {
	store.steps = append(store.steps, step)
	return nil
}

func (store *memoryRunStore) GetRunState(_ context.Context, runID string) (RunStateRecord, error) {
	if store.run.ID != runID {
		return RunStateRecord{}, ErrRunNotFound
	}
	active := store.activeInterruption()
	return RunStateRecord{
		Run:                store.run,
		Interruptions:      append([]Interruption{}, store.interruptions...),
		ActiveInterruption: active,
		Observations:       append([]Observation{}, store.observations...),
	}, nil
}

func (store *memoryRunStore) CreateInterruption(_ context.Context, interruption Interruption) (Interruption, error) {
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

func (store *memoryRunStore) MarkRunInterrupted(_ context.Context, run Run, _ Interruption) error {
	run.Status = RunStatusInterrupted
	store.run = run
	store.waitingRun = run
	return nil
}

func (store *memoryRunStore) ResolveInterruption(_ context.Context, interruptionID string, messageID string, status InterruptionStatus) error {
	for index := range store.interruptions {
		if store.interruptions[index].ID == interruptionID {
			store.interruptions[index].Status = status
			store.interruptions[index].RespondedAt = time.Now()
			_ = messageID
		}
	}
	return nil
}

func (store *memoryRunStore) SaveObservation(_ context.Context, record ObservationRecord) error {
	store.observations = append(store.observations, record.Observation)
	return nil
}

func (store *memoryRunStore) CreateChatSession(_ context.Context, record CreateChatSessionRecord) (ChatSession, error) {
	session := ChatSession{ID: "chat_test", Title: record.Title, Status: ChatSessionStatusOpen, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	store.chats = append(store.chats, session)
	return session, nil
}

func (store *memoryRunStore) GetChatSession(_ context.Context, sessionID string) (ChatSession, error) {
	for _, session := range store.chats {
		if session.ID == sessionID {
			return session, nil
		}
	}
	return ChatSession{}, ErrChatSessionNotFound
}

func (store *memoryRunStore) ListChatMessages(_ context.Context, sessionID string) ([]ChatMessage, error) {
	messages := []ChatMessage{}
	for _, message := range store.messages {
		if message.SessionID == sessionID {
			messages = append(messages, message)
		}
	}
	return messages, nil
}

func (store *memoryRunStore) CreateChatMessage(_ context.Context, record CreateChatMessageRecord) (ChatMessage, error) {
	message := ChatMessage{
		ID:          fmt.Sprintf("msg_%d", len(store.messages)+1),
		SessionID:   record.SessionID,
		RunID:       record.RunID,
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

func (store *memoryRunStore) ActiveInterruption(_ context.Context, sessionID string) (*Interruption, error) {
	for index := range store.interruptions {
		if store.interruptions[index].SessionID == sessionID && store.interruptions[index].Status == InterruptionStatusAwaitingReview {
			interruption := store.interruptions[index]
			return &interruption, nil
		}
	}
	return nil, ErrNoActiveInterruption
}

func (store *memoryRunStore) activeInterruption() *Interruption {
	for index := range store.interruptions {
		if store.interruptions[index].Status == InterruptionStatusAwaitingReview {
			interruption := store.interruptions[index]
			return &interruption
		}
	}
	return nil
}

func runStores(store *memoryRunStore) RunStore {
	return RunStore{
		startRun:            store.StartRun,
		getRunState:         store.GetRunState,
		finishRun:           store.FinishRun,
		saveStep:            store.SaveStep,
		createInterruption:  store.CreateInterruption,
		markRunInterrupted:  store.MarkRunInterrupted,
		resolveInterruption: store.ResolveInterruption,
		saveObservation:     store.SaveObservation,
		createChatSession:   store.CreateChatSession,
		getChatSession:      store.GetChatSession,
		listChatMessages:    store.ListChatMessages,
		createChatMessage:   store.CreateChatMessage,
		activeInterruption:  store.ActiveInterruption,
	}
}

type blockingFinishRunStore struct {
	finishStarted chan struct{}
	finishContext context.Context
	finishedRun   Run
	memoryRunStore
}

func newBlockingFinishRunStore() *blockingFinishRunStore {
	store := &blockingFinishRunStore{finishStarted: make(chan struct{})}
	store.chats = append(store.chats, ChatSession{ID: "chat_test", Status: ChatSessionStatusOpen})
	return store
}

func (store *blockingFinishRunStore) FinishRun(ctx context.Context, run Run) error {
	store.finishContext = ctx
	store.finishedRun = run
	close(store.finishStarted)
	<-ctx.Done()
	return fmt.Errorf("finish wait: %w", ctx.Err())
}

type runResult struct {
	response SubmitChatMessageResponse
	err      error
}

func immediateRunScheduler(task func()) {
	task()
}

func TestServiceCreateChatMessageSchedulesRunWithoutExecutingImmediately(t *testing.T) {
	runStore := newChatMemoryRunStore()
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeFinalAnswer, Answer: "done"}}}
	bus := NewEventBusWithBuffer(8)
	events, unsubscribe := bus.Subscribe("chat_test")
	defer unsubscribe()

	var scheduled func()
	service := NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{},
		Executor:     &fakeExecutor{},
		RunStore:     runStores(runStore),
		MaxSteps:     3,
		TotalTimeout: time.Second,
		EventBus:     bus,
		Schedule: func(task func()) {
			scheduled = task
		},
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "export report"})
	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != RunStatusRunning || response.RunID == "" || response.UserMessage.Role != ChatMessageRoleUser {
		t.Fatalf("response = %#v, want accepted running response", response)
	}
	if scheduled == nil {
		t.Fatal("scheduled task is nil")
	}
	if len(planner.requests) != 0 {
		t.Fatalf("planner requests = %d, want 0 before scheduled task runs", len(planner.requests))
	}

	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeRunStarted)

	scheduled()

	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeRunCompleted)
}

func TestServiceCreateChatMessageResolvesInterruptionAndSchedulesExistingRun(t *testing.T) {
	runStore := newChatMemoryRunStore()
	runStore.run = Run{ID: "run_existing", SessionID: "chat_test", Status: RunStatusInterrupted}
	runStore.interruptions = append(runStore.interruptions, Interruption{
		ID:        "int_1",
		SessionID: "chat_test",
		RunID:     "run_existing",
		Type:      InterruptionTypeApproval,
		Status:    InterruptionStatusAwaitingReview,
		Message:   "Confirm import?",
	})
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeFinalAnswer, Answer: "import complete"}}}
	bus := NewEventBusWithBuffer(8)
	events, unsubscribe := bus.Subscribe("chat_test")
	defer unsubscribe()

	var scheduled func()
	service := NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{},
		Executor:     &fakeExecutor{},
		RunStore:     runStores(runStore),
		MaxSteps:     3,
		TotalTimeout: time.Second,
		EventBus:     bus,
		Schedule: func(task func()) {
			scheduled = task
		},
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "confirm"})
	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.RunID != "run_existing" || response.Status != RunStatusRunning {
		t.Fatalf("response = %#v, want existing run running", response)
	}
	if runStore.interruptions[0].Status != InterruptionStatusResolved {
		t.Fatalf("interruption status = %q, want resolved", runStore.interruptions[0].Status)
	}

	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeInterruptionResolved)
	assertNextEvent(t, events, EventTypeRunResumed)

	scheduled()

	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeRunCompleted)
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
	runStore := newChatMemoryRunStore()
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "export_report", Inputs: json.RawMessage(`{"month":"2026-05"}`)},
		{Type: ActionTypeFinalAnswer, Answer: "done", Outputs: map[string]any{"report_file": "ctx://export_report/file"}},
	}}
	executor := &fakeExecutor{observations: []Observation{{Status: StepStatusSucceeded, Outputs: map[string]any{"report_file": "ctx://export_report/file"}}}}
	bus := NewEventBusWithBuffer(8)
	events, unsubscribe := bus.Subscribe("chat_test")
	defer unsubscribe()
	service := NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "export_report", TimeoutMS: 1000}}, instructions: toolcatalog.Instructions{Content: "Use tools."}},
		Executor:     executor,
		RunStore:     runStores(runStore),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
		EventBus:     bus,
		Schedule:     immediateRunScheduler,
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "export report"})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != RunStatusRunning || response.RunID != testRunID {
		t.Fatalf("response = %#v, want accepted running response", response)
	}
	if runStore.finishedRun.Status != RunStatusSucceeded {
		t.Fatalf("finished run status = %q, want succeeded", runStore.finishedRun.Status)
	}
	if assistant := lastAssistantMessage(runStore); assistant == nil || assistant.Content != "done" {
		t.Fatalf("assistant message = %#v, want done", assistant)
	}
	if len(runStore.steps) != 1 {
		t.Fatalf("saved steps = %d, want 1", len(runStore.steps))
	}
	if runStore.record.SessionID != "chat_test" || runStore.record.TriggerMessageID == "" {
		t.Fatalf("CreateRunRecord = %#v, want chat session and trigger message", runStore.record)
	}
	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeRunStarted)
	toolStarted := assertNextEvent(t, events, EventTypeToolStarted)
	if toolStarted.ToolName != "export_report" {
		t.Fatalf("toolStarted.ToolName = %q, want export_report", toolStarted.ToolName)
	}
	toolFinished := assertNextEvent(t, events, EventTypeToolFinished)
	if toolFinished.Observation == nil || toolFinished.Observation.Status != StepStatusSucceeded {
		t.Fatalf("toolFinished.Observation = %#v, want succeeded observation", toolFinished.Observation)
	}
	assertNextEvent(t, events, EventTypeMessageCreated)
	assertNextEvent(t, events, EventTypeRunCompleted)
}

func TestServiceCreateChatMessageFailsOnUnknownToolAfterRetry(t *testing.T) {
	runStore := newChatMemoryRunStore()
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "missing", Inputs: json.RawMessage(`{}`)},
		{Type: ActionTypeCallTool, Tool: "missing", Inputs: json.RawMessage(`{}`)},
	}}
	service := newTestService(planner, runStore)

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "run missing"})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != RunStatusRunning {
		t.Fatalf("response status = %q, want running", response.Status)
	}
	if runStore.finishedRun.Status != RunStatusFailed {
		t.Fatalf("finished run status = %q, want failed", runStore.finishedRun.Status)
	}
}

func TestServiceCreateChatMessageBoundsFailedRunPersistence(t *testing.T) {
	const cleanupTimeout = 20 * time.Millisecond

	runStore := newBlockingFinishRunStore()
	service := NewService(ServiceConfig{
		Planner:      &fakePlanner{},
		Catalog:      fakeCatalog{},
		Executor:     &fakeExecutor{},
		RunStore:     runStores(&runStore.memoryRunStore),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
		Schedule:     immediateRunScheduler,
	})
	service.runStore.finishRun = runStore.FinishRun
	service.failedRunFinishTimeout = cleanupTimeout

	done := make(chan runResult, 1)
	go func() {
		response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "run fails"})
		done <- runResult{response: response, err: err}
	}()

	select {
	case <-runStore.finishStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("FinishRun was not called for failed run")
	}

	requireFailedRunFinishContext(t, runStore)
	requireFailedRunReturned(t, done)
}

func requireFailedRunFinishContext(t *testing.T, runStore *blockingFinishRunStore) {
	t.Helper()

	if _, ok := runStore.finishContext.Deadline(); !ok {
		t.Fatal("FinishRun context has no deadline")
	}
	if runStore.finishedRun.Status != RunStatusFailed {
		t.Fatalf("finished run status = %q, want failed", runStore.finishedRun.Status)
	}
}

func requireFailedRunReturned(t *testing.T, done <-chan runResult) {
	t.Helper()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("CreateChatMessage() error = %v, want nil submit error", result.err)
		}
		if result.response.Status != RunStatusRunning {
			t.Fatalf("response status = %q, want running", result.response.Status)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("CreateChatMessage() did not return after failed-run cleanup timeout")
	}
}

func TestServiceCreateChatMessagePassesAttachmentsToPlanner(t *testing.T) {
	runStore := newChatMemoryRunStore()
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeFinalAnswer, Answer: "done"}}}
	service := newTestService(planner, runStore)
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

func TestServiceCreateChatMessageCreatesInterruptionAndAssistantMessage(t *testing.T) {
	runStore := newChatMemoryRunStore()
	planner := &fakePlanner{actions: []PlannerAction{{
		Type:    ActionTypeAskUser,
		Message: "Which account should I use?",
		Payload: json.RawMessage(`{"kind":"choice"}`),
	}}}
	service := newTestService(planner, runStore)

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "delete duplicate"})
	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != RunStatusRunning {
		t.Fatalf("response status = %q, want running", response.Status)
	}
	if len(runStore.interruptions) != 1 || runStore.interruptions[0].Message != "Which account should I use?" {
		t.Fatalf("interruptions = %#v, want planner interruption", runStore.interruptions)
	}
	if assistant := lastAssistantMessage(runStore); assistant == nil || assistant.Content != "Which account should I use?" {
		t.Fatalf("assistant message = %#v, want interruption message", assistant)
	}
	if runStore.waitingRun.Status != RunStatusInterrupted {
		t.Fatalf("waitingRun.Status = %q, want interrupted", runStore.waitingRun.Status)
	}
}

func TestServiceCreateChatMessageResumesInterruptedRunWithAttachmentsAndObservations(t *testing.T) {
	runStore := newInterruptedMemoryRunStore()
	runStore.observations = []Observation{{
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
		Planner:      planner,
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "export_report", TimeoutMS: 1000}}},
		Executor:     executor,
		RunStore:     runStores(runStore),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
		Schedule:     immediateRunScheduler,
	})
	attachments := []Attachment{{ID: "att_turn", Filename: "accounts.csv", MIMEType: "text/csv", Kind: AttachmentKindCSV, Size: 7, Data: "YSxiCg=="}}

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "ok", Attachments: attachments})
	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.RunID != testRunID || response.Status != RunStatusRunning {
		t.Fatalf("response = %#v, want same run accepted running", response)
	}
	if runStore.finishedRun.Status != RunStatusSucceeded {
		t.Fatalf("finished run status = %q, want succeeded", runStore.finishedRun.Status)
	}
	requirePlannerSawInterruptionResume(t, planner)
	if len(runStore.steps) != 1 || runStore.steps[0].StepOrder != 2 {
		t.Fatalf("saved steps = %#v, want resumed step order 2", runStore.steps)
	}
	if runStore.interruptions[0].Status != InterruptionStatusResolved {
		t.Fatalf("interruption status = %q, want resolved", runStore.interruptions[0].Status)
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

func TestServiceCreateChatMessageFailsAtStepLimit(t *testing.T) {
	runStore := newChatMemoryRunStore()
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "export_report", Inputs: json.RawMessage(`{}`)},
		{Type: ActionTypeCallTool, Tool: "export_report", Inputs: json.RawMessage(`{}`)},
	}}
	executor := &fakeExecutor{observations: []Observation{
		{Status: StepStatusSucceeded},
		{Status: StepStatusSucceeded},
	}}
	service := NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "export_report", TimeoutMS: 1000}}},
		Executor:     executor,
		RunStore:     runStores(runStore),
		MaxSteps:     1,
		TotalTimeout: time.Minute,
		Schedule:     immediateRunScheduler,
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "too many"})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != RunStatusRunning {
		t.Fatalf("response status = %q, want running", response.Status)
	}
	if runStore.finishedRun.Status != RunStatusFailed {
		t.Fatalf("finished run status = %q, want failed", runStore.finishedRun.Status)
	}
}

func TestServiceCreateChatMessageAllowsOneBusinessErrorFollowUp(t *testing.T) {
	runStore := newChatMemoryRunStore()
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "find_partner", Inputs: json.RawMessage(`{"partner_name":"missing"}`)},
		{Type: ActionTypeFinalAnswer, Answer: "partner not found"},
	}}
	executor := &fakeExecutor{observations: []Observation{{Status: StepStatusFailed, Error: "partner_not_found"}}}
	service := NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "find_partner", TimeoutMS: 1000}}},
		Executor:     executor,
		RunStore:     runStores(runStore),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
		Schedule:     immediateRunScheduler,
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "export missing partner"})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != RunStatusRunning {
		t.Fatalf("response status = %q, want running", response.Status)
	}
	if assistant := lastAssistantMessage(runStore); assistant == nil || assistant.Content != "partner not found" {
		t.Fatalf("assistant message = %#v, want partner not found", assistant)
	}
	if len(runStore.steps) != 1 || runStore.steps[0].Status != StepStatusFailed {
		t.Fatalf("saved steps = %#v, want one failed step", runStore.steps)
	}
}

func TestServiceCreateChatMessageFailsRepeatedBusinessError(t *testing.T) {
	runStore := newChatMemoryRunStore()
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "find_partner", Inputs: json.RawMessage(`{"partner_name":"missing"}`)},
		{Type: ActionTypeCallTool, Tool: "find_partner", Inputs: json.RawMessage(`{"partner_name":"missing"}`)},
	}}
	executor := &fakeExecutor{observations: []Observation{
		{Status: StepStatusFailed, Error: "partner_not_found"},
		{Status: StepStatusFailed, Error: "partner_not_found"},
	}}
	service := NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "find_partner", TimeoutMS: 1000}}},
		Executor:     executor,
		RunStore:     runStores(runStore),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
		Schedule:     immediateRunScheduler,
	})

	response, err := service.CreateChatMessage(context.Background(), "chat_test", CreateChatMessageRequest{Message: "export missing partner"})

	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}
	if response.Status != RunStatusRunning {
		t.Fatalf("response status = %q, want running", response.Status)
	}
	if runStore.finishedRun.Status != RunStatusFailed {
		t.Fatalf("finished run status = %q, want failed", runStore.finishedRun.Status)
	}
}

func newTestService(planner Planner, runStore *memoryRunStore) Service {
	return NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "export_report", TimeoutMS: 1000}}},
		Executor:     &fakeExecutor{},
		RunStore:     runStores(runStore),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
		Schedule:     immediateRunScheduler,
	})
}

func lastAssistantMessage(store *memoryRunStore) *ChatMessage {
	for index := len(store.messages) - 1; index >= 0; index-- {
		if store.messages[index].Role == ChatMessageRoleAssistant {
			return &store.messages[index]
		}
	}
	return nil
}

func newChatMemoryRunStore() *memoryRunStore {
	return &memoryRunStore{
		chats: []ChatSession{{ID: "chat_test", Status: ChatSessionStatusOpen, CreatedAt: time.Now(), UpdatedAt: time.Now()}},
	}
}

func newInterruptedMemoryRunStore() *memoryRunStore {
	store := newChatMemoryRunStore()
	store.run = Run{ID: testRunID, SessionID: "chat_test", Status: RunStatusInterrupted, StartedAt: time.Now()}
	store.interruptions = []Interruption{{
		ID:        "int_test",
		SessionID: "chat_test",
		RunID:     testRunID,
		Type:      InterruptionTypeInputRequest,
		Status:    InterruptionStatusAwaitingReview,
		Message:   "Which account should I use?",
	}}
	return store
}
