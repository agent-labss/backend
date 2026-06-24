# Agent User Turn Resume Design

## Summary

Add first-class support for agent runs that pause for user interaction and continue under the same `run_id` when the user sends another natural-language turn.

The current agent loop is synchronous and terminal:

```text
create run -> planner -> tool loop -> succeeded | failed
```

That does not fit task flows where the agent needs more information, confirmation, or revised direction from the user. The API should not force users to send structured approval decisions such as `approve` or `deny`. Users may reply with natural language:

```text
ok
不太行，先别删
用我刚上传的这个文件继续
```

The backend stores the run as `waiting_for_user`, accepts another user turn with optional attachments, gives that turn back to the planner with the pending interaction context, and continues the same run.

```text
POST /api/agent/runs
        |
        v
running -> waiting_for_user
              |
              v
POST /api/agent/runs/{run_id}/turns
              |
              v
running -> waiting_for_user | succeeded | failed
```

## Goals

- Pause a run when the planner needs user interaction.
- Continue the same `run_id` when the user sends a follow-up turn.
- Let user follow-up turns be natural language, not structured approval commands.
- Support attachments on both the initial run and follow-up turns.
- Persist enough run state to continue after the original HTTP request has ended.
- Preserve the existing controlled execution model: the model plans, but the backend executes only registered tools.
- Keep the first implementation scoped to HTTP request/response flows, without background workers or streaming.
- Audit the pending interaction and each user turn that resolves or advances it.

## Non-Goals

- Building a fully asynchronous distributed worker system.
- Streaming live agent events to clients.
- Supporting multiple simultaneous pending interactions for one run in V1.
- Adding a public `/resume` endpoint.
- Adding a public `/cancel` endpoint.
- Requiring users to send structured `approve`, `deny`, or `answer` fields.
- Implementing backend-enforced dangerous-tool approval policy in V1.
- Implementing organization-wide policy management UI.
- Solving encrypted persistence for raw sensitive context values in this slice.

## External Guidance

Major agent products and SDKs use interruption-oriented designs, but the API shape varies:

- Codex combines sandbox boundaries with approval policies and exposes thread resume primitives.
- Claude Agent SDK routes tool approval requests and `AskUserQuestion` clarifications through an application callback. Execution pauses until the application returns user input.
- OpenAI Agents SDK can return `interruptions` plus serialized run state, then continue after the application resolves them.

For this backend, the user-facing HTTP API should be simpler: the user always sends another turn to the same run. The planner interprets that turn in context.

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

The current loop keeps `observations`, `RunContext`, unknown-tool count, and business-error counts in memory. That is enough for a synchronous run, but not enough for a run that waits for a later user turn after the HTTP request returns.

The current create-run handler already supports JSON and multipart input with attachments. The follow-up turn endpoint should reuse the same multimodal input boundary.

## Required Status Change

Add one run status:

```text
waiting_for_user
```

Need: this state is neither success nor failure. The backend has intentionally stopped and is waiting for another user turn before it can continue.

Alternatives considered:

- Reuse `running`: misleading because no work is executing, and clients cannot tell that user input is required.
- Reuse `failed`: wrong behaviorally; a user interaction checkpoint is expected.
- Return a final answer asking the user to start a new run: loses run context and makes audit weaker.

Blast radius:

- `internal/agent` status constants and response handling.
- Database status values stored in `agent_runs`.
- Handler responses for create run, get run, and create turn.
- Tests for run lifecycle and repository persistence.
- Any clients that branch on run status.

This change is approved as part of this design.

## Interaction Model

Add a pending interaction record owned by `internal/agent`.

```go
type InteractionType string
type InteractionStatus string

const (
	InteractionTypeUserInput InteractionType = "user_input"
)

const (
	InteractionStatusPending   InteractionStatus = "pending"
	InteractionStatusResponded InteractionStatus = "responded"
)

type Interaction struct {
	ID         string
	RunID      string
	Type       InteractionType
	Status     InteractionStatus
	Message    string
	Payload    json.RawMessage
	CreatedAt  time.Time
	RespondedAt time.Time
}
```

Need: pending user interaction needs durable identity, display text, optional structured payload, and status. It should be queryable independently of final run output.

Alternatives considered:

- Store pending prompt text directly on `agent_runs`: too narrow for UI payloads, future choices, and audit history.
- Store interaction only in step records: step records describe completed tool execution attempts; a user interaction happens between planner turns.
- Keep it only in memory: breaks when the HTTP request ends or the server restarts.

Blast radius:

- New database model/table for agent interactions.
- Repository methods for create, fetch pending, and mark responded.
- Handler/API changes to expose pending interactions.
- Service changes to return `waiting_for_user`.
- Tests for waiting, user-turn response, and terminal-state rejection paths.

This change is approved as part of this design.

## Planner Contract

The planner needs a new action type:

```text
ask_user
```

Need: missing information or confirmation is not a tool failure or final answer. It is an explicit request for another user turn.

Alternatives considered:

- Encode the request as `final_answer`: ends the run and loses continuation semantics.
- Encode it as a fake business tool: mixes user interaction with registered operation tools.
- Let the HTTP API expose structured approval actions: too rigid for natural language replies such as `ok` or `不太行`.

Blast radius:

- `internal/agent` action constants and validation.
- Planner prompt output contract.
- Tests for parser validation.

This change is approved as part of this design.

Example planner action:

```json
{
  "type": "ask_user",
  "message": "I found two matching accounts. Which one should I use?",
  "payload": {
    "questions": [
      {
        "id": "account",
        "header": "Account",
        "question": "Which account should I use?",
        "options": [
          {
            "label": "Test",
            "description": "Use the test account."
          },
          {
            "label": "Production",
            "description": "Use the production account."
          }
        ],
        "allow_free_text": true
      }
    ]
  }
}
```

The payload is for UI assistance only. The user is not required to answer in that structure. A plain text reply is valid.

## User Turn Model

Add a user turn request owned by `internal/agent`.

```go
type CreateRunTurnRequest struct {
	Message     string       `json:"message"`
	Attachments []Attachment `json:"attachments,omitempty"`
}
```

This intentionally mirrors `CreateRunRequest` so users can provide new files while answering or clarifying.

Examples:

```json
{
  "message": "ok"
}
```

```json
{
  "message": "不太行，先别删。用这个表里的账号继续。",
  "attachments": [
    {
      "filename": "accounts.csv",
      "mime_type": "text/csv",
      "data": "base64..."
    }
  ]
}
```

The service stores the user turn as run history and passes it to the planner with:

- the original user request;
- previous observations;
- the pending interaction message and payload;
- the new user turn message;
- the new turn attachments.

The planner decides whether the user intended approval, refusal, correction, extra data, or a change of direction.

## HTTP API

### Create Run

`POST /api/agent/runs`

Supports the existing JSON and multipart formats.

JSON:

```json
{
  "message": "根据我上传的 pdf，更新商家的目录",
  "attachments": [
    {
      "filename": "merchant_catalog.pdf",
      "mime_type": "application/pdf",
      "data": "base64..."
    }
  ]
}
```

Multipart:

```text
POST /api/agent/runs
Content-Type: multipart/form-data

message=根据我上传的 pdf，更新商家的目录
files[]=merchant_catalog.pdf
```

Terminal response:

```json
{
  "run_id": "run_123",
  "status": "succeeded",
  "answer": "Done."
}
```

Waiting response:

```json
{
  "run_id": "run_123",
  "status": "waiting_for_user",
  "interaction": {
    "id": "int_456",
    "type": "user_input",
    "message": "I found two matching accounts. Which one should I use?",
    "payload": {
      "questions": [
        {
          "id": "account",
          "header": "Account",
          "question": "Which account should I use?",
          "options": [
            {
              "label": "Test",
              "description": "Use the test account."
            },
            {
              "label": "Production",
              "description": "Use the production account."
            }
          ],
          "allow_free_text": true
        }
      ]
    }
  }
}
```

### Get Run

`GET /api/agent/runs/{run_id}`

Returns the current run status, final output when terminal, and pending interaction when waiting.

### Create User Turn

`POST /api/agent/runs/{run_id}/turns`

Supports JSON and multipart, matching create run.

JSON:

```json
{
  "message": "ok"
}
```

Multipart:

```text
POST /api/agent/runs/run_123/turns
Content-Type: multipart/form-data

message=用我新上传的这个表继续
files[]=accounts.csv
```

The response shape matches create run. A turn may complete the task, fail the task, or produce another `waiting_for_user` interaction.

There is no public `/resume` endpoint. Resuming is an internal service behavior triggered by creating a user turn.

There is no public `/cancel` endpoint in V1. If the user wants to stop, they send a turn such as `不做了` or `取消这个任务`; the planner can then return a final answer or stop path. A hard cancellation endpoint can be added later if product requirements need it.

## Fiber Route Shape

`internal/httpapi.AgentHandler` should grow from one method to three:

```go
type AgentHandler interface {
	CreateRun(c fiber.Ctx) error
	GetRun(c fiber.Ctx) error
	CreateRunTurn(c fiber.Ctx) error
}
```

Route registration:

```go
app.Post(agent.AgentRunsPath, config.AgentHandler.CreateRun)
app.Get(agent.AgentRunsPath+"/:run_id", config.AgentHandler.GetRun)
app.Post(agent.AgentRunsPath+"/:run_id/turns", config.AgentHandler.CreateRunTurn)
```

Route paths should remain constants or typed constants owned by `internal/agent` if they are repeated.

The existing router body limit issue remains relevant: `httpapi.uploadBodyLimit` is `10MB`, while agent upload configuration can allow a larger total. The implementation should either align the Fiber body limit with configured agent upload limits or explicitly document that the HTTP layer caps the request earlier.

## Service Flow

Create run and create user turn both use the same core loop after they persist their inbound user input.

```text
create run:
  store run and initial user message/attachments
  build state
  continue loop

create user turn:
  require run status waiting_for_user
  store user turn message/attachments
  mark pending interaction responded
  build state with pending interaction + user turn
  continue loop

continue loop:
  while step <= max_steps:
    ask planner for next action

    if final_answer:
      finish succeeded

    if ask_user:
      persist interaction
      mark waiting_for_user
      return

    if call_tool:
      execute tool
      persist step and observation

  finish failed if max steps exceeded
```

Planner interpretation of the user turn is intentional. For example:

- `ok` may lead the planner to proceed with the previously described action.
- `不太行` may lead the planner to choose another approach.
- `先别删，导出数据` may lead the planner to call an export tool instead of a delete tool.
- A new attachment may give the planner the missing input it asked for.

## State Persistence

To continue the same run, the service needs durable state beyond `agent_runs`.

Minimum V1 persisted state:

- current step order;
- initial user message and attachments;
- follow-up user turns and attachments;
- observations so far;
- pending interaction message and payload;
- unknown-tool count;
- repeated business-error counts;
- enough run context to resolve references used by future tool inputs.

The sensitive context decision remains constrained. Today sensitive tool outputs live only in memory as `ctx://` references. A later user turn may need those values after the original HTTP request returns.

V1 should take a conservative path:

- persist non-sensitive observations as JSON;
- persist `ctx://` references in observations;
- do not persist raw sensitive values unless a specific implementation design adds encryption or another explicit secret-store mechanism;
- if a waiting run cannot continue because it would require an unavailable sensitive context value, fail the run with a clear error and audit message.

This avoids silently writing secrets to SQLite while keeping the first implementation tractable.

## Error Handling

- `GET` with an unknown run returns `404`.
- Creating a turn for an unknown run returns `404`.
- Creating a turn when the run is not `waiting_for_user` returns `409`.
- Creating a turn with blank message and no attachments returns `400`.
- Invalid JSON, invalid multipart data, unsupported attachments, and oversized attachments follow the existing create-run validation style.
- A user turn is not itself an approval or denial at the HTTP layer. It is user input that gets fed back to the planner.
- Tool execution failure after a user turn follows the current failed-step behavior.
- If finishing a failed run also fails, preserve the current bounded finish behavior.

## Testing

Add focused tests around the new lifecycle:

- create run can return `waiting_for_user` with an interaction;
- `GET /api/agent/runs/{run_id}` returns pending interaction for a waiting run;
- creating a JSON user turn continues the same `run_id`;
- creating a multipart user turn accepts attachments and passes them to the planner;
- creating a turn for a terminal run returns `409`;
- creating a turn with blank message and no attachments returns `400`;
- planner `ask_user` action creates a user interaction;
- answering via natural language lets the planner decide the next action;
- waiting runs, user turns, and attachments are persisted and visible through repository methods;
- sensitive context is not persisted in plaintext.

## Future TODO: Backend-Enforced Dangerous Tool Approval

Backend-enforced dangerous tool approval is important but intentionally out of V1.

Future work should add approval metadata to registered tools:

```json
{
  "requires_approval": true,
  "risk_level": "destructive"
}
```

The backend would then pause before executing approval-required tools, regardless of whether the model remembered to ask. This creates a stronger safety boundary than relying on prompt instructions alone.

When the user sends a natural-language turn after such a pause, the planner may interpret the message and decide whether to request the same tool call again. The backend must still bind execution to the current pending interaction: only the dangerous operation represented by that pending interaction can be released. Any new dangerous operation, even if similar, must create a new pending interaction and return to `waiting_for_user`.

This future work needs a separate design decision because it affects:

- `internal/toolcatalog` tool model, validation, API request/response, and tests;
- default behavior for existing tool records that lack approval metadata;
- `internal/agent` enforcement before tool execution;
- how natural-language user turns authorize only the pending risky action without authorizing unrelated future risky actions.

V1 does not implement this enforcement. In V1, user interaction is planner-driven through `ask_user` and follow-up user turns.

## First Implementation Slice

The smallest useful slice is:

1. Add `waiting_for_user` run status.
2. Add `ask_user` planner action.
3. Add agent interaction persistence.
4. Add user turn persistence.
5. Add `GET /api/agent/runs/{run_id}`.
6. Add `POST /api/agent/runs/{run_id}/turns`.
7. Support JSON and multipart user turns, including attachments.
8. Continue the same `run_id` after a user turn.
9. Preserve current synchronous behavior for runs that do not need user interaction.

This keeps the HTTP API task-oriented while allowing users to respond naturally through the agent.
