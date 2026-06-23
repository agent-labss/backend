package toolcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const testInstructionsContent = "Use tools carefully."

type memoryRepository struct {
	tools        []Tool
	instructions Instructions
}

func (repository *memoryRepository) SaveTool(_ context.Context, tool Tool) (Tool, error) {
	for _, existing := range repository.tools {
		if existing.Name == tool.Name {
			return Tool{}, ErrDuplicateToolName
		}
	}
	tool.ID = "tool_1"
	repository.tools = append(repository.tools, tool)
	return tool, nil
}

func (repository *memoryRepository) ListEnabledTools(_ context.Context) ([]Tool, error) {
	var enabled []Tool
	for _, tool := range repository.tools {
		if tool.NormalizedStatus() == ToolStatusEnabled {
			enabled = append(enabled, tool)
		}
	}
	return enabled, nil
}

func (repository *memoryRepository) UpdateInstructions(_ context.Context, instructions Instructions) (Instructions, error) {
	repository.instructions = instructions
	return instructions, nil
}

func (repository *memoryRepository) GetInstructions(_ context.Context) (Instructions, error) {
	if repository.instructions.Content == "" {
		return Instructions{}, ErrInstructionsNotFound
	}
	return repository.instructions, nil
}

func TestServiceRegisterToolValidatesAndSaves(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "export_report")
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	service := newService(storeFromMemoryRepository(&memoryRepository{}), dir)
	tool, err := service.RegisterTool(context.Background(), RegisterToolRequest{
		Name:         "export_report",
		Description:  "Export a partner report.",
		CommandPath:  commandPath,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		TimeoutMS:    1000,
	})

	if err != nil {
		t.Fatalf("RegisterTool() error = %v", err)
	}
	if tool.Name != "export_report" {
		t.Fatalf("tool.Name = %q, want export_report", tool.Name)
	}
}

func TestServiceRegisterToolRejectsInvalidTool(t *testing.T) {
	service := newService(storeFromMemoryRepository(&memoryRepository{}), t.TempDir())

	_, err := service.RegisterTool(context.Background(), RegisterToolRequest{
		Name: "Bad Name",
	})

	if !errors.Is(err, ErrInvalidTool) {
		t.Fatalf("RegisterTool() error = %v, want ErrInvalidTool", err)
	}
}

func TestServiceUpdateAndGetInstructions(t *testing.T) {
	service := newService(storeFromMemoryRepository(&memoryRepository{}), t.TempDir())

	updated, err := service.UpdateInstructions(context.Background(), UpdateInstructionsRequest{Content: testInstructionsContent})
	if err != nil {
		t.Fatalf("UpdateInstructions() error = %v", err)
	}
	if updated.Content != testInstructionsContent {
		t.Fatalf("updated.Content = %q, want content", updated.Content)
	}

	got, err := service.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions() error = %v", err)
	}
	if got.Content != testInstructionsContent {
		t.Fatalf("got.Content = %q, want content", got.Content)
	}
}

func TestServiceGetInstructionsReturnsEmptyWhenMissing(t *testing.T) {
	service := newService(storeFromMemoryRepository(&memoryRepository{}), t.TempDir())

	instructions, err := service.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions() error = %v", err)
	}
	if instructions.Content != "" {
		t.Fatalf("instructions.Content = %q, want empty", instructions.Content)
	}
}

func storeFromMemoryRepository(repository *memoryRepository) store {
	return store{
		saveTool:           repository.SaveTool,
		listEnabledTools:   repository.ListEnabledTools,
		updateInstructions: repository.UpdateInstructions,
		getInstructions:    repository.GetInstructions,
	}
}
