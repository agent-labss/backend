# CLI Tool Agent Design

## Summary

Build a first-stage AI operations backend where Codex or a developer turns business actions into small local CLI tools, registers those tools through an API, and lets a Go-based agent orchestrate them through an LLM-controlled tool loop.

This design intentionally narrows the broader production workflow generation idea. The first slice does not auto-discover production APIs, does not use Playwright at runtime, does not expose MCP, does not connect to WeChat, Teams, WhatsApp, or customer chat, and does not build a searchable wiki. It focuses on the smallest useful closed loop:

1. Codex explores a business process outside this backend and produces atomic CLI tools.
2. The backend registers those tools with metadata and schemas.
3. The backend stores a static agent instruction document.
4. A user submits a natural-language task.
5. The backend calls OpenAI to choose tools and generate parameters.
6. The backend executes registered CLI tools locally with retries, timeouts, logging, and audit.
7. Sensitive outputs are held in run-scoped context and exposed to the LLM only as references.

## Goals

- Register local CLI tools through an HTTP API.
- Restrict registered commands to a configured trusted tool directory.
- Store tool descriptions, input schemas, output schemas, and timeout settings.
- Store one global static instruction document for business rules and tool-use guidance.
- Run a controlled LLM tool loop that can call multiple registered tools in sequence.
- Execute tools through fixed command paths with JSON stdin and JSON stdout.
- Preserve sensitive tool outputs in a run-scoped context and show the LLM only `ctx://` references.
- Record run and step audit with redacted inputs, outputs, errors, duration, and status.

## Non-Goals

- Automatically discovering internal APIs from browser traffic.
- Automatically generating CLI tools inside the Go process.
- Running arbitrary shell commands or letting the LLM construct command lines.
- Building a full wiki, retrieval system, embedding pipeline, or document UI.
- Integrating external chat surfaces such as WeChat, Teams, WhatsApp, or customer chat.
- Exposing an MCP server in the first slice.
- Supporting remote workers, containers, queues, or long-running asynchronous orchestration.

## Existing Repository Context

The current backend is small and has strict package ownership:

- `cmd/server` loads config and calls `app.Run`.
- `internal/app` wires dependencies and graceful shutdown.
- `internal/config` reads environment settings only.
- `internal/httpapi` owns Fiber routes, middleware, and HTTP helpers.
- `internal/database` owns GORM models, GORM CLI query inputs, and generated query helpers.
- `internal/platform/datastore` owns database driver selection, close, and ping adapters.
- `internal/platform/sqlite` owns SQLite connectivity and migration.
- `internal/status` owns status behavior only.

Repository guardrails block new packages, new dependencies, new architecture layers, interfaces, or enum-like values unless the need, alternatives, and blast radius are explicit and approved.

This feature cannot fit inside the existing packages without breaking ownership. It introduces tool registration, agent execution, LLM orchestration, command execution, and audit records.

## Required New Boundaries

### `internal/toolcatalog`

Owns registered tool metadata, trusted path validation, tool status, schema storage, static agent instructions, and repository access for those concepts.

Need: tool registration and metadata are durable business concepts. They should not live in HTTP routing or application wiring.

Alternative considered: store tool metadata logic directly in `internal/httpapi` or `internal/app`. That would mix routing or wiring with business rules and make command-path validation hard to test independently.

Blast radius: new package allowlist entries, architecture tests, SQLite/GORM models for tools and instructions, GORM CLI query helpers where custom SQL is needed, HTTP handlers for registering and listing tools, and config for the trusted tool directory.

### `internal/agent`

Owns agent runs, LLM loop, CLI execution, run-scoped context, sensitive output handling, redaction, and run audit.

Need: orchestration is separate from the catalog. The agent consumes tools, executes them, and records what happened. The catalog should not execute commands or call OpenAI.

Alternative considered: combine agent behavior with `internal/toolcatalog`. That would couple durable metadata with runtime execution and make it harder to reason about security and audit boundaries.

Blast radius: new package allowlist entries, architecture tests, SQLite/GORM models for runs and steps, HTTP handler for creating runs, OpenAI configuration, and command execution tests.

`internal/agent` may depend on `internal/toolcatalog`. `internal/toolcatalog` must not depend on `internal/agent`.

## Required New Direct Dependency

The implementation should use the official OpenAI Go SDK for LLM orchestration.

Need: the backend must call OpenAI from Go to run the controlled tool loop. Using the SDK keeps request construction, response parsing, authentication, structured output handling, and future model changes behind a maintained client instead of hand-written HTTP code.

Alternative considered: call OpenAI with the standard-library `net/http`. That would avoid a new dependency, but it would push API payload shape, error parsing, retryable response handling, and structured action parsing into this codebase. That is a poor tradeoff for the core orchestration path.

Blast radius: `go.mod` and `go.sum` gain one direct OpenAI SDK dependency, `scripts/repo-guard.sh` must allow it, and `internal/agent` owns the only OpenAI client wrapper. No other package should import the SDK directly.

## Architecture

The system has four main surfaces.

### Tool Registration

Codex or a developer creates an atomic CLI tool outside this backend and places it in a trusted directory such as `./tools` or `/opt/ai-tools`.

The registration API accepts the tool name, description, command path, input schema, output schema, and timeout. The backend resolves and validates the command path before persisting it.

Command paths must be fixed at registration time. Runtime callers and the LLM cannot provide shell fragments, command flags, or alternate executable paths.

### Static Instructions

The backend stores one global markdown instruction document. It contains business rules, tool selection guidance, parameter conventions, examples, and forbidden actions.

This is not a wiki in the first slice. It is static context injected into the LLM tool loop.

### Agent Run

A caller submits a natural-language task such as:

```text
导出 Acme 合作伙伴 2026-05 报表
```

The backend builds an LLM prompt from:

- global static instructions;
- enabled tool names and descriptions;
- tool input and output schemas;
- the user task;
- the latest redacted observation.

The LLM may return only one of two structured actions:

- `call_tool`: select a registered tool and provide JSON object parameters.
- `final_answer`: finish the run with an answer and optional outputs.

The backend validates each action before doing anything. The LLM never gets direct command execution.

### CLI Execution

The backend executes tools with:

```text
exec.CommandContext(ctx, registeredCommandPath)
stdin: tool input envelope JSON
stdout: tool result JSON
stderr: redacted error/debug summary only
```

No shell is used. The command path is the registered, validated executable path.

## HTTP API

### Register Tool

`POST /api/tools`

Request:

```json
{
  "name": "login_internal_site",
  "description": "Login to the internal site.",
  "command_path": "./tools/login_internal_site",
  "input_schema": {
    "type": "object",
    "properties": {}
  },
  "output_schema": {
    "type": "object",
    "properties": {
      "session_ref": { "type": "string" }
    }
  },
  "timeout_ms": 10000
}
```

Rules:

- `name` is unique and uses lowercase letters, numbers, and underscores.
- `command_path` must resolve inside `TRUSTED_TOOL_DIR`.
- `input_schema` and `output_schema` must be valid JSON objects.
- Full JSON Schema validation is not required in the first slice, avoiding a new direct dependency.
- Registered tools default to enabled.

### List Tools

`GET /api/tools`

Returns registered tools with safe metadata. It must not return credentials or run-scoped context values.

### Update Agent Instructions

`PUT /api/agent/instructions`

Request:

```json
{
  "content": "业务规则、工具调用顺序、禁止事项、报表月份格式等..."
}
```

The first slice stores one global instruction document.

### Create Agent Run

`POST /api/agent/runs`

Request:

```json
{
  "message": "导出 Acme 合作伙伴 2026-05 报表"
}
```

Successful response:

```json
{
  "run_id": "run_123",
  "status": "succeeded",
  "answer": "报表已导出。",
  "outputs": {
    "report_file": "ctx://step/export_report/report_file"
  }
}
```

The first slice may execute synchronously. It must still enforce a max step count, per-tool timeout, total timeout, and audit persistence.

## CLI Tool Protocol

All tools receive the same input envelope on stdin:

```json
{
  "run_id": "run_123",
  "step_id": "step_2",
  "inputs": {
    "partner_name": "Acme",
    "month": "2026-05"
  },
  "context": {
    "session_ref": "real-cookie-or-token-after-resolution"
  }
}
```

Audit records must not store `username`, `password`, cookies, tokens, or resolved sensitive context values. The LLM sees `ctx://` references instead.

Successful output:

```json
{
  "status": "ok",
  "outputs": {
    "session": {
      "sensitive": true,
      "value": "cookie-or-token"
    },
    "partner_id": {
      "sensitive": false,
      "value": "p_123"
    }
  },
  "summary": "Logged in and resolved partner."
}
```

Failure output:

```json
{
  "status": "error",
  "error": {
    "code": "partner_not_found",
    "message": "No partner matched the requested name."
  }
}
```

Processing rules:

- `sensitive=true` outputs are stored in run context and replaced with `ctx://step/output` references in observations and audit summaries.
- `sensitive=false` outputs may be shown to the LLM and used as final outputs.
- Invalid JSON, `status=error`, command timeout, or non-zero exit code marks the step failed.
- `stderr` is never returned raw. It is redacted and stored only as an error/debug summary.

## LLM Loop

The agent loop is controlled by Go:

1. Load global instructions and enabled tools.
2. Ask OpenAI for the next structured action.
3. If the action is `call_tool`, validate tool name and JSON parameters.
4. Execute the tool.
5. Redact and store the observation.
6. Repeat until `final_answer`, step limit, total timeout, or failure.

Guardrails:

- Unknown tool: return a recoverable observation once; repeated unknown tools fail the run.
- Invalid parameter JSON: return a recoverable observation once; repeated invalid parameters fail the run.
- Tool timeout: fail the run.
- Tool business error: allow the LLM one follow-up action. Repeating the same tool and same error fails the run.
- Max steps defaults to 8.
- Total timeout defaults to 60 seconds.
- Final answers are redacted before returning to the caller.

## Data Model

### `tools`

Stores registered CLI tool metadata.

Key fields:

- `id`
- `name`
- `description`
- `command_path`
- `input_schema`
- `output_schema`
- `timeout_ms`
- `status`
- `created_at`
- `updated_at`

### `agent_instructions`

Stores the global static instruction document.

Key fields:

- `id`
- `content`
- `updated_at`

### `agent_runs`

Stores each user task run.

Key fields:

- `id`
- `message`
- `status`
- `answer_summary`
- `output_summary`
- `error_summary`
- `started_at`
- `finished_at`

### `agent_run_steps`

Stores per-step execution audit.

Key fields:

- `id`
- `run_id`
- `step_order`
- `tool_id`
- `input_summary`
- `output_summary`
- `duration_ms`
- `status`
- `error_summary`
- `created_at`

## Configuration

Required or optional environment settings:

- `OPENAI_API_KEY`
- `OPENAI_BASE_URL`
- `OPENAI_MODEL`
- `TRUSTED_TOOL_DIR`
- `AGENT_MAX_STEPS`
- `AGENT_TOTAL_TIMEOUT_MS`

## Error Handling

- Tool registration with a path outside `TRUSTED_TOOL_DIR`: reject with validation error.
- Duplicate tool name: reject with conflict error.
- OpenAI unavailable: fail the run with an orchestration error.
- Tool emits invalid JSON: fail the step and run.
- Tool emits sensitive data in summary or error text: redact before storing or returning.
- Step limit exceeded: fail the run with a clear error summary.
- Total timeout exceeded: cancel the run and any active command context.

## Security Requirements

- Do not run shell command strings.
- Do not let the LLM provide command paths, flags, or shell syntax.
- Only execute registered tools inside `TRUSTED_TOOL_DIR`.
- Redact `password`, `token`, `cookie`, `authorization`, `secret`, credential-like keys, bearer tokens, and cookie-like values from observations, audit, and final answers.
- Do not persist raw credentials.
- Do not persist raw sensitive tool outputs.
- Treat static instructions, tool metadata, user messages, and run audit as sensitive operational data.

## Testing Strategy

- `toolcatalog` tests for tool name validation, trusted directory validation, JSON object schema validation, duplicate registration, and instruction storage.
- CLI executor tests for stdin envelope construction, stdout parsing, invalid JSON, non-zero exit code, timeout, stderr redaction, and sensitive output conversion to `ctx://` references.
- Agent loop tests with a fake LLM for login-to-export multi-step execution, unknown tools, invalid parameters, repeated tool errors, step limit, and final answer redaction.
- HTTP handler tests for registering tools, listing tools, updating instructions, and creating agent runs.
- Architecture tests to enforce the new package boundaries.
- Repository guard must pass after implementation: `./scripts/repo-guard.sh`.

## Rollout

The first usable business flow should be partner report export decomposed into atomic tools such as:

- `login_internal_site`
- `find_partner`
- `export_partner_report`

After this works reliably, later specs can add:

- generated workflow review;
- explicit MCP exposure;
- document-backed wiki retrieval;
- external chat connectors;
- automatic Playwright/API discovery;
- remote workers or container isolation.
