# Chat SSE Design

## Goal

Move chat message execution from synchronous HTTP responses to an asynchronous run model with chat-scoped Server-Sent Events (SSE). A user message should return quickly while the backend continues planner/tool execution in the background and pushes live chat/run updates to connected clients.

## Current State

The current chat-first path is synchronous:

1. `POST /api/chats/:chat_id/messages` saves the user message.
2. The service creates or resumes an agent run.
3. The run calls the planner, tools, interruption logic, and final-answer logic before the HTTP request returns.
4. The response includes the user message, optional assistant message, run response, and optional interruption.

This works for short tasks, but long tool calls hold the HTTP request until `totalTimeout` or completion. The UI cannot receive live progress such as tool start/finish while the request is in flight.

## Decisions

- Use chat-scoped SSE: `GET /api/chats/:chat_id/events`.
- Change `POST /api/chats/:chat_id/messages` to return `202 Accepted` after persisting the user message and scheduling the run.
- Keep event delivery in memory. Events are not persisted and do not support `Last-Event-ID` replay.
- Keep persisted chat messages, runs, interruptions, observations, and steps as the source of truth.
- Use `GET /api/chats/:chat_id/messages` to recover UI state after refresh, reconnect, or process restart.
- Preserve `totalTimeout` as a background run execution limit, not an HTTP request limit.

## API

### Subscribe To Chat Events

```http
GET /api/chats/:chat_id/events
Accept: text/event-stream
```

Behavior:

- Return `404` if the chat does not exist.
- Return `Content-Type: text/event-stream`.
- Send a `connected` event immediately after subscription succeeds.
- Push only new realtime events after the connection is established.
- Do not replay missed events.

### Submit Chat Message

```http
POST /api/chats/:chat_id/messages
```

The endpoint persists the user message, creates or resumes a run, starts background execution, and returns:

```json
{
  "chat_id": "chat_123",
  "user_message": {},
  "run_id": "run_456",
  "status": "running"
}
```

Status code: `202 Accepted`.

If the chat has an active interruption, the same endpoint saves the user's confirmation/cancellation text, resolves the interruption, resumes the existing run, and returns the existing `run_id`.

## SSE Events

Initial event set:

- `connected`: subscription is active.
- `message.created`: a user or assistant chat message was persisted.
- `run.started`: a new run started.
- `run.resumed`: an interrupted run resumed.
- `tool.started`: a tool call is starting.
- `tool.finished`: a tool call finished and observation was saved.
- `interruption.created`: the run stopped for user input or approval.
- `interruption.resolved`: the user responded to an interruption.
- `run.completed`: the run completed successfully.
- `run.failed`: the run failed.

Each event should include `chat_id`, an event-specific payload, and enough IDs for the client to reconcile with persisted state, such as `message_id`, `run_id`, `tool_name`, or `interruption_id`.

Assistant replies are represented by `message.created` with `role = "assistant"`.

## Internal Architecture

### Event Bus

Add an in-process `agent.EventBus`:

- `Subscribe(chatID)` returns a subscription channel and cleanup function.
- `Publish(chatID, event)` broadcasts to current subscribers for that chat.
- Subscriber channels are buffered.
- If a subscriber buffer fills, close that subscriber to avoid blocking run execution.
- Disconnect cleanup removes the subscriber.

The event bus is intentionally process-local for this phase. Multi-instance delivery can later be replaced with Redis/pubsub or persisted events without changing the external SSE API.

### Service Execution

Split message handling into submission and background execution:

- `CreateChatMessage` becomes a submit/schedule operation.
- It validates the chat, saves the user message, checks for active interruption, creates or resolves run state, publishes immediate events, starts a goroutine, and returns `202` data.
- The goroutine reuses existing planner/tool/run/interruption/final-answer behavior.
- Existing repository persistence remains the source of truth.

Publish points:

- After saving the user message: `message.created`.
- After creating a new run: `run.started`.
- After resolving an interruption: `interruption.resolved`, then `run.resumed`.
- Before tool execution: `tool.started`.
- After observation/step persistence: `tool.finished`.
- After creating an interruption and assistant prompt: `interruption.created`, `message.created`.
- After final assistant message: `message.created`, `run.completed`.
- On run failure: `run.failed`.

### Timeouts

`totalTimeout` applies to the background run context. The HTTP submit request should not wait for planner/tool completion, so long tool calls no longer block the user request. Failed or timed-out runs still finish through the existing failure path and publish `run.failed`.

## Error Handling

- Missing chat on SSE subscribe returns `404`.
- Missing chat on message submit keeps existing not-found behavior.
- SSE client disconnect does not cancel background run execution.
- Backend process restart loses realtime events but not persisted state.
- Clients recover by fetching chat messages after reconnect.
- Run errors are redacted with existing redaction behavior before being persisted or published.

## Testing

Add focused tests for:

- Event bus subscribe, publish, unsubscribe, and slow-subscriber closure.
- `POST /messages` returning `202` without waiting for tool completion.
- Scheduling a new run publishes user message and run started events.
- Background completion publishes assistant message and run completed events.
- Active interruption submit resolves the interruption and resumes the existing run.
- SSE handler returns `text/event-stream`, sends `connected`, and forwards published events.
- Existing repository and service behavior remains covered by `go test ./...`.

Final verification remains:

```bash
./scripts/repo-guard.sh
```

## Out Of Scope

- Persisted SSE event history.
- `Last-Event-ID` replay.
- WebSocket support.
- Multi-process event distribution.
- A dedicated approval/cancel endpoint. Confirmation and cancellation remain normal chat messages interpreted by the planner.
