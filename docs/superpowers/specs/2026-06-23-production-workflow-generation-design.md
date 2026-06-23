# Production Workflow Generation Design

## Summary

Build a developer-facing system that uses an LLM-controlled Playwright exploration session to observe a production internal web application, extract the underlying business API calls, synthesize a structured workflow, store it in PostgreSQL, and automatically publish it for later execution by the Go backend.

The target experience is close to a "Cloudflare-style" prompt-to-deploy flow: a developer describes a business task, provides production login credentials for the internal site, and the system turns the observed browser/API behavior into a reusable backend workflow.

This design intentionally covers the high-automation version selected during brainstorming:

- The exploration target is the production internal website.
- The developer provides a real username and password.
- Playwright logs in automatically; no CAPTCHA or MFA is assumed for the first version.
- Playwright is used only for exploration and API discovery, not for steady-state execution.
- The LLM automatically infers workflow parameters from observed requests and responses.
- The workflow is persisted as structured database records and automatically published.
- All action types are in scope, including write operations.

Because this is the highest-risk operating mode, the design treats incorrect workflows, incorrect parameter generalization, and repeated execution of harmful actions as expected failure modes that must be audited and contained.

## Goals

- Accept a developer prompt that describes an internal website business task.
- Automatically log in to the production internal site using developer-provided credentials.
- Use Playwright to complete the task while capturing network requests, responses, downloads, and timing.
- Distill the browser network trace into a business API call chain.
- Synthesize a structured workflow with input schema, step templates, extractors, and success conditions.
- Store the workflow in PostgreSQL and automatically publish it.
- Execute a published workflow through deterministic Go HTTP calls, not browser automation.
- Record enough evidence to trace each workflow back to its source exploration run and API trace.

## Non-Goals

- Building the customer or operations prompt-to-workflow matcher in the first implementation slice.
- Replacing existing internal authentication or authorization systems.
- Guaranteeing semantic correctness of arbitrary production actions.
- Building a generic RPA system that keeps using Playwright at runtime.
- Adding a UI for visual workflow review in the first slice.

## Existing Repository Context

The current Go backend is intentionally small:

- `cmd/server` loads configuration and calls `app.Run`.
- `internal/app` wires dependencies and graceful shutdown.
- `internal/config` reads environment settings.
- `internal/httpapi` owns Fiber routes and middleware.
- `internal/platform/postgres` owns PostgreSQL connectivity.
- `internal/status` owns health and readiness behavior.

The repository guardrails prohibit adding direct dependencies, new `internal/` packages, new architectural layers, interfaces, or enum-like status values without an explicit need, alternatives, and blast-radius explanation.

This feature cannot fit cleanly inside the existing packages. It introduces new domain concepts and external execution boundaries, so it requires new packages and database schema.

## Required New Boundaries

### Workflow Domain

Owns workflow definitions, versions, steps, invocation records, input/output schemas, and publication state.

Need: workflow is the core durable business concept and must not be mixed into status checks, HTTP routing, or PostgreSQL connectivity.

Alternative considered: store workflow logic directly in `httpapi` or `app`. That would break existing package ownership and make workflow execution hard to test.

Blast radius: new package such as `internal/workflow`, new PostgreSQL tables, new repository methods, and new HTTP routes for creating and invoking workflows.

### Exploration Domain

Owns exploration runs, trace capture metadata, distillation, synthesis, and publication handoff.

Need: exploration has its own lifecycle and failure modes separate from workflow invocation.

Alternative considered: combine exploration with workflow. That would make one package own both temporary trace-processing state and durable execution state.

Blast radius: new package such as `internal/exploration`, new tables for exploration runs and traces, and one developer-facing HTTP API for submitting exploration tasks.

### Runner Integration

Owns the boundary to Playwright, Codex/Claude Code, and LLM-based trace analysis.

Need: browser automation and agent execution must be isolated from request handlers, with explicit timeouts, log handling, and credential scope.

Alternative considered: call Playwright directly from HTTP handlers. That would couple long-running browser work to request lifecycles and make failures hard to control.

Blast radius: new worker process or asynchronous runner abstraction, new runtime configuration, and possibly new dependencies after approval.

### Workflow Executor

Owns deterministic HTTP execution of structured workflow steps, variable interpolation, extractor evaluation, retries, success conditions, and invocation audit.

Need: production runtime must be replayable and auditable. The backend should execute API workflows, not re-run the browser agent.

Alternative considered: persist generated scripts or CLI tools. That would weaken parameter validation, audit, rate limiting, and centralized execution control.

Blast radius: new execution service inside the workflow boundary, HTTP client configuration, templating rules, and sensitive field redaction.

## Architecture

The system has five main stages.

### 1. Developer Prompt Intake

A developer submits:

- Natural-language task prompt.
- Target internal website URL.
- Username and password for the production website.
- Optional execution hints such as partner name, month, target report, or expected output.

Credentials are sensitive ephemeral inputs. They may be passed to the exploration runner for a single run, but they must not be stored in workflow definitions, API traces, audit logs, or error messages.

### 2. Browser Exploration Runner

The backend starts an isolated Playwright session and uses the supplied credentials to log in to the production internal website. An LLM-controlled agent then completes the requested business task.

During exploration, Playwright records:

- Request URL, method, query, headers, and body.
- Response status, headers, and body summary.
- Download metadata.
- Request timing and dependency ordering.
- Redirects and page navigation.
- Initiator information when available.

Playwright is an API discovery mechanism only. It is not the runtime workflow engine.

### 3. API Trace Distiller

The distiller filters the raw network trace and identifies the business API chain. It should remove or mark as non-business traffic:

- Static assets.
- Telemetry and analytics calls.
- Feature flag polling.
- Unrelated background refresh requests.
- One-time trace IDs.
- Browser-only preflight details unless needed for execution.

It should retain:

- Authentication/session dependencies.
- CSRF or anti-forgery token sources.
- Lookup requests needed to resolve user-facing names to IDs.
- Final action requests.
- Polling or download requests needed to obtain the final result.
- Response fields required by later steps.

### 4. Workflow Synthesizer

The synthesizer converts the distilled API chain into a structured workflow:

- Workflow name and description.
- Input schema inferred from the prompt, observed request values, and response dependencies.
- Step list with method, URL template, headers template, body template, timeout, retry policy, success condition, and extractors.
- Output schema and response mapping.
- Parameter provenance for each inferred input.

Parameter inference is automatic in the first version. Each inferred parameter must still keep provenance such as `request.body.month`, `request.query.keyword`, or `response.body.data[0].id`. This allows later debugging of incorrect generalization.

### 5. Auto Publisher and Executor

The generated workflow is persisted and automatically marked as published. Later execution uses the Go workflow executor to call the discovered APIs directly.

Runtime execution should be deterministic:

- Select workflow by explicit workflow ID in the first slice.
- Validate caller inputs against the workflow input schema.
- Interpolate inputs and extracted step values into URL, headers, and body templates.
- Execute HTTP steps with configured timeout and retry behavior.
- Evaluate success conditions.
- Extract outputs.
- Persist invocation audit.

The first slice does not include natural-language workflow matching for operations or customer prompts. That should be added after workflow generation and explicit execution are stable.

## Data Model

### exploration_runs

Stores one developer exploration task.

Key fields:

- `id`
- `prompt`
- `target_url`
- `status`
- `agent_name`
- `started_at`
- `finished_at`
- `created_by`
- `credential_supplied`
- `trace_summary`
- `error_summary`

Credentials are never stored.

### api_traces

Stores redacted network evidence for an exploration run.

Key fields:

- `id`
- `exploration_run_id`
- `step_order`
- `method`
- `url`
- `request_headers_redacted`
- `request_body_redacted`
- `response_status`
- `response_headers_redacted`
- `response_body_summary`
- `download_metadata`
- `initiator`
- `occurred_at`
- `business_relevance`

### workflows

Stores published workflow metadata.

Key fields:

- `id`
- `name`
- `description`
- `version`
- `status`
- `source_exploration_run_id`
- `risk_level`
- `input_schema`
- `output_schema`
- `parameter_provenance`
- `created_by`
- `published_at`

In the selected direction, generated workflows are automatically published.

### workflow_steps

Stores executable API steps.

Key fields:

- `id`
- `workflow_id`
- `step_order`
- `name`
- `method`
- `url_template`
- `headers_template`
- `body_template`
- `timeout_ms`
- `retry_policy`
- `success_condition`
- `extractors`

### workflow_invocations

Stores each execution of a published workflow.

Key fields:

- `id`
- `workflow_id`
- `workflow_version`
- `invoked_by`
- `input_summary`
- `status`
- `started_at`
- `finished_at`
- `output_summary`
- `error_summary`

### workflow_invocation_steps

Stores per-step execution audit.

Key fields:

- `id`
- `workflow_invocation_id`
- `step_order`
- `request_summary`
- `response_status`
- `response_summary`
- `duration_ms`
- `error_summary`

## Workflow Definition Shape

The database can store normalized records, JSON documents, or a hybrid. Conceptually, a workflow should look like this:

```json
{
  "name": "export_partner_monthly_report",
  "description": "Export a partner monthly report from the internal admin API.",
  "inputs": {
    "partner_name": {
      "type": "string",
      "required": true,
      "provenance": "request.query.keyword"
    },
    "month": {
      "type": "string",
      "format": "yyyy-mm",
      "required": true,
      "provenance": "request.body.month"
    }
  },
  "steps": [
    {
      "name": "find_partner",
      "method": "GET",
      "url": "https://internal.example.com/api/partners?keyword={{inputs.partner_name}}",
      "extract": {
        "partner_id": "$.data[0].id"
      },
      "success": "status in [200]"
    },
    {
      "name": "export_report",
      "method": "POST",
      "url": "https://internal.example.com/api/reports/monthly",
      "body": {
        "partnerId": "{{steps.find_partner.partner_id}}",
        "month": "{{inputs.month}}"
      },
      "success": "status in [200, 201]"
    }
  ],
  "outputs": {
    "download_url": "{{steps.export_report.response.downloadUrl}}"
  }
}
```

## Generation Flow

1. Developer submits prompt, target URL, username, and password.
2. Backend creates an `exploration_run`.
3. Runner starts isolated Playwright session.
4. Runner logs in with supplied credentials.
5. LLM-controlled exploration completes the requested task.
6. Playwright captures raw network trace.
7. Backend redacts and stores trace evidence.
8. Distiller extracts a business API chain.
9. Synthesizer generates workflow metadata, inputs, steps, extractors, success conditions, and output schema.
10. Backend persists the workflow and workflow steps.
11. Backend automatically publishes the workflow.
12. Optional replay may validate the generated workflow, but in the selected direction replay failure must not silently erase evidence.

## Invocation Flow

1. Caller invokes a workflow by explicit workflow ID and structured inputs.
2. Executor validates inputs against the workflow schema.
3. Executor resolves templates for each step.
4. Executor calls production APIs in step order.
5. Executor extracts step variables and outputs.
6. Executor evaluates success conditions.
7. Executor records invocation and per-step audit.
8. Executor returns output data, download metadata, or an error with the failing step.

Natural-language matching from customer or operations prompts is intentionally deferred to a later slice.

## Error Handling

- Login failure: mark exploration failed and do not generate a workflow.
- Exploration task incomplete: preserve trace evidence and mark exploration failed.
- Trace too complex or ambiguous: fail synthesis and retain the run for inspection.
- Sensitive data detected in generated workflow: fail publication until redaction rules remove the secret.
- Workflow replay failure: record failure evidence. The first slice may still publish because the selected direction prioritizes full automation, but this should be highly visible in workflow metadata.
- Invocation validation failure: return field-level input errors.
- Step execution failure: return the failing step, status code, and redacted response summary.
- Response shape mismatch: fail the invocation and record extractor errors.

The executor must not let the LLM invent success. Success is determined by explicit status checks, response predicates, and extractor results.

## Risk Controls

This design allows all production action types, so risk controls focus on containment, evidence, and rollback rather than blocking categories of work.

Required controls:

- Full audit from workflow back to exploration run and trace evidence.
- Immutable workflow versions; generated updates create a new version.
- Sensitive field redaction for headers, cookies, tokens, passwords, secrets, and configured PII fields.
- No credential persistence from developer prompt.
- Per-user, per-workflow, and per-target-host rate limits.
- Failure-based circuit breakers that disable or quarantine workflows after repeated failures or response-shape drift.
- Invocation records with resolved input summaries and per-step response summaries.
- Operator visibility into high-risk metadata such as HTTP method, target host, affected object IDs, amount-like fields, and batch-size-like fields.

The design does not rely on LLM risk judgment as the only control.

## Security Requirements

- Developer-supplied username and password are ephemeral and must not be stored.
- Raw trace capture must be redacted before persistence.
- Authorization, Cookie, Set-Cookie, password, token, secret, key, and credential-like fields must be redacted by default.
- Workflow templates must not contain concrete session cookies or bearer tokens from the exploration run.
- Runtime authentication should move toward service accounts or short-lived tokens instead of replaying developer credentials.
- Prompt text and generated workflow content must be treated as sensitive operational data.

## First Implementation Slice

The first slice should include:

- Developer API to submit an exploration task.
- Playwright runner that logs in and captures network trace.
- Trace redaction and persistence.
- LLM distiller/synthesizer that produces structured workflow JSON.
- Workflow repository that stores and automatically publishes generated workflows.
- Executor API that invokes a workflow by explicit ID and structured inputs.
- Invocation audit.

The first slice should not include:

- Operations/customer natural-language workflow matching.
- Visual workflow review UI.
- Full marketplace or tool catalog UX.
- CLI generation.
- Human approval gates before publication.

## Testing Strategy

- Unit tests for trace redaction rules.
- Unit tests for workflow schema validation and template resolution.
- Unit tests for extractor behavior and success condition evaluation.
- Repository tests for workflow versioning and invocation audit persistence.
- HTTP handler tests for exploration submission and explicit workflow invocation.
- Integration tests with a simulated internal site that exposes login, lookup, action, and download APIs.
- Runner tests using a controlled Playwright fixture, not production credentials.
- Failure tests for login failure, response shape mismatch, extractor failure, timeout, and redaction of sensitive fields.

## Open Risks

- Automatic parameter inference can generalize the wrong fields.
- Production website behavior can change after workflow publication.
- Automatically published write workflows can cause repeated production impact.
- Some APIs may depend on browser-only state or anti-automation controls.
- Replay and invocation may require an authentication model that is different from the developer login used during exploration.

These risks are accepted for the selected direction but must remain visible in implementation plans and rollout decisions.
