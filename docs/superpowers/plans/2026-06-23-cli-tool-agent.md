# CLI Tool Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first vertical slice for registering trusted local CLI tools and letting a Go/OpenAI agent execute them in a controlled multi-step loop with redacted audit.

**Architecture:** Add `internal/toolcatalog` for registered tool metadata, trusted path validation, and static agent instructions. Add `internal/agent` for OpenAI orchestration, local CLI execution, run-scoped sensitive context, redaction, and audit. The backend exposes HTTP APIs for tool registration, tool listing, instruction update, and synchronous agent runs.

**Tech Stack:** Go 1.25, Fiber v3, pgx v5, PostgreSQL, official OpenAI Go SDK `github.com/openai/openai-go/v3`, standard-library `os/exec`, JSON stdin/stdout CLI protocol.

---

## Scope And Boundary Decisions

This plan implements the accepted spec in `docs/superpowers/specs/2026-06-23-cli-tool-agent-design.md`.

Approved new package boundaries:

- `internal/toolcatalog`: tool registration, trusted command path validation, tool schemas, tool status, static instructions, and related HTTP handlers.
- `internal/agent`: controlled LLM loop, OpenAI client boundary, CLI executor, redaction, run-scoped context, run audit, and related HTTP handlers.

Approved new direct dependency:

- `github.com/openai/openai-go/v3`: used only by `internal/agent` behind a small client wrapper.

Explicitly out of scope:

- MCP exposure.
- WeChat, Teams, WhatsApp, or customer chat connectors.
- Playwright/API auto-discovery.
- Searchable wiki or embedding retrieval.
- Remote workers, queues, containers, or asynchronous polling APIs.

## File Structure

- Modify `scripts/repo-guard.sh`: allow OpenAI SDK dependency and new packages.
- Modify `internal/architecture/architecture_test.go`: allow and protect `internal/toolcatalog` and `internal/agent`.
- Modify `internal/config/config.go`: read OpenAI, tool directory, service account, and agent limit settings.
- Modify `internal/config/config_test.go`: cover default and override behavior for new config.
- Create `internal/toolcatalog/types.go`: typed constants, request/response/domain structs, validation helpers.
- Create `internal/toolcatalog/types_test.go`: validation tests for tool names, schema JSON objects, timeouts, and trusted paths.
- Create `internal/toolcatalog/repository.go`: PostgreSQL schema creation and persistence for tools and instructions.
- Create `internal/toolcatalog/repository_test.go`: nil-pool and SQL-shape tests that do not require a live database.
- Create `internal/toolcatalog/service.go`: register/list tools and update/get instructions.
- Create `internal/toolcatalog/service_test.go`: service tests with in-memory repository fakes.
- Create `internal/toolcatalog/handler.go`: Fiber handlers for `/api/tools` and `/api/agent/instructions`.
- Create `internal/toolcatalog/handler_test.go`: request/response tests.
- Create `internal/agent/types.go`: run/action/step/tool protocol structs and typed statuses.
- Create `internal/agent/redact.go`: secret redaction for maps, JSON-like text, and common token/cookie patterns.
- Create `internal/agent/redact_test.go`: redaction tests.
- Create `internal/agent/context.go`: run-scoped sensitive context store and `ctx://` reference resolution.
- Create `internal/agent/context_test.go`: context reference tests.
- Create `internal/agent/executor.go`: local CLI executor with JSON stdin/stdout, timeout, stderr redaction, and sensitive output conversion.
- Create `internal/agent/executor_test.go`: tests using temporary helper scripts.
- Create `internal/agent/llm.go`: OpenAI client wrapper and fakeable `Planner` interface.
- Create `internal/agent/llm_test.go`: action parsing and wrapper-free planner tests.
- Create `internal/agent/repository.go`: PostgreSQL schema and audit persistence for runs and steps.
- Create `internal/agent/repository_test.go`: nil-pool and SQL-shape tests.
- Create `internal/agent/service.go`: controlled agent loop.
- Create `internal/agent/service_test.go`: fake planner, fake tool catalog, and fake executor tests.
- Create `internal/agent/handler.go`: Fiber handler for `POST /api/agent/runs`.
- Create `internal/agent/handler_test.go`: handler tests.
- Modify `internal/httpapi/router.go`: route toolcatalog and agent handlers.
- Modify `internal/httpapi/router_test.go`: route and CORS method tests.
- Modify `internal/httpapi/middleware.go`: allow `POST` and `PUT`.
- Modify `internal/app/app.go`: wire repositories, services, OpenAI planner, CLI executor, and handlers.
- Modify `internal/app/app_test.go`: keep postgres error wrapping covered.

## API Constants

Use package-owned constants rather than repeated strings:

- `toolcatalog.ToolsPath = "/api/tools"`
- `toolcatalog.AgentInstructionsPath = "/api/agent/instructions"`
- `agent.AgentRunsPath = "/api/agent/runs"`

`internal/httpapi` should import these route constants when registering handlers.

## Task 1: Open Guardrails For Approved Package Boundaries And SDK

**Files:**
- Modify: `scripts/repo-guard.sh`
- Modify: `internal/architecture/architecture_test.go`

- [x] **Step 1: Write failing architecture allowlist expectations**

In `internal/architecture/architecture_test.go`, add the approved packages to `allowedPackages`:

```go
modulePath + "/internal/agent",
modulePath + "/internal/toolcatalog",
```

Add these boundary assertions to `TestPackageBoundaries`:

```go
assertDoesNotImport(t, packages, modulePath+"/internal/toolcatalog", modulePath+"/internal/agent")
assertDoesNotImport(t, packages, modulePath+"/internal/toolcatalog", modulePath+"/internal/httpapi")
assertDoesNotImport(t, packages, modulePath+"/internal/toolcatalog", modulePath+"/internal/platform/postgres")
assertDoesNotImport(t, packages, modulePath+"/internal/agent", modulePath+"/internal/httpapi")
assertDoesNotImport(t, packages, modulePath+"/internal/agent", modulePath+"/internal/platform/postgres")
assertDoesNotImport(t, packages, modulePath+"/internal/platform/postgres", modulePath+"/internal/agent")
assertDoesNotImport(t, packages, modulePath+"/internal/platform/postgres", modulePath+"/internal/toolcatalog")
```

Rationale: domain packages may define repository types that accept pgx interfaces, but platform postgres remains only connectivity and must not import domains. Handlers may live in domain packages and import Fiber, matching the existing `internal/status` pattern.

- [x] **Step 2: Update repo guard package allowlist**

In `scripts/repo-guard.sh`, add these allowed packages:

```bash
  'orderbuddy-ai/backend/internal/agent' \
  'orderbuddy-ai/backend/internal/toolcatalog' \
```

- [x] **Step 3: Allow the OpenAI SDK in repo guard**

In `scripts/repo-guard.sh`, add the direct dependency to `allowed_direct_deps`:

```bash
  'github.com/openai/openai-go/v3' \
```

- [x] **Step 4: Run architecture tests**

Run:

```bash
go test ./internal/architecture
```

Expected: `ok orderbuddy-ai/backend/internal/architecture`.

- [x] **Step 5: Commit**

```bash
git add scripts/repo-guard.sh internal/architecture/architecture_test.go
git commit -m "Allow CLI tool agent boundaries"
```

## Task 2: Add Configuration For Agent Runtime

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [x] **Step 1: Write failing config tests**

Add to `internal/config/config_test.go`:

```go
func TestLoadUsesAgentDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("TRUSTED_TOOL_DIR", "")
	t.Setenv("INTERNAL_REPORT_USERNAME", "")
	t.Setenv("INTERNAL_REPORT_PASSWORD", "")
	t.Setenv("AGENT_MAX_STEPS", "")
	t.Setenv("AGENT_TOTAL_TIMEOUT_MS", "")

	cfg := Load()

	if cfg.OpenAIAPIKey != "" {
		t.Fatalf("OpenAIAPIKey = %q, want empty", cfg.OpenAIAPIKey)
	}
	if cfg.OpenAIModel != DefaultOpenAIModel {
		t.Fatalf("OpenAIModel = %q, want %q", cfg.OpenAIModel, DefaultOpenAIModel)
	}
	if cfg.TrustedToolDir != DefaultTrustedToolDir {
		t.Fatalf("TrustedToolDir = %q, want %q", cfg.TrustedToolDir, DefaultTrustedToolDir)
	}
	if cfg.InternalReportUsername != "" {
		t.Fatalf("InternalReportUsername = %q, want empty", cfg.InternalReportUsername)
	}
	if cfg.InternalReportPassword != "" {
		t.Fatalf("InternalReportPassword = %q, want empty", cfg.InternalReportPassword)
	}
	if cfg.AgentMaxSteps != DefaultAgentMaxSteps {
		t.Fatalf("AgentMaxSteps = %d, want %d", cfg.AgentMaxSteps, DefaultAgentMaxSteps)
	}
	if cfg.AgentTotalTimeoutMS != DefaultAgentTotalTimeoutMS {
		t.Fatalf("AgentTotalTimeoutMS = %d, want %d", cfg.AgentTotalTimeoutMS, DefaultAgentTotalTimeoutMS)
	}
}

func TestLoadUsesAgentEnvironmentOverrides(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("OPENAI_MODEL", "gpt-5-mini")
	t.Setenv("TRUSTED_TOOL_DIR", "/opt/orderbuddy-tools")
	t.Setenv("INTERNAL_REPORT_USERNAME", "svc-user")
	t.Setenv("INTERNAL_REPORT_PASSWORD", "svc-pass")
	t.Setenv("AGENT_MAX_STEPS", "12")
	t.Setenv("AGENT_TOTAL_TIMEOUT_MS", "90000")

	cfg := Load()

	if cfg.OpenAIAPIKey != "sk-test" {
		t.Fatalf("OpenAIAPIKey = %q, want override", cfg.OpenAIAPIKey)
	}
	if cfg.OpenAIModel != "gpt-5-mini" {
		t.Fatalf("OpenAIModel = %q, want override", cfg.OpenAIModel)
	}
	if cfg.TrustedToolDir != "/opt/orderbuddy-tools" {
		t.Fatalf("TrustedToolDir = %q, want override", cfg.TrustedToolDir)
	}
	if cfg.InternalReportUsername != "svc-user" {
		t.Fatalf("InternalReportUsername = %q, want override", cfg.InternalReportUsername)
	}
	if cfg.InternalReportPassword != "svc-pass" {
		t.Fatalf("InternalReportPassword = %q, want override", cfg.InternalReportPassword)
	}
	if cfg.AgentMaxSteps != 12 {
		t.Fatalf("AgentMaxSteps = %d, want 12", cfg.AgentMaxSteps)
	}
	if cfg.AgentTotalTimeoutMS != 90000 {
		t.Fatalf("AgentTotalTimeoutMS = %d, want 90000", cfg.AgentTotalTimeoutMS)
	}
}

func TestLoadFallsBackForInvalidAgentNumbers(t *testing.T) {
	t.Setenv("AGENT_MAX_STEPS", "invalid")
	t.Setenv("AGENT_TOTAL_TIMEOUT_MS", "-1")

	cfg := Load()

	if cfg.AgentMaxSteps != DefaultAgentMaxSteps {
		t.Fatalf("AgentMaxSteps = %d, want default", cfg.AgentMaxSteps)
	}
	if cfg.AgentTotalTimeoutMS != DefaultAgentTotalTimeoutMS {
		t.Fatalf("AgentTotalTimeoutMS = %d, want default", cfg.AgentTotalTimeoutMS)
	}
}
```

- [x] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/config
```

Expected: fail because the new config fields and constants do not exist.

- [x] **Step 3: Implement config fields and parsing**

Update `internal/config/config.go`:

```go
package config

import (
	"os"
	"strconv"
)

const (
	DefaultAppEnv              = "development"
	DefaultHTTPAddr            = ":8080"
	DefaultDatabaseURL         = "postgres://orderbuddy_ai:orderbuddy_ai@localhost:5432/orderbuddy_ai?sslmode=disable"
	DefaultOpenAIModel         = "gpt-5-mini"
	DefaultTrustedToolDir      = "./tools"
	DefaultAgentMaxSteps       = 8
	DefaultAgentTotalTimeoutMS = 60000
)

type Config struct {
	AppEnv                 string
	HTTPAddr               string
	DatabaseURL            string
	OpenAIAPIKey           string
	OpenAIModel            string
	TrustedToolDir         string
	InternalReportUsername string
	InternalReportPassword string
	AgentMaxSteps          int
	AgentTotalTimeoutMS    int
}

func Load() Config {
	return Config{
		AppEnv:                 getEnv("APP_ENV", DefaultAppEnv),
		HTTPAddr:               getEnv("HTTP_ADDR", DefaultHTTPAddr),
		DatabaseURL:            getEnv("DATABASE_URL", DefaultDatabaseURL),
		OpenAIAPIKey:           getEnv("OPENAI_API_KEY", ""),
		OpenAIModel:            getEnv("OPENAI_MODEL", DefaultOpenAIModel),
		TrustedToolDir:         getEnv("TRUSTED_TOOL_DIR", DefaultTrustedToolDir),
		InternalReportUsername: getEnv("INTERNAL_REPORT_USERNAME", ""),
		InternalReportPassword: getEnv("INTERNAL_REPORT_PASSWORD", ""),
		AgentMaxSteps:          getPositiveIntEnv("AGENT_MAX_STEPS", DefaultAgentMaxSteps),
		AgentTotalTimeoutMS:    getPositiveIntEnv("AGENT_TOTAL_TIMEOUT_MS", DefaultAgentTotalTimeoutMS),
	}
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}

func getPositiveIntEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}
```

- [x] **Step 4: Run config tests**

Run:

```bash
go test ./internal/config
```

Expected: pass.

- [x] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "Add agent runtime configuration"
```

## Task 3: Define Tool Catalog Types And Validation

**Files:**
- Create: `internal/toolcatalog/types.go`
- Create: `internal/toolcatalog/types_test.go`

- [x] **Step 1: Write failing validation tests**

Create `internal/toolcatalog/types_test.go`:

```go
package toolcatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestToolValidateAcceptsTrustedCommand(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "login_internal_site")
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := Tool{
		Name:        "login_internal_site",
		Description: "Login to internal site.",
		CommandPath: commandPath,
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		TimeoutMS:   10000,
		Status:      ToolStatusEnabled,
	}

	if err := tool.Validate(dir); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestToolValidateRejectsInvalidName(t *testing.T) {
	tool := Tool{
		Name:         "Login Tool",
		Description:  "Login.",
		CommandPath:  "/tmp/login",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		TimeoutMS:    1000,
	}

	if err := tool.Validate("/tmp"); err == nil {
		t.Fatal("Validate() error = nil, want invalid name error")
	}
}

func TestToolValidateRejectsPathOutsideTrustedDir(t *testing.T) {
	trustedDir := t.TempDir()
	outsideDir := t.TempDir()
	commandPath := filepath.Join(outsideDir, "tool")
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := Tool{
		Name:         "export_report",
		Description:  "Export report.",
		CommandPath:  commandPath,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		TimeoutMS:    1000,
	}

	if err := tool.Validate(trustedDir); err == nil {
		t.Fatal("Validate() error = nil, want trusted directory error")
	}
}

func TestToolValidateRejectsNonObjectSchema(t *testing.T) {
	tool := Tool{
		Name:         "export_report",
		Description:  "Export report.",
		CommandPath:  "/tmp/export_report",
		InputSchema:  json.RawMessage(`[]`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		TimeoutMS:    1000,
	}

	if err := tool.Validate("/tmp"); err == nil {
		t.Fatal("Validate() error = nil, want schema error")
	}
}

func TestToolValidateDefaultsStatusToEnabled(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "export_report")
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := Tool{
		Name:         "export_report",
		Description:  "Export report.",
		CommandPath:  commandPath,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		TimeoutMS:    1000,
	}

	if err := tool.Validate(dir); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if tool.NormalizedStatus() != ToolStatusEnabled {
		t.Fatalf("NormalizedStatus() = %q, want %q", tool.NormalizedStatus(), ToolStatusEnabled)
	}
}
```

- [x] **Step 2: Run test to verify failure**

Run:

```bash
go test ./internal/toolcatalog
```

Expected: fail because the package has no implementation.

- [x] **Step 3: Implement tool catalog types**

Create `internal/toolcatalog/types.go`:

```go
package toolcatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	ToolsPath             = "/api/tools"
	AgentInstructionsPath = "/api/agent/instructions"
)

const (
	ToolStatusEnabled  ToolStatus = "enabled"
	ToolStatusDisabled ToolStatus = "disabled"
)

var (
	ErrInvalidTool          = errors.New("invalid tool")
	ErrToolNotFound         = errors.New("tool not found")
	ErrDuplicateToolName    = errors.New("duplicate tool name")
	ErrInstructionsNotFound = errors.New("agent instructions not found")
)

var toolNamePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

type ToolStatus string

type Tool struct {
	ID                     string          `json:"id"`
	Name                   string          `json:"name"`
	Description            string          `json:"description"`
	CommandPath            string          `json:"command_path"`
	InputSchema            json.RawMessage `json:"input_schema"`
	OutputSchema           json.RawMessage `json:"output_schema"`
	TimeoutMS              int             `json:"timeout_ms"`
	RequiresServiceAccount bool            `json:"requires_service_account"`
	Status                 ToolStatus      `json:"status"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

type RegisterToolRequest struct {
	Name                   string          `json:"name"`
	Description            string          `json:"description"`
	CommandPath            string          `json:"command_path"`
	InputSchema            json.RawMessage `json:"input_schema"`
	OutputSchema           json.RawMessage `json:"output_schema"`
	TimeoutMS              int             `json:"timeout_ms"`
	RequiresServiceAccount bool            `json:"requires_service_account"`
}

type Instructions struct {
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateInstructionsRequest struct {
	Content string `json:"content"`
}

func (request RegisterToolRequest) Tool() Tool {
	return Tool{
		Name:                   strings.TrimSpace(request.Name),
		Description:            strings.TrimSpace(request.Description),
		CommandPath:            strings.TrimSpace(request.CommandPath),
		InputSchema:            request.InputSchema,
		OutputSchema:           request.OutputSchema,
		TimeoutMS:              request.TimeoutMS,
		RequiresServiceAccount: request.RequiresServiceAccount,
		Status:                 ToolStatusEnabled,
	}
}

func (tool Tool) Validate(trustedToolDir string) error {
	if !toolNamePattern.MatchString(tool.Name) {
		return fmt.Errorf("%w: name must use lowercase letters, numbers, and underscores", ErrInvalidTool)
	}
	if strings.TrimSpace(tool.Description) == "" {
		return fmt.Errorf("%w: description is required", ErrInvalidTool)
	}
	if tool.TimeoutMS <= 0 {
		return fmt.Errorf("%w: timeout_ms must be positive", ErrInvalidTool)
	}
	if err := validateJSONObject(tool.InputSchema); err != nil {
		return fmt.Errorf("%w: input_schema must be a JSON object", ErrInvalidTool)
	}
	if err := validateJSONObject(tool.OutputSchema); err != nil {
		return fmt.Errorf("%w: output_schema must be a JSON object", ErrInvalidTool)
	}
	if tool.NormalizedStatus() != ToolStatusEnabled && tool.NormalizedStatus() != ToolStatusDisabled {
		return fmt.Errorf("%w: status is invalid", ErrInvalidTool)
	}
	if err := validateTrustedCommandPath(tool.CommandPath, trustedToolDir); err != nil {
		return err
	}

	return nil
}

func (tool Tool) NormalizedStatus() ToolStatus {
	if tool.Status == "" {
		return ToolStatusEnabled
	}

	return tool.Status
}

func validateJSONObject(raw json.RawMessage) error {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if value == nil {
		return errors.New("schema is not an object")
	}

	return nil
}

func validateTrustedCommandPath(commandPath string, trustedToolDir string) error {
	if strings.TrimSpace(commandPath) == "" {
		return fmt.Errorf("%w: command_path is required", ErrInvalidTool)
	}
	if strings.TrimSpace(trustedToolDir) == "" {
		return fmt.Errorf("%w: trusted tool directory is required", ErrInvalidTool)
	}

	absoluteTrustedDir, err := filepath.Abs(trustedToolDir)
	if err != nil {
		return fmt.Errorf("%w: resolve trusted tool directory: %v", ErrInvalidTool, err)
	}
	absoluteCommandPath, err := filepath.Abs(commandPath)
	if err != nil {
		return fmt.Errorf("%w: resolve command path: %v", ErrInvalidTool, err)
	}

	relativePath, err := filepath.Rel(absoluteTrustedDir, absoluteCommandPath)
	if err != nil {
		return fmt.Errorf("%w: compare command path: %v", ErrInvalidTool, err)
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)) || filepath.IsAbs(relativePath) {
		return fmt.Errorf("%w: command_path must be inside trusted tool directory", ErrInvalidTool)
	}

	info, err := os.Stat(absoluteCommandPath)
	if err != nil {
		return fmt.Errorf("%w: command_path must exist", ErrInvalidTool)
	}
	if info.IsDir() {
		return fmt.Errorf("%w: command_path must be a file", ErrInvalidTool)
	}

	return nil
}
```

- [x] **Step 4: Run tests**

Run:

```bash
go test ./internal/toolcatalog
```

Expected: pass.

- [x] **Step 5: Commit**

```bash
git add internal/toolcatalog/types.go internal/toolcatalog/types_test.go
git commit -m "Add tool catalog validation"
```

## Task 4: Add Tool Catalog Repository And Service

**Files:**
- Create: `internal/toolcatalog/repository.go`
- Create: `internal/toolcatalog/repository_test.go`
- Create: `internal/toolcatalog/service.go`
- Create: `internal/toolcatalog/service_test.go`

Implementation note: the final service implementation uses an unexported function-backed store and `NewService(repository Repository, trustedToolDir string)` instead of a four-method `Store` interface, because `repo-guard` rejects interfaces with more than three methods.

- [x] **Step 1: Write repository tests**

Create `internal/toolcatalog/repository_test.go`:

```go
package toolcatalog

import (
	"context"
	"strings"
	"testing"
)

func TestRepositoryCreateSchemaRequiresPool(t *testing.T) {
	repository := NewRepository(nil)

	err := repository.CreateSchema(context.Background())

	if err == nil {
		t.Fatal("CreateSchema() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "tool catalog database is missing") {
		t.Fatalf("CreateSchema() error = %q, want missing database context", err)
	}
}

func TestRepositorySchemaContainsTables(t *testing.T) {
	schema := schemaSQL()

	for _, table := range []string{"tools", "agent_instructions"} {
		if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("schema missing table %q", table)
		}
	}
}
```

- [x] **Step 2: Write service tests**

Create `internal/toolcatalog/service_test.go`:

```go
package toolcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type memoryRepository struct {
	tools        []Tool
	instructions Instructions
}

func (repository *memoryRepository) SaveTool(_ context.Context, tool Tool) (Tool, error) {
	for _, existing := range repository.tools {
		if existing.Name == tool.Name {
			return Tool{}, ErrDuplicateToolName
		}
	}
	tool.ID = "tool_1"
	repository.tools = append(repository.tools, tool)
	return tool, nil
}

func (repository *memoryRepository) ListEnabledTools(_ context.Context) ([]Tool, error) {
	var enabled []Tool
	for _, tool := range repository.tools {
		if tool.NormalizedStatus() == ToolStatusEnabled {
			enabled = append(enabled, tool)
		}
	}
	return enabled, nil
}

func (repository *memoryRepository) UpdateInstructions(_ context.Context, instructions Instructions) (Instructions, error) {
	repository.instructions = instructions
	return instructions, nil
}

func (repository *memoryRepository) GetInstructions(_ context.Context) (Instructions, error) {
	if repository.instructions.Content == "" {
		return Instructions{}, ErrInstructionsNotFound
	}
	return repository.instructions, nil
}

func TestServiceRegisterToolValidatesAndSaves(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "export_report")
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	service := NewService(&memoryRepository{}, dir)
	tool, err := service.RegisterTool(context.Background(), RegisterToolRequest{
		Name:         "export_report",
		Description:  "Export a partner report.",
		CommandPath:  commandPath,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		TimeoutMS:    1000,
	})

	if err != nil {
		t.Fatalf("RegisterTool() error = %v", err)
	}
	if tool.Name != "export_report" {
		t.Fatalf("tool.Name = %q, want export_report", tool.Name)
	}
}

func TestServiceRegisterToolRejectsInvalidTool(t *testing.T) {
	service := NewService(&memoryRepository{}, t.TempDir())

	_, err := service.RegisterTool(context.Background(), RegisterToolRequest{
		Name: "Bad Name",
	})

	if !errors.Is(err, ErrInvalidTool) {
		t.Fatalf("RegisterTool() error = %v, want ErrInvalidTool", err)
	}
}

func TestServiceUpdateAndGetInstructions(t *testing.T) {
	service := NewService(&memoryRepository{}, t.TempDir())

	updated, err := service.UpdateInstructions(context.Background(), UpdateInstructionsRequest{Content: "Use tools carefully."})
	if err != nil {
		t.Fatalf("UpdateInstructions() error = %v", err)
	}
	if updated.Content != "Use tools carefully." {
		t.Fatalf("updated.Content = %q, want content", updated.Content)
	}

	got, err := service.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions() error = %v", err)
	}
	if got.Content != "Use tools carefully." {
		t.Fatalf("got.Content = %q, want content", got.Content)
	}
}

func TestServiceGetInstructionsReturnsEmptyWhenMissing(t *testing.T) {
	service := NewService(&memoryRepository{}, t.TempDir())

	instructions, err := service.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions() error = %v", err)
	}
	if instructions.Content != "" {
		t.Fatalf("instructions.Content = %q, want empty", instructions.Content)
	}
}
```

- [x] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/toolcatalog
```

Expected: fail because repository and service types do not exist.

- [x] **Step 4: Implement repository**

Create `internal/toolcatalog/repository.go`:

```go
package toolcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const uniqueViolationCode = "23505"

var ErrDatabaseMissing = errors.New("tool catalog database is missing")

type database interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	database database
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return Repository{database: pool}
}

func (repository Repository) CreateSchema(ctx context.Context) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}
	if _, err := repository.database.Exec(ctx, schemaSQL()); err != nil {
		return fmt.Errorf("create tool catalog schema: %w", err)
	}

	return nil
}

func (repository Repository) SaveTool(ctx context.Context, tool Tool) (Tool, error) {
	if repository.database == nil {
		return Tool{}, ErrDatabaseMissing
	}

	row := repository.database.QueryRow(ctx, `
INSERT INTO tools (name, description, command_path, input_schema, output_schema, timeout_ms, requires_service_account, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id::text, name, description, command_path, input_schema, output_schema, timeout_ms, requires_service_account, status, created_at, updated_at
`,
		tool.Name,
		tool.Description,
		tool.CommandPath,
		[]byte(tool.InputSchema),
		[]byte(tool.OutputSchema),
		tool.TimeoutMS,
		tool.RequiresServiceAccount,
		tool.NormalizedStatus(),
	)

	saved, err := scanTool(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return Tool{}, ErrDuplicateToolName
		}
		return Tool{}, fmt.Errorf("save tool: %w", err)
	}

	return saved, nil
}

func (repository Repository) ListEnabledTools(ctx context.Context) ([]Tool, error) {
	if repository.database == nil {
		return nil, ErrDatabaseMissing
	}

	rows, err := repository.database.Query(ctx, `
SELECT id::text, name, description, command_path, input_schema, output_schema, timeout_ms, requires_service_account, status, created_at, updated_at
FROM tools
WHERE status = $1
ORDER BY name
`, ToolStatusEnabled)
	if err != nil {
		return nil, fmt.Errorf("list enabled tools: %w", err)
	}
	defer rows.Close()

	var tools []Tool
	for rows.Next() {
		tool, err := scanTool(rows)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tools: %w", err)
	}

	return tools, nil
}

func (repository Repository) UpdateInstructions(ctx context.Context, instructions Instructions) (Instructions, error) {
	if repository.database == nil {
		return Instructions{}, ErrDatabaseMissing
	}

	row := repository.database.QueryRow(ctx, `
INSERT INTO agent_instructions (id, content)
VALUES (1, $1)
ON CONFLICT (id)
DO UPDATE SET content = EXCLUDED.content, updated_at = now()
RETURNING content, updated_at
`, instructions.Content)

	var saved Instructions
	if err := row.Scan(&saved.Content, &saved.UpdatedAt); err != nil {
		return Instructions{}, fmt.Errorf("update agent instructions: %w", err)
	}

	return saved, nil
}

func (repository Repository) GetInstructions(ctx context.Context) (Instructions, error) {
	if repository.database == nil {
		return Instructions{}, ErrDatabaseMissing
	}

	row := repository.database.QueryRow(ctx, `SELECT content, updated_at FROM agent_instructions WHERE id = 1`)

	var instructions Instructions
	if err := row.Scan(&instructions.Content, &instructions.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Instructions{}, ErrInstructionsNotFound
		}
		return Instructions{}, fmt.Errorf("get agent instructions: %w", err)
	}

	return instructions, nil
}

func scanTool(row pgx.Row) (Tool, error) {
	var tool Tool
	var inputSchema []byte
	var outputSchema []byte
	if err := row.Scan(
		&tool.ID,
		&tool.Name,
		&tool.Description,
		&tool.CommandPath,
		&inputSchema,
		&outputSchema,
		&tool.TimeoutMS,
		&tool.RequiresServiceAccount,
		&tool.Status,
		&tool.CreatedAt,
		&tool.UpdatedAt,
	); err != nil {
		return Tool{}, err
	}
	tool.InputSchema = json.RawMessage(inputSchema)
	tool.OutputSchema = json.RawMessage(outputSchema)

	return tool, nil
}

func schemaSQL() string {
	return `
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS tools (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	name text NOT NULL UNIQUE,
	description text NOT NULL,
	command_path text NOT NULL,
	input_schema jsonb NOT NULL,
	output_schema jsonb NOT NULL,
	timeout_ms integer NOT NULL,
	requires_service_account boolean NOT NULL DEFAULT false,
	status text NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_instructions (
	id integer PRIMARY KEY,
	content text NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now()
);
`
}
```

- [x] **Step 5: Implement service**

Create `internal/toolcatalog/service.go`:

```go
package toolcatalog

import (
	"context"
	"errors"
	"strings"
)

type Store interface {
	SaveTool(ctx context.Context, tool Tool) (Tool, error)
	ListEnabledTools(ctx context.Context) ([]Tool, error)
	UpdateInstructions(ctx context.Context, instructions Instructions) (Instructions, error)
	GetInstructions(ctx context.Context) (Instructions, error)
}

type Service struct {
	store          Store
	trustedToolDir string
}

func NewService(store Store, trustedToolDir string) Service {
	return Service{store: store, trustedToolDir: trustedToolDir}
}

func (service Service) RegisterTool(ctx context.Context, request RegisterToolRequest) (Tool, error) {
	tool := request.Tool()
	if err := tool.Validate(service.trustedToolDir); err != nil {
		return Tool{}, err
	}

	return service.store.SaveTool(ctx, tool)
}

func (service Service) ListEnabledTools(ctx context.Context) ([]Tool, error) {
	return service.store.ListEnabledTools(ctx)
}

func (service Service) UpdateInstructions(ctx context.Context, request UpdateInstructionsRequest) (Instructions, error) {
	return service.store.UpdateInstructions(ctx, Instructions{Content: strings.TrimSpace(request.Content)})
}

func (service Service) GetInstructions(ctx context.Context) (Instructions, error) {
	instructions, err := service.store.GetInstructions(ctx)
	if errors.Is(err, ErrInstructionsNotFound) {
		return Instructions{}, nil
	}

	return instructions, err
}
```

- [x] **Step 6: Run tests**

Run:

```bash
go test ./internal/toolcatalog
```

Expected: pass.

- [x] **Step 7: Commit**

```bash
git add internal/toolcatalog/repository.go internal/toolcatalog/repository_test.go internal/toolcatalog/service.go internal/toolcatalog/service_test.go
git commit -m "Add tool catalog storage"
```

## Task 5: Add Tool Catalog HTTP Handlers

**Files:**
- Create: `internal/toolcatalog/handler.go`
- Create: `internal/toolcatalog/handler_test.go`

Implementation note: handler tests use `newService(storeFromMemoryRepository(...), trustedToolDir)` because Task 4 replaced the planned four-method `Store` interface with a function-backed store to satisfy `repo-guard`.

- [x] **Step 1: Write handler tests**

Create `internal/toolcatalog/handler_test.go`:

```go
package toolcatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestHandlerRegisterToolReturnsCreated(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "export_report")
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	handler := NewHandler(NewService(&memoryRepository{}, dir))
	app := fiber.New()
	app.Post(ToolsPath, handler.RegisterTool)

	body := []byte(`{
		"name":"export_report",
		"description":"Export report.",
		"command_path":"` + commandPath + `",
		"input_schema":{"type":"object"},
		"output_schema":{"type":"object"},
		"timeout_ms":1000,
		"requires_service_account":true
	}`)
	req, err := http.NewRequest(http.MethodPost, ToolsPath, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var tool Tool
	if err := json.NewDecoder(resp.Body).Decode(&tool); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if tool.Name != "export_report" {
		t.Fatalf("tool.Name = %q, want export_report", tool.Name)
	}
}

func TestHandlerRegisterToolRejectsBadJSON(t *testing.T) {
	handler := NewHandler(NewService(&memoryRepository{}, t.TempDir()))
	app := fiber.New()
	app.Post(ToolsPath, handler.RegisterTool)

	req, err := http.NewRequest(http.MethodPost, ToolsPath, bytes.NewReader([]byte(`{`)))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandlerListToolsReturnsTools(t *testing.T) {
	repository := &memoryRepository{}
	service := NewService(repository, t.TempDir())
	repository.tools = []Tool{{Name: "export_report", Description: "Export.", Status: ToolStatusEnabled}}
	handler := NewHandler(service)
	app := fiber.New()
	app.Get(ToolsPath, handler.ListTools)

	req, err := http.NewRequest(http.MethodGet, ToolsPath, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandlerUpdateInstructionsReturnsUpdatedInstructions(t *testing.T) {
	handler := NewHandler(NewService(&memoryRepository{}, t.TempDir()))
	app := fiber.New()
	app.Put(AgentInstructionsPath, handler.UpdateInstructions)

	req, err := http.NewRequest(http.MethodPut, AgentInstructionsPath, bytes.NewReader([]byte(`{"content":"Use report tools."}`)))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

var _ Store = (*memoryRepository)(nil)

func TestMemoryRepositoryImplementsStore(_ *testing.T) {
	_ = context.Background()
}
```

- [x] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/toolcatalog
```

Expected: fail because `Handler` does not exist.

- [x] **Step 3: Implement handler**

Create `internal/toolcatalog/handler.go`:

```go
package toolcatalog

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v3"
)

const errorField = "error"

type Handler struct {
	service Service
}

func NewHandler(service Service) Handler {
	return Handler{service: service}
}

func (handler Handler) RegisterTool(c fiber.Ctx) error {
	var request RegisterToolRequest
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: "invalid JSON request body"})
	}

	tool, err := handler.service.RegisterTool(c.Context(), request)
	if err != nil {
		if errors.Is(err, ErrInvalidTool) {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: err.Error()})
		}
		if errors.Is(err, ErrDuplicateToolName) {
			return c.Status(http.StatusConflict).JSON(fiber.Map{errorField: err.Error()})
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{errorField: "register tool failed"})
	}

	return c.Status(http.StatusCreated).JSON(tool)
}

func (handler Handler) ListTools(c fiber.Ctx) error {
	tools, err := handler.service.ListEnabledTools(c.Context())
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{errorField: "list tools failed"})
	}

	return c.Status(http.StatusOK).JSON(tools)
}

func (handler Handler) UpdateInstructions(c fiber.Ctx) error {
	var request UpdateInstructionsRequest
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: "invalid JSON request body"})
	}

	instructions, err := handler.service.UpdateInstructions(c.Context(), request)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{errorField: "update instructions failed"})
	}

	return c.Status(http.StatusOK).JSON(instructions)
}
```

- [x] **Step 4: Run tests**

Run:

```bash
go test ./internal/toolcatalog
```

Expected: pass.

- [x] **Step 5: Commit**

```bash
git add internal/toolcatalog/handler.go internal/toolcatalog/handler_test.go
git commit -m "Add tool catalog HTTP handlers"
```

## Task 6: Define Agent Types, Redaction, And Run Context

**Files:**
- Create: `internal/agent/types.go`
- Create: `internal/agent/redact.go`
- Create: `internal/agent/redact_test.go`
- Create: `internal/agent/context.go`
- Create: `internal/agent/context_test.go`

Implementation note: the final redaction/context implementation avoids `ireturn` by making `RedactJSONValue` generic and returning a concrete `ContextValue` from `RunContext.Resolve`.

- [x] **Step 1: Write redaction tests**

Create `internal/agent/redact_test.go`:

```go
package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactTextRemovesBearerTokensAndCookies(t *testing.T) {
	input := "Authorization: Bearer abc.def.ghi\nCookie: session_id=secret-value; theme=light"

	redacted := RedactText(input)

	if strings.Contains(redacted, "abc.def.ghi") || strings.Contains(redacted, "secret-value") {
		t.Fatalf("RedactText() = %q, want secrets removed", redacted)
	}
}

func TestRedactJSONValueRemovesSensitiveKeys(t *testing.T) {
	value := map[string]any{
		"password": "secret",
		"nested": map[string]any{
			"token": "abc",
			"name":  "Acme",
		},
	}

	redacted := RedactJSONValue(value)
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := string(encoded)

	if strings.Contains(text, "secret") || strings.Contains(text, "abc") {
		t.Fatalf("RedactJSONValue() = %s, want secrets removed", text)
	}
	if !strings.Contains(text, "Acme") {
		t.Fatalf("RedactJSONValue() = %s, want non-sensitive value preserved", text)
	}
}
```

- [x] **Step 2: Write context tests**

Create `internal/agent/context_test.go`:

```go
package agent

import "testing"

func TestRunContextStoresSensitiveValuesAsReferences(t *testing.T) {
	ctx := NewRunContext()

	ref := ctx.Store("login", "session", "cookie-value")

	if ref != "ctx://login/session" {
		t.Fatalf("ref = %q, want ctx://login/session", ref)
	}

	value, ok := ctx.Resolve(ref)
	if !ok {
		t.Fatal("Resolve() ok = false, want true")
	}
	if value != "cookie-value" {
		t.Fatalf("Resolve() value = %q, want cookie-value", value)
	}
}

func TestRunContextResolveLeavesPlainValuesUnresolved(t *testing.T) {
	ctx := NewRunContext()

	_, ok := ctx.Resolve("plain-value")

	if ok {
		t.Fatal("Resolve() ok = true, want false")
	}
}
```

- [x] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/agent
```

Expected: fail because the package has no implementation.

- [x] **Step 4: Implement agent types**

Create `internal/agent/types.go`:

```go
package agent

import (
	"encoding/json"
	"time"
)

const AgentRunsPath = "/api/agent/runs"

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
)

const (
	StepStatusSucceeded StepStatus = "succeeded"
	StepStatusFailed    StepStatus = "failed"
)

const (
	ToolResultStatusOK    ToolResultStatus = "ok"
	ToolResultStatusError ToolResultStatus = "error"
)

const (
	ActionTypeCallTool    ActionType = "call_tool"
	ActionTypeFinalAnswer ActionType = "final_answer"
)

type RunStatus string
type StepStatus string
type ToolResultStatus string
type ActionType string

type CreateRunRequest struct {
	Message string `json:"message"`
}

type RunResponse struct {
	RunID   string         `json:"run_id"`
	Status  RunStatus      `json:"status"`
	Answer  string         `json:"answer,omitempty"`
	Outputs map[string]any `json:"outputs,omitempty"`
	Error   string         `json:"error,omitempty"`
}

type Run struct {
	ID          string
	Message     string
	Status      RunStatus
	Answer      string
	Outputs     map[string]any
	ErrorSummary string
	StartedAt    time.Time
	FinishedAt   time.Time
}

type StepRecord struct {
	ID            string
	RunID         string
	StepOrder     int
	ToolName      string
	InputSummary  json.RawMessage
	OutputSummary json.RawMessage
	DurationMS    int64
	Status        StepStatus
	ErrorSummary  string
	CreatedAt     time.Time
}

type PlannerAction struct {
	Type    ActionType       `json:"type"`
	Tool    string           `json:"tool,omitempty"`
	Inputs  json.RawMessage  `json:"inputs,omitempty"`
	Answer  string           `json:"answer,omitempty"`
	Outputs map[string]any   `json:"outputs,omitempty"`
}

type Observation struct {
	StepOrder int            `json:"step_order"`
	ToolName  string         `json:"tool_name"`
	Status    StepStatus     `json:"status"`
	Outputs   map[string]any `json:"outputs,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type ToolInputEnvelope struct {
	RunID          string         `json:"run_id"`
	StepID         string         `json:"step_id"`
	Inputs         map[string]any `json:"inputs"`
	Context        map[string]any `json:"context"`
	ServiceAccount ServiceAccount `json:"service_account,omitempty"`
}

type ServiceAccount struct {
	Profile  string `json:"profile"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type ToolResult struct {
	Status  ToolResultStatus       `json:"status"`
	Outputs map[string]ToolOutput  `json:"outputs,omitempty"`
	Summary string                 `json:"summary,omitempty"`
	Error   *ToolError             `json:"error,omitempty"`
}

type ToolOutput struct {
	Sensitive bool `json:"sensitive"`
	Value     any  `json:"value"`
}

type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
```

- [x] **Step 5: Implement redaction**

Create `internal/agent/redact.go`:

```go
package agent

import (
	"regexp"
	"strings"
)

const redactedValue = "[REDACTED]"

var sensitiveKeyFragments = []string{
	"authorization",
	"cookie",
	"credential",
	"key",
	"password",
	"secret",
	"token",
}

var bearerPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
var cookiePairPattern = regexp.MustCompile(`(?i)(cookie:\s*)[^\n\r]+`)

func RedactText(value string) string {
	redacted := bearerPattern.ReplaceAllString(value, "Bearer "+redactedValue)
	redacted = cookiePairPattern.ReplaceAllString(redacted, "${1}"+redactedValue)
	return redacted
}

func RedactJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, nested := range typed {
			if isSensitiveKey(key) {
				redacted[key] = redactedValue
				continue
			}
			redacted[key] = RedactJSONValue(nested)
		}
		return redacted
	case []any:
		redacted := make([]any, 0, len(typed))
		for _, nested := range typed {
			redacted = append(redacted, RedactJSONValue(nested))
		}
		return redacted
	case string:
		return RedactText(typed)
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}

	return false
}
```

- [x] **Step 6: Implement run context**

Create `internal/agent/context.go`:

```go
package agent

import (
	"strings"
	"sync"
)

const contextReferencePrefix = "ctx://"

type RunContext struct {
	mutex  sync.RWMutex
	values map[string]any
}

func NewRunContext() *RunContext {
	return &RunContext{values: make(map[string]any)}
}

func (context *RunContext) Store(stepName string, outputName string, value any) string {
	ref := contextReferencePrefix + stepName + "/" + outputName
	context.mutex.Lock()
	defer context.mutex.Unlock()
	context.values[ref] = value
	return ref
}

func (context *RunContext) Resolve(value string) (any, bool) {
	if !strings.HasPrefix(value, contextReferencePrefix) {
		return nil, false
	}

	context.mutex.RLock()
	defer context.mutex.RUnlock()
	resolved, ok := context.values[value]
	return resolved, ok
}
```

- [x] **Step 7: Run tests**

Run:

```bash
go test ./internal/agent
```

Expected: pass.

- [x] **Step 8: Commit**

```bash
git add internal/agent/types.go internal/agent/redact.go internal/agent/redact_test.go internal/agent/context.go internal/agent/context_test.go
git commit -m "Add agent redaction and context"
```

## Task 7: Add CLI Executor

**Files:**
- Create: `internal/agent/executor.go`
- Create: `internal/agent/executor_test.go`

- [ ] **Step 1: Write executor tests**

Create `internal/agent/executor_test.go`:

```go
package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orderbuddy-ai/backend/internal/toolcatalog"
)

func TestCLIExecutorReturnsObservationAndStoresSensitiveOutput(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "tool.sh")
	script := `#!/usr/bin/env sh
cat >/dev/null
printf '%s\n' '{"status":"ok","outputs":{"session":{"sensitive":true,"value":"cookie-value"},"partner_id":{"sensitive":false,"value":"p_123"}},"summary":"done"}'
`
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	executor := NewCLIExecutor(ServiceAccount{Profile: "internal_report_service", Username: "svc", Password: "secret"})
	runContext := NewRunContext()
	observation, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      "run_1",
		StepID:     "step_1",
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "login", CommandPath: commandPath, TimeoutMS: 1000, RequiresServiceAccount: true},
		Inputs:     map[string]any{},
		RunContext: runContext,
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if observation.Outputs["session"] != "ctx://login/session" {
		t.Fatalf("session output = %v, want ctx ref", observation.Outputs["session"])
	}
	if observation.Outputs["partner_id"] != "p_123" {
		t.Fatalf("partner_id output = %v, want p_123", observation.Outputs["partner_id"])
	}
	if resolved, ok := runContext.Resolve("ctx://login/session"); !ok || resolved != "cookie-value" {
		t.Fatalf("resolved session = %v, %v; want cookie-value, true", resolved, ok)
	}
}

func TestCLIExecutorFailsOnInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "tool.sh")
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\nprintf 'not-json'\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	executor := NewCLIExecutor(ServiceAccount{})
	_, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      "run_1",
		StepID:     "step_1",
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "bad", CommandPath: commandPath, TimeoutMS: 1000},
		RunContext: NewRunContext(),
	})

	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
}

func TestCLIExecutorReturnsFailedObservationForToolBusinessError(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "tool.sh")
	script := `#!/usr/bin/env sh
printf '%s\n' '{"status":"error","error":{"code":"partner_not_found","message":"No partner matched token abc.def.ghi"}}'
`
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	executor := NewCLIExecutor(ServiceAccount{})
	observation, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      "run_1",
		StepID:     "step_1",
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "find_partner", CommandPath: commandPath, TimeoutMS: 1000},
		RunContext: NewRunContext(),
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if observation.Status != StepStatusFailed {
		t.Fatalf("Status = %q, want failed", observation.Status)
	}
	if strings.Contains(observation.Error, "abc.def.ghi") {
		t.Fatalf("observation.Error = %q, want redacted token", observation.Error)
	}
}

func TestCLIExecutorRedactsStderrOnFailure(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "tool.sh")
	script := "#!/usr/bin/env sh\necho 'Authorization: Bearer secret-token' >&2\nexit 2\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	executor := NewCLIExecutor(ServiceAccount{})
	_, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      "run_1",
		StepID:     "step_1",
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "fail", CommandPath: commandPath, TimeoutMS: 1000},
		RunContext: NewRunContext(),
	})

	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("Execute() error = %q, want redacted token", err)
	}
}

func TestCLIExecutorTimesOut(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "tool.sh")
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\nsleep 2\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	executor := NewCLIExecutor(ServiceAccount{})
	_, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      "run_1",
		StepID:     "step_1",
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "slow", CommandPath: commandPath, TimeoutMS: 10},
		RunContext: NewRunContext(),
	})

	if err == nil {
		t.Fatal("Execute() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Execute() error = %q, want timeout context", err)
	}

	_ = time.Second
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/agent
```

Expected: fail because executor types do not exist.

- [ ] **Step 3: Implement CLI executor**

Create `internal/agent/executor.go`:

```go
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"orderbuddy-ai/backend/internal/toolcatalog"
)

var ErrToolExecutionFailed = errors.New("tool execution failed")

type ExecuteRequest struct {
	RunID      string
	StepID     string
	StepOrder  int
	Tool       toolcatalog.Tool
	Inputs     map[string]any
	RunContext *RunContext
}

type CLIExecutor struct {
	serviceAccount ServiceAccount
}

func NewCLIExecutor(serviceAccount ServiceAccount) CLIExecutor {
	return CLIExecutor{serviceAccount: serviceAccount}
}

func (executor CLIExecutor) Execute(parent context.Context, request ExecuteRequest) (Observation, error) {
	timeout := time.Duration(request.Tool.TimeoutMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	envelope := ToolInputEnvelope{
		RunID:   request.RunID,
		StepID:  request.StepID,
		Inputs:  resolveInputs(request.Inputs, request.RunContext),
		Context: map[string]any{},
	}
	if request.Tool.RequiresServiceAccount {
		envelope.ServiceAccount = executor.serviceAccount
	}

	stdin, err := json.Marshal(envelope)
	if err != nil {
		return Observation{}, fmt.Errorf("%w: encode stdin: %v", ErrToolExecutionFailed, err)
	}

	cmd := exec.CommandContext(ctx, request.Tool.CommandPath)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if ctx.Err() != nil {
		return Observation{}, fmt.Errorf("%w: tool %q timed out", ErrToolExecutionFailed, request.Tool.Name)
	}
	if err != nil {
		return Observation{}, fmt.Errorf("%w: tool %q exited with error: %s", ErrToolExecutionFailed, request.Tool.Name, RedactText(stderr.String()))
	}

	var result ToolResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return Observation{}, fmt.Errorf("%w: decode stdout JSON: %v", ErrToolExecutionFailed, err)
	}
	if result.Status == ToolResultStatusError {
		if result.Error != nil {
			return Observation{
				StepOrder: request.StepOrder,
				ToolName:  request.Tool.Name,
				Status:    StepStatusFailed,
				Error:     RedactText(result.Error.Code + ": " + result.Error.Message),
			}, nil
		}
		return Observation{
			StepOrder: request.StepOrder,
			ToolName:  request.Tool.Name,
			Status:    StepStatusFailed,
			Error:     fmt.Sprintf("tool returned status %q", result.Status),
		}, nil
	}
	if result.Status != ToolResultStatusOK {
		return Observation{}, fmt.Errorf("%w: tool returned status %q", ErrToolExecutionFailed, result.Status)
	}

	outputs := make(map[string]any, len(result.Outputs))
	for name, output := range result.Outputs {
		if output.Sensitive {
			outputs[name] = request.RunContext.Store(request.Tool.Name, name, output.Value)
			continue
		}
		outputs[name] = RedactJSONValue(output.Value)
	}

	return Observation{
		StepOrder: request.StepOrder,
		ToolName:  request.Tool.Name,
		Status:    StepStatusSucceeded,
		Outputs:   outputs,
	}, nil
}

func resolveInputs(inputs map[string]any, runContext *RunContext) map[string]any {
	resolved := make(map[string]any, len(inputs))
	for key, value := range inputs {
		if stringValue, ok := value.(string); ok {
			if resolvedValue, ok := runContext.Resolve(stringValue); ok {
				resolved[key] = resolvedValue
				continue
			}
		}
		resolved[key] = value
	}

	return resolved
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/agent
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/executor.go internal/agent/executor_test.go
git commit -m "Add CLI tool executor"
```

## Task 8: Add Agent Planner Boundary

**Files:**
- Create: `internal/agent/llm.go`
- Create: `internal/agent/llm_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add OpenAI SDK dependency**

Run:

```bash
go get github.com/openai/openai-go/v3@latest
```

Expected: `go.mod` gains direct dependency `github.com/openai/openai-go/v3`, and `go.sum` is updated.

- [ ] **Step 2: Write planner parsing tests**

Create `internal/agent/llm_test.go`:

```go
package agent

import (
	"encoding/json"
	"testing"
)

func TestParsePlannerActionAcceptsCallTool(t *testing.T) {
	action, err := ParsePlannerAction([]byte(`{"type":"call_tool","tool":"export_report","inputs":{"month":"2026-05"}}`))
	if err != nil {
		t.Fatalf("ParsePlannerAction() error = %v", err)
	}
	if action.Type != ActionTypeCallTool {
		t.Fatalf("action.Type = %q, want call_tool", action.Type)
	}
	if action.Tool != "export_report" {
		t.Fatalf("action.Tool = %q, want export_report", action.Tool)
	}

	var inputs map[string]any
	if err := json.Unmarshal(action.Inputs, &inputs); err != nil {
		t.Fatalf("Unmarshal inputs error = %v", err)
	}
	if inputs["month"] != "2026-05" {
		t.Fatalf("month = %v, want 2026-05", inputs["month"])
	}
}

func TestParsePlannerActionRejectsUnknownAction(t *testing.T) {
	_, err := ParsePlannerAction([]byte(`{"type":"run_shell"}`))

	if err == nil {
		t.Fatal("ParsePlannerAction() error = nil, want error")
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/agent
```

Expected: fail because planner functions do not exist.

- [ ] **Step 4: Implement planner boundary**

Create `internal/agent/llm.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"orderbuddy-ai/backend/internal/toolcatalog"
)

var ErrInvalidPlannerAction = errors.New("invalid planner action")

type PlanRequest struct {
	Instructions string
	Message      string
	Tools        []toolcatalog.Tool
	Observations []Observation
}

type Planner interface {
	NextAction(ctx context.Context, request PlanRequest) (PlannerAction, error)
}

type OpenAIPlanner struct {
	client openai.Client
	model  string
}

func NewOpenAIPlanner(apiKey string, model string) OpenAIPlanner {
	return OpenAIPlanner{
		client: openai.NewClient(option.WithAPIKey(apiKey)),
		model:  model,
	}
}

func (planner OpenAIPlanner) NextAction(ctx context.Context, request PlanRequest) (PlannerAction, error) {
	prompt, err := json.Marshal(map[string]any{
		"instructions": request.Instructions,
		"message":      request.Message,
		"tools":        request.Tools,
		"observations": request.Observations,
		"allowed_actions": []string{
			string(ActionTypeCallTool),
			string(ActionTypeFinalAnswer),
		},
	})
	if err != nil {
		return PlannerAction{}, fmt.Errorf("build planner prompt: %w", err)
	}

	response, err := planner.client.Responses.New(ctx, responses.ResponseNewParams{
		Model: openai.ChatModel(planner.model),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(string(prompt)),
		},
	})
	if err != nil {
		return PlannerAction{}, fmt.Errorf("openai response: %w", err)
	}

	return ParsePlannerAction([]byte(response.OutputText()))
}

func ParsePlannerAction(raw []byte) (PlannerAction, error) {
	var action PlannerAction
	if err := json.Unmarshal(raw, &action); err != nil {
		return PlannerAction{}, fmt.Errorf("%w: decode JSON: %v", ErrInvalidPlannerAction, err)
	}
	switch action.Type {
	case ActionTypeCallTool:
		if strings.TrimSpace(action.Tool) == "" {
			return PlannerAction{}, fmt.Errorf("%w: tool is required", ErrInvalidPlannerAction)
		}
		if !isJSONObject(action.Inputs) {
			return PlannerAction{}, fmt.Errorf("%w: inputs must be a JSON object", ErrInvalidPlannerAction)
		}
	case ActionTypeFinalAnswer:
		if strings.TrimSpace(action.Answer) == "" {
			return PlannerAction{}, fmt.Errorf("%w: answer is required", ErrInvalidPlannerAction)
		}
	default:
		return PlannerAction{}, fmt.Errorf("%w: unknown action type %q", ErrInvalidPlannerAction, action.Type)
	}

	return action, nil
}

func isJSONObject(raw json.RawMessage) bool {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}

	return value != nil
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/agent
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/llm.go internal/agent/llm_test.go go.mod go.sum
git commit -m "Add agent planner boundary"
```

## Task 9: Add Agent Repository

**Files:**
- Create: `internal/agent/repository.go`
- Create: `internal/agent/repository_test.go`

- [ ] **Step 1: Write repository tests**

Create `internal/agent/repository_test.go`:

```go
package agent

import (
	"context"
	"strings"
	"testing"
)

func TestRepositoryCreateSchemaRequiresPool(t *testing.T) {
	repository := NewRepository(nil)

	err := repository.CreateSchema(context.Background())

	if err == nil {
		t.Fatal("CreateSchema() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "agent database is missing") {
		t.Fatalf("CreateSchema() error = %q, want missing database context", err)
	}
}

func TestRepositorySchemaContainsTables(t *testing.T) {
	schema := schemaSQL()

	for _, table := range []string{"agent_runs", "agent_run_steps"} {
		if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("schema missing table %q", table)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/agent
```

Expected: fail because repository does not exist.

- [ ] **Step 3: Implement repository**

Create `internal/agent/repository.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrDatabaseMissing = errors.New("agent database is missing")

type database interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type Repository struct {
	database database
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return Repository{database: pool}
}

func (repository Repository) CreateSchema(ctx context.Context) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}
	if _, err := repository.database.Exec(ctx, schemaSQL()); err != nil {
		return fmt.Errorf("create agent schema: %w", err)
	}

	return nil
}

func (repository Repository) StartRun(ctx context.Context, message string) (Run, error) {
	if repository.database == nil {
		return Run{}, ErrDatabaseMissing
	}

	run := Run{
		ID:        newRuntimeID("run"),
		Message:   message,
		Status:    RunStatusRunning,
		StartedAt: time.Now().UTC(),
	}
	_, err := repository.database.Exec(ctx, `
INSERT INTO agent_runs (id, message, status, started_at)
VALUES ($1, $2, $3, $4)
`, run.ID, run.Message, run.Status, run.StartedAt)
	if err != nil {
		return Run{}, fmt.Errorf("start run: %w", err)
	}

	return run, nil
}

func (repository Repository) FinishRun(ctx context.Context, run Run) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	outputSummary, err := json.Marshal(RedactJSONValue(run.Outputs))
	if err != nil {
		return fmt.Errorf("marshal output summary: %w", err)
	}
	_, err = repository.database.Exec(ctx, `
UPDATE agent_runs
SET status = $2, answer_summary = $3, output_summary = $4, error_summary = $5, finished_at = $6
WHERE id = $1
`, run.ID, run.Status, RedactText(run.Answer), outputSummary, RedactText(run.ErrorSummary), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}

	return nil
}

func (repository Repository) SaveStep(ctx context.Context, step StepRecord) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	_, err := repository.database.Exec(ctx, `
INSERT INTO agent_run_steps (id, run_id, step_order, tool_name, input_summary, output_summary, duration_ms, status, error_summary, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
`,
		newRuntimeID("step"),
		step.RunID,
		step.StepOrder,
		step.ToolName,
		[]byte(step.InputSummary),
		[]byte(step.OutputSummary),
		step.DurationMS,
		step.Status,
		RedactText(step.ErrorSummary),
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("save step: %w", err)
	}

	return nil
}

func schemaSQL() string {
	return `
CREATE TABLE IF NOT EXISTS agent_runs (
	id text PRIMARY KEY,
	message text NOT NULL,
	status text NOT NULL,
	answer_summary text NOT NULL DEFAULT '',
	output_summary jsonb NOT NULL DEFAULT '{}'::jsonb,
	error_summary text NOT NULL DEFAULT '',
	started_at timestamptz NOT NULL,
	finished_at timestamptz
);

CREATE TABLE IF NOT EXISTS agent_run_steps (
	id text PRIMARY KEY,
	run_id text NOT NULL REFERENCES agent_runs(id),
	step_order integer NOT NULL,
	tool_name text NOT NULL,
	input_summary jsonb NOT NULL,
	output_summary jsonb NOT NULL,
	duration_ms integer NOT NULL,
	status text NOT NULL,
	error_summary text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL
);
`
}
```

- [ ] **Step 4: Add runtime ID helper**

Append to `internal/agent/repository.go`:

```go
func newRuntimeID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/agent
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/repository.go internal/agent/repository_test.go
git commit -m "Add agent run audit repository"
```

## Task 10: Add Agent Service Loop

**Files:**
- Create: `internal/agent/service.go`
- Create: `internal/agent/service_test.go`

- [ ] **Step 1: Write service tests**

Create `internal/agent/service_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"orderbuddy-ai/backend/internal/toolcatalog"
)

type fakePlanner struct {
	actions []PlannerAction
	index   int
}

func (planner *fakePlanner) NextAction(_ context.Context, _ PlanRequest) (PlannerAction, error) {
	if planner.index >= len(planner.actions) {
		return PlannerAction{}, errors.New("no more actions")
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
		return Observation{}, errors.New("no observation")
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
	return Run{ID: "run_1", Message: message, Status: RunStatusRunning, StartedAt: time.Now()}, nil
}

func (store *memoryRunStore) FinishRun(_ context.Context, run Run) error {
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
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/agent
```

Expected: fail because service types do not exist.

- [ ] **Step 3: Implement service loop**

Create `internal/agent/service.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"orderbuddy-ai/backend/internal/toolcatalog"
)

var ErrRunFailed = errors.New("agent run failed")

type Catalog interface {
	ListEnabledTools(ctx context.Context) ([]toolcatalog.Tool, error)
	GetInstructions(ctx context.Context) (toolcatalog.Instructions, error)
}

type Executor interface {
	Execute(ctx context.Context, request ExecuteRequest) (Observation, error)
}

type RunStore interface {
	StartRun(ctx context.Context, message string) (Run, error)
	FinishRun(ctx context.Context, run Run) error
	SaveStep(ctx context.Context, step StepRecord) error
}

type ServiceConfig struct {
	Planner      Planner
	Catalog      Catalog
	Executor     Executor
	RunStore     RunStore
	MaxSteps     int
	TotalTimeout time.Duration
}

type Service struct {
	planner      Planner
	catalog      Catalog
	executor     Executor
	runStore     RunStore
	maxSteps     int
	totalTimeout time.Duration
}

func NewService(config ServiceConfig) Service {
	return Service{
		planner:      config.Planner,
		catalog:      config.Catalog,
		executor:     config.Executor,
		runStore:     config.RunStore,
		maxSteps:     config.MaxSteps,
		totalTimeout: config.TotalTimeout,
	}
}

func (service Service) Run(parent context.Context, request CreateRunRequest) (RunResponse, error) {
	ctx, cancel := context.WithTimeout(parent, service.totalTimeout)
	defer cancel()

	run, err := service.runStore.StartRun(ctx, request.Message)
	if err != nil {
		return RunResponse{}, err
	}

	response, runErr := service.run(ctx, run, request)
	if runErr != nil {
		run.Status = RunStatusFailed
		run.ErrorSummary = runErr.Error()
		_ = service.runStore.FinishRun(context.Background(), run)
		response.RunID = run.ID
		response.Status = RunStatusFailed
		response.Error = RedactText(runErr.Error())
		return response, runErr
	}

	run.Status = RunStatusSucceeded
	run.Answer = response.Answer
	run.Outputs = response.Outputs
	if err := service.runStore.FinishRun(ctx, run); err != nil {
		return RunResponse{}, err
	}

	return response, nil
}

func (service Service) run(ctx context.Context, run Run, request CreateRunRequest) (RunResponse, error) {
	instructions, err := service.catalog.GetInstructions(ctx)
	if err != nil {
		return RunResponse{}, err
	}
	tools, err := service.catalog.ListEnabledTools(ctx)
	if err != nil {
		return RunResponse{}, err
	}
	toolsByName := indexToolsByName(tools)
	runContext := NewRunContext()
	var observations []Observation
	unknownToolCount := 0
	businessErrorCounts := map[string]int{}

	for stepOrder := 1; stepOrder <= service.maxSteps; stepOrder++ {
		action, err := service.planner.NextAction(ctx, PlanRequest{
			Instructions: instructions.Content,
			Message:      request.Message,
			Tools:        tools,
			Observations: observations,
		})
		if err != nil {
			return RunResponse{}, err
		}

		switch action.Type {
		case ActionTypeFinalAnswer:
			return RunResponse{
				RunID:   run.ID,
				Status:  RunStatusSucceeded,
				Answer:  RedactText(action.Answer),
				Outputs: redactOutputs(action.Outputs),
			}, nil
		case ActionTypeCallTool:
			tool, ok := toolsByName[action.Tool]
			if !ok {
				unknownToolCount++
				observation := Observation{StepOrder: stepOrder, ToolName: action.Tool, Status: StepStatusFailed, Error: "unknown tool"}
				_ = service.runStore.SaveStep(ctx, StepRecord{
					RunID:         run.ID,
					StepOrder:     stepOrder,
					ToolName:      action.Tool,
					InputSummary:  mustMarshalJSON(map[string]any{}),
					OutputSummary: mustMarshalJSON(map[string]any{"error": observation.Error}),
					Status:        StepStatusFailed,
					ErrorSummary:  observation.Error,
				})
				if unknownToolCount > 1 {
					return RunResponse{}, fmt.Errorf("%w: unknown tool %q", ErrRunFailed, action.Tool)
				}
				observations = append(observations, observation)
				continue
			}
			inputs, err := decodeInputs(action.Inputs)
			if err != nil {
				return RunResponse{}, err
			}
			started := time.Now()
			observation, err := service.executor.Execute(ctx, ExecuteRequest{
				RunID:      run.ID,
				StepID:     fmt.Sprintf("step_%d", stepOrder),
				StepOrder:  stepOrder,
				Tool:       tool,
				Inputs:     inputs,
				RunContext: runContext,
			})
			duration := time.Since(started).Milliseconds()
			step := StepRecord{
				RunID:         run.ID,
				StepOrder:     stepOrder,
				ToolName:      tool.Name,
				InputSummary:  mustMarshalJSON(RedactJSONValue(inputs)),
				OutputSummary: mustMarshalJSON(RedactJSONValue(observation.Outputs)),
				DurationMS:    duration,
				Status:        StepStatusSucceeded,
			}
			if err != nil {
				step.Status = StepStatusFailed
				step.ErrorSummary = err.Error()
				_ = service.runStore.SaveStep(ctx, step)
				return RunResponse{}, err
			}
			if observation.Status == StepStatusFailed {
				step.Status = StepStatusFailed
				step.ErrorSummary = observation.Error
				step.OutputSummary = mustMarshalJSON(map[string]any{"error": observation.Error})
			}
			if err := service.runStore.SaveStep(ctx, step); err != nil {
				return RunResponse{}, err
			}
			observations = append(observations, observation)
			if observation.Status == StepStatusFailed {
				errorKey := tool.Name + "\x00" + observation.Error
				businessErrorCounts[errorKey]++
				if businessErrorCounts[errorKey] > 1 {
					return RunResponse{}, fmt.Errorf("%w: repeated tool error from %q: %s", ErrRunFailed, tool.Name, observation.Error)
				}
			}
		default:
			return RunResponse{}, fmt.Errorf("%w: invalid action type %q", ErrRunFailed, action.Type)
		}
	}

	return RunResponse{}, fmt.Errorf("%w: step limit exceeded", ErrRunFailed)
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
		return nil, fmt.Errorf("%w: decode tool inputs: %v", ErrRunFailed, err)
	}
	if inputs == nil {
		return map[string]any{}, nil
	}
	return inputs, nil
}

func mustMarshalJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func redactOutputs(outputs map[string]any) map[string]any {
	if outputs == nil {
		return map[string]any{}
	}
	redacted, ok := RedactJSONValue(outputs).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return redacted
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/agent
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/service.go internal/agent/service_test.go
git commit -m "Add controlled agent loop"
```

## Task 11: Add Agent HTTP Handler

**Files:**
- Create: `internal/agent/handler.go`
- Create: `internal/agent/handler_test.go`

- [ ] **Step 1: Write handler tests**

Create `internal/agent/handler_test.go`:

```go
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type fakeRunService struct {
	response RunResponse
	err      error
}

func (service fakeRunService) Run(_ context.Context, _ CreateRunRequest) (RunResponse, error) {
	return service.response, service.err
}

func TestHandlerCreateRunReturnsResponse(t *testing.T) {
	handler := NewHandler(fakeRunService{response: RunResponse{RunID: "run_1", Status: RunStatusSucceeded, Answer: "done"}})
	app := fiber.New()
	app.Post(AgentRunsPath, handler.CreateRun)

	req, err := http.NewRequest(http.MethodPost, AgentRunsPath, bytes.NewReader([]byte(`{"message":"export report"}`)))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body RunResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body.RunID != "run_1" {
		t.Fatalf("RunID = %q, want run_1", body.RunID)
	}
}

func TestHandlerCreateRunRejectsBadJSON(t *testing.T) {
	handler := NewHandler(fakeRunService{})
	app := fiber.New()
	app.Post(AgentRunsPath, handler.CreateRun)

	req, err := http.NewRequest(http.MethodPost, AgentRunsPath, bytes.NewReader([]byte(`{`)))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/agent
```

Expected: fail because handler does not exist.

- [ ] **Step 3: Implement handler**

Create `internal/agent/handler.go`:

```go
package agent

import (
	"context"
	"net/http"

	"github.com/gofiber/fiber/v3"
)

const errorField = "error"

type Runner interface {
	Run(ctx context.Context, request CreateRunRequest) (RunResponse, error)
}

type Handler struct {
	runner Runner
}

func NewHandler(runner Runner) Handler {
	return Handler{runner: runner}
}

func (handler Handler) CreateRun(c fiber.Ctx) error {
	var request CreateRunRequest
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: "invalid JSON request body"})
	}

	response, err := handler.runner.Run(c.Context(), request)
	if err != nil {
		if response.Status == RunStatusFailed {
			return c.Status(http.StatusBadRequest).JSON(response)
		}
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{errorField: "agent run failed"})
	}

	return c.Status(http.StatusOK).JSON(response)
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
go test ./internal/agent
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/handler.go internal/agent/handler_test.go
git commit -m "Add agent run handler"
```

## Task 12: Wire Routes And CORS

**Files:**
- Modify: `internal/httpapi/router.go`
- Modify: `internal/httpapi/router_test.go`
- Modify: `internal/httpapi/middleware.go`

- [ ] **Step 1: Update router tests**

In `internal/httpapi/router_test.go`, add the Fiber import:

```go
"github.com/gofiber/fiber/v3"
```

Add fake handlers and route tests to `internal/httpapi/router_test.go`:

```go
type fakeToolHandler struct{}

func (handler fakeToolHandler) RegisterTool(c fiber.Ctx) error {
	return c.SendStatus(http.StatusCreated)
}

func (handler fakeToolHandler) ListTools(c fiber.Ctx) error {
	return c.SendStatus(http.StatusOK)
}

func (handler fakeToolHandler) UpdateInstructions(c fiber.Ctx) error {
	return c.SendStatus(http.StatusOK)
}

type fakeAgentHandler struct{}

func (handler fakeAgentHandler) CreateRun(c fiber.Ctx) error {
	return c.SendStatus(http.StatusOK)
}

func TestToolRoutesAreRegistered(t *testing.T) {
	app := NewRouter(RouterConfig{
		StatusHandler: status.NewHandler(status.NewService(&fakeDatabase{}), "test"),
		ToolHandler:   fakeToolHandler{},
		AgentHandler:  fakeAgentHandler{},
	})

	req, err := http.NewRequest(http.MethodPost, "/api/tools", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer closeResponseBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
}

func TestAgentRunRouteIsRegistered(t *testing.T) {
	app := NewRouter(RouterConfig{
		StatusHandler: status.NewHandler(status.NewService(&fakeDatabase{}), "test"),
		ToolHandler:   fakeToolHandler{},
		AgentHandler:  fakeAgentHandler{},
	})

	req, err := http.NewRequest(http.MethodPost, "/api/agent/runs", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer closeResponseBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
```

Update `TestOptionsRequestReturnsCORSHeaders` to also assert:

```go
if got := resp.Header.Get(headerAccessControlAllowMethods); got != corsAllowedMethods {
	t.Fatalf("Access-Control-Allow-Methods = %q, want %q", got, corsAllowedMethods)
}
```

- [ ] **Step 2: Run router tests to verify failure**

Run:

```bash
go test ./internal/httpapi
```

Expected: fail because `RouterConfig` does not include tool or agent handlers and CORS does not allow POST/PUT.

- [ ] **Step 3: Update CORS methods**

In `internal/httpapi/middleware.go`, change:

```go
corsAllowedMethods = "GET, OPTIONS"
```

to:

```go
corsAllowedMethods = "GET, POST, PUT, OPTIONS"
```

- [ ] **Step 4: Update router config and routes**

In `internal/httpapi/router.go`, define small route interfaces and register routes:

```go
package httpapi

import (
	"github.com/gofiber/fiber/v3"

	"orderbuddy-ai/backend/internal/agent"
	"orderbuddy-ai/backend/internal/status"
	"orderbuddy-ai/backend/internal/toolcatalog"
)

const (
	HealthzPath = "/healthz"
	ReadyzPath  = "/readyz"
	StatusPath  = "/api/status"
)

type ToolHandler interface {
	RegisterTool(c fiber.Ctx) error
	ListTools(c fiber.Ctx) error
	UpdateInstructions(c fiber.Ctx) error
}

type AgentHandler interface {
	CreateRun(c fiber.Ctx) error
}

type RouterConfig struct {
	StatusHandler status.Handler
	ToolHandler   ToolHandler
	AgentHandler  AgentHandler
}

func NewRouter(config RouterConfig) *fiber.App {
	app := fiber.New()
	app.Use(withCORS)
	app.Get(HealthzPath, healthz)
	app.Get(ReadyzPath, config.StatusHandler.Readyz)
	app.Get(StatusPath, config.StatusHandler.Status)
	if config.ToolHandler != nil {
		app.Post(toolcatalog.ToolsPath, config.ToolHandler.RegisterTool)
		app.Get(toolcatalog.ToolsPath, config.ToolHandler.ListTools)
		app.Put(toolcatalog.AgentInstructionsPath, config.ToolHandler.UpdateInstructions)
	}
	if config.AgentHandler != nil {
		app.Post(agent.AgentRunsPath, config.AgentHandler.CreateRun)
	}
	return app
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/httpapi
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/httpapi/router.go internal/httpapi/router_test.go internal/httpapi/middleware.go
git commit -m "Register agent and tool routes"
```

## Task 13: Wire App Dependencies

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

- [ ] **Step 1: Update app wiring**

Modify `internal/app/app.go` imports to include:

```go
"time"

"orderbuddy-ai/backend/internal/agent"
"orderbuddy-ai/backend/internal/toolcatalog"
```

After postgres connection, create schemas and wire services:

```go
toolRepository := toolcatalog.NewRepository(pool)
if err := toolRepository.CreateSchema(context.Background()); err != nil {
	return fmt.Errorf("create tool catalog schema: %w", err)
}
toolService := toolcatalog.NewService(toolRepository, cfg.TrustedToolDir)
toolHandler := toolcatalog.NewHandler(toolService)

agentRepository := agent.NewRepository(pool)
if err := agentRepository.CreateSchema(context.Background()); err != nil {
	return fmt.Errorf("create agent schema: %w", err)
}
planner := agent.NewOpenAIPlanner(cfg.OpenAIAPIKey, cfg.OpenAIModel)
executor := agent.NewCLIExecutor(agent.ServiceAccount{
	Profile:  "internal_report_service",
	Username: cfg.InternalReportUsername,
	Password: cfg.InternalReportPassword,
})
agentService := agent.NewService(agent.ServiceConfig{
	Planner:      planner,
	Catalog:      toolService,
	Executor:     executor,
	RunStore:     agentRepository,
	MaxSteps:     cfg.AgentMaxSteps,
	TotalTimeout: time.Duration(cfg.AgentTotalTimeoutMS) * time.Millisecond,
})
agentHandler := agent.NewHandler(agentService)
```

Update router construction:

```go
router := httpapi.NewRouter(httpapi.RouterConfig{
	StatusHandler: statusHandler,
	ToolHandler:   toolHandler,
	AgentHandler:  agentHandler,
})
```

- [ ] **Step 2: Run app tests**

Run:

```bash
go test ./internal/app
```

Expected: pass. `TestRunWrapsPostgresConnectionErrors` should still pass because invalid PostgreSQL URL fails before schema creation.

- [ ] **Step 3: Run package tests touched so far**

Run:

```bash
go test ./internal/app ./internal/httpapi ./internal/toolcatalog ./internal/agent
```

Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go
git commit -m "Wire CLI tool agent"
```

## Task 14: Final Verification

**Files:**
- No source changes expected unless verification fails.

- [ ] **Step 1: Format all Go files**

Run:

```bash
gofmt -w internal/architecture/architecture_test.go internal/config/config.go internal/config/config_test.go internal/toolcatalog internal/agent internal/httpapi internal/app
```

Expected: no output.

- [ ] **Step 2: Run full tests**

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

Expected: `repo guard passed`.

- [ ] **Step 4: Commit verification fixes if needed**

If formatting, lint, or tests required code changes:

```bash
git add .
git commit -m "Finish CLI tool agent verification"
```

If no changes were needed, do not create an empty commit.
