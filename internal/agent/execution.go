package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"ai/backend/internal/toolcatalog"
)

const (
	AgentExecutionStatusRunning     AgentExecutionStatus = "running"
	AgentExecutionStatusInterrupted AgentExecutionStatus = "interrupted"
	AgentExecutionStatusSucceeded   AgentExecutionStatus = "succeeded"
	AgentExecutionStatusFailed      AgentExecutionStatus = "failed"
)

const (
	StepStatusSucceeded StepStatus = "succeeded"
	StepStatusFailed    StepStatus = "failed"
)

const (
	ActionTypeCallTool    ActionType = "call_tool"
	ActionTypeAskUser     ActionType = "ask_user"
	ActionTypeFinalAnswer ActionType = "final_answer"
)

const (
	InterruptionTypeApproval     InterruptionType = "approval"
	InterruptionTypeInputRequest InterruptionType = "input_request"
)

const (
	InterruptionStatusAwaitingReview InterruptionStatus = "awaiting_review"
	InterruptionStatusApproved       InterruptionStatus = "approved"
	InterruptionStatusRejected       InterruptionStatus = "rejected"
	InterruptionStatusResolved       InterruptionStatus = "resolved"
	InterruptionStatusCancelled      InterruptionStatus = "cancelled"
)

type AgentExecutionStatus string
type StepStatus string
type ActionType string
type InterruptionType string
type InterruptionStatus string

type executionRequest struct {
	Message     string       `json:"message"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type CreateAgentExecutionRecord struct {
	SessionID        string
	TriggerMessageID string
}

type AgentExecutionResponse struct {
	ExecutionID  string               `json:"execution_id"`
	Status       AgentExecutionStatus `json:"status"`
	Answer       string               `json:"answer,omitempty"`
	Outputs      map[string]any       `json:"outputs,omitempty"`
	Error        string               `json:"error,omitempty"`
	Interruption *Interruption        `json:"interruption,omitempty"`
}

type AgentExecution struct {
	ID               string
	SessionID        string
	TriggerMessageID string
	Status           AgentExecutionStatus
	ErrorSummary     string
	StartedAt        time.Time
	FinishedAt       time.Time
}

type Interruption struct {
	ID          string             `json:"id"`
	SessionID   string             `json:"session_id,omitempty"`
	ExecutionID string             `json:"execution_id,omitempty"`
	Type        InterruptionType   `json:"type"`
	Status      InterruptionStatus `json:"status,omitempty"`
	Message     string             `json:"message"`
	Payload     json.RawMessage    `json:"payload,omitempty"`
	CreatedAt   time.Time          `json:"created_at,omitempty"`
	RespondedAt time.Time          `json:"responded_at,omitempty"`
}

type ObservationRecord struct {
	ExecutionID string
	StepOrder   int
	Observation Observation
}

type AgentExecutionStateRecord struct {
	AgentExecution     AgentExecution
	Interruptions      []Interruption
	ActiveInterruption *Interruption
	Observations       []Observation
}

type StepRecord struct {
	ID            string
	ExecutionID   string
	StepOrder     int
	ToolID        string
	InputSummary  json.RawMessage
	OutputSummary json.RawMessage
	DurationMS    int64
	Status        StepStatus
	ErrorSummary  string
	CreatedAt     time.Time
}

type PlannerAction struct {
	Type    ActionType      `json:"type"`
	Tool    string          `json:"tool,omitempty"`
	Inputs  json.RawMessage `json:"inputs,omitempty"`
	Answer  string          `json:"answer,omitempty"`
	Outputs map[string]any  `json:"outputs,omitempty"`
	Message string          `json:"message,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Observation struct {
	StepOrder int            `json:"step_order"`
	ToolName  string         `json:"tool_name"`
	Status    StepStatus     `json:"status"`
	Outputs   map[string]any `json:"outputs,omitempty"`
	Error     string         `json:"error,omitempty"`
}

func (service Service) executeNewChatExecution(parent context.Context, execution AgentExecution, sessionID string, request executionRequest) {
	ctx, cancel := context.WithTimeout(parent, service.totalTimeout)
	defer cancel()

	response, executionErr := service.execute(ctx, execution, request)
	service.finishBackgroundExecution(ctx, execution, sessionID, response, executionErr)
}

func (service Service) executeResumedChatExecution(parent context.Context, execution AgentExecution, sessionID string, request executionRequest, interruption *Interruption, observations []Observation) {
	ctx, cancel := context.WithTimeout(parent, service.totalTimeout)
	defer cancel()

	response, executionErr := service.resumeExecution(ctx, execution, request, interruption, observations)
	service.finishBackgroundExecution(ctx, execution, sessionID, response, executionErr)
}

func (service Service) execute(ctx context.Context, execution AgentExecution, request executionRequest) (AgentExecutionResponse, error) {
	return service.executeFromStep(ctx, execution, request, nil, nil, 1)
}

func (service Service) resumeExecution(
	ctx context.Context,
	execution AgentExecution,
	request executionRequest,
	interruption *Interruption,
	observations []Observation,
) (AgentExecutionResponse, error) {
	return service.executeFromStep(ctx, execution, request, interruption, observations, nextStepOrder(observations))
}

func (service Service) executeFromStep(
	ctx context.Context,
	execution AgentExecution,
	request executionRequest,
	interruption *Interruption,
	observations []Observation,
	startStepOrder int,
) (AgentExecutionResponse, error) {
	state, err := service.newExecutionState(ctx, execution, request)
	if err != nil {
		return AgentExecutionResponse{}, err
	}
	state.interruption = interruption
	state.observations = append(state.observations, observations...)

	for stepOrder := startStepOrder; stepOrder < startStepOrder+service.maxSteps; stepOrder++ {
		response, done, err := service.executionStep(ctx, state, stepOrder)
		if err != nil || done {
			return response, err
		}
	}

	return AgentExecutionResponse{}, fmt.Errorf("%w: step limit exceeded", ErrAgentExecutionFailed)
}

func nextStepOrder(observations []Observation) int {
	maxStepOrder := 0
	for _, observation := range observations {
		if observation.StepOrder > maxStepOrder {
			maxStepOrder = observation.StepOrder
		}
	}
	return maxStepOrder + 1
}

func (service Service) newExecutionState(ctx context.Context, execution AgentExecution, request executionRequest) (*executionState, error) {
	instructions, err := service.catalog.GetInstructions(ctx)
	if err != nil {
		return nil, fmt.Errorf("get instructions: %w", err)
	}
	tools, err := service.catalog.ListEnabledTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	return &executionState{
		execution:           execution,
		message:             request.Message,
		attachments:         request.Attachments,
		instructions:        instructions,
		tools:               tools,
		toolsByName:         indexToolsByName(tools),
		executionContext:    NewExecutionContext(),
		businessErrorCounts: make(map[string]int),
	}, nil
}

func (service Service) executionStep(ctx context.Context, state *executionState, stepOrder int) (AgentExecutionResponse, bool, error) {
	action, err := service.planner.NextAction(ctx, PlanRequest{
		Instructions: state.instructions.Content,
		Message:      state.message,
		Attachments:  state.attachments,
		Interruption: state.interruption,
		Tools:        state.tools,
		Observations: state.observations,
	})
	if err != nil {
		return AgentExecutionResponse{}, false, fmt.Errorf("plan next action: %w", err)
	}

	switch action.Type {
	case ActionTypeFinalAnswer:
		return finalExecutionResponse(state.execution.ID, action), true, nil
	case ActionTypeAskUser:
		response, err := service.askUser(ctx, state, action)
		return response, true, err
	case ActionTypeCallTool:
		return AgentExecutionResponse{}, false, service.callTool(ctx, state, stepOrder, action)
	default:
		return AgentExecutionResponse{}, false, fmt.Errorf("%w: invalid action type %q", ErrAgentExecutionFailed, action.Type)
	}
}

func (service Service) askUser(ctx context.Context, state *executionState, action PlannerAction) (AgentExecutionResponse, error) {
	interruption, err := service.executionStore.createInterruption(ctx, Interruption{
		SessionID:   state.execution.SessionID,
		ExecutionID: state.execution.ID,
		Type:        InterruptionTypeInputRequest,
		Status:      InterruptionStatusAwaitingReview,
		Message:     action.Message,
		Payload:     action.Payload,
	})
	if err != nil {
		return AgentExecutionResponse{}, fmt.Errorf("create interruption: %w", err)
	}

	state.execution.Status = AgentExecutionStatusInterrupted
	if err := service.executionStore.markExecutionInterrupted(ctx, state.execution, interruption); err != nil {
		return AgentExecutionResponse{}, fmt.Errorf("mark execution interrupted: %w", err)
	}

	return AgentExecutionResponse{
		ExecutionID:  state.execution.ID,
		Status:       AgentExecutionStatusInterrupted,
		Interruption: &interruption,
	}, nil
}

func (service Service) callTool(ctx context.Context, state *executionState, stepOrder int, action PlannerAction) error {
	tool, ok := state.toolsByName[action.Tool]
	if !ok {
		return service.recordUnknownTool(ctx, state, stepOrder, action.Tool)
	}

	inputs, err := decodeInputs(action.Inputs)
	if err != nil {
		return err
	}

	service.eventBus.Publish(state.execution.SessionID, ChatEvent{
		Type:        EventTypeToolStarted,
		ChatID:      state.execution.SessionID,
		ExecutionID: state.execution.ID,
		ToolName:    tool.Name,
	})
	observation, step, err := service.executeTool(ctx, state, stepOrder, tool, inputs)
	if saveErr := service.saveStep(ctx, step); saveErr != nil {
		return saveErr
	}
	if err != nil {
		service.publishToolFinished(state.execution, tool.Name, observation)
		return err
	}

	state.observations = append(state.observations, observation)
	if err := service.executionStore.saveObservation(ctx, ObservationRecord{
		ExecutionID: state.execution.ID,
		StepOrder:   stepOrder,
		Observation: observation,
	}); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}
	service.publishToolFinished(state.execution, tool.Name, observation)
	if observation.Status == StepStatusFailed {
		return recordBusinessError(state, tool.Name, observation.Error)
	}

	return nil
}

func (service Service) executeTool(
	ctx context.Context,
	state *executionState,
	stepOrder int,
	tool toolcatalog.Tool,
	inputs map[string]any,
) (Observation, StepRecord, error) {
	started := time.Now()
	observation, err := service.executor.Execute(ctx, ExecuteRequest{
		ExecutionID:      state.execution.ID,
		StepID:           fmt.Sprintf("step_%d", stepOrder),
		StepOrder:        stepOrder,
		Tool:             tool,
		Inputs:           inputs,
		ExecutionContext: state.executionContext,
	})
	step := stepRecord(state.execution.ID, stepOrder, tool.ID, inputs, observation, time.Since(started).Milliseconds())
	if err != nil {
		step.Status = StepStatusFailed
		step.ErrorSummary = err.Error()
		return observation, step, fmt.Errorf("execute tool %q: %w", tool.Name, err)
	}

	return observation, step, nil
}

func (service Service) recordUnknownTool(ctx context.Context, state *executionState, stepOrder int, toolName string) error {
	state.unknownToolCount++
	observation := Observation{StepOrder: stepOrder, ToolName: toolName, Status: StepStatusFailed, Error: "unknown tool"}
	step := StepRecord{
		ExecutionID:   state.execution.ID,
		StepOrder:     stepOrder,
		ToolID:        "",
		InputSummary:  mustMarshalJSON(map[string]any{}),
		OutputSummary: mustMarshalJSON(map[string]any{errorField: observation.Error}),
		Status:        StepStatusFailed,
		ErrorSummary:  observation.Error,
	}
	if err := service.saveStep(ctx, step); err != nil {
		return err
	}
	if state.unknownToolCount > 1 {
		return fmt.Errorf("%w: unknown tool %q", ErrAgentExecutionFailed, toolName)
	}

	state.observations = append(state.observations, observation)
	if err := service.executionStore.saveObservation(ctx, ObservationRecord{
		ExecutionID: state.execution.ID,
		StepOrder:   stepOrder,
		Observation: observation,
	}); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}
	service.publishToolFinished(state.execution, toolName, observation)
	return nil
}

func (service Service) publishToolFinished(execution AgentExecution, toolName string, observation Observation) {
	savedObservation := observation
	service.eventBus.Publish(execution.SessionID, ChatEvent{
		Type:        EventTypeToolFinished,
		ChatID:      execution.SessionID,
		ExecutionID: execution.ID,
		ToolName:    toolName,
		Observation: &savedObservation,
	})
}

func (service Service) saveStep(ctx context.Context, step StepRecord) error {
	if err := service.executionStore.saveStep(ctx, step); err != nil {
		return fmt.Errorf("save step: %w", err)
	}

	return nil
}

func stepRecord(
	executionID string,
	stepOrder int,
	toolID string,
	inputs map[string]any,
	observation Observation,
	durationMS int64,
) StepRecord {
	step := StepRecord{
		ExecutionID:   executionID,
		StepOrder:     stepOrder,
		ToolID:        toolID,
		InputSummary:  mustMarshalJSON(RedactJSONValue(inputs)),
		OutputSummary: mustMarshalJSON(RedactJSONValue(nonNilMap(observation.Outputs))),
		DurationMS:    durationMS,
		Status:        StepStatusSucceeded,
	}
	if observation.Status == StepStatusFailed {
		step.Status = StepStatusFailed
		step.ErrorSummary = observation.Error
		step.OutputSummary = mustMarshalJSON(map[string]any{"error": observation.Error})
	}

	return step
}

func recordBusinessError(state *executionState, toolName string, errorSummary string) error {
	errorKey := toolName + "\x00" + errorSummary
	state.businessErrorCounts[errorKey]++
	if state.businessErrorCounts[errorKey] > 1 {
		return fmt.Errorf("%w: repeated tool error from %q: %s", ErrAgentExecutionFailed, toolName, errorSummary)
	}

	return nil
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
		return nil, fmt.Errorf("%w: decode tool inputs: %w", ErrAgentExecutionFailed, err)
	}
	return nonNilMap(inputs), nil
}

func mustMarshalJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func redactOutputs(outputs map[string]any) map[string]any {
	return RedactJSONValue(nonNilMap(outputs))
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}

	return value
}
