package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/cli/gorm/typed"
	"gorm.io/gorm"

	"ai/backend/internal/database"
)

var ErrDatabaseMissing = errors.New("agent database is missing")

type Repository struct {
	database *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	if db == nil {
		return Repository{}
	}

	return Repository{database: db}
}

func (repository Repository) StartRun(ctx context.Context, record CreateRunRecord) (Run, error) {
	if repository.database == nil {
		return Run{}, ErrDatabaseMissing
	}

	run := Run{
		ID:        newRuntimeID("run"),
		Message:   RedactText(record.Message),
		Status:    RunStatusRunning,
		StartedAt: time.Now().UTC(),
	}
	runRecord := database.AgentRun{
		ID:            run.ID,
		Message:       run.Message,
		Status:        string(run.Status),
		AnswerSummary: "",
		OutputSummary: database.JSON([]byte(`{}`)),
		ErrorSummary:  "",
		StartedAt:     run.StartedAt,
	}
	if err := repository.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := typed.G[database.AgentRun](tx).Create(ctx, &runRecord); err != nil {
			return fmt.Errorf("start run: %w", err)
		}
		return saveAttachments(ctx, tx, run.ID, record.Attachments)
	}); err != nil {
		return Run{}, fmt.Errorf("start run transaction: %w", err)
	}

	return run, nil
}

func saveAttachments(ctx context.Context, db *gorm.DB, runID string, attachments []Attachment) error {
	for _, attachment := range attachments {
		record := database.AgentRunAttachment{
			ID:             attachment.ID,
			RunID:          runID,
			Filename:       RedactText(attachment.Filename),
			MIMEType:       attachment.MIMEType,
			Kind:           string(attachment.Kind),
			SizeBytes:      attachment.Size,
			ProviderFileID: attachment.FileID,
			CreatedAt:      time.Now().UTC(),
		}
		if err := typed.G[database.AgentRunAttachment](db).Create(ctx, &record); err != nil {
			return fmt.Errorf("save run attachment: %w", err)
		}
	}

	return nil
}

func (repository Repository) FinishRun(ctx context.Context, run Run) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	outputSummary, err := json.Marshal(RedactJSONValue(run.Outputs))
	if err != nil {
		return fmt.Errorf("marshal output summary: %w", err)
	}

	if err := typed.G[database.AgentRun](repository.database).Exec(ctx, `
UPDATE agent_runs
SET status = ?, answer_summary = ?, output_summary = ?, error_summary = ?, finished_at = ?
WHERE id = ?
`, string(run.Status), RedactText(run.Answer), database.JSON(outputSummary), RedactText(run.ErrorSummary), sql.NullTime{Time: time.Now().UTC(), Valid: true}, run.ID); err != nil {
		return fmt.Errorf("finish run: %w", err)
	}

	return nil
}

func (repository Repository) SaveStep(ctx context.Context, step StepRecord) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	record := database.AgentRunStep{
		ID:            newRuntimeID("step"),
		RunID:         step.RunID,
		StepOrder:     step.StepOrder,
		ToolID:        step.ToolID,
		InputSummary:  database.JSON(step.InputSummary),
		OutputSummary: database.JSON(step.OutputSummary),
		DurationMS:    step.DurationMS,
		Status:        string(step.Status),
		ErrorSummary:  RedactText(step.ErrorSummary),
		CreatedAt:     time.Now().UTC(),
	}
	if err := typed.G[database.AgentRunStep](repository.database).Create(ctx, &record); err != nil {
		return fmt.Errorf("save step: %w", err)
	}

	return nil
}

func newRuntimeID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}
