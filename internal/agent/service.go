package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ai/backend/internal/toolcatalog"
)

var ErrRunFailed = errors.New("agent run failed")

const defaultFailedRunFinishTimeout = 5 * time.Second

type Catalog interface {
	ListEnabledTools(ctx context.Context) ([]toolcatalog.Tool, error)
	GetInstructions(ctx context.Context) (toolcatalog.Instructions, error)
}

type Executor interface {
	Execute(ctx context.Context, request ExecuteRequest) (Observation, error)
}

type ServiceConfig struct {
	Planner      Planner
	Catalog      Catalog
	Executor     Executor
	RunStore     RunStore
	MaxSteps     int
	TotalTimeout time.Duration
}

type RunStore struct {
	startRun                 func(ctx context.Context, record CreateRunRecord) (Run, error)
	getRun                   func(ctx context.Context, runID string) (RunResponse, error)
	getRunState              func(ctx context.Context, runID string) (RunStateRecord, error)
	finishRun                func(ctx context.Context, run Run) error
	saveStep                 func(ctx context.Context, step StepRecord) error
	createInteraction        func(ctx context.Context, interaction Interaction) (Interaction, error)
	markRunWaiting           func(ctx context.Context, run Run, interaction Interaction) error
	createRunTurn            func(ctx context.Context, record CreateRunTurnRecord) (RunTurn, error)
	markInteractionResponded func(ctx context.Context, interactionID string, turnID string) error
	saveObservation          func(ctx context.Context, record ObservationRecord) error
}

func NewRunStore(repository Repository) RunStore {
	return RunStore{
		startRun:                 repository.StartRun,
		getRun:                   repository.GetRun,
		getRunState:              repository.GetRunState,
		finishRun:                repository.FinishRun,
		saveStep:                 repository.SaveStep,
		createInteraction:        repository.CreateInteraction,
		markRunWaiting:           repository.MarkRunWaiting,
		createRunTurn:            repository.CreateRunTurn,
		markInteractionResponded: repository.MarkInteractionResponded,
		saveObservation:          repository.SaveObservation,
	}
}

type Service struct {
	planner                Planner
	catalog                Catalog
	executor               Executor
	runStore               RunStore
	maxSteps               int
	totalTimeout           time.Duration
	failedRunFinishTimeout time.Duration
}

type runState struct {
	run                 Run
	message             string
	attachments         []Attachment
	interaction         *Interaction
	instructions        toolcatalog.Instructions
	tools               []toolcatalog.Tool
	toolsByName         map[string]toolcatalog.Tool
	runContext          *RunContext
	observations        []Observation
	unknownToolCount    int
	businessErrorCounts map[string]int
}

func NewService(config ServiceConfig) Service {
	return Service{
		planner:                config.Planner,
		catalog:                config.Catalog,
		executor:               config.Executor,
		runStore:               config.RunStore,
		maxSteps:               config.MaxSteps,
		totalTimeout:           config.TotalTimeout,
		failedRunFinishTimeout: defaultFailedRunFinishTimeout,
	}
}

func (service Service) Run(parent context.Context, request CreateRunRequest) (RunResponse, error) {
	ctx, cancel := context.WithTimeout(parent, service.totalTimeout)
	defer cancel()

	run, err := service.runStore.startRun(ctx, CreateRunRecord(request))
	if err != nil {
		return RunResponse{}, fmt.Errorf("start run: %w", err)
	}

	response, runErr := service.run(ctx, run, request)
	if runErr != nil {
		return service.failRun(run, response, runErr)
	}

	if err := service.completeRun(ctx, run, response); err != nil {
		return RunResponse{}, err
	}

	return response, nil
}

func (service Service) GetRun(ctx context.Context, runID string) (RunResponse, error) {
	response, err := service.runStore.getRun(ctx, runID)
	if err != nil {
		return RunResponse{}, fmt.Errorf("get run: %w", err)
	}
	return response, nil
}

func (service Service) CreateRunTurn(parent context.Context, runID string, request CreateRunTurnRequest) (RunResponse, error) {
	ctx, cancel := context.WithTimeout(parent, service.totalTimeout)
	defer cancel()

	stateRecord, err := service.runStore.getRunState(ctx, runID)
	if err != nil {
		return RunResponse{}, fmt.Errorf("get run state: %w", err)
	}
	if stateRecord.Run.Status != RunStatusWaitingForUser {
		return RunResponse{RunID: runID, Status: stateRecord.Run.Status}, ErrRunNotWaiting
	}
	if stateRecord.Pending == nil {
		return RunResponse{RunID: runID, Status: stateRecord.Run.Status}, ErrRunNotWaiting
	}

	turn, err := service.runStore.createRunTurn(ctx, CreateRunTurnRecord{
		RunID:       runID,
		Message:     request.Message,
		Attachments: request.Attachments,
	})
	if err != nil {
		return RunResponse{}, fmt.Errorf("create run turn: %w", err)
	}
	if err := service.runStore.markInteractionResponded(ctx, stateRecord.Pending.ID, turn.ID); err != nil {
		return RunResponse{}, fmt.Errorf("mark interaction responded: %w", err)
	}

	run := stateRecord.Run
	run.Status = RunStatusRunning
	response, runErr := service.resumeRun(ctx, run, CreateRunRequest(request), stateRecord.Pending, stateRecord.Observations)
	if runErr != nil {
		return service.failRun(run, response, runErr)
	}
	if err := service.completeRun(ctx, run, response); err != nil {
		return RunResponse{}, err
	}

	return response, nil
}

func (service Service) run(ctx context.Context, run Run, request CreateRunRequest) (RunResponse, error) {
	return service.runFromStep(ctx, run, request, nil, nil, 1)
}

func (service Service) resumeRun(
	ctx context.Context,
	run Run,
	request CreateRunRequest,
	interaction *Interaction,
	observations []Observation,
) (RunResponse, error) {
	return service.runFromStep(ctx, run, request, interaction, observations, nextStepOrder(observations))
}

func (service Service) runFromStep(
	ctx context.Context,
	run Run,
	request CreateRunRequest,
	interaction *Interaction,
	observations []Observation,
	startStepOrder int,
) (RunResponse, error) {
	state, err := service.newRunState(ctx, run, request)
	if err != nil {
		return RunResponse{}, err
	}
	state.interaction = interaction
	state.observations = append(state.observations, observations...)

	for stepOrder := startStepOrder; stepOrder < startStepOrder+service.maxSteps; stepOrder++ {
		response, done, err := service.runStep(ctx, state, stepOrder)
		if err != nil || done {
			return response, err
		}
	}

	return RunResponse{}, fmt.Errorf("%w: step limit exceeded", ErrRunFailed)
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

func (service Service) newRunState(ctx context.Context, run Run, request CreateRunRequest) (*runState, error) {
	instructions, err := service.catalog.GetInstructions(ctx)
	if err != nil {
		return nil, fmt.Errorf("get instructions: %w", err)
	}
	tools, err := service.catalog.ListEnabledTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	return &runState{
		run:                 run,
		message:             request.Message,
		attachments:         request.Attachments,
		instructions:        instructions,
		tools:               tools,
		toolsByName:         indexToolsByName(tools),
		runContext:          NewRunContext(),
		businessErrorCounts: make(map[string]int),
	}, nil
}

func (service Service) runStep(ctx context.Context, state *runState, stepOrder int) (RunResponse, bool, error) {
	action, err := service.planner.NextAction(ctx, PlanRequest{
		Instructions: state.instructions.Content,
		Message:      state.message,
		Attachments:  state.attachments,
		Interaction:  state.interaction,
		Tools:        state.tools,
		Observations: state.observations,
	})
	if err != nil {
		return RunResponse{}, false, fmt.Errorf("plan next action: %w", err)
	}

	switch action.Type {
	case ActionTypeFinalAnswer:
		return finalResponse(state.run.ID, action), true, nil
	case ActionTypeAskUser:
		response, err := service.askUser(ctx, state, action)
		return response, true, err
	case ActionTypeCallTool:
		return RunResponse{}, false, service.callTool(ctx, state, stepOrder, action)
	default:
		return RunResponse{}, false, fmt.Errorf("%w: invalid action type %q", ErrRunFailed, action.Type)
	}
}

func (service Service) askUser(ctx context.Context, state *runState, action PlannerAction) (RunResponse, error) {
	interaction, err := service.runStore.createInteraction(ctx, Interaction{
		RunID:   state.run.ID,
		Type:    InteractionTypeUserInput,
		Status:  InteractionStatusPending,
		Message: action.Message,
		Payload: action.Payload,
	})
	if err != nil {
		return RunResponse{}, fmt.Errorf("create interaction: %w", err)
	}

	state.run.Status = RunStatusWaitingForUser
	if err := service.runStore.markRunWaiting(ctx, state.run, interaction); err != nil {
		return RunResponse{}, fmt.Errorf("mark run waiting: %w", err)
	}

	return RunResponse{
		RunID:       state.run.ID,
		Status:      RunStatusWaitingForUser,
		Interaction: &interaction,
	}, nil
}

func (service Service) callTool(ctx context.Context, state *runState, stepOrder int, action PlannerAction) error {
	tool, ok := state.toolsByName[action.Tool]
	if !ok {
		return service.recordUnknownTool(ctx, state, stepOrder, action.Tool)
	}

	inputs, err := decodeInputs(action.Inputs)
	if err != nil {
		return err
	}

	observation, step, err := service.executeTool(ctx, state, stepOrder, tool, inputs)
	if saveErr := service.saveStep(ctx, step); saveErr != nil {
		return saveErr
	}
	if err != nil {
		return err
	}

	state.observations = append(state.observations, observation)
	if err := service.runStore.saveObservation(ctx, ObservationRecord{
		RunID:       state.run.ID,
		StepOrder:   stepOrder,
		Observation: observation,
	}); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}
	if observation.Status == StepStatusFailed {
		return recordBusinessError(state, tool.Name, observation.Error)
	}

	return nil
}

func (service Service) executeTool(
	ctx context.Context,
	state *runState,
	stepOrder int,
	tool toolcatalog.Tool,
	inputs map[string]any,
) (Observation, StepRecord, error) {
	started := time.Now()
	observation, err := service.executor.Execute(ctx, ExecuteRequest{
		RunID:      state.run.ID,
		StepID:     fmt.Sprintf("step_%d", stepOrder),
		StepOrder:  stepOrder,
		Tool:       tool,
		Inputs:     inputs,
		RunContext: state.runContext,
	})
	step := stepRecord(state.run.ID, stepOrder, tool.ID, inputs, observation, time.Since(started).Milliseconds())
	if err != nil {
		step.Status = StepStatusFailed
		step.ErrorSummary = err.Error()
		return observation, step, fmt.Errorf("execute tool %q: %w", tool.Name, err)
	}

	return observation, step, nil
}

func (service Service) recordUnknownTool(ctx context.Context, state *runState, stepOrder int, toolName string) error {
	state.unknownToolCount++
	observation := Observation{StepOrder: stepOrder, ToolName: toolName, Status: StepStatusFailed, Error: "unknown tool"}
	step := StepRecord{
		RunID:         state.run.ID,
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
		return fmt.Errorf("%w: unknown tool %q", ErrRunFailed, toolName)
	}

	state.observations = append(state.observations, observation)
	if err := service.runStore.saveObservation(ctx, ObservationRecord{
		RunID:       state.run.ID,
		StepOrder:   stepOrder,
		Observation: observation,
	}); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}
	return nil
}

func (service Service) saveStep(ctx context.Context, step StepRecord) error {
	if err := service.runStore.saveStep(ctx, step); err != nil {
		return fmt.Errorf("save step: %w", err)
	}

	return nil
}

func (service Service) completeRun(ctx context.Context, run Run, response RunResponse) error {
	if response.Status == RunStatusWaitingForUser {
		return nil
	}

	run.Status = response.Status
	run.Answer = response.Answer
	run.Outputs = response.Outputs
	run.ErrorSummary = response.Error
	if err := service.runStore.finishRun(ctx, run); err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	return nil
}

func (service Service) failRun(run Run, response RunResponse, runErr error) (RunResponse, error) {
	run.Status = RunStatusFailed
	run.ErrorSummary = runErr.Error()
	ctx, cancel := context.WithTimeout(context.Background(), service.failedRunFinishTimeout)
	defer cancel()
	if err := service.runStore.finishRun(ctx, run); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("finish failed run: %w", err))
	}

	response.RunID = run.ID
	response.Status = RunStatusFailed
	response.Error = RedactText(runErr.Error())
	return response, runErr
}

func finalResponse(runID string, action PlannerAction) RunResponse {
	return RunResponse{
		RunID:   runID,
		Status:  RunStatusSucceeded,
		Answer:  RedactText(action.Answer),
		Outputs: redactOutputs(action.Outputs),
	}
}

func stepRecord(
	runID string,
	stepOrder int,
	toolID string,
	inputs map[string]any,
	observation Observation,
	durationMS int64,
) StepRecord {
	step := StepRecord{
		RunID:         runID,
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

func recordBusinessError(state *runState, toolName string, errorSummary string) error {
	errorKey := toolName + "\x00" + errorSummary
	state.businessErrorCounts[errorKey]++
	if state.businessErrorCounts[errorKey] > 1 {
		return fmt.Errorf("%w: repeated tool error from %q: %s", ErrRunFailed, toolName, errorSummary)
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
		return nil, fmt.Errorf("%w: decode tool inputs: %w", ErrRunFailed, err)
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
