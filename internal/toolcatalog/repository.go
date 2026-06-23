package toolcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"orderbuddy-ai/backend/internal/database"
	"orderbuddy-ai/backend/internal/database/generated"
)

var ErrDatabaseMissing = errors.New("tool catalog database is missing")

type Repository struct {
	database *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	if db == nil {
		return Repository{}
	}

	return Repository{database: db}
}

func (repository Repository) CreateSchema(context.Context) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	return nil
}

func (repository Repository) SaveTool(ctx context.Context, tool Tool) (Tool, error) {
	if repository.database == nil {
		return Tool{}, ErrDatabaseMissing
	}

	record := toolRecord(tool)
	queries := generated.ToolQueries[database.Tool](repository.database)
	if err := queries.Create(ctx, &record); err != nil {
		if isUniqueConstraintError(err) {
			return Tool{}, ErrDuplicateToolName
		}
		return Tool{}, fmt.Errorf("save tool: %w", err)
	}

	return toolFromRecord(record), nil
}

func (repository Repository) ListEnabledTools(ctx context.Context) ([]Tool, error) {
	if repository.database == nil {
		return nil, ErrDatabaseMissing
	}

	records, err := generated.ToolQueries[database.Tool](repository.database).ListByStatus(ctx, string(ToolStatusEnabled))
	if err != nil {
		return nil, fmt.Errorf("list enabled tools: %w", err)
	}

	tools := make([]Tool, 0, len(records))
	for _, record := range records {
		tools = append(tools, toolFromRecord(record))
	}

	return tools, nil
}

func (repository Repository) UpdateInstructions(ctx context.Context, instructions Instructions) (Instructions, error) {
	if repository.database == nil {
		return Instructions{}, ErrDatabaseMissing
	}

	record := database.AgentInstruction{
		ID:        1,
		Content:   instructions.Content,
		UpdatedAt: time.Now().UTC(),
	}
	if err := repository.database.WithContext(ctx).Save(&record).Error; err != nil {
		return Instructions{}, fmt.Errorf("update agent instructions: %w", err)
	}

	return instructionsFromRecord(record), nil
}

func (repository Repository) GetInstructions(ctx context.Context) (Instructions, error) {
	if repository.database == nil {
		return Instructions{}, ErrDatabaseMissing
	}

	record, err := generated.AgentInstructionQueries[database.AgentInstruction](repository.database).GetByID(ctx, 1)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Instructions{}, ErrInstructionsNotFound
		}
		return Instructions{}, fmt.Errorf("get agent instructions: %w", err)
	}

	return instructionsFromRecord(record), nil
}

func toolRecord(tool Tool) database.Tool {
	now := time.Now().UTC()
	return database.Tool{
		ID:                     newRecordID("tool"),
		Name:                   tool.Name,
		Description:            tool.Description,
		CommandPath:            tool.CommandPath,
		InputSchema:            database.JSON(tool.InputSchema),
		OutputSchema:           database.JSON(tool.OutputSchema),
		TimeoutMS:              tool.TimeoutMS,
		RequiresServiceAccount: tool.RequiresServiceAccount,
		Status:                 string(tool.NormalizedStatus()),
		CreatedAt:              now,
		UpdatedAt:              now,
	}
}

func toolFromRecord(record database.Tool) Tool {
	return Tool{
		ID:                     record.ID,
		Name:                   record.Name,
		Description:            record.Description,
		CommandPath:            record.CommandPath,
		InputSchema:            json.RawMessage(record.InputSchema),
		OutputSchema:           json.RawMessage(record.OutputSchema),
		TimeoutMS:              record.TimeoutMS,
		RequiresServiceAccount: record.RequiresServiceAccount,
		Status:                 ToolStatus(record.Status),
		CreatedAt:              record.CreatedAt,
		UpdatedAt:              record.UpdatedAt,
	}
}

func instructionsFromRecord(record database.AgentInstruction) Instructions {
	return Instructions{
		Content:   record.Content,
		UpdatedAt: record.UpdatedAt,
	}
}

func newRecordID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}

func isUniqueConstraintError(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
