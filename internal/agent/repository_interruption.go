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

func (repository Repository) CreateInterruption(ctx context.Context, interruption Interruption) (Interruption, error) {
	if repository.database == nil {
		return Interruption{}, ErrDatabaseMissing
	}

	now := time.Now().UTC()
	if interruption.ID == "" {
		interruption.ID = newRuntimeID("int")
	}
	if interruption.Type == "" {
		interruption.Type = InterruptionTypeInputRequest
	}
	if interruption.Status == "" {
		interruption.Status = InterruptionStatusAwaitingReview
	}
	interruption.Message = RedactText(interruption.Message)
	interruption.CreatedAt = now
	payload := interruption.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	record := database.AgentInterruption{
		ID:          interruption.ID,
		SessionID:   interruption.SessionID,
		ExecutionID: interruption.ExecutionID,
		Type:        string(interruption.Type),
		Status:      string(interruption.Status),
		Message:     interruption.Message,
		Payload:     database.JSON(payload),
		CreatedAt:   now,
	}
	if err := generated.AgentInterruptionQueries[database.AgentInterruption](repository.database).Create(ctx, &record); err != nil {
		return Interruption{}, fmt.Errorf("create interruption: %w", err)
	}

	return interruption, nil
}

func (repository Repository) MarkAgentExecutionInterrupted(ctx context.Context, execution AgentExecution, _ Interruption) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	if err := generated.AgentExecutionQueries[database.AgentExecution](repository.database).MarkInterruptedByID(ctx, string(AgentExecutionStatusInterrupted), execution.ID); err != nil {
		return fmt.Errorf("mark execution interrupted: %w", err)
	}

	return nil
}

func (repository Repository) ResolveInterruption(ctx context.Context, interruptionID string, messageID string, status InterruptionStatus) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	if status == "" {
		status = InterruptionStatusResolved
	}
	record, err := generated.AgentInterruptionQueries[database.AgentInterruption](repository.database).ResolveAwaitingByID(ctx, string(status), messageID, sql.NullTime{Time: time.Now().UTC(), Valid: true}, interruptionID, string(InterruptionStatusAwaitingReview))
	if err != nil {
		return fmt.Errorf("resolve interruption: %w", err)
	}
	if record.ID == "" {
		return ErrAgentExecutionNotWaiting
	}

	return nil
}

func (repository Repository) ActiveInterruption(ctx context.Context, sessionID string) (*Interruption, error) {
	if repository.database == nil {
		return nil, ErrDatabaseMissing
	}

	record, err := generated.AgentInterruptionQueries[database.AgentInterruption](repository.database).ActiveBySessionID(ctx, sessionID, string(InterruptionStatusAwaitingReview))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoActiveInterruption
		}
		return nil, fmt.Errorf("active interruption: %w", err)
	}
	if record.ID == "" {
		return nil, ErrNoActiveInterruption
	}
	interruption := interruptionFromRecord(record)
	return &interruption, nil
}

func (repository Repository) executionInterruptions(ctx context.Context, executionID string) ([]Interruption, *Interruption, error) {
	records, err := generated.AgentInterruptionQueries[database.AgentInterruption](repository.database).ListByExecutionID(ctx, executionID)
	if err != nil {
		return nil, nil, fmt.Errorf("list execution interruptions: %w", err)
	}
	interruptions := make([]Interruption, 0, len(records))
	var active *Interruption
	for _, record := range records {
		interruption := interruptionFromRecord(record)
		interruptions = append(interruptions, interruption)
		if interruption.Status == InterruptionStatusAwaitingReview {
			activeCopy := interruption
			active = &activeCopy
		}
	}
	return interruptions, active, nil
}

func interruptionFromRecord(record database.AgentInterruption) Interruption {
	interruption := Interruption{
		ID:          record.ID,
		SessionID:   record.SessionID,
		ExecutionID: record.ExecutionID,
		Type:        InterruptionType(record.Type),
		Status:      InterruptionStatus(record.Status),
		Message:     record.Message,
		Payload:     json.RawMessage(record.Payload),
		CreatedAt:   record.CreatedAt,
	}
	if record.ResolvedAt.Valid {
		interruption.RespondedAt = record.ResolvedAt.Time
	}
	return interruption
}
