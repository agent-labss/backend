package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrDatabaseMissing = errors.New("agent database is missing")

type database interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type Repository struct {
	database database
}

func NewRepository(pool *pgxpool.Pool) Repository {
	if pool == nil {
		return Repository{}
	}

	return Repository{database: pool}
}

func (repository Repository) CreateSchema(ctx context.Context) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}
	if _, err := repository.database.Exec(ctx, schemaSQL()); err != nil {
		return fmt.Errorf("create agent schema: %w", err)
	}

	return nil
}

func (repository Repository) StartRun(ctx context.Context, message string) (Run, error) {
	if repository.database == nil {
		return Run{}, ErrDatabaseMissing
	}

	run := Run{
		ID:        newRuntimeID("run"),
		Message:   message,
		Status:    RunStatusRunning,
		StartedAt: time.Now().UTC(),
	}
	if _, err := repository.database.Exec(ctx, `
INSERT INTO agent_runs (id, message, status, started_at)
VALUES ($1, $2, $3, $4)
`, run.ID, run.Message, run.Status, run.StartedAt); err != nil {
		return Run{}, fmt.Errorf("start run: %w", err)
	}

	return run, nil
}

func (repository Repository) FinishRun(ctx context.Context, run Run) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	outputSummary, err := json.Marshal(RedactJSONValue(run.Outputs))
	if err != nil {
		return fmt.Errorf("marshal output summary: %w", err)
	}
	if _, err := repository.database.Exec(ctx, `
UPDATE agent_runs
SET status = $2, answer_summary = $3, output_summary = $4, error_summary = $5, finished_at = $6
WHERE id = $1
`, run.ID, run.Status, RedactText(run.Answer), outputSummary, RedactText(run.ErrorSummary), time.Now().UTC()); err != nil {
		return fmt.Errorf("finish run: %w", err)
	}

	return nil
}

func (repository Repository) SaveStep(ctx context.Context, step StepRecord) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	if _, err := repository.database.Exec(ctx, `
INSERT INTO agent_run_steps (id, run_id, step_order, tool_name, input_summary, output_summary, duration_ms, status, error_summary, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
`,
		newRuntimeID("step"),
		step.RunID,
		step.StepOrder,
		step.ToolName,
		[]byte(step.InputSummary),
		[]byte(step.OutputSummary),
		step.DurationMS,
		step.Status,
		RedactText(step.ErrorSummary),
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("save step: %w", err)
	}

	return nil
}

func schemaSQL() string {
	return `
CREATE TABLE IF NOT EXISTS agent_runs (
	id text PRIMARY KEY,
	message text NOT NULL,
	status text NOT NULL,
	answer_summary text NOT NULL DEFAULT '',
	output_summary jsonb NOT NULL DEFAULT '{}'::jsonb,
	error_summary text NOT NULL DEFAULT '',
	started_at timestamptz NOT NULL,
	finished_at timestamptz
);

CREATE TABLE IF NOT EXISTS agent_run_steps (
	id text PRIMARY KEY,
	run_id text NOT NULL REFERENCES agent_runs(id),
	step_order integer NOT NULL,
	tool_name text NOT NULL,
	input_summary jsonb NOT NULL,
	output_summary jsonb NOT NULL,
	duration_ms integer NOT NULL,
	status text NOT NULL,
	error_summary text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL
);
`
}

func newRuntimeID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}
