package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"ai/backend/internal/database"
	"ai/backend/internal/database/generated"
)

func (repository Repository) StartAgentExecution(ctx context.Context, record CreateAgentExecutionRecord) (AgentExecution, error) {
	if repository.database == nil {
		return AgentExecution{}, ErrDatabaseMissing
	}

	execution := AgentExecution{
		ID:               newRuntimeID("exec"),
		SessionID:        record.SessionID,
		TriggerMessageID: record.TriggerMessageID,
		Status:           AgentExecutionStatusRunning,
		StartedAt:        time.Now().UTC(),
	}
	executionRecord := database.AgentExecution{
		ID:               execution.ID,
		SessionID:        execution.SessionID,
		TriggerMessageID: execution.TriggerMessageID,
		Status:           string(execution.Status),
		ErrorSummary:     "",
		StartedAt:        execution.StartedAt,
	}
	if err := generated.AgentExecutionQueries[database.AgentExecution](repository.database).Create(ctx, &executionRecord); err != nil {
		if isUniqueConstraintError(err) {
			return AgentExecution{}, ErrAgentExecutionActive
		}
		return AgentExecution{}, fmt.Errorf("start execution: %w", err)
	}

	return execution, nil
}

func (repository Repository) FinishAgentExecution(ctx context.Context, execution AgentExecution) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	if err := generated.AgentExecutionQueries[database.AgentExecution](repository.database).FinishByID(ctx, string(execution.Status), RedactText(execution.ErrorSummary), sql.NullTime{Time: time.Now().UTC(), Valid: true}, execution.ID); err != nil {
		return fmt.Errorf("finish execution: %w", err)
	}

	return nil
}

func (repository Repository) GetAgentExecutionState(ctx context.Context, executionID string) (AgentExecutionStateRecord, error) {
	if repository.database == nil {
		return AgentExecutionStateRecord{}, ErrDatabaseMissing
	}

	executionRecord, err := repository.executionRecord(ctx, executionID)
	if err != nil {
		return AgentExecutionStateRecord{}, err
	}
	state := AgentExecutionStateRecord{AgentExecution: agentExecutionFromRecord(executionRecord)}
	if err := repository.loadExecutionStateRelations(ctx, &state); err != nil {
		return AgentExecutionStateRecord{}, err
	}

	return state, nil
}

func (repository Repository) ActiveAgentExecution(ctx context.Context, sessionID string) (*AgentExecution, error) {
	if repository.database == nil {
		return nil, ErrDatabaseMissing
	}

	record, err := generated.AgentExecutionQueries[database.AgentExecution](repository.database).ActiveBySessionID(ctx, sessionID, string(AgentExecutionStatusRunning), string(AgentExecutionStatusInterrupted))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAgentExecutionNotFound
		}
		return nil, fmt.Errorf("active execution: %w", err)
	}
	if record.ID == "" {
		return nil, ErrAgentExecutionNotFound
	}
	execution := agentExecutionFromRecord(record)
	return &execution, nil
}

func (repository Repository) SaveObservation(ctx context.Context, record ObservationRecord) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	payload, err := json.Marshal(RedactJSONValue(record.Observation))
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	observationRecord := database.AgentExecutionObservation{
		ID:          newRuntimeID("obs"),
		ExecutionID: record.ExecutionID,
		StepOrder:   record.StepOrder,
		Payload:     database.JSON(payload),
		CreatedAt:   time.Now().UTC(),
	}
	if err := generated.AgentExecutionObservationQueries[database.AgentExecutionObservation](repository.database).Create(ctx, &observationRecord); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}

	return nil
}

func (repository Repository) SaveStep(ctx context.Context, step StepRecord) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	record := database.AgentExecutionStep{
		ID:            newRuntimeID("step"),
		ExecutionID:   step.ExecutionID,
		StepOrder:     step.StepOrder,
		ToolID:        step.ToolID,
		InputSummary:  database.JSON(step.InputSummary),
		OutputSummary: database.JSON(step.OutputSummary),
		DurationMS:    step.DurationMS,
		Status:        string(step.Status),
		ErrorSummary:  RedactText(step.ErrorSummary),
		CreatedAt:     time.Now().UTC(),
	}
	if err := generated.AgentExecutionStepQueries[database.AgentExecutionStep](repository.database).Create(ctx, &record); err != nil {
		return fmt.Errorf("save step: %w", err)
	}

	return nil
}

func (repository Repository) executionRecord(ctx context.Context, executionID string) (database.AgentExecution, error) {
	executionRecord, err := generated.AgentExecutionQueries[database.AgentExecution](repository.database).GetByID(ctx, executionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return database.AgentExecution{}, ErrAgentExecutionNotFound
		}
		return database.AgentExecution{}, fmt.Errorf("get execution: %w", err)
	}
	if executionRecord.ID == "" {
		return database.AgentExecution{}, ErrAgentExecutionNotFound
	}
	return executionRecord, nil
}

func (repository Repository) loadExecutionStateRelations(ctx context.Context, state *AgentExecutionStateRecord) error {
	interruptions, activeInterruption, err := repository.executionInterruptions(ctx, state.AgentExecution.ID)
	if err != nil {
		return err
	}
	state.Interruptions = interruptions
	state.ActiveInterruption = activeInterruption

	observations, err := repository.executionObservations(ctx, state.AgentExecution.ID)
	if err != nil {
		return err
	}
	state.Observations = observations

	return nil
}

func (repository Repository) executionObservations(ctx context.Context, executionID string) ([]Observation, error) {
	records, err := generated.AgentExecutionObservationQueries[database.AgentExecutionObservation](repository.database).ListByExecutionID(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("list observations: %w", err)
	}
	observations := make([]Observation, 0, len(records))
	for _, record := range records {
		var observation Observation
		if err := json.Unmarshal(record.Payload, &observation); err != nil {
			return nil, fmt.Errorf("decode observation: %w", err)
		}
		observations = append(observations, observation)
	}
	return observations, nil
}

func agentExecutionFromRecord(record database.AgentExecution) AgentExecution {
	execution := AgentExecution{
		ID:               record.ID,
		SessionID:        record.SessionID,
		TriggerMessageID: record.TriggerMessageID,
		Status:           AgentExecutionStatus(record.Status),
		ErrorSummary:     record.ErrorSummary,
		StartedAt:        record.StartedAt,
	}
	if record.FinishedAt.Valid {
		execution.FinishedAt = record.FinishedAt.Time
	}
	return execution
}
