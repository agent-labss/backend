# Agent Run Turns Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Add same-`run_id` user turns so an agent run can enter `waiting_for_user`, accept a natural-language follow-up with optional attachments, and continue.

**Architecture:** Keep ownership inside existing packages. `internal/agent` owns run statuses, planner actions, interactions, user turns, repository persistence, and handlers. `internal/httpapi` only registers the new Fiber routes. `internal/database` only gains GORM models for interactions, turns, turn attachments, and persisted observations.

**Tech Stack:** Go, Fiber v3, GORM, SQLite, existing OpenAI Responses planner.

---

### Task 1: Planner Contract And Response Types

**Files:**
- Modify: `internal/agent/types.go`
- Modify: `internal/agent/llm.go`
- Test: `internal/agent/llm_test.go`

- [x] **Step 1: Write failing planner tests**

Add tests proving `ask_user` is accepted, requires a message, and appears in prompt `allowed_actions`.

```go
func TestParsePlannerActionAcceptsAskUser(t *testing.T) {
	action, err := ParsePlannerAction([]byte(`{"type":"ask_user","message":"Which account should I use?","payload":{"kind":"choice"}}`))
	if err != nil {
		t.Fatalf("ParsePlannerAction() error = %v", err)
	}
	if action.Type != ActionTypeAskUser {
		t.Fatalf("action.Type = %q, want %q", action.Type, ActionTypeAskUser)
	}
	if action.Message != "Which account should I use?" {
		t.Fatalf("Message = %q, want planner question", action.Message)
	}
}

func TestParsePlannerActionRejectsAskUserWithoutMessage(t *testing.T) {
	_, err := ParsePlannerAction([]byte(`{"type":"ask_user"}`))
	if err == nil {
		t.Fatal("ParsePlannerAction() error = nil, want error")
	}
	if !errors.Is(err, ErrInvalidPlannerAction) {
		t.Fatalf("ParsePlannerAction() error = %v, want ErrInvalidPlannerAction", err)
	}
}

func TestBuildPlannerPromptAllowsAskUser(t *testing.T) {
	prompt, err := buildPlannerPrompt(PlanRequest{Message: "delete duplicate account"})
	if err != nil {
		t.Fatalf("buildPlannerPrompt() error = %v", err)
	}
	if !strings.Contains(string(prompt), string(ActionTypeAskUser)) {
		t.Fatalf("prompt = %s, want ask_user in allowed actions", prompt)
	}
}
```

- [x] **Step 2: Run red test**

Run: `go test ./internal/agent -run 'Test(ParsePlannerActionAcceptsAskUser|ParsePlannerActionRejectsAskUserWithoutMessage|BuildPlannerPromptAllowsAskUser)'`

Expected: FAIL because `ActionTypeAskUser` and `Message` do not exist yet.

- [x] **Step 3: Implement minimal planner contract**

Add:

```go
RunStatusWaitingForUser RunStatus = "waiting_for_user"
ActionTypeAskUser       ActionType = "ask_user"
```

Extend `PlannerAction` with:

```go
Message string          `json:"message,omitempty"`
Payload json.RawMessage `json:"payload,omitempty"`
```

Extend `RunResponse` with:

```go
Interaction *Interaction `json:"interaction,omitempty"`
```

Add `Interaction`, `CreateRunTurnRequest`, and `GetRun` response types in `types.go`.

Update `buildPlannerPrompt` to include `ask_user`. Update `validatePlannerAction` with `validateAskUserAction` requiring a nonblank message and allowing empty or object payload.

- [x] **Step 4: Run green test**

Run: `go test ./internal/agent -run 'Test(ParsePlannerActionAcceptsAskUser|ParsePlannerActionRejectsAskUserWithoutMessage|BuildPlannerPromptAllowsAskUser)'`

Expected: PASS.

### Task 2: Repository Persistence

**Files:**
- Modify: `internal/database/models.go`
- Modify: `internal/agent/repository.go`
- Test: `internal/agent/repository_test.go`

- [x] **Step 1: Write failing repository tests**

Add tests for creating a waiting interaction, loading the run with the pending interaction, creating a user turn with an attachment, marking the interaction responded, and saving/loading observations.

```go
func TestRepositoryPersistsWaitingInteractionAndRunStatus(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))
	run, err := repository.StartRun(context.Background(), CreateRunRecord{Message: "delete duplicate account"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	interaction := Interaction{
		RunID:   run.ID,
		Type:    InteractionTypeUserInput,
		Status:  InteractionStatusPending,
		Message: "Delete the duplicate account?",
		Payload: json.RawMessage(`{"risk":"destructive"}`),
	}

	saved, err := repository.CreateInteraction(context.Background(), interaction)
	if err != nil {
		t.Fatalf("CreateInteraction() error = %v", err)
	}
	run.Status = RunStatusWaitingForUser
	if err := repository.MarkRunWaiting(context.Background(), run, saved); err != nil {
		t.Fatalf("MarkRunWaiting() error = %v", err)
	}

	loaded, err := repository.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if loaded.Status != RunStatusWaitingForUser {
		t.Fatalf("Status = %q, want waiting_for_user", loaded.Status)
	}
	if loaded.Interaction == nil || loaded.Interaction.Message != "Delete the duplicate account?" {
		t.Fatalf("Interaction = %#v, want pending interaction", loaded.Interaction)
	}
}

func TestRepositoryPersistsUserTurnAttachmentsAndRespondsInteraction(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))
	run, err := repository.StartRun(context.Background(), CreateRunRecord{Message: "update catalog"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	interaction, err := repository.CreateInteraction(context.Background(), Interaction{
		RunID: run.ID, Type: InteractionTypeUserInput, Status: InteractionStatusPending, Message: "Upload the catalog file.",
	})
	if err != nil {
		t.Fatalf("CreateInteraction() error = %v", err)
	}
	if err := repository.MarkRunWaiting(context.Background(), run, interaction); err != nil {
		t.Fatalf("MarkRunWaiting() error = %v", err)
	}

	turn, err := repository.CreateRunTurn(context.Background(), CreateRunTurnRecord{
		RunID: run.ID,
		Message: "Use this file.",
		Attachments: []Attachment{{ID: "att_turn", Filename: "accounts.csv", MIMEType: "text/csv", Kind: AttachmentKindCSV, Size: 7, Data: "ignored"}},
	})
	if err != nil {
		t.Fatalf("CreateRunTurn() error = %v", err)
	}
	if err := repository.MarkInteractionResponded(context.Background(), interaction.ID, turn.ID); err != nil {
		t.Fatalf("MarkInteractionResponded() error = %v", err)
	}

	loaded, err := repository.GetRunState(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRunState() error = %v", err)
	}
	if len(loaded.Turns) != 1 || loaded.Turns[0].Message != "Use this file." {
		t.Fatalf("Turns = %#v, want saved turn", loaded.Turns)
	}
	if len(loaded.Turns[0].Attachments) != 1 || loaded.Turns[0].Attachments[0].Filename != "accounts.csv" {
		t.Fatalf("Turn attachments = %#v, want accounts.csv", loaded.Turns[0].Attachments)
	}
}
```

- [x] **Step 2: Run red test**

Run: `go test ./internal/agent -run 'TestRepositoryPersistsWaitingInteractionAndRunStatus|TestRepositoryPersistsUserTurnAttachmentsAndRespondsInteraction'`

Expected: FAIL because repository methods and database models do not exist yet.

- [x] **Step 3: Implement minimal persistence**

Add GORM models:

```go
AgentRunInteraction
AgentRunTurn
AgentRunTurnAttachment
AgentRunObservation
```

Add repository methods:

```go
GetRun(ctx, id)
GetRunState(ctx, id)
CreateInteraction(ctx, interaction)
MarkRunWaiting(ctx, run, interaction)
CreateRunTurn(ctx, record)
MarkInteractionResponded(ctx, interactionID, turnID)
SaveObservation(ctx, record)
```

Use existing redaction helpers. Persist attachment metadata only, not raw `Data`.

- [x] **Step 4: Run green test**

Run: `go test ./internal/agent -run 'TestRepositoryPersistsWaitingInteractionAndRunStatus|TestRepositoryPersistsUserTurnAttachmentsAndRespondsInteraction'`

Expected: PASS.

### Task 3: Service Waiting And Turn Continuation

**Files:**
- Modify: `internal/agent/service.go`
- Test: `internal/agent/service_test.go`

- [x] **Step 1: Write failing service tests**

Add tests proving `ask_user` returns `waiting_for_user`, creates an interaction, and `CreateRunTurn` continues the same run with the user turn message and attachments.

```go
func TestServiceRunWaitsForUserOnAskUser(t *testing.T) {
	runStore := &memoryRunStore{}
	planner := &fakePlanner{actions: []PlannerAction{{
		Type: ActionTypeAskUser, Message: "Which account should I use?", Payload: json.RawMessage(`{"kind":"choice"}`),
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
	attachments := []Attachment{{ID: "att_turn", Filename: "accounts.csv", MIMEType: "text/csv", Kind: AttachmentKindCSV, Size: 7, Data: "YSxiCg=="}}

	response, err := service.CreateRunTurn(context.Background(), testRunID, CreateRunTurnRequest{Message: "use this file", Attachments: attachments})
	if err != nil {
		t.Fatalf("CreateRunTurn() error = %v", err)
	}
	if response.RunID != testRunID || response.Status != RunStatusSucceeded {
		t.Fatalf("response = %#v, want same run succeeded", response)
	}
	if len(planner.requests) != 1 || planner.requests[0].Message != "use this file" {
		t.Fatalf("planner requests = %#v, want user turn message", planner.requests)
	}
	if len(planner.requests[0].Attachments) != 1 {
		t.Fatalf("planner attachments = %d, want 1", len(planner.requests[0].Attachments))
	}
}
```

- [x] **Step 2: Run red test**

Run: `go test ./internal/agent -run 'TestServiceRunWaitsForUserOnAskUser|TestServiceCreateRunTurnContinuesSameRunWithAttachments'`

Expected: FAIL because service waiting and turn continuation do not exist yet.

- [x] **Step 3: Implement minimal service flow**

Extend `RunStore` with the repository methods from Task 2. Implement:

```go
func (service Service) GetRun(ctx context.Context, runID string) (RunResponse, error)
func (service Service) CreateRunTurn(ctx context.Context, runID string, request CreateRunTurnRequest) (RunResponse, error)
```

Handle `ActionTypeAskUser` in `runStep` by creating an interaction, marking the run waiting, and returning a waiting response. For user turns, require loaded run status `waiting_for_user`, persist the turn, mark the pending interaction responded, then run the loop with the turn message and attachments.

- [x] **Step 4: Run green test**

Run: `go test ./internal/agent -run 'TestServiceRunWaitsForUserOnAskUser|TestServiceCreateRunTurnContinuesSameRunWithAttachments'`

Expected: PASS.

### Task 4: HTTP Handler And Fiber Routes

**Files:**
- Modify: `internal/agent/handler.go`
- Modify: `internal/httpapi/router.go`
- Test: `internal/agent/handler_test.go`
- Test: `internal/httpapi/router_test.go`

- [x] **Step 1: Write failing HTTP tests**

Add tests for `GET /api/agent/runs/:run_id`, JSON turn creation, multipart turn creation, blank turn rejection, and route registration.

```go
func TestHandlerCreateRunTurnAcceptsMultipartFiles(t *testing.T) {
	service := &fakeRunService{response: RunResponse{RunID: testRunID, Status: RunStatusSucceeded, Answer: "continued"}}
	resp := performMultipartCreateRunTurn(t, service, testRunID, "用这个文件继续", "accounts.csv", "text/csv", []byte("a,b\n"))
	defer closeAgentResponseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if service.turnRunID != testRunID || service.turnRequest.Message != "用这个文件继续" {
		t.Fatalf("turn = %s %#v, want same run message", service.turnRunID, service.turnRequest)
	}
	if len(service.turnRequest.Attachments) != 1 || service.turnRequest.Attachments[0].Kind != AttachmentKindCSV {
		t.Fatalf("attachments = %#v, want csv attachment", service.turnRequest.Attachments)
	}
}
```

- [x] **Step 2: Run red test**

Run: `go test ./internal/agent ./internal/httpapi -run 'TestHandler(CreateRunTurn|GetRun)|TestAgentRun(Get|Turn)RouteIsRegistered'`

Expected: FAIL because methods and routes do not exist yet.

- [x] **Step 3: Implement handler and router**

Extend `Runner` with:

```go
GetRun(ctx context.Context, runID string) (RunResponse, error)
CreateRunTurn(ctx context.Context, runID string, request CreateRunTurnRequest) (RunResponse, error)
```

Add `Handler.GetRun` and `Handler.CreateRunTurn`. Reuse JSON and multipart attachment normalization by extracting shared body parsing into a turn-compatible helper. Allow a turn with either a nonblank message or at least one attachment.

Extend `httpapi.AgentHandler` and register:

```go
app.Get(agent.AgentRunPath, config.AgentHandler.GetRun)
app.Post(agent.AgentRunTurnsPath, config.AgentHandler.CreateRunTurn)
```

- [x] **Step 4: Run green test**

Run: `go test ./internal/agent ./internal/httpapi -run 'TestHandler(CreateRunTurn|GetRun)|TestAgentRun(Get|Turn)RouteIsRegistered'`

Expected: PASS.

### Task 5: Full Verification And Commit

**Files:**
- All changed files

- [x] **Step 1: Run focused package tests**

Run: `go test ./internal/agent ./internal/httpapi ./internal/database ./internal/app`

Expected: PASS.

- [x] **Step 2: Run repository guard**

Run: `./scripts/repo-guard.sh`

Expected: `repo guard passed`.

- [x] **Step 3: Review diff**

Run: `git diff --check && git diff --stat`

Expected: no whitespace errors; diff only touches the planned files.

- [x] **Step 4: Commit**

Run:

```bash
git add internal/agent internal/httpapi internal/database internal/app docs/superpowers/plans/2026-06-24-agent-run-turns.md
git commit -m "Add agent run user turns"
```

## Execution Notes

Implemented in this session. The final implementation also passes the pending interaction into the planner prompt on user turns and restores persisted observations so resumed tool calls continue at the next step order.
