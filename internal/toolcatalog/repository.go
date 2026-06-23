package toolcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const uniqueViolationCode = "23505"

var ErrDatabaseMissing = errors.New("tool catalog database is missing")

type database interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
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
		return fmt.Errorf("create tool catalog schema: %w", err)
	}

	return nil
}

func (repository Repository) SaveTool(ctx context.Context, tool Tool) (Tool, error) {
	if repository.database == nil {
		return Tool{}, ErrDatabaseMissing
	}

	row := repository.database.QueryRow(ctx, `
INSERT INTO tools (name, description, command_path, input_schema, output_schema, timeout_ms, requires_service_account, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id::text, name, description, command_path, input_schema, output_schema, timeout_ms, requires_service_account, status, created_at, updated_at
`,
		tool.Name,
		tool.Description,
		tool.CommandPath,
		[]byte(tool.InputSchema),
		[]byte(tool.OutputSchema),
		tool.TimeoutMS,
		tool.RequiresServiceAccount,
		tool.NormalizedStatus(),
	)

	saved, err := scanTool(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return Tool{}, ErrDuplicateToolName
		}
		return Tool{}, fmt.Errorf("save tool: %w", err)
	}

	return saved, nil
}

func (repository Repository) ListEnabledTools(ctx context.Context) ([]Tool, error) {
	if repository.database == nil {
		return nil, ErrDatabaseMissing
	}

	rows, err := repository.database.Query(ctx, `
SELECT id::text, name, description, command_path, input_schema, output_schema, timeout_ms, requires_service_account, status, created_at, updated_at
FROM tools
WHERE status = $1
ORDER BY name
`, ToolStatusEnabled)
	if err != nil {
		return nil, fmt.Errorf("list enabled tools: %w", err)
	}
	defer rows.Close()

	tools, err := scanTools(rows)
	if err != nil {
		return nil, err
	}

	return tools, nil
}

func (repository Repository) UpdateInstructions(ctx context.Context, instructions Instructions) (Instructions, error) {
	if repository.database == nil {
		return Instructions{}, ErrDatabaseMissing
	}

	row := repository.database.QueryRow(ctx, `
INSERT INTO agent_instructions (id, content)
VALUES (1, $1)
ON CONFLICT (id)
DO UPDATE SET content = EXCLUDED.content, updated_at = now()
RETURNING content, updated_at
`, instructions.Content)

	var saved Instructions
	if err := row.Scan(&saved.Content, &saved.UpdatedAt); err != nil {
		return Instructions{}, fmt.Errorf("update agent instructions: %w", err)
	}

	return saved, nil
}

func (repository Repository) GetInstructions(ctx context.Context) (Instructions, error) {
	if repository.database == nil {
		return Instructions{}, ErrDatabaseMissing
	}

	row := repository.database.QueryRow(ctx, `SELECT content, updated_at FROM agent_instructions WHERE id = 1`)

	var instructions Instructions
	if err := row.Scan(&instructions.Content, &instructions.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Instructions{}, ErrInstructionsNotFound
		}
		return Instructions{}, fmt.Errorf("get agent instructions: %w", err)
	}

	return instructions, nil
}

func scanTools(rows pgx.Rows) ([]Tool, error) {
	var tools []Tool
	for rows.Next() {
		tool, err := scanTool(rows)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tools: %w", err)
	}

	return tools, nil
}

func scanTool(row pgx.Row) (Tool, error) {
	var tool Tool
	var inputSchema []byte
	var outputSchema []byte
	if err := row.Scan(
		&tool.ID,
		&tool.Name,
		&tool.Description,
		&tool.CommandPath,
		&inputSchema,
		&outputSchema,
		&tool.TimeoutMS,
		&tool.RequiresServiceAccount,
		&tool.Status,
		&tool.CreatedAt,
		&tool.UpdatedAt,
	); err != nil {
		return Tool{}, fmt.Errorf("scan tool: %w", err)
	}
	tool.InputSchema = json.RawMessage(inputSchema)
	tool.OutputSchema = json.RawMessage(outputSchema)

	return tool, nil
}

func schemaSQL() string {
	return `
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS tools (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	name text NOT NULL UNIQUE,
	description text NOT NULL,
	command_path text NOT NULL,
	input_schema jsonb NOT NULL,
	output_schema jsonb NOT NULL,
	timeout_ms integer NOT NULL,
	requires_service_account boolean NOT NULL DEFAULT false,
	status text NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_instructions (
	id integer PRIMARY KEY,
	content text NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now()
);
`
}
