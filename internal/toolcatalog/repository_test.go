package toolcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	"orderbuddy-ai/backend/internal/platform/sqlite"
)

func TestRepositoryCreateSchemaRequiresPool(t *testing.T) {
	repository := NewRepository(nil)

	err := repository.CreateSchema(context.Background())

	if err == nil {
		t.Fatal("CreateSchema() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "tool catalog database is missing") {
		t.Fatalf("CreateSchema() error = %q, want missing database context", err)
	}
}

func TestRepositorySavesAndListsEnabledTools(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))

	saved, err := repository.SaveTool(context.Background(), Tool{
		Name:         "report_tool",
		Description:  "Builds a report",
		CommandPath:  "/tmp/report-tool",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		TimeoutMS:    1000,
		Status:       ToolStatusEnabled,
	})
	if err != nil {
		t.Fatalf("SaveTool() error = %v, want nil", err)
	}
	if saved.ID == "" {
		t.Fatal("SaveTool() ID is empty")
	}

	tools, err := repository.ListEnabledTools(context.Background())
	if err != nil {
		t.Fatalf("ListEnabledTools() error = %v, want nil", err)
	}
	if len(tools) != 1 || tools[0].Name != "report_tool" {
		t.Fatalf("ListEnabledTools() = %+v, want saved tool", tools)
	}
}

func TestRepositoryUpdateAndGetsInstructions(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))

	saved, err := repository.UpdateInstructions(context.Background(), Instructions{Content: "Use tools carefully."})
	if err != nil {
		t.Fatalf("UpdateInstructions() error = %v, want nil", err)
	}
	if saved.Content != "Use tools carefully." {
		t.Fatalf("UpdateInstructions() Content = %q, want saved content", saved.Content)
	}

	got, err := repository.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions() error = %v, want nil", err)
	}
	if got.Content != saved.Content {
		t.Fatalf("GetInstructions() Content = %q, want %q", got.Content, saved.Content)
	}
}

func newTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := sqlite.Connect(context.Background(), "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("connect sqlite: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			if closeErr := sqlDB.Close(); closeErr != nil {
				t.Fatal(fmt.Errorf("close sqlite: %w", closeErr))
			}
		}
	})
	return db
}
