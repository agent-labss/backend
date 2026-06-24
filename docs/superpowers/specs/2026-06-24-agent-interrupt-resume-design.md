# Agent Interrupt Resume Design

## Summary

Add first-class pause and resume support to agent runs so the backend can stop before risky actions or when the model needs more user input, then continue the same run after the user responds.

The current agent loop is synchronous and terminal:

```text
create run -> planner -> tool loop -> succeeded | failed
```

That does not fit common agent behavior for dangerous operations, approvals, or missing information. Mainstream agent systems model these cases as an interruption:

```text
create run -> planner -> pending interrupt -> waiting for user
                                       |
                              user approval or answer
                                       |
                                       v
                              resume same run_id
```

This design uses the same `run_id` across the whole lifecycle. A user confirmation or clarification is not a new run. It is a continuation of the original run.

## Goals

- Pause a run when a planned tool call requires user approval.
- Pause a run when the planner needs user-provided clarification.
- Resume the same `run_id` after the user approves, denies, modifies, or answers.
- Persist enough state to resume after the original HTTP request has ended.
- Preserve the existing controlled execution model: the model plans, but the backend decides whether and when a registered tool executes.
- Keep the first implementation scoped to HTTP request/response flows, without background workers or queues.
- Audit pending, approved, denied, answered, and canceled interruptions.

## Non-Goals

- Building a fully asynchronous distributed worker system.
- Streaming live agent events to clients.
- Supporting multiple simultaneous active interruptions for one run in V1.
- Letting the model directly execute destructive operations.
- Trusting the model alone to decide whether an operation is dangerous.
- Implementing organization-wide policy management UI.
- Solving cross-process storage for sensitive context values beyond the minimum needed for same-run resume.

## External Guidance

The major agent products and SDKs use interruption-oriented designs:

- Codex combines sandbox boundaries with approval policies. When the agent needs to cross a configured boundary or call side-effecting/destructive tools, it pauses for approval. Codex app-server and SDK expose thread resume primitives.
- Claude Agent SDK routes both tool approval requests and `AskUserQuestion` clarifications through a `canUseTool` callback. Execution remains paused until the application returns a decision, or the process defers and resumes from persisted session state.
- OpenAI Agents SDK uses `needsApproval` on tools. A run returns `interruptions` plus serialized `RunState`; the application approves or rejects and then resumes the same run state.

References:

- https://developers.openai.com/codex/agent-approvals-security
- https://developers.openai.com/codex/app-server
- https://code.claude.com/docs/en/agent-sdk/user-input.md
- https://code.claude.com/docs/en/agent-sdk/permissions.md
- https://code.claude.com/docs/en/agent-sdk/sessions.md
- https://openai.github.io/openai-agents-js/guides/human-in-the-loop/

## Existing Repository Context

The current agent path is:

1. `internal/httpapi` routes `POST /api/agent/runs`.
2. `internal/agent.Handler` validates the request and calls `Runner.Run`.
3. `internal/agent.Service.Run` starts a persisted run.
4. `Service.run` loops up to `MaxSteps`.
5. `OpenAIPlanner.NextAction` returns `call_tool` or `final_answer`.
6. `CLIExecutor` runs registered tools.
7. `Repository` persists run and step audit.

Current run status values are:

```text
running | succeeded | failed
```

Current planner action values are:

```text
call_tool | final_answer
```

The current loop keeps `observations`, `RunContext`, unknown-tool count, and business-error counts in memory. That is enough for a synchronous run, but not enough for a run that waits for user input after the HTTP request returns.

## Required Status Change

Add one run status:

```text
waiting_for_user
```

Need: this state is neither success nor failure. The backend has intentionally stopped and is waiting for an external user decision before it can continue.

Alternatives considered:

- Reuse `running`: misleading because no work is currently executing, and clients cannot tell that user input is required.
- Reuse `failed`: wrong behaviorally; user approval or clarification is an expected control point.
- Return a final answer asking the user to start a new run: loses run context and makes audit weaker.

Blast radius:

- `internal/agent` status constants and response handling.
- Database status values stored in `agent_runs`.
- Handler responses for create/resume.
- Tests for run lifecycle and repository persistence.
- Any clients that branch on run status.

This change is approved as part of this design.

## Interrupt Model

Add an interruption record owned by `internal/agent`.

```go
type InterruptType string
type InterruptStatus string

const (
	InterruptTypeApproval InterruptType = "approval"
	InterruptTypeQuestion InterruptType = "question"
)

const (
	InterruptStatusPending  InterruptStatus = "pending"
	InterruptStatusApproved InterruptStatus = "approved"
	InterruptStatusDenied   InterruptStatus = "denied"
	InterruptStatusAnswered InterruptStatus = "answered"
	InterruptStatusCanceled InterruptStatus = "canceled"
)

type Interrupt struct {
	ID        string
	RunID     string
	Type      InterruptType
	Status    InterruptStatus
	Payload   json.RawMessage
	Response  json.RawMessage
	CreatedAt time.Time
	ResolvedAt time.Time
}
```

Need: pending user interaction needs durable identity, status, payload, and response. It should be queryable independently of the final run output.

Alternatives considered:

- Store pending prompt text directly on `agent_runs`: too narrow for approvals, modified tool inputs, future UI details, and audit history.
- Store interruption only in step records: step records describe completed tool execution attempts; an approval prompt happens before execution.
- Keep it only in memory: breaks when the HTTP request ends or the server restarts.

Blast radius:

- New database model/table for agent interruptions.
- Repository methods for create, fetch pending, resolve.
- Handler/API changes to expose pending interruptions.
- Service changes to return `waiting_for_user`.
- Tests for approval, denial, question answer, and cancel paths.

This change is approved as part of this design.

## Interrupt Payloads

### Approval

An approval interruption captures the exact pending tool call.

```json
{
  "tool": "delete_account",
  "inputs": {
    "account_id": "acct_123"
  },
  "risk_level": "destructive",
  "reason": "This tool can permanently delete an account."
}
```

Allowed responses:

```json
{ "decision": "approve" }
{ "decision": "deny", "message": "Do not delete this account." }
{ "decision": "approve_with_changes", "inputs": { "account_id": "acct_456" } }
```

When approved, the service executes the pending tool call and continues the same run loop. When denied, the service appends an observation telling the planner that the user denied the action, then lets the planner choose an alternative or final answer.

### Question

A question interruption captures planner-generated clarifying questions.

```json
{
  "questions": [
    {
      "id": "target_account",
      "header": "Account",
      "question": "Which account should be deleted?",
      "options": [
        {
          "label": "Acme test",
          "description": "Delete the Acme test account."
        },
        {
          "label": "Cancel",
          "description": "Do not delete any account."
        }
      ],
      "multi_select": false
    }
  ],
  "allow_free_text": true
}
```

Allowed response:

```json
{
  "answers": {
    "target_account": "Acme test"
  },
  "response": "Use the test account, not the production account."
}
```

The service appends the answer as an observation and continues the planner loop.

## Approval Source

Dangerous operations must be determined by backend-owned policy, not by model self-reporting alone.

V1 should add approval metadata to registered tools:

```json
{
  "requires_approval": true,
  "risk_level": "destructive"
}
```

The model may also ask for confirmation as part of its plan, but the backend must enforce approval before executing any tool whose metadata requires it.

Need: the trusted boundary is the backend and tool catalog, not the planner output.

Alternatives considered:

- Prompt the model to ask before dangerous actions: useful as guidance but not enforceable.
- Pattern-match tool names such as `delete_*`: acceptable as a fallback but too brittle as the main policy.
- Require approval for every tool: safe but too noisy and makes the agent hard to use.

Blast radius:

- `internal/toolcatalog` tool model, validation, API request/response, and tests.
- Existing registered tools default to `requires_approval = true` when metadata is absent. This is intentionally conservative: old tool records should pause until a human or a later migration explicitly marks them safe to run without approval.
- `internal/agent` must check tool metadata before execution.

## State Persistence

To resume the same run, the service needs durable state beyond `agent_runs`.

Minimum V1 persisted state:

- current step order;
- observations so far;
- pending planner action for approval interruptions;
- pending question payload for question interruptions;
- unknown-tool count;
- repeated business-error counts;
- enough run context to resolve references used by future tool inputs.

The sensitive context decision is the hardest part. Today sensitive tool outputs live only in memory as `ctx://` references. A resumed run may need those values after the HTTP request returns.

V1 should take a conservative path:

- persist non-sensitive observations as JSON;
- persist `ctx://` references in observations;
- do not persist raw sensitive values unless a specific implementation design adds encryption or another explicit secret-store mechanism;
- if a pending run cannot resume because it would require an unavailable sensitive context value, fail the run with a clear error and audit message.

This avoids silently writing secrets to SQLite while keeping the first implementation tractable.

## HTTP API

### Create Run

`POST /api/agent/runs`

Existing successful terminal response remains valid:

```json
{
  "run_id": "run_123",
  "status": "succeeded",
  "answer": "Done."
}
```

New waiting response:

```json
{
  "run_id": "run_123",
  "status": "waiting_for_user",
  "interrupt": {
    "id": "int_456",
    "type": "approval",
    "payload": {
      "tool": "delete_account",
      "inputs": {
        "account_id": "acct_123"
      },
      "risk_level": "destructive",
      "reason": "This tool can permanently delete an account."
    }
  }
}
```

### Get Run

`GET /api/agent/runs/{run_id}`

Returns the current run status, final output when terminal, and pending interrupt when waiting.

### Resume Run

`POST /api/agent/runs/{run_id}/resume`

Approval request:

```json
{
  "interrupt_id": "int_456",
  "decision": "approve"
}
```

Question answer:

```json
{
  "interrupt_id": "int_789",
  "answers": {
    "target_account": "Acme test"
  }
}
```

The response shape matches create run: it may return another `waiting_for_user`, `succeeded`, or `failed`.

### Cancel Run

`POST /api/agent/runs/{run_id}/cancel`

Cancels a waiting run. V1 does not add a separate terminal run status for cancellation. It resolves the pending interrupt as `canceled`, marks the run `failed`, and stores an error summary such as `user canceled run`.

## Service Flow

Create or resume both use the same core loop.

```text
load run state
while step <= max_steps:
  if resuming an approved pending tool:
    execute pending tool
  else if resuming a denied approval:
    append denial observation
  else if resuming answered question:
    append answer observation
  else:
    ask planner for next action

  if final_answer:
    finish succeeded

  if question action:
    persist interrupt
    mark waiting_for_user
    return

  if call_tool and tool requires approval:
    persist interrupt with pending tool call
    mark waiting_for_user
    return

  execute tool
  persist step and observation

finish failed if max steps exceeded
```

## Planner Contract

The planner needs a new action type for clarifying questions:

```text
ask_user
```

Need: missing information is not a tool failure or final answer. It is an explicit request for user input.

Alternatives considered:

- Encode questions as `final_answer`: ends the run and loses continuation semantics.
- Encode questions as a fake tool: possible, but it mixes user interaction with business tools and makes policy harder to reason about.

Blast radius:

- `internal/agent` action constants and validation.
- Planner prompt output contract.
- Tests for parser validation.

This change is approved as part of this design.

Example planner action:

```json
{
  "type": "ask_user",
  "questions": [
    {
      "id": "account",
      "header": "Account",
      "question": "Which account should I delete?",
      "options": [
        {
          "label": "Test",
          "description": "Delete the test account."
        },
        {
          "label": "Production",
          "description": "Delete the production account."
        }
      ]
    }
  ]
}
```

## Error Handling

- Resume with an unknown run returns `404`.
- Resume when the run is not waiting returns `409`.
- Resume with the wrong interrupt id returns `409`.
- Resume with invalid approval or answer body returns `400`.
- Approval denial is not an HTTP error; it is user input that gets fed back to the planner.
- Tool execution failure after approval follows the current failed-step behavior.
- If finishing a failed run also fails, preserve the current bounded finish behavior.

## Testing

Add focused tests around the new lifecycle:

- create run pauses before a tool with `requires_approval`;
- approving resumes the same `run_id` and executes the tool;
- denying resumes the same `run_id` and gives the planner a denial observation;
- planner `ask_user` action creates a question interruption;
- answering a question resumes the same `run_id`;
- resume rejects stale, wrong, or already-resolved interrupt ids;
- waiting runs are persisted and visible through repository methods;
- sensitive context is not persisted in plaintext.

## First Implementation Slice

The smallest useful slice is:

1. Add `waiting_for_user` run status.
2. Add `ask_user` planner action.
3. Add tool approval metadata in `internal/toolcatalog`.
4. Add agent interruption persistence.
5. Add resume endpoint.
6. Pause before approval-required tools execute.
7. Resume approval and denial flows.
8. Add question interruptions and answer resume.
9. Preserve current synchronous behavior for runs that do not interrupt.

This keeps the architecture close to the existing service while making interruption a durable, first-class state.
