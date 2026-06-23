package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"ai/backend/internal/toolcatalog"
)

var (
	errNoMoreActions = errors.New("no more actions")
	errNoObservation = errors.New("no observation")
)

type fakePlanner struct {
	actions []PlannerAction
	index   int
}

func (planner *fakePlanner) NextAction(_ context.Context, _ PlanRequest) (PlannerAction, error) {
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
	steps []StepRecord
}

func (store *memoryRunStore) StartRun(_ context.Context, message string) (Run, error) {
	return Run{ID: testRunID, Message: message, Status: RunStatusRunning, StartedAt: time.Now()}, nil
}

func (store *memoryRunStore) FinishRun(_ context.Context, _ Run) error {
	return nil
}

func (store *memoryRunStore) SaveStep(_ context.Context, step StepRecord) error {
	store.steps = append(store.steps, step)
	return nil
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
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{Name: "export_report", TimeoutMS: 1000}}, instructions: toolcatalog.Instructions{Content: "Use tools."}},
		Executor:     executor,
		RunStore:     runStore,
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
		RunStore:     &memoryRunStore{},
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
		RunStore:     runStore,
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
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{Name: "export_report", TimeoutMS: 1000}}},
		Executor:     executor,
		RunStore:     &memoryRunStore{},
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
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{Name: "find_partner", TimeoutMS: 1000}}},
		Executor:     executor,
		RunStore:     runStore,
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
		Catalog:      fakeCatalog{tools: []toolcatalog.Tool{{Name: "find_partner", TimeoutMS: 1000}}},
		Executor:     executor,
		RunStore:     &memoryRunStore{},
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
