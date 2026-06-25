package queryinput

import (
	"database/sql"
	"time"

	database "ai/backend/internal/database"
)

type ToolQueries interface {
	// SELECT * FROM @@table WHERE status = @status ORDER BY name
	ListByStatus(status string) ([]database.Tool, error)

	// SELECT * FROM @@table WHERE name = @name LIMIT 1
	GetByName(name string) (database.Tool, error)
}

type AgentInstructionQueries interface {
	// SELECT * FROM @@table WHERE id = @id LIMIT 1
	GetByID(id int) (database.AgentInstruction, error)

	// INSERT INTO @@table (id, content, updated_at) VALUES (@id, @content, @updatedAt) ON CONFLICT(id) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at
	UpsertByID(id int, content string, updatedAt time.Time) error
}

type AgentRunQueries interface {
	// SELECT * FROM @@table WHERE id = @id LIMIT 1
	GetByID(id string) (database.AgentRun, error)

	// UPDATE @@table SET status = @status, error_summary = @errorSummary, finished_at = @finishedAt WHERE id = @id
	FinishByID(status string, errorSummary string, finishedAt sql.NullTime, id string) error

	// UPDATE @@table SET status = @status, error_summary = '', finished_at = NULL WHERE id = @id
	MarkInterruptedByID(status string, id string) error
}

type AgentRunStepQueries interface {
	// SELECT * FROM @@table WHERE id = @id LIMIT 1
	GetByID(id string) (database.AgentRunStep, error)
}

type ChatSessionQueries interface {
	// SELECT * FROM @@table WHERE id = @id LIMIT 1
	GetByID(id string) (database.ChatSession, error)

	// UPDATE @@table SET updated_at = @updatedAt WHERE id = @id
	UpdateUpdatedAtByID(updatedAt time.Time, id string) error
}

type ChatMessageQueries interface {
	// SELECT * FROM @@table WHERE session_id = @sessionID ORDER BY sequence ASC
	ListBySessionID(sessionID string) ([]database.ChatMessage, error)

	// SELECT COALESCE(MAX(sequence), 0) FROM @@table WHERE session_id = @sessionID
	MaxSequenceBySessionID(sessionID string) (int, error)
}

type ChatAttachmentQueries interface {
	// SELECT * FROM @@table WHERE session_id = @sessionID ORDER BY created_at ASC
	ListBySessionID(sessionID string) ([]database.ChatAttachment, error)
}

type AgentInterruptionQueries interface {
	// SELECT * FROM @@table WHERE run_id = @runID ORDER BY created_at ASC
	ListByRunID(runID string) ([]database.AgentInterruption, error)

	// SELECT * FROM @@table WHERE session_id = @sessionID AND status = @status ORDER BY created_at DESC LIMIT 1
	ActiveBySessionID(sessionID string, status string) (database.AgentInterruption, error)

	// UPDATE @@table SET status = @status, response_message_id = @messageID, resolved_at = @resolvedAt WHERE id = @id AND status = @awaitingStatus
	ResolveAwaitingByID(status string, messageID string, resolvedAt sql.NullTime, id string, awaitingStatus string) error
}

type AgentRunObservationQueries interface {
	// SELECT * FROM @@table WHERE run_id = @runID ORDER BY step_order ASC, created_at ASC
	ListByRunID(runID string) ([]database.AgentRunObservation, error)
}
