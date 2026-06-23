package queryinput

import database "ai/backend/internal/database"

type ToolQueries interface {
	// SELECT * FROM @@table WHERE status = @status ORDER BY name
	ListByStatus(status string) ([]database.Tool, error)

	// SELECT * FROM @@table WHERE name = @name LIMIT 1
	GetByName(name string) (database.Tool, error)
}

type AgentInstructionQueries interface {
	// SELECT * FROM @@table WHERE id = @id LIMIT 1
	GetByID(id int) (database.AgentInstruction, error)
}

type AgentRunQueries interface {
	// SELECT * FROM @@table WHERE id = @id LIMIT 1
	GetByID(id string) (database.AgentRun, error)
}

type AgentRunStepQueries interface {
	// SELECT * FROM @@table WHERE id = @id LIMIT 1
	GetByID(id string) (database.AgentRunStep, error)
}
