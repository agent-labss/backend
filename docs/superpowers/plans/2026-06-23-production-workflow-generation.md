# Production Workflow Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first vertical slice for production API workflow generation: submit an exploration prompt, run an external Playwright/LLM worker, persist the generated workflow, automatically publish it, and invoke it by explicit workflow ID.

**Architecture:** The Go backend owns durable workflow storage, execution, HTTP APIs, and audit. Playwright and LLM exploration run outside the Go process behind a JSON stdin/stdout command boundary, so the backend adds no new direct Go dependencies. The first slice excludes natural-language workflow matching and uses explicit workflow IDs for invocation.

**Tech Stack:** Go 1.25, Fiber v3, pgx v5, PostgreSQL, standard-library `net/http`, standard-library `os/exec`, JSON templates stored as `json.RawMessage`.

---

## Scope And Boundary Decisions

This implementation needs new package boundaries that were approved in the design:

- `internal/workflow`: workflow definitions, steps, validation, execution, repository, invocation audit.
- `internal/exploration`: exploration run lifecycle, trace redaction, external runner protocol, workflow publication handoff.

No new direct Go dependency is added. The Playwright/LLM worker is an external command configured by environment variable and invoked through `os/exec`. That worker can be implemented in Node, Python, Codex, Claude Code, or another runtime without changing this Go module.

The first slice uses explicit workflow invocation:

- `POST /api/exploration-runs`: submit prompt, target URL, username, password; backend invokes runner and auto-publishes returned workflow.
- `GET /api/workflows/:workflow_id`: inspect published workflow metadata.
- `POST /api/workflows/:workflow_id/invocations`: execute workflow with structured JSON inputs.

## File Structure

- Modify `scripts/repo-guard.sh`: allow `internal/workflow` and `internal/exploration`.
- Modify `internal/architecture/architecture_test.go`: allow and protect new package boundaries.
- Modify `internal/config/config.go`: read `EXPLORATION_RUNNER_COMMAND`.
- Modify `internal/config/config_test.go`: cover runner command default and override.
- Create `internal/workflow/types.go`: typed constants, workflow/step/invocation structs, validation helpers.
- Create `internal/workflow/types_test.go`: validation and status behavior.
- Create `internal/workflow/template.go`: `{{inputs.name}}` and `{{steps.step.value}}` interpolation.
- Create `internal/workflow/template_test.go`: template resolution tests.
- Create `internal/workflow/extract.go`: small JSON extractor for paths such as `$.data[0].id`.
- Create `internal/workflow/extract_test.go`: extractor tests.
- Create `internal/workflow/executor.go`: deterministic HTTP workflow executor.
- Create `internal/workflow/executor_test.go`: `httptest.Server` execution and failure tests.
- Create `internal/workflow/repository.go`: PostgreSQL schema creation and persistence methods.
- Create `internal/workflow/repository_test.go`: repository nil-pool and SQL shape tests that do not require a live database.
- Create `internal/workflow/handler.go`: Fiber handlers for get workflow and invoke workflow.
- Create `internal/workflow/handler_test.go`: handler tests with an in-memory service.
- Create `internal/exploration/types.go`: exploration request/result and API trace structs.
- Create `internal/exploration/redact.go`: sensitive key redaction.
- Create `internal/exploration/redact_test.go`: redaction tests.
- Create `internal/exploration/repository.go`: PostgreSQL persistence for exploration runs and API traces.
- Create `internal/exploration/repository_test.go`: schema and nil-pool tests.
- Create `internal/exploration/runner.go`: external command runner protocol.
- Create `internal/exploration/runner_test.go`: command success, invalid JSON, timeout tests.
- Create `internal/exploration/service.go`: run exploration, redact traces, publish returned workflow.
- Create `internal/exploration/service_test.go`: service behavior with fake runner and in-memory publisher.
- Create `internal/exploration/handler.go`: Fiber handler for exploration submission.
- Create `internal/exploration/handler_test.go`: HTTP request validation and secret non-echo tests.
- Modify `internal/httpapi/router.go`: register exploration and workflow routes.
- Modify `internal/httpapi/router_test.go`: route and CORS tests.
- Modify `internal/httpapi/middleware.go`: allow POST in CORS methods.
- Modify `internal/app/app.go`: wire repositories, services, handlers, and runner command.
- Modify `internal/app/app_test.go`: cover missing runner command wiring where feasible.

## Task 1: Open Architecture Guardrails For Approved Packages

**Files:**
- Modify: `scripts/repo-guard.sh`
- Modify: `internal/architecture/architecture_test.go`

- [ ] **Step 1: Update the architecture test allowlist**

In `internal/architecture/architecture_test.go`, add these entries to `allowedPackages`:

```go
modulePath + "/internal/exploration",
modulePath + "/internal/workflow",
```

Add package boundary assertions in `TestPackageBoundaries`:

```go
assertDoesNotImport(t, packages, modulePath+"/internal/workflow", modulePath+"/internal/httpapi")
assertDoesNotImport(t, packages, modulePath+"/internal/workflow", modulePath+"/internal/exploration")
assertDoesNotImport(t, packages, modulePath+"/internal/exploration", modulePath+"/internal/httpapi")
assertDoesNotImport(t, packages, modulePath+"/internal/platform/postgres", modulePath+"/internal/workflow")
assertDoesNotImport(t, packages, modulePath+"/internal/platform/postgres", modulePath+"/internal/exploration")
```

Rationale: `exploration` may import `workflow` to publish generated workflows, but `workflow` must not import `exploration`.

- [ ] **Step 2: Update repo guard package allowlist**

In `scripts/repo-guard.sh`, add these lines to `allowed_packages`:

```bash
  'orderbuddy-ai/backend/internal/exploration' \
  'orderbuddy-ai/backend/internal/workflow' \
```

- [ ] **Step 3: Run architecture tests**

Run:

```bash
go test ./internal/architecture
```

Expected: `ok orderbuddy-ai/backend/internal/architecture`.

- [ ] **Step 4: Commit**

```bash
git add scripts/repo-guard.sh internal/architecture/architecture_test.go
git commit -m "Allow workflow generation packages"
```

## Task 2: Add Runner Configuration

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write failing config tests**

Add tests to `internal/config/config_test.go`:

```go
func TestLoadUsesDefaultExplorationRunnerCommand(t *testing.T) {
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("EXPLORATION_RUNNER_COMMAND", "")

	cfg := Load()

	if cfg.ExplorationRunnerCommand != "" {
		t.Fatalf("ExplorationRunnerCommand = %q, want empty", cfg.ExplorationRunnerCommand)
	}
}

func TestLoadUsesExplorationRunnerCommandOverride(t *testing.T) {
	t.Setenv("EXPLORATION_RUNNER_COMMAND", "node ./workers/explore.mjs")

	cfg := Load()

	if cfg.ExplorationRunnerCommand != "node ./workers/explore.mjs" {
		t.Fatalf("ExplorationRunnerCommand = %q, want override", cfg.ExplorationRunnerCommand)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
go test ./internal/config
```

Expected: fail with `cfg.ExplorationRunnerCommand undefined`.

- [ ] **Step 3: Add config field and loader**

In `internal/config/config.go`, add:

```go
const explorationRunnerCommandEnv = "EXPLORATION_RUNNER_COMMAND"

type Config struct {
	HTTPAddr                 string
	AppEnv                   string
	DatabaseURL              string
	ExplorationRunnerCommand string
}
```

In `Load()`, populate the field:

```go
return Config{
	HTTPAddr:                 getEnv(httpAddrEnv, defaultHTTPAddr),
	AppEnv:                   getEnv(appEnvEnv, defaultAppEnv),
	DatabaseURL:              getEnv(databaseURLEnv, defaultDatabaseURL),
	ExplorationRunnerCommand: getEnv(explorationRunnerCommandEnv, ""),
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/config
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "Add exploration runner configuration"
```

## Task 3: Define Workflow Domain Types And Validation

**Files:**
- Create: `internal/workflow/types.go`
- Create: `internal/workflow/types_test.go`

- [ ] **Step 1: Write failing validation tests**

Create `internal/workflow/types_test.go`:

```go
package workflow

import (
	"encoding/json"
	"testing"
)

func TestDefinitionValidateRequiresName(t *testing.T) {
	definition := Definition{
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Steps: []Step{{
			Name:       "call_api",
			Method:     "GET",
			URLTemplate: "https://internal.example.test/api",
			Success:    SuccessCondition{Statuses: []int{200}},
		}},
	}

	if err := definition.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestDefinitionValidateAcceptsPublishedWorkflow(t *testing.T) {
	definition := Definition{
		Name:        "export_report",
		Description: "Export a report.",
		Status:      WorkflowStatusPublished,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Steps: []Step{{
			Name:       "call_api",
			Method:     "POST",
			URLTemplate: "https://internal.example.test/api/reports",
			BodyTemplate: json.RawMessage(`{"month":"{{inputs.month}}"}`),
			Success:    SuccessCondition{Statuses: []int{200, 201}},
		}},
	}

	if err := definition.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestDefinitionValidateRejectsInvalidStepMethod(t *testing.T) {
	definition := Definition{
		Name:        "bad_method",
		Status:      WorkflowStatusPublished,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Steps: []Step{{
			Name:       "call_api",
			Method:     "TRACE",
			URLTemplate: "https://internal.example.test/api",
			Success:    SuccessCondition{Statuses: []int{200}},
		}},
	}

	if err := definition.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}
```

- [ ] **Step 2: Run test to verify package missing**

Run:

```bash
go test ./internal/workflow
```

Expected: fail because `internal/workflow` has no implementation.

- [ ] **Step 3: Create workflow types**

Create `internal/workflow/types.go` with:

```go
package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	WorkflowStatusPublished WorkflowStatus = "published"
	WorkflowStatusDisabled  WorkflowStatus = "disabled"

	InvocationStatusSucceeded InvocationStatus = "succeeded"
	InvocationStatusFailed    InvocationStatus = "failed"
)

var (
	ErrDefinitionInvalid = errors.New("workflow definition is invalid")
	ErrWorkflowMissing   = errors.New("workflow is missing")
)

type WorkflowStatus string
type InvocationStatus string

type Definition struct {
	ID                  int64           `json:"id"`
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	Version             int             `json:"version"`
	Status              WorkflowStatus  `json:"status"`
	SourceRunID         int64           `json:"source_run_id"`
	RiskLevel           string          `json:"risk_level"`
	InputSchema         json.RawMessage `json:"input_schema"`
	OutputSchema        json.RawMessage `json:"output_schema"`
	ParameterProvenance json.RawMessage `json:"parameter_provenance"`
	Steps               []Step          `json:"steps"`
	CreatedBy           string          `json:"created_by"`
	PublishedAt         time.Time       `json:"published_at"`
}

type Step struct {
	ID              int64             `json:"id"`
	WorkflowID      int64             `json:"workflow_id"`
	Order           int               `json:"order"`
	Name            string            `json:"name"`
	Method          string            `json:"method"`
	URLTemplate     string            `json:"url_template"`
	HeadersTemplate map[string]string `json:"headers_template"`
	BodyTemplate    json.RawMessage   `json:"body_template"`
	TimeoutMS       int               `json:"timeout_ms"`
	RetryPolicy     json.RawMessage   `json:"retry_policy"`
	Success         SuccessCondition  `json:"success"`
	Extractors      map[string]string `json:"extractors"`
}

type SuccessCondition struct {
	Statuses []int `json:"statuses"`
}

type Invocation struct {
	ID              int64            `json:"id"`
	WorkflowID      int64            `json:"workflow_id"`
	WorkflowVersion int              `json:"workflow_version"`
	InvokedBy       string           `json:"invoked_by"`
	Inputs          map[string]any   `json:"inputs"`
	Status          InvocationStatus `json:"status"`
	StartedAt       time.Time        `json:"started_at"`
	FinishedAt      time.Time        `json:"finished_at"`
	Output          map[string]any   `json:"output"`
	Error           string           `json:"error,omitempty"`
	Steps           []InvocationStep `json:"steps"`
}

type InvocationStep struct {
	Order          int    `json:"order"`
	RequestSummary string `json:"request_summary"`
	ResponseStatus int    `json:"response_status"`
	ResponseSummary string `json:"response_summary"`
	DurationMS      int64  `json:"duration_ms"`
	Error           string `json:"error,omitempty"`
}

func (definition Definition) Validate() error {
	if strings.TrimSpace(definition.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrDefinitionInvalid)
	}
	if definition.Status == "" {
		return fmt.Errorf("%w: status is required", ErrDefinitionInvalid)
	}
	if !json.Valid(definition.InputSchema) {
		return fmt.Errorf("%w: input schema must be valid json", ErrDefinitionInvalid)
	}
	if len(definition.Steps) == 0 {
		return fmt.Errorf("%w: at least one step is required", ErrDefinitionInvalid)
	}
	for index, step := range definition.Steps {
		if err := step.Validate(); err != nil {
			return fmt.Errorf("%w: step %d: %w", ErrDefinitionInvalid, index+1, err)
		}
	}
	return nil
}

func (step Step) Validate() error {
	if strings.TrimSpace(step.Name) == "" {
		return errors.New("name is required")
	}
	if !slices.Contains([]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}, step.Method) {
		return fmt.Errorf("method %q is not allowed", step.Method)
	}
	if strings.TrimSpace(step.URLTemplate) == "" {
		return errors.New("url template is required")
	}
	if len(step.Success.Statuses) == 0 {
		return errors.New("success statuses are required")
	}
	if len(step.BodyTemplate) > 0 && !json.Valid(step.BodyTemplate) {
		return errors.New("body template must be valid json")
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/workflow/types.go internal/workflow/types_test.go
go test ./internal/workflow
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/workflow/types.go internal/workflow/types_test.go
git commit -m "Add workflow domain types"
```

## Task 4: Add Workflow Template Resolution

**Files:**
- Create: `internal/workflow/template.go`
- Create: `internal/workflow/template_test.go`

- [ ] **Step 1: Write failing template tests**

Create `internal/workflow/template_test.go`:

```go
package workflow

import "testing"

func TestResolveTemplateUsesInputsAndStepValues(t *testing.T) {
	values := executionValues{
		inputs: map[string]any{
			"month": "2026-05",
		},
		steps: map[string]map[string]any{
			"find_partner": {
				"partner_id": "partner-123",
			},
		},
	}

	got, err := resolveTemplate("partner={{steps.find_partner.partner_id}}&month={{inputs.month}}", values)
	if err != nil {
		t.Fatalf("resolveTemplate() error = %v", err)
	}

	want := "partner=partner-123&month=2026-05"
	if got != want {
		t.Fatalf("resolveTemplate() = %q, want %q", got, want)
	}
}

func TestResolveTemplateRejectsMissingValue(t *testing.T) {
	_, err := resolveTemplate("{{inputs.missing}}", executionValues{inputs: map[string]any{}})
	if err == nil {
		t.Fatal("resolveTemplate() error = nil, want error")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/workflow
```

Expected: fail with `undefined: executionValues`.

- [ ] **Step 3: Implement resolver**

Create `internal/workflow/template.go`:

```go
package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

var templatePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_\.]+)\s*\}\}`)

type executionValues struct {
	inputs map[string]any
	steps  map[string]map[string]any
}

func resolveTemplate(template string, values executionValues) (string, error) {
	var resolveErr error
	result := templatePattern.ReplaceAllStringFunc(template, func(match string) string {
		if resolveErr != nil {
			return match
		}
		parts := templatePattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			resolveErr = fmt.Errorf("invalid template expression %q", match)
			return match
		}
		value, err := values.lookup(parts[1])
		if err != nil {
			resolveErr = err
			return match
		}
		return fmt.Sprint(value)
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return result, nil
}

func (values executionValues) lookup(path string) (any, error) {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("template path %q is invalid", path)
	}
	switch parts[0] {
	case "inputs":
		value, ok := values.inputs[parts[1]]
		if !ok {
			return nil, fmt.Errorf("input %q is missing", parts[1])
		}
		return value, nil
	case "steps":
		if len(parts) != 3 {
			return nil, fmt.Errorf("step path %q is invalid", path)
		}
		stepValues, ok := values.steps[parts[1]]
		if !ok {
			return nil, fmt.Errorf("step %q is missing", parts[1])
		}
		value, ok := stepValues[parts[2]]
		if !ok {
			return nil, fmt.Errorf("step value %q.%q is missing", parts[1], parts[2])
		}
		return value, nil
	default:
		return nil, fmt.Errorf("template root %q is invalid", parts[0])
	}
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/workflow/template.go internal/workflow/template_test.go
go test ./internal/workflow
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/workflow/template.go internal/workflow/template_test.go
git commit -m "Add workflow template resolution"
```

## Task 5: Add JSON Extractors

**Files:**
- Create: `internal/workflow/extract.go`
- Create: `internal/workflow/extract_test.go`

- [ ] **Step 1: Write failing extractor tests**

Create `internal/workflow/extract_test.go`:

```go
package workflow

import "testing"

func TestExtractJSONPathReturnsNestedArrayValue(t *testing.T) {
	body := []byte(`{"data":[{"id":"partner-123","name":"Acme"}]}`)

	got, err := extractJSONPath(body, "$.data[0].id")
	if err != nil {
		t.Fatalf("extractJSONPath() error = %v", err)
	}

	if got != "partner-123" {
		t.Fatalf("extractJSONPath() = %#v, want partner-123", got)
	}
}

func TestExtractJSONPathRejectsMissingField(t *testing.T) {
	body := []byte(`{"data":[]}`)

	_, err := extractJSONPath(body, "$.data[0].id")
	if err == nil {
		t.Fatal("extractJSONPath() error = nil, want error")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/workflow
```

Expected: fail with `undefined: extractJSONPath`.

- [ ] **Step 3: Implement extractor**

Create `internal/workflow/extract.go`:

```go
package workflow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func extractJSONPath(body []byte, path string) (any, error) {
	if !strings.HasPrefix(path, "$.") {
		return nil, fmt.Errorf("json path %q must start with $.", path)
	}
	var current any
	if err := json.Unmarshal(body, &current); err != nil {
		return nil, fmt.Errorf("decode json response: %w", err)
	}
	tokens := strings.Split(strings.TrimPrefix(path, "$."), ".")
	for _, token := range tokens {
		var err error
		current, err = descendJSON(current, token)
		if err != nil {
			return nil, err
		}
	}
	return current, nil
}

func descendJSON(current any, token string) (any, error) {
	field := token
	index := -1
	if left := strings.Index(token, "["); left >= 0 {
		right := strings.Index(token, "]")
		if right <= left {
			return nil, fmt.Errorf("json path token %q has invalid index", token)
		}
		field = token[:left]
		parsed, err := strconv.Atoi(token[left+1 : right])
		if err != nil {
			return nil, fmt.Errorf("json path token %q has invalid index: %w", token, err)
		}
		index = parsed
	}
	if field != "" {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("json path field %q expected object", field)
		}
		var exists bool
		current, exists = object[field]
		if !exists {
			return nil, fmt.Errorf("json path field %q is missing", field)
		}
	}
	if index >= 0 {
		array, ok := current.([]any)
		if !ok {
			return nil, fmt.Errorf("json path index %d expected array", index)
		}
		if index >= len(array) {
			return nil, fmt.Errorf("json path index %d is out of range", index)
		}
		current = array[index]
	}
	return current, nil
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/workflow/extract.go internal/workflow/extract_test.go
go test ./internal/workflow
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/workflow/extract.go internal/workflow/extract_test.go
git commit -m "Add workflow response extractors"
```

## Task 6: Add Deterministic HTTP Workflow Executor

**Files:**
- Create: `internal/workflow/executor.go`
- Create: `internal/workflow/executor_test.go`

- [ ] **Step 1: Write failing executor tests**

Create `internal/workflow/executor_test.go`:

```go
package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExecutorRunsStepsAndExtractsOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/partners":
			if r.URL.Query().Get("keyword") != "Acme" {
				t.Fatalf("keyword = %q, want Acme", r.URL.Query().Get("keyword"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"partner-123"}]}`))
		case "/reports":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"downloadUrl":"https://files.example.test/report.csv"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	definition := Definition{
		ID:          7,
		Name:        "export_report",
		Version:     1,
		Status:      WorkflowStatusPublished,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Steps: []Step{
			{
				Name:       "find_partner",
				Method:     http.MethodGet,
				URLTemplate: server.URL + "/partners?keyword={{inputs.partner_name}}",
				Success:    SuccessCondition{Statuses: []int{200}},
				Extractors: map[string]string{"partner_id": "$.data[0].id"},
			},
			{
				Name:         "export_report",
				Method:       http.MethodPost,
				URLTemplate:  server.URL + "/reports",
				BodyTemplate: json.RawMessage(`{"partnerId":"{{steps.find_partner.partner_id}}","month":"{{inputs.month}}"}`),
				Success:      SuccessCondition{Statuses: []int{200}},
				Extractors:   map[string]string{"download_url": "$.downloadUrl"},
			},
		},
	}

	executor := NewExecutor(server.Client())
	invocation, err := executor.Execute(context.Background(), definition, map[string]any{
		"partner_name": "Acme",
		"month":        "2026-05",
	}, "ops@example.test")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if invocation.Status != InvocationStatusSucceeded {
		t.Fatalf("Status = %q, want %q", invocation.Status, InvocationStatusSucceeded)
	}
	if invocation.Output["download_url"] != "https://files.example.test/report.csv" {
		t.Fatalf("download_url = %#v", invocation.Output["download_url"])
	}
}

func TestExecutorFailsWhenStatusDoesNotMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadGateway)
	}))
	defer server.Close()

	definition := Definition{
		ID:          8,
		Name:        "bad_status",
		Version:     1,
		Status:      WorkflowStatusPublished,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Steps: []Step{{
			Name:       "call_api",
			Method:     http.MethodGet,
			URLTemplate: server.URL,
			Success:    SuccessCondition{Statuses: []int{200}},
		}},
	}

	executor := NewExecutor(server.Client())
	invocation, err := executor.Execute(context.Background(), definition, map[string]any{}, "ops@example.test")
	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
	if invocation.Status != InvocationStatusFailed {
		t.Fatalf("Status = %q, want %q", invocation.Status, InvocationStatusFailed)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/workflow
```

Expected: fail with `undefined: NewExecutor`.

- [ ] **Step 3: Implement executor**

Create `internal/workflow/executor.go`:

```go
package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"
)

type Executor struct {
	client *http.Client
}

func NewExecutor(client *http.Client) Executor {
	if client == nil {
		client = http.DefaultClient
	}
	return Executor{client: client}
}

func (executor Executor) Execute(ctx context.Context, definition Definition, inputs map[string]any, invokedBy string) (Invocation, error) {
	started := time.Now().UTC()
	invocation := Invocation{
		WorkflowID:      definition.ID,
		WorkflowVersion: definition.Version,
		InvokedBy:       invokedBy,
		Inputs:          inputs,
		Status:          InvocationStatusFailed,
		StartedAt:       started,
		Output:          map[string]any{},
	}
	if err := definition.Validate(); err != nil {
		invocation.FinishedAt = time.Now().UTC()
		invocation.Error = err.Error()
		return invocation, err
	}

	values := executionValues{inputs: inputs, steps: map[string]map[string]any{}}
	for _, step := range definition.Steps {
		stepResult, responseBody, err := executor.executeStep(ctx, step, values)
		invocation.Steps = append(invocation.Steps, stepResult)
		if err != nil {
			invocation.FinishedAt = time.Now().UTC()
			invocation.Error = err.Error()
			return invocation, err
		}
		extracted := map[string]any{}
		for name, path := range step.Extractors {
			value, err := extractJSONPath(responseBody, path)
			if err != nil {
				invocation.FinishedAt = time.Now().UTC()
				invocation.Error = err.Error()
				return invocation, err
			}
			extracted[name] = value
			invocation.Output[name] = value
		}
		values.steps[step.Name] = extracted
	}
	invocation.Status = InvocationStatusSucceeded
	invocation.FinishedAt = time.Now().UTC()
	return invocation, nil
}

func (executor Executor) executeStep(ctx context.Context, step Step, values executionValues) (InvocationStep, []byte, error) {
	started := time.Now()
	url, err := resolveTemplate(step.URLTemplate, values)
	if err != nil {
		return InvocationStep{Order: step.Order, Error: err.Error()}, nil, err
	}
	bodyBytes, err := resolveBody(step.BodyTemplate, values)
	if err != nil {
		return InvocationStep{Order: step.Order, Error: err.Error()}, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, step.Method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return InvocationStep{Order: step.Order, Error: err.Error()}, nil, err
	}
	for key, template := range step.HeadersTemplate {
		value, err := resolveTemplate(template, values)
		if err != nil {
			return InvocationStep{Order: step.Order, Error: err.Error()}, nil, err
		}
		req.Header.Set(key, value)
	}
	if len(bodyBytes) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := executor.client.Do(req)
	if err != nil {
		return InvocationStep{Order: step.Order, RequestSummary: step.Method + " " + url, Error: err.Error()}, nil, err
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	result := InvocationStep{
		Order:           step.Order,
		RequestSummary:  step.Method + " " + url,
		ResponseStatus:  resp.StatusCode,
		ResponseSummary: string(responseBody),
		DurationMS:      time.Since(started).Milliseconds(),
	}
	if readErr != nil {
		result.Error = readErr.Error()
		return result, nil, readErr
	}
	if !slices.Contains(step.Success.Statuses, resp.StatusCode) {
		err := fmt.Errorf("step %q returned status %d", step.Name, resp.StatusCode)
		result.Error = err.Error()
		return result, responseBody, err
	}
	return result, responseBody, nil
}

func resolveBody(template json.RawMessage, values executionValues) ([]byte, error) {
	if len(template) == 0 {
		return nil, nil
	}
	resolved, err := resolveTemplate(string(template), values)
	if err != nil {
		return nil, err
	}
	return []byte(resolved), nil
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/workflow/executor.go internal/workflow/executor_test.go
go test ./internal/workflow
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/workflow/executor.go internal/workflow/executor_test.go
git commit -m "Add workflow HTTP executor"
```

## Task 7: Add Workflow Repository

**Files:**
- Create: `internal/workflow/repository.go`
- Create: `internal/workflow/repository_test.go`

- [ ] **Step 1: Write repository tests**

Create `internal/workflow/repository_test.go`:

```go
package workflow

import (
	"context"
	"strings"
	"testing"
)

func TestRepositoryEnsureSchemaRejectsMissingPool(t *testing.T) {
	repository := NewRepository(nil)

	err := repository.EnsureSchema(context.Background())
	if err == nil {
		t.Fatal("EnsureSchema() error = nil, want error")
	}
}

func TestSchemaSQLContainsWorkflowTables(t *testing.T) {
	required := []string{
		"CREATE TABLE IF NOT EXISTS workflows",
		"CREATE TABLE IF NOT EXISTS workflow_steps",
		"CREATE TABLE IF NOT EXISTS workflow_invocations",
		"CREATE TABLE IF NOT EXISTS workflow_invocation_steps",
	}

	for _, want := range required {
		if !strings.Contains(schemaSQL, want) {
			t.Fatalf("schemaSQL does not contain %q", want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/workflow
```

Expected: fail with `undefined: NewRepository`.

- [ ] **Step 3: Implement repository skeleton and schema**

Create `internal/workflow/repository.go`:

```go
package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrRepositoryUnavailable = errors.New("workflow repository is unavailable")

const schemaSQL = `
CREATE TABLE IF NOT EXISTS workflows (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	version INTEGER NOT NULL DEFAULT 1,
	status TEXT NOT NULL,
	source_exploration_run_id BIGINT NOT NULL DEFAULT 0,
	risk_level TEXT NOT NULL DEFAULT '',
	input_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
	output_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
	parameter_provenance JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_by TEXT NOT NULL DEFAULT '',
	published_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workflow_steps (
	id BIGSERIAL PRIMARY KEY,
	workflow_id BIGINT NOT NULL REFERENCES workflows(id),
	step_order INTEGER NOT NULL,
	name TEXT NOT NULL,
	method TEXT NOT NULL,
	url_template TEXT NOT NULL,
	headers_template JSONB NOT NULL DEFAULT '{}'::jsonb,
	body_template JSONB,
	timeout_ms INTEGER NOT NULL DEFAULT 0,
	retry_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
	success_condition JSONB NOT NULL DEFAULT '{}'::jsonb,
	extractors JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS workflow_invocations (
	id BIGSERIAL PRIMARY KEY,
	workflow_id BIGINT NOT NULL REFERENCES workflows(id),
	workflow_version INTEGER NOT NULL,
	invoked_by TEXT NOT NULL DEFAULT '',
	input_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
	status TEXT NOT NULL,
	started_at TIMESTAMPTZ NOT NULL,
	finished_at TIMESTAMPTZ NOT NULL,
	output_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
	error_summary TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS workflow_invocation_steps (
	id BIGSERIAL PRIMARY KEY,
	workflow_invocation_id BIGINT NOT NULL REFERENCES workflow_invocations(id),
	step_order INTEGER NOT NULL,
	request_summary TEXT NOT NULL DEFAULT '',
	response_status INTEGER NOT NULL DEFAULT 0,
	response_summary TEXT NOT NULL DEFAULT '',
	duration_ms BIGINT NOT NULL DEFAULT 0,
	error_summary TEXT NOT NULL DEFAULT ''
);`

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return Repository{pool: pool}
}

func (repository Repository) EnsureSchema(ctx context.Context) error {
	if repository.pool == nil {
		return ErrRepositoryUnavailable
	}
	if _, err := repository.pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("ensure workflow schema: %w", err)
	}
	return nil
}

func (repository Repository) CreateWorkflow(ctx context.Context, definition Definition) (Definition, error) {
	if repository.pool == nil {
		return Definition{}, ErrRepositoryUnavailable
	}
	if err := definition.Validate(); err != nil {
		return Definition{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Definition{}, fmt.Errorf("begin create workflow: %w", err)
	}
	defer tx.Rollback(ctx)

	inputSchema, _ := json.Marshal(definition.InputSchema)
	outputSchema, _ := json.Marshal(definition.OutputSchema)
	provenance, _ := json.Marshal(definition.ParameterProvenance)
	err = tx.QueryRow(ctx, `
		INSERT INTO workflows (name, description, version, status, source_exploration_run_id, risk_level, input_schema, output_schema, parameter_provenance, created_by, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::jsonb, $10, NOW())
		RETURNING id, published_at`,
		definition.Name,
		definition.Description,
		definition.Version,
		definition.Status,
		definition.SourceRunID,
		definition.RiskLevel,
		string(inputSchema),
		string(outputSchema),
		string(provenance),
		definition.CreatedBy,
	).Scan(&definition.ID, &definition.PublishedAt)
	if err != nil {
		return Definition{}, fmt.Errorf("insert workflow: %w", err)
	}
	for index := range definition.Steps {
		step := definition.Steps[index]
		step.WorkflowID = definition.ID
		if step.Order == 0 {
			step.Order = index + 1
		}
		if err := insertStep(ctx, tx, step); err != nil {
			return Definition{}, err
		}
		definition.Steps[index] = step
	}
	if err := tx.Commit(ctx); err != nil {
		return Definition{}, fmt.Errorf("commit create workflow: %w", err)
	}
	return definition, nil
}

func insertStep(ctx context.Context, tx pgx.Tx, step Step) error {
	headers, _ := json.Marshal(step.HeadersTemplate)
	retryPolicy, _ := json.Marshal(step.RetryPolicy)
	success, _ := json.Marshal(step.Success)
	extractors, _ := json.Marshal(step.Extractors)
	_, err := tx.Exec(ctx, `
		INSERT INTO workflow_steps (workflow_id, step_order, name, method, url_template, headers_template, body_template, timeout_ms, retry_policy, success_condition, extractors)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9::jsonb, $10::jsonb, $11::jsonb)`,
		step.WorkflowID,
		step.Order,
		step.Name,
		step.Method,
		step.URLTemplate,
		string(headers),
		string(step.BodyTemplate),
		step.TimeoutMS,
		string(retryPolicy),
		string(success),
		string(extractors),
	)
	if err != nil {
		return fmt.Errorf("insert workflow step: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Add read and invocation persistence methods**

Append these methods to `internal/workflow/repository.go`:

```go
func (repository Repository) GetWorkflow(ctx context.Context, id int64) (Definition, error) {
	if repository.pool == nil {
		return Definition{}, ErrRepositoryUnavailable
	}
	var definition Definition
	err := repository.pool.QueryRow(ctx, `
		SELECT id, name, description, version, status, source_exploration_run_id, risk_level, input_schema, output_schema, parameter_provenance, created_by, published_at
		FROM workflows
		WHERE id = $1`, id).
		Scan(&definition.ID, &definition.Name, &definition.Description, &definition.Version, &definition.Status, &definition.SourceRunID, &definition.RiskLevel, &definition.InputSchema, &definition.OutputSchema, &definition.ParameterProvenance, &definition.CreatedBy, &definition.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Definition{}, ErrWorkflowMissing
	}
	if err != nil {
		return Definition{}, fmt.Errorf("get workflow: %w", err)
	}
	rows, err := repository.pool.Query(ctx, `
		SELECT id, workflow_id, step_order, name, method, url_template, headers_template, body_template, timeout_ms, retry_policy, success_condition, extractors
		FROM workflow_steps
		WHERE workflow_id = $1
		ORDER BY step_order`, id)
	if err != nil {
		return Definition{}, fmt.Errorf("list workflow steps: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var step Step
		if err := rows.Scan(&step.ID, &step.WorkflowID, &step.Order, &step.Name, &step.Method, &step.URLTemplate, &step.HeadersTemplate, &step.BodyTemplate, &step.TimeoutMS, &step.RetryPolicy, &step.Success, &step.Extractors); err != nil {
			return Definition{}, fmt.Errorf("scan workflow step: %w", err)
		}
		definition.Steps = append(definition.Steps, step)
	}
	if err := rows.Err(); err != nil {
		return Definition{}, fmt.Errorf("iterate workflow steps: %w", err)
	}
	return definition, nil
}

func (repository Repository) RecordInvocation(ctx context.Context, invocation Invocation) (Invocation, error) {
	if repository.pool == nil {
		return Invocation{}, ErrRepositoryUnavailable
	}
	inputs, _ := json.Marshal(invocation.Inputs)
	output, _ := json.Marshal(invocation.Output)
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return Invocation{}, fmt.Errorf("begin record invocation: %w", err)
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `
		INSERT INTO workflow_invocations (workflow_id, workflow_version, invoked_by, input_summary, status, started_at, finished_at, output_summary, error_summary)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8::jsonb, $9)
		RETURNING id`,
		invocation.WorkflowID,
		invocation.WorkflowVersion,
		invocation.InvokedBy,
		string(inputs),
		invocation.Status,
		invocation.StartedAt,
		invocation.FinishedAt,
		string(output),
		invocation.Error,
	).Scan(&invocation.ID)
	if err != nil {
		return Invocation{}, fmt.Errorf("insert workflow invocation: %w", err)
	}
	for _, step := range invocation.Steps {
		_, err := tx.Exec(ctx, `
			INSERT INTO workflow_invocation_steps (workflow_invocation_id, step_order, request_summary, response_status, response_summary, duration_ms, error_summary)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			invocation.ID,
			step.Order,
			step.RequestSummary,
			step.ResponseStatus,
			step.ResponseSummary,
			step.DurationMS,
			step.Error,
		)
		if err != nil {
			return Invocation{}, fmt.Errorf("insert workflow invocation step: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Invocation{}, fmt.Errorf("commit record invocation: %w", err)
	}
	return invocation, nil
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
gofmt -w internal/workflow/repository.go internal/workflow/repository_test.go
go test ./internal/workflow
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/workflow/repository.go internal/workflow/repository_test.go
git commit -m "Add workflow repository"
```

## Task 8: Add Workflow Service And HTTP Handler

**Files:**
- Create: `internal/workflow/service.go`
- Create: `internal/workflow/handler.go`
- Create: `internal/workflow/handler_test.go`

- [ ] **Step 1: Write handler tests**

Create `internal/workflow/handler_test.go`:

```go
package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type memoryStore struct {
	definition Definition
	invocation Invocation
}

func (store *memoryStore) GetWorkflow(_ context.Context, id int64) (Definition, error) {
	if store.definition.ID != id {
		return Definition{}, ErrWorkflowMissing
	}
	return store.definition, nil
}

func (store *memoryStore) RecordInvocation(_ context.Context, invocation Invocation) (Invocation, error) {
	invocation.ID = 99
	store.invocation = invocation
	return invocation, nil
}

func TestHandlerInvokeExecutesWorkflow(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	store := &memoryStore{definition: Definition{
		ID:          7,
		Name:        "ping",
		Version:     1,
		Status:      WorkflowStatusPublished,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Steps: []Step{{
			Name:       "call_api",
			Method:     http.MethodGet,
			URLTemplate: api.URL,
			Success:    SuccessCondition{Statuses: []int{200}},
		}},
	}}
	service := NewService(store, NewExecutor(api.Client()))
	handler := NewHandler(service)
	app := fiber.New()
	app.Post("/api/workflows/:workflow_id/invocations", handler.Invoke)

	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/workflows/7/invocations", bytes.NewBufferString(`{"inputs":{}}`)))
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body Invocation
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body.ID != 99 {
		t.Fatalf("ID = %d, want 99", body.ID)
	}
}

func TestHandlerGetReturnsWorkflow(t *testing.T) {
	store := &memoryStore{definition: Definition{
		ID:          7,
		Name:        "ping",
		Version:     1,
		Status:      WorkflowStatusPublished,
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Steps: []Step{{
			Name:       "call_api",
			Method:     http.MethodGet,
			URLTemplate: "https://internal.example.test",
			Success:    SuccessCondition{Statuses: []int{200}},
		}},
	}}
	service := NewService(store, NewExecutor(http.DefaultClient))
	handler := NewHandler(service)
	app := fiber.New()
	app.Get("/api/workflows/:workflow_id", handler.Get)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/workflows/7", nil))
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body Definition
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body.Name != "ping" {
		t.Fatalf("Name = %q, want ping", body.Name)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/workflow
```

Expected: fail with `undefined: NewService`.

- [ ] **Step 3: Implement service**

Create `internal/workflow/service.go`:

```go
package workflow

import (
	"context"
	"fmt"
)

type Store interface {
	GetWorkflow(ctx context.Context, id int64) (Definition, error)
	RecordInvocation(ctx context.Context, invocation Invocation) (Invocation, error)
}

type Service struct {
	store    Store
	executor Executor
}

func NewService(store Store, executor Executor) Service {
	return Service{store: store, executor: executor}
}

func (service Service) Invoke(ctx context.Context, workflowID int64, inputs map[string]any, invokedBy string) (Invocation, error) {
	definition, err := service.store.GetWorkflow(ctx, workflowID)
	if err != nil {
		return Invocation{}, fmt.Errorf("get workflow: %w", err)
	}
	invocation, executeErr := service.executor.Execute(ctx, definition, inputs, invokedBy)
	recorded, recordErr := service.store.RecordInvocation(ctx, invocation)
	if recordErr != nil {
		return Invocation{}, fmt.Errorf("record invocation: %w", recordErr)
	}
	if executeErr != nil {
		return recorded, executeErr
	}
	return recorded, nil
}

func (service Service) Get(ctx context.Context, workflowID int64) (Definition, error) {
	return service.store.GetWorkflow(ctx, workflowID)
}
```

This interface is intentionally local to `workflow` so handlers can be tested without PostgreSQL.

- [ ] **Step 4: Implement handler**

Create `internal/workflow/handler.go`:

```go
package workflow

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	service Service
}

type invokeRequest struct {
	Inputs    map[string]any `json:"inputs"`
	InvokedBy string         `json:"invoked_by"`
}

func NewHandler(service Service) Handler {
	return Handler{service: service}
}

func (handler Handler) Get(c fiber.Ctx) error {
	workflowID, err := strconv.ParseInt(c.Params("workflow_id"), 10, 64)
	if err != nil || workflowID <= 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "workflow_id must be a positive integer"})
	}
	definition, err := handler.service.Get(c.Context(), workflowID)
	if errors.Is(err, ErrWorkflowMissing) {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "workflow not found"})
	}
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "get workflow failed"})
	}
	return c.Status(http.StatusOK).JSON(definition)
}

func (handler Handler) Invoke(c fiber.Ctx) error {
	workflowID, err := strconv.ParseInt(c.Params("workflow_id"), 10, 64)
	if err != nil || workflowID <= 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "workflow_id must be a positive integer"})
	}
	var req invokeRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "request body must be valid json"})
	}
	if req.Inputs == nil {
		req.Inputs = map[string]any{}
	}
	invocation, err := handler.service.Invoke(c.Context(), workflowID, req.Inputs, req.InvokedBy)
	if errors.Is(err, ErrWorkflowMissing) {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "workflow not found"})
	}
	if err != nil {
		return c.Status(http.StatusBadGateway).JSON(invocation)
	}
	return c.Status(http.StatusOK).JSON(invocation)
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
gofmt -w internal/workflow/service.go internal/workflow/handler.go internal/workflow/handler_test.go
go test ./internal/workflow
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/workflow/service.go internal/workflow/handler.go internal/workflow/handler_test.go
git commit -m "Add workflow invocation handler"
```

## Task 9: Add Exploration Types And Redaction

**Files:**
- Create: `internal/exploration/types.go`
- Create: `internal/exploration/redact.go`
- Create: `internal/exploration/redact_test.go`

- [ ] **Step 1: Write redaction tests**

Create `internal/exploration/redact_test.go`:

```go
package exploration

import "testing"

func TestRedactMapRemovesSensitiveValues(t *testing.T) {
	input := map[string]any{
		"Authorization": "Bearer abc",
		"Cookie":        "session=abc",
		"X-Trace":       "trace-123",
		"nested": map[string]any{
			"password": "secret",
			"month":    "2026-05",
		},
	}

	got := redactMap(input)

	if got["Authorization"] != redactedValue {
		t.Fatalf("Authorization = %#v, want redacted", got["Authorization"])
	}
	if got["Cookie"] != redactedValue {
		t.Fatalf("Cookie = %#v, want redacted", got["Cookie"])
	}
	nested := got["nested"].(map[string]any)
	if nested["password"] != redactedValue {
		t.Fatalf("password = %#v, want redacted", nested["password"])
	}
	if nested["month"] != "2026-05" {
		t.Fatalf("month = %#v, want 2026-05", nested["month"])
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/exploration
```

Expected: fail because package has no implementation.

- [ ] **Step 3: Add exploration types**

Create `internal/exploration/types.go`:

```go
package exploration

import (
	"encoding/json"
	"time"

	"orderbuddy-ai/backend/internal/workflow"
)

const (
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
)

type RunStatus string

type Request struct {
	Prompt    string `json:"prompt"`
	TargetURL string `json:"target_url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	CreatedBy string `json:"created_by"`
}

type Run struct {
	ID                 int64           `json:"id"`
	Prompt             string          `json:"prompt"`
	TargetURL          string          `json:"target_url"`
	Status             RunStatus       `json:"status"`
	AgentName          string          `json:"agent_name"`
	CreatedBy          string          `json:"created_by"`
	CredentialSupplied bool            `json:"credential_supplied"`
	TraceSummary       json.RawMessage `json:"trace_summary"`
	ErrorSummary       string          `json:"error_summary,omitempty"`
	StartedAt          time.Time       `json:"started_at"`
	FinishedAt         time.Time       `json:"finished_at"`
	Workflow           workflow.Definition `json:"workflow"`
}

type Trace struct {
	StepOrder               int            `json:"step_order"`
	Method                  string         `json:"method"`
	URL                     string         `json:"url"`
	RequestHeadersRedacted  map[string]any `json:"request_headers_redacted"`
	RequestBodyRedacted     map[string]any `json:"request_body_redacted"`
	ResponseStatus          int            `json:"response_status"`
	ResponseHeadersRedacted map[string]any `json:"response_headers_redacted"`
	ResponseBodySummary     string         `json:"response_body_summary"`
	DownloadMetadata        map[string]any `json:"download_metadata"`
	Initiator               string         `json:"initiator"`
	BusinessRelevance       string         `json:"business_relevance"`
	OccurredAt              time.Time      `json:"occurred_at"`
}

type RunnerResult struct {
	AgentName    string              `json:"agent_name"`
	TraceSummary json.RawMessage     `json:"trace_summary"`
	Traces       []Trace             `json:"traces"`
	Workflow     workflow.Definition `json:"workflow"`
}
```

- [ ] **Step 4: Add redaction**

Create `internal/exploration/redact.go`:

```go
package exploration

import "strings"

const redactedValue = "[REDACTED]"

var sensitiveKeys = []string{
	"authorization",
	"cookie",
	"set-cookie",
	"password",
	"token",
	"secret",
	"credential",
	"api-key",
	"x-api-key",
}

func redactMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		if isSensitiveKey(key) {
			output[key] = redactedValue
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			output[key] = redactMap(nested)
			continue
		}
		output[key] = value
	}
	return output
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(normalized, sensitive) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
gofmt -w internal/exploration/types.go internal/exploration/redact.go internal/exploration/redact_test.go
go test ./internal/exploration
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/exploration/types.go internal/exploration/redact.go internal/exploration/redact_test.go
git commit -m "Add exploration types and redaction"
```

## Task 10: Add Exploration Repository

**Files:**
- Create: `internal/exploration/repository.go`
- Create: `internal/exploration/repository_test.go`

- [ ] **Step 1: Write repository tests**

Create `internal/exploration/repository_test.go`:

```go
package exploration

import (
	"context"
	"strings"
	"testing"
)

func TestRepositoryEnsureSchemaRejectsMissingPool(t *testing.T) {
	repository := NewRepository(nil)

	err := repository.EnsureSchema(context.Background())
	if err == nil {
		t.Fatal("EnsureSchema() error = nil, want error")
	}
}

func TestSchemaSQLContainsExplorationTables(t *testing.T) {
	required := []string{
		"CREATE TABLE IF NOT EXISTS exploration_runs",
		"CREATE TABLE IF NOT EXISTS api_traces",
	}

	for _, want := range required {
		if !strings.Contains(schemaSQL, want) {
			t.Fatalf("schemaSQL does not contain %q", want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/exploration
```

Expected: fail with `undefined: NewRepository`.

- [ ] **Step 3: Implement repository**

Create `internal/exploration/repository.go`:

```go
package exploration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrRepositoryUnavailable = errors.New("exploration repository is unavailable")

const schemaSQL = `
CREATE TABLE IF NOT EXISTS exploration_runs (
	id BIGSERIAL PRIMARY KEY,
	prompt TEXT NOT NULL,
	target_url TEXT NOT NULL,
	status TEXT NOT NULL,
	agent_name TEXT NOT NULL DEFAULT '',
	created_by TEXT NOT NULL DEFAULT '',
	credential_supplied BOOLEAN NOT NULL DEFAULT FALSE,
	trace_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
	error_summary TEXT NOT NULL DEFAULT '',
	started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	finished_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS api_traces (
	id BIGSERIAL PRIMARY KEY,
	exploration_run_id BIGINT NOT NULL REFERENCES exploration_runs(id),
	step_order INTEGER NOT NULL,
	method TEXT NOT NULL,
	url TEXT NOT NULL,
	request_headers_redacted JSONB NOT NULL DEFAULT '{}'::jsonb,
	request_body_redacted JSONB NOT NULL DEFAULT '{}'::jsonb,
	response_status INTEGER NOT NULL DEFAULT 0,
	response_headers_redacted JSONB NOT NULL DEFAULT '{}'::jsonb,
	response_body_summary TEXT NOT NULL DEFAULT '',
	download_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
	initiator TEXT NOT NULL DEFAULT '',
	business_relevance TEXT NOT NULL DEFAULT '',
	occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return Repository{pool: pool}
}

func (repository Repository) EnsureSchema(ctx context.Context) error {
	if repository.pool == nil {
		return ErrRepositoryUnavailable
	}
	if _, err := repository.pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("ensure exploration schema: %w", err)
	}
	return nil
}

func (repository Repository) CreateRun(ctx context.Context, run Run) (Run, error) {
	if repository.pool == nil {
		return Run{}, ErrRepositoryUnavailable
	}
	traceSummary := run.TraceSummary
	if len(traceSummary) == 0 {
		traceSummary = json.RawMessage(`{}`)
	}
	err := repository.pool.QueryRow(ctx, `
		INSERT INTO exploration_runs (prompt, target_url, status, agent_name, created_by, credential_supplied, trace_summary, error_summary, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)
		RETURNING id`,
		run.Prompt,
		run.TargetURL,
		run.Status,
		run.AgentName,
		run.CreatedBy,
		run.CredentialSupplied,
		string(traceSummary),
		run.ErrorSummary,
		run.StartedAt,
		run.FinishedAt,
	).Scan(&run.ID)
	if err != nil {
		return Run{}, fmt.Errorf("insert exploration run: %w", err)
	}
	return run, nil
}

func (repository Repository) RecordTraces(ctx context.Context, runID int64, traces []Trace) error {
	if repository.pool == nil {
		return ErrRepositoryUnavailable
	}
	for _, trace := range traces {
		requestHeaders, _ := json.Marshal(redactMap(trace.RequestHeadersRedacted))
		requestBody, _ := json.Marshal(redactMap(trace.RequestBodyRedacted))
		responseHeaders, _ := json.Marshal(redactMap(trace.ResponseHeadersRedacted))
		downloadMetadata, _ := json.Marshal(redactMap(trace.DownloadMetadata))
		_, err := repository.pool.Exec(ctx, `
			INSERT INTO api_traces (exploration_run_id, step_order, method, url, request_headers_redacted, request_body_redacted, response_status, response_headers_redacted, response_body_summary, download_metadata, initiator, business_relevance, occurred_at)
			VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8::jsonb, $9, $10::jsonb, $11, $12, $13)`,
			runID,
			trace.StepOrder,
			trace.Method,
			trace.URL,
			string(requestHeaders),
			string(requestBody),
			trace.ResponseStatus,
			string(responseHeaders),
			trace.ResponseBodySummary,
			string(downloadMetadata),
			trace.Initiator,
			trace.BusinessRelevance,
			trace.OccurredAt,
		)
		if err != nil {
			return fmt.Errorf("insert api trace: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/exploration/repository.go internal/exploration/repository_test.go
go test ./internal/exploration
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/exploration/repository.go internal/exploration/repository_test.go
git commit -m "Add exploration repository"
```

## Task 11: Add External Exploration Runner

**Files:**
- Create: `internal/exploration/runner.go`
- Create: `internal/exploration/runner_test.go`

- [ ] **Step 1: Write runner tests**

Create `internal/exploration/runner_test.go`:

```go
package exploration

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestCommandRunnerReturnsResult(t *testing.T) {
	if os.Getenv("EXPLORATION_RUNNER_HELPER") == "1" {
		os.Stdout.WriteString(`{"agent_name":"test-agent","trace_summary":{},"workflow":{"name":"generated","status":"published","input_schema":{},"steps":[{"name":"call_api","method":"GET","url_template":"https://internal.example.test","success":{"statuses":[200]}}]}}`)
		return
	}
	runner := NewCommandRunner(func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestCommandRunnerReturnsResult")
		cmd.Env = append(os.Environ(), "EXPLORATION_RUNNER_HELPER=1")
		return cmd
	})

	result, err := runner.Run(context.Background(), Request{Prompt: "export report", TargetURL: "https://internal.example.test", Username: "user", Password: "pass"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.AgentName != "test-agent" {
		t.Fatalf("AgentName = %q, want test-agent", result.AgentName)
	}
	if result.Workflow.Name != "generated" {
		t.Fatalf("Workflow.Name = %q, want generated", result.Workflow.Name)
	}
}

func TestCommandRunnerFailsOnEmptyCommand(t *testing.T) {
	runner := CommandRunner{}

	_, err := runner.Run(context.Background(), Request{Prompt: "export report"})
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
}

func TestCommandRunnerHonorsTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := NewCommandRunner(func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, "sleep", "1")
	})
	_, err := runner.Run(ctx, Request{Prompt: "export report"})
	if err == nil {
		t.Fatal("Run() error = nil, want timeout error")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/exploration
```

Expected: fail with `undefined: NewCommandRunner`.

- [ ] **Step 3: Implement command runner**

Create `internal/exploration/runner.go`:

```go
package exploration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

var ErrRunnerUnavailable = errors.New("exploration runner is unavailable")

type Runner interface {
	Run(ctx context.Context, request Request) (RunnerResult, error)
}

type CommandFactory func(ctx context.Context) *exec.Cmd

type CommandRunner struct {
	factory CommandFactory
}

func NewCommandRunner(factory CommandFactory) CommandRunner {
	return CommandRunner{factory: factory}
}

func (runner CommandRunner) Run(ctx context.Context, request Request) (RunnerResult, error) {
	if runner.factory == nil {
		return RunnerResult{}, ErrRunnerUnavailable
	}
	command := runner.factory(ctx)
	if command == nil {
		return RunnerResult{}, ErrRunnerUnavailable
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return RunnerResult{}, fmt.Errorf("encode runner request: %w", err)
	}
	command.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return RunnerResult{}, ctx.Err()
		}
		return RunnerResult{}, fmt.Errorf("run exploration command: %w: %s", err, stderr.String())
	}
	var result RunnerResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return RunnerResult{}, fmt.Errorf("decode runner result: %w", err)
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w internal/exploration/runner.go internal/exploration/runner_test.go
go test ./internal/exploration
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/exploration/runner.go internal/exploration/runner_test.go
git commit -m "Add exploration command runner"
```

## Task 12: Add Exploration Service And Handler

**Files:**
- Create: `internal/exploration/service.go`
- Create: `internal/exploration/service_test.go`
- Create: `internal/exploration/handler.go`
- Create: `internal/exploration/handler_test.go`

- [ ] **Step 1: Write service tests**

Create `internal/exploration/service_test.go`:

```go
package exploration

import (
	"context"
	"encoding/json"
	"testing"

	"orderbuddy-ai/backend/internal/workflow"
)

type fakeRunner struct {
	result RunnerResult
	err    error
}

func (runner fakeRunner) Run(_ context.Context, _ Request) (RunnerResult, error) {
	return runner.result, runner.err
}

type fakePublisher struct {
	definition workflow.Definition
}

func (publisher *fakePublisher) CreateWorkflow(_ context.Context, definition workflow.Definition) (workflow.Definition, error) {
	definition.ID = 42
	publisher.definition = definition
	return definition, nil
}

type fakeRunStore struct {
	run    Run
	traces []Trace
}

func (store *fakeRunStore) CreateRun(_ context.Context, run Run) (Run, error) {
	run.ID = 11
	store.run = run
	return run, nil
}

func (store *fakeRunStore) RecordTraces(_ context.Context, runID int64, traces []Trace) error {
	store.traces = traces
	return nil
}

func TestServiceRunPublishesWorkflow(t *testing.T) {
	publisher := &fakePublisher{}
	runStore := &fakeRunStore{}
	service := NewService(fakeRunner{result: RunnerResult{
		AgentName:    "test-agent",
		TraceSummary: json.RawMessage(`{"requests":1}`),
		Workflow: workflow.Definition{
			Name:        "generated",
			Status:      workflow.WorkflowStatusPublished,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Steps: []workflow.Step{{
				Name:       "call_api",
				Method:     "GET",
				URLTemplate: "https://internal.example.test",
				Success:    workflow.SuccessCondition{Statuses: []int{200}},
			}},
		},
		Traces: []Trace{{
			StepOrder: 1,
			Method:    "GET",
			URL:       "https://internal.example.test/api",
			RequestHeadersRedacted: map[string]any{
				"Authorization": "Bearer secret",
			},
		}},
	}}, publisher, runStore)

	run, err := service.Run(context.Background(), Request{
		Prompt:    "export report",
		TargetURL: "https://internal.example.test",
		Username:  "user",
		Password:  "secret",
		CreatedBy: "dev@example.test",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if run.Workflow.ID != 42 {
		t.Fatalf("Workflow.ID = %d, want 42", run.Workflow.ID)
	}
	if run.CredentialSupplied != true {
		t.Fatal("CredentialSupplied = false, want true")
	}
	if publisher.definition.CreatedBy != "dev@example.test" {
		t.Fatalf("CreatedBy = %q, want dev@example.test", publisher.definition.CreatedBy)
	}
	if run.ID != 11 {
		t.Fatalf("Run.ID = %d, want 11", run.ID)
	}
	if len(runStore.traces) != 1 {
		t.Fatalf("recorded traces = %d, want 1", len(runStore.traces))
	}
}
```

- [ ] **Step 2: Write handler test**

Create `internal/exploration/handler_test.go`:

```go
package exploration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"orderbuddy-ai/backend/internal/workflow"
)

func TestHandlerCreateRunDoesNotEchoPassword(t *testing.T) {
	publisher := &fakePublisher{}
	service := NewService(fakeRunner{result: RunnerResult{
		AgentName:    "test-agent",
		TraceSummary: json.RawMessage(`{}`),
		Workflow: workflow.Definition{
			Name:        "generated",
			Status:      workflow.WorkflowStatusPublished,
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Steps: []workflow.Step{{
				Name:       "call_api",
				Method:     "GET",
				URLTemplate: "https://internal.example.test",
				Success:    workflow.SuccessCondition{Statuses: []int{200}},
			}},
		},
	}}, publisher, &fakeRunStore{})
	handler := NewHandler(service)
	app := fiber.New()
	app.Post("/api/exploration-runs", handler.CreateRun)

	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/exploration-runs", bytes.NewBufferString(`{"prompt":"export","target_url":"https://internal.example.test","username":"u","password":"super-secret"}`)))
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer resp.Body.Close()

	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(resp.Body)
	if bytes.Contains(body.Bytes(), []byte("super-secret")) {
		t.Fatalf("response leaked password: %s", body.String())
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/exploration
```

Expected: fail with `undefined: NewService`.

- [ ] **Step 4: Implement service**

Create `internal/exploration/service.go`:

```go
package exploration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"orderbuddy-ai/backend/internal/workflow"
)

var ErrRequestInvalid = errors.New("exploration request is invalid")

type Publisher interface {
	CreateWorkflow(ctx context.Context, definition workflow.Definition) (workflow.Definition, error)
}

type RunStore interface {
	CreateRun(ctx context.Context, run Run) (Run, error)
	RecordTraces(ctx context.Context, runID int64, traces []Trace) error
}

type Service struct {
	runner    Runner
	publisher Publisher
	runStore  RunStore
}

func NewService(runner Runner, publisher Publisher, runStore RunStore) Service {
	return Service{runner: runner, publisher: publisher, runStore: runStore}
}

func (service Service) Run(ctx context.Context, request Request) (Run, error) {
	started := time.Now().UTC()
	run := Run{
		Prompt:             request.Prompt,
		TargetURL:          request.TargetURL,
		Status:             RunStatusFailed,
		CreatedBy:          request.CreatedBy,
		CredentialSupplied: request.Username != "" || request.Password != "",
		StartedAt:          started,
	}
	if strings.TrimSpace(request.Prompt) == "" {
		run.FinishedAt = time.Now().UTC()
		run.ErrorSummary = "prompt is required"
		return run, fmt.Errorf("%w: prompt is required", ErrRequestInvalid)
	}
	if strings.TrimSpace(request.TargetURL) == "" {
		run.FinishedAt = time.Now().UTC()
		run.ErrorSummary = "target_url is required"
		return run, fmt.Errorf("%w: target_url is required", ErrRequestInvalid)
	}
	result, err := service.runner.Run(ctx, request)
	if err != nil {
		run.FinishedAt = time.Now().UTC()
		run.ErrorSummary = err.Error()
		return run, fmt.Errorf("run exploration: %w", err)
	}
	result.Workflow.Status = workflow.WorkflowStatusPublished
	result.Workflow.CreatedBy = request.CreatedBy
	published, err := service.publisher.CreateWorkflow(ctx, result.Workflow)
	if err != nil {
		run.FinishedAt = time.Now().UTC()
		run.ErrorSummary = err.Error()
		return run, fmt.Errorf("publish generated workflow: %w", err)
	}
	run.AgentName = result.AgentName
	run.TraceSummary = result.TraceSummary
	run.Workflow = published
	run.Status = RunStatusSucceeded
	run.FinishedAt = time.Now().UTC()
	stored, err := service.runStore.CreateRun(ctx, run)
	if err != nil {
		return run, fmt.Errorf("store exploration run: %w", err)
	}
	if err := service.runStore.RecordTraces(ctx, stored.ID, result.Traces); err != nil {
		return stored, fmt.Errorf("store api traces: %w", err)
	}
	stored.Workflow = published
	return stored, nil
}
```

- [ ] **Step 5: Implement handler**

Create `internal/exploration/handler.go`:

```go
package exploration

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) Handler {
	return Handler{service: service}
}

func (handler Handler) CreateRun(c fiber.Ctx) error {
	var req Request
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "request body must be valid json"})
	}
	run, err := handler.service.Run(c.Context(), req)
	if errors.Is(err, ErrRequestInvalid) {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": run.ErrorSummary})
	}
	if err != nil {
		return c.Status(http.StatusBadGateway).JSON(run)
	}
	return c.Status(http.StatusCreated).JSON(run)
}
```

- [ ] **Step 6: Run tests**

Run:

```bash
gofmt -w internal/exploration/service.go internal/exploration/service_test.go internal/exploration/handler.go internal/exploration/handler_test.go
go test ./internal/exploration
```

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add internal/exploration/service.go internal/exploration/service_test.go internal/exploration/handler.go internal/exploration/handler_test.go
git commit -m "Add exploration service and handler"
```

## Task 13: Register HTTP Routes

**Files:**
- Modify: `internal/httpapi/router.go`
- Modify: `internal/httpapi/router_test.go`
- Modify: `internal/httpapi/middleware.go`

- [ ] **Step 1: Add route constants and router config fields**

In `internal/httpapi/router.go`, add imports:

```go
"orderbuddy-ai/backend/internal/exploration"
"orderbuddy-ai/backend/internal/workflow"
```

Add constants:

```go
ExplorationRunsPath       = "/api/exploration-runs"
WorkflowPath              = "/api/workflows/:workflow_id"
WorkflowInvocationsPath   = "/api/workflows/:workflow_id/invocations"
```

Extend `RouterConfig`:

```go
type RouterConfig struct {
	StatusHandler      status.Handler
	ExplorationHandler exploration.Handler
	WorkflowHandler    workflow.Handler
}
```

Register optional routes:

```go
if config.ExplorationHandler.Enabled() {
	app.Post(ExplorationRunsPath, config.ExplorationHandler.CreateRun)
}
if config.WorkflowHandler.Enabled() {
	app.Get(WorkflowPath, config.WorkflowHandler.Get)
	app.Post(WorkflowInvocationsPath, config.WorkflowHandler.Invoke)
}
```

- [ ] **Step 2: Update CORS methods**

In `internal/httpapi/middleware.go`, change:

```go
corsAllowedMethods = "GET, POST, OPTIONS"
```

- [ ] **Step 3: Update router tests**

In `internal/httpapi/router_test.go`, keep the existing health and CORS tests using zero-value optional handlers:

```go
app := NewRouter(RouterConfig{
	StatusHandler: status.NewHandler(status.NewService(&fakeDatabase{}), "test"),
})
```

Make this work by adding `Enabled() bool` methods to `exploration.Handler` and `workflow.Handler`:

```go
func (handler Handler) Enabled() bool {
	return handler.service.Enabled()
}
```

Add `Enabled() bool` methods to both services:

```go
func (service Service) Enabled() bool {
	return service.runner != nil && service.publisher != nil && service.runStore != nil
}
```

and:

```go
func (service Service) Enabled() bool {
	return service.store != nil
}
```

- [ ] **Step 4: Add CORS assertion for POST**

In `TestOptionsRequestReturnsCORSHeaders`, add:

```go
if got := resp.Header.Get(headerAccessControlAllowMethods); got != corsAllowedMethods {
	t.Fatalf("Access-Control-Allow-Methods = %q, want %q", got, corsAllowedMethods)
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
gofmt -w internal/httpapi/router.go internal/httpapi/router_test.go internal/httpapi/middleware.go internal/exploration/service.go internal/exploration/handler.go internal/workflow/service.go internal/workflow/handler.go
go test ./internal/httpapi ./internal/exploration ./internal/workflow
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/httpapi/router.go internal/httpapi/router_test.go internal/httpapi/middleware.go internal/exploration/service.go internal/exploration/handler.go internal/workflow/service.go internal/workflow/handler.go
git commit -m "Register workflow generation routes"
```

## Task 14: Wire Application Dependencies

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

- [ ] **Step 1: Add command factory helper**

In `internal/app/app.go`, add imports:

```go
"os/exec"
"strings"
"net/http"

"orderbuddy-ai/backend/internal/exploration"
"orderbuddy-ai/backend/internal/workflow"
```

Add helper:

```go
func newExplorationCommandFactory(command string) exploration.CommandFactory {
	return func(ctx context.Context) *exec.Cmd {
		parts := strings.Fields(command)
		if len(parts) == 0 {
			return nil
		}
		return exec.CommandContext(ctx, parts[0], parts[1:]...)
	}
}
```

Adjust `CommandRunner.Run` to call `factory(ctx)` when `factory` is set and return `ErrRunnerUnavailable` when it returns nil.

- [ ] **Step 2: Wire repositories and handlers in `Run`**

After creating `pool`, add:

```go
workflowRepository := workflow.NewRepository(pool)
if err := workflowRepository.EnsureSchema(context.Background()); err != nil {
	return fmt.Errorf("ensure workflow schema: %w", err)
}
explorationRepository := exploration.NewRepository(pool)
if err := explorationRepository.EnsureSchema(context.Background()); err != nil {
	return fmt.Errorf("ensure exploration schema: %w", err)
}
workflowService := workflow.NewService(workflowRepository, workflow.NewExecutor(http.DefaultClient))
workflowHandler := workflow.NewHandler(workflowService)

explorationRunner := exploration.NewCommandRunner(newExplorationCommandFactory(cfg.ExplorationRunnerCommand))
explorationService := exploration.NewService(explorationRunner, workflowRepository, explorationRepository)
explorationHandler := exploration.NewHandler(explorationService)
```

Change router construction:

```go
router := httpapi.NewRouter(httpapi.RouterConfig{
	StatusHandler:      statusHandler,
	ExplorationHandler: explorationHandler,
	WorkflowHandler:    workflowHandler,
})
```

- [ ] **Step 3: Run app package tests**

Run:

```bash
gofmt -w internal/app/app.go internal/app/app_test.go internal/exploration/runner.go
go test ./internal/app ./internal/exploration
```

Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go internal/exploration/runner.go
git commit -m "Wire workflow generation services"
```

## Task 15: Final Verification And Guard

**Files:**
- Modify only files required by failures found in this task.

- [ ] **Step 1: Run gofmt over all Go files**

Run:

```bash
gofmt -w $(find . -name '*.go' -not -path './vendor/*')
```

Expected: no output.

- [ ] **Step 2: Run all tests**

Run:

```bash
go test ./...
```

Expected: all packages pass.

- [ ] **Step 3: Run repository guard**

Run:

```bash
./scripts/repo-guard.sh
```

Expected:

```text
repo guard passed
```

- [ ] **Step 4: Inspect git status**

Run:

```bash
git status --short
```

Expected: only intentional source and test files changed since the previous commit.

- [ ] **Step 5: Commit final fixes when present**

If Step 4 shows files changed by verification fixes, commit them:

```bash
git add .
git commit -m "Verify workflow generation slice"
```

If Step 4 is empty, do not create an empty commit.

## Self-Review

Spec coverage:

- Developer prompt intake is covered by Task 12.
- Production credential handling is covered by Task 12 response behavior and Task 9 redaction.
- Playwright exploration is represented by the external runner boundary in Task 11.
- API trace distillation and synthesis are represented by the external runner result contract in Tasks 9 and 11.
- Structured workflow persistence is covered by Task 7.
- Exploration run and trace persistence is covered by Task 10.
- Automatic publishing is covered by Task 12 setting `WorkflowStatusPublished`.
- Deterministic Go HTTP execution is covered by Tasks 4, 5, and 6.
- Invocation audit persistence is covered by Task 7.
- Explicit workflow inspection and invocation APIs are covered by Tasks 8 and 13.
- Natural-language workflow matching remains outside this first slice, matching the design.

Placeholder scan:

- This plan does not use open-ended implementation placeholders.
- Every code-changing task names exact files and includes concrete snippets or full file bodies.
- Every task includes verification commands and expected results.

Type consistency:

- Workflow status type is `workflow.WorkflowStatus`.
- Published status constant is `workflow.WorkflowStatusPublished`.
- Exploration service publishes through `CreateWorkflow`.
- Workflow service invokes through `GetWorkflow` and `RecordInvocation`.
- Route path parameter is consistently `workflow_id`.
