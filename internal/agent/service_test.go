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
	record       CreateRunRecord
	run          Run
	steps        []StepRecord
	interactions []Interaction
	turns        []RunTurn
	observations []Observation
	waitingRun   Run
	finishedRun  Run
}

func (store *memoryRunStore) StartRun(_ context.Context, record CreateRunRecord) (Run, error) {
	store.record = record
	store.run = Run{ID: testRunID, Message: record.Message, Status: RunStatusRunning, StartedAt: time.Now()}
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

func (store *memoryRunStore) GetRun(_ context.Context, runID string) (RunResponse, error) {
	if store.run.ID != runID {
		return RunResponse{}, ErrRunNotFound
	}
	return RunResponse{RunID: store.run.ID, Status: store.run.Status, Answer: store.run.Answer, Outputs: store.run.Outputs, Error: store.run.ErrorSummary, Interaction: store.pendingInteraction()}, nil
}

func (store *memoryRunStore) GetRunState(_ context.Context, runID string) (RunStateRecord, error) {
	if store.run.ID != runID {
		return RunStateRecord{}, ErrRunNotFound
	}
	return RunStateRecord{
		Run:          store.run,
		Attachments:  store.record.Attachments,
		Interactions: append([]Interaction{}, store.interactions...),
		Pending:      store.pendingInteraction(),
		Turns:        append([]RunTurn{}, store.turns...),
		Observations: append([]Observation{}, store.observations...),
	}, nil
}

func (store *memoryRunStore) CreateInteraction(_ context.Context, interaction Interaction) (Interaction, error) {
	if interaction.ID == "" {
		interaction.ID = "int_test"
	}
	if interaction.Type == "" {
		interaction.Type = InteractionTypeUserInput
	}
	if interaction.Status == "" {
		interaction.Status = InteractionStatusPending
	}
	store.interactions = append(store.interactions, interaction)
	return interaction, nil
}

func (store *memoryRunStore) MarkRunWaiting(_ context.Context, run Run, _ Interaction) error {
	run.Status = RunStatusWaitingForUser
	store.run = run
	store.waitingRun = run
	return nil
}

func (store *memoryRunStore) CreateRunTurn(_ context.Context, record CreateRunTurnRecord) (RunTurn, error) {
	turn := RunTurn{ID: "turn_test", RunID: record.RunID, Message: record.Message, Attachments: record.Attachments, CreatedAt: time.Now()}
	store.turns = append(store.turns, turn)
	return turn, nil
}

func (store *memoryRunStore) MarkInteractionResponded(_ context.Context, interactionID string, _ string) error {
	for index := range store.interactions {
		if store.interactions[index].ID == interactionID {
			store.interactions[index].Status = InteractionStatusResponded
			store.interactions[index].RespondedAt = time.Now()
		}
	}
	return nil
}

func (store *memoryRunStore) SaveObservation(_ context.Context, record ObservationRecord) error {
	store.observations = append(store.observations, record.Observation)
	return nil
}

func (store *memoryRunStore) pendingInteraction() *Interaction {
	for index := range store.interactions {
		if store.interactions[index].Status == InteractionStatusPending {
			interaction := store.interactions[index]
			return &interaction
		}
	}
	return nil
}

func runStores(store *memoryRunStore) RunStore {
	return RunStore{
		startRun:                 store.StartRun,
		getRun:                   store.GetRun,
		getRunState:              store.GetRunState,
		finishRun:                store.FinishRun,
		saveStep:                 store.SaveStep,
		createInteraction:        store.CreateInteraction,
		markRunWaiting:           store.MarkRunWaiting,
		createRunTurn:            store.CreateRunTurn,
		markInteractionResponded: store.MarkInteractionResponded,
		saveObservation:          store.SaveObservation,
	}
}

type blockingFinishRunStore struct {
	finishStarted chan struct{}
	finishContext context.Context
	finishedRun   Run
	memoryRunStore
}

func newBlockingFinishRunStore() *blockingFinishRunStore {
	return &blockingFinishRunStore{finishStarted: make(chan struct{})}
}

func blockingRunStores(store *blockingFinishRunStore) RunStore {
	return RunStore{
		startRun:                 store.StartRun,
		getRun:                   store.GetRun,
		getRunState:              store.GetRunState,
		finishRun:                store.FinishRun,
		saveStep:                 store.SaveStep,
		createInteraction:        store.CreateInteraction,
		markRunWaiting:           store.MarkRunWaiting,
		createRunTurn:            store.CreateRunTurn,
		markInteractionResponded: store.MarkInteractionResponded,
		saveObservation:          store.SaveObservation,
	}
}

func (store *blockingFinishRunStore) StartRun(_ context.Context, record CreateRunRecord) (Run, error) {
	store.run = Run{ID: testRunID, Message: record.Message, Status: RunStatusRunning, StartedAt: time.Now()}
	return store.run, nil
}

func (store *blockingFinishRunStore) FinishRun(ctx context.Context, run Run) error {
	store.finishContext = ctx
	store.finishedRun = run
	close(store.finishStarted)
	<-ctx.Done()
	return fmt.Errorf("finish wait: %w", ctx.Err())
}

func (store *blockingFinishRunStore) SaveStep(_ context.Context, _ StepRecord) error {
	return nil
}

type runResult struct {
	response RunResponse
	err      error
}

func TestServiceRunExecutesToolThenFinalAnswer(t *testing.T) {
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "export_report", Inputs: json.RawMessage(`{"month":"2026-05"}`)},
		{Type: ActionTypeFinalAnswer, Answer: "done", Outputs: map[string]any{"report_file": "ctx://export_report/file"}},
	}}
	executor := &fakeExecutor{observations: []Observation{{Status: StepStatusSucceeded, Outputs: map[string]any{"report_file": "ctx://export_report/file"}}}}
	runStore := &memoryRunStore{}
	service := NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "export_report", TimeoutMS: 1000}}, instructions: toolcatalog.Instructions{Content: "Use tools."}},
		Executor:     executor,
		RunStore:     runStores(runStore),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
	})

	response, err := service.Run(context.Background(), CreateRunRequest{Message: "export report"})

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != RunStatusSucceeded {
		t.Fatalf("Status = %q, want succeeded", response.Status)
	}
	if response.Answer != "done" {
		t.Fatalf("Answer = %q, want done", response.Answer)
	}
	if len(runStore.steps) != 1 {
		t.Fatalf("saved steps = %d, want 1", len(runStore.steps))
	}
	if runStore.steps[0].ToolID != testToolID {
		t.Fatalf("saved step ToolID = %q, want %s", runStore.steps[0].ToolID, testToolID)
	}
}

func TestServiceRunFailsOnUnknownToolAfterRetry(t *testing.T) {
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "missing", Inputs: json.RawMessage(`{}`)},
		{Type: ActionTypeCallTool, Tool: "missing", Inputs: json.RawMessage(`{}`)},
	}}
	service := NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{},
		Executor:     &fakeExecutor{},
		RunStore:     runStores(&memoryRunStore{}),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
	})

	response, err := service.Run(context.Background(), CreateRunRequest{Message: "run missing"})

	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if response.Status != RunStatusFailed {
		t.Fatalf("Status = %q, want failed", response.Status)
	}
}

func TestServiceRunBoundsFailedRunPersistence(t *testing.T) {
	const cleanupTimeout = 20 * time.Millisecond

	runStore := newBlockingFinishRunStore()
	service := newServiceWithFailedRunFinishTimeout(runStore, cleanupTimeout)

	done := make(chan runResult, 1)
	go func() {
		response, err := service.Run(context.Background(), CreateRunRequest{Message: "run fails"})
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

func newServiceWithFailedRunFinishTimeout(runStore *blockingFinishRunStore, timeout time.Duration) Service {
	service := NewService(ServiceConfig{
		Planner:      &fakePlanner{},
		Catalog:      fakeCatalog{},
		Executor:     &fakeExecutor{},
		RunStore:     blockingRunStores(runStore),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
	})
	service.failedRunFinishTimeout = timeout
	return service
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
		if result.err == nil {
			t.Fatal("Run() error = nil, want planner error joined with cleanup deadline")
		}
		if !errors.Is(result.err, context.DeadlineExceeded) {
			t.Fatalf("Run() error = %v, want context deadline exceeded", result.err)
		}
		if result.response.Status != RunStatusFailed {
			t.Fatalf("response status = %q, want failed", result.response.Status)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Run() did not return after failed-run cleanup timeout")
	}
}

func TestServiceRunAuditsUnknownToolAttempt(t *testing.T) {
	runStore := &memoryRunStore{}
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "missing", Inputs: json.RawMessage(`{}`)},
		{Type: ActionTypeFinalAnswer, Answer: "cannot continue"},
	}}
	service := NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{},
		Executor:     &fakeExecutor{},
		RunStore:     runStores(runStore),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
	})

	_, err := service.Run(context.Background(), CreateRunRequest{Message: "run missing"})

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runStore.steps) != 1 {
		t.Fatalf("saved steps = %d, want 1", len(runStore.steps))
	}
	if runStore.steps[0].Status != StepStatusFailed {
		t.Fatalf("step status = %q, want failed", runStore.steps[0].Status)
	}
}

func TestServiceRunPassesAttachmentsToPlanner(t *testing.T) {
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeFinalAnswer, Answer: "done"}}}
	service := NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{},
		Executor:     &fakeExecutor{},
		RunStore:     runStores(&memoryRunStore{}),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
	})
	attachments := []Attachment{{
		ID:       "att_pdf",
		Filename: "merchant_catalog.pdf",
		MIMEType: "application/pdf",
		Kind:     AttachmentKindPDF,
		Size:     8,
		Data:     "JVBERi0xLjc=",
	}}

	_, err := service.Run(context.Background(), CreateRunRequest{Message: "update catalog", Attachments: attachments})

	if err != nil {
		t.Fatalf("Run() error = %v", err)
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

func TestServiceRunWaitsForUserOnAskUser(t *testing.T) {
	runStore := &memoryRunStore{}
	planner := &fakePlanner{actions: []PlannerAction{{
		Type:    ActionTypeAskUser,
		Message: "Which account should I use?",
		Payload: json.RawMessage(`{"kind":"choice"}`),
	}}}
	service := newTestService(planner, runStore)

	response, err := service.Run(context.Background(), CreateRunRequest{Message: "delete duplicate"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != RunStatusWaitingForUser {
		t.Fatalf("Status = %q, want waiting_for_user", response.Status)
	}
	if response.Interaction == nil || response.Interaction.Message != "Which account should I use?" {
		t.Fatalf("Interaction = %#v, want planner interaction", response.Interaction)
	}
	if runStore.waitingRun.Status != RunStatusWaitingForUser {
		t.Fatalf("waitingRun.Status = %q, want waiting_for_user", runStore.waitingRun.Status)
	}
}

func TestServiceCreateRunTurnContinuesSameRunWithAttachments(t *testing.T) {
	runStore := newWaitingMemoryRunStore()
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeFinalAnswer, Answer: "continued"}}}
	service := newTestService(planner, runStore)
	attachments := []Attachment{{
		ID:       "att_turn",
		Filename: "accounts.csv",
		MIMEType: "text/csv",
		Kind:     AttachmentKindCSV,
		Size:     7,
		Data:     "YSxiCg==",
	}}

	response, err := service.CreateRunTurn(context.Background(), testRunID, CreateRunTurnRequest{Message: "use this file", Attachments: attachments})
	if err != nil {
		t.Fatalf("CreateRunTurn() error = %v", err)
	}
	if response.RunID != testRunID || response.Status != RunStatusSucceeded {
		t.Fatalf("response = %#v, want same run succeeded", response)
	}
	requirePlannerSawRunTurn(t, planner)
}

func TestServiceCreateRunTurnContinuesWithPriorObservationsAndStepOrder(t *testing.T) {
	runStore := newWaitingMemoryRunStore()
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
	})

	response, err := service.CreateRunTurn(context.Background(), testRunID, CreateRunTurnRequest{Message: "ok"})
	if err != nil {
		t.Fatalf("CreateRunTurn() error = %v", err)
	}
	if response.Status != RunStatusSucceeded {
		t.Fatalf("Status = %q, want succeeded", response.Status)
	}
	if len(planner.requests) == 0 || len(planner.requests[0].Observations) != 1 {
		t.Fatalf("planner observations = %#v, want prior observation", planner.requests)
	}
	if len(runStore.steps) != 1 || runStore.steps[0].StepOrder != 2 {
		t.Fatalf("saved steps = %#v, want resumed step order 2", runStore.steps)
	}
}

func requirePlannerSawRunTurn(t *testing.T, planner *fakePlanner) {
	t.Helper()

	if len(planner.requests) != 1 || planner.requests[0].Message != "use this file" {
		t.Fatalf("planner requests = %#v, want user turn message", planner.requests)
	}
	if planner.requests[0].Interaction == nil || planner.requests[0].Interaction.Message != "Which account should I use?" {
		t.Fatalf("planner interaction = %#v, want pending interaction context", planner.requests[0].Interaction)
	}
	if len(planner.requests[0].Attachments) != 1 {
		t.Fatalf("planner attachments = %d, want 1", len(planner.requests[0].Attachments))
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
	})
}

func newWaitingMemoryRunStore() *memoryRunStore {
	return &memoryRunStore{
		run: Run{ID: testRunID, Message: "delete duplicate", Status: RunStatusWaitingForUser, StartedAt: time.Now()},
		interactions: []Interaction{{
			ID:      "int_test",
			RunID:   testRunID,
			Type:    InteractionTypeUserInput,
			Status:  InteractionStatusPending,
			Message: "Which account should I use?",
		}},
	}
}

func TestServiceRunFailsAtStepLimit(t *testing.T) {
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
		RunStore:     runStores(&memoryRunStore{}),
		MaxSteps:     1,
		TotalTimeout: time.Minute,
	})

	response, err := service.Run(context.Background(), CreateRunRequest{Message: "too many"})

	if err == nil {
		t.Fatal("Run() error = nil, want step limit error")
	}
	if response.Status != RunStatusFailed {
		t.Fatalf("Status = %q, want failed", response.Status)
	}
}

func TestServiceRunAllowsOneBusinessErrorFollowUp(t *testing.T) {
	planner := &fakePlanner{actions: []PlannerAction{
		{Type: ActionTypeCallTool, Tool: "find_partner", Inputs: json.RawMessage(`{"partner_name":"missing"}`)},
		{Type: ActionTypeFinalAnswer, Answer: "partner not found"},
	}}
	executor := &fakeExecutor{observations: []Observation{{Status: StepStatusFailed, Error: "partner_not_found"}}}
	runStore := &memoryRunStore{}
	service := NewService(ServiceConfig{
		Planner:      planner,
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{ID: testToolID, Name: "find_partner", TimeoutMS: 1000}}},
		Executor:     executor,
		RunStore:     runStores(runStore),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
	})

	response, err := service.Run(context.Background(), CreateRunRequest{Message: "export missing partner"})

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Answer != "partner not found" {
		t.Fatalf("Answer = %q, want partner not found", response.Answer)
	}
	if len(runStore.steps) != 1 || runStore.steps[0].Status != StepStatusFailed {
		t.Fatalf("saved steps = %#v, want one failed step", runStore.steps)
	}
}

func TestServiceRunFailsRepeatedBusinessError(t *testing.T) {
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
		RunStore:     runStores(&memoryRunStore{}),
		MaxSteps:     8,
		TotalTimeout: time.Minute,
	})

	response, err := service.Run(context.Background(), CreateRunRequest{Message: "export missing partner"})

	if err == nil {
		t.Fatal("Run() error = nil, want repeated business error")
	}
	if response.Status != RunStatusFailed {
		t.Fatalf("Status = %q, want failed", response.Status)
	}
}
