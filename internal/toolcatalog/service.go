package toolcatalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type store struct {
	saveTool           func(ctx context.Context, tool Tool) (Tool, error)
	listEnabledTools   func(ctx context.Context) ([]Tool, error)
	updateInstructions func(ctx context.Context, instructions Instructions) (Instructions, error)
	getInstructions    func(ctx context.Context) (Instructions, error)
}

type Service struct {
	store          store
	trustedToolDir string
}

func NewService(repository Repository, trustedToolDir string) Service {
	return newService(storeFromRepository(repository), trustedToolDir)
}

func newService(store store, trustedToolDir string) Service {
	return Service{store: store, trustedToolDir: trustedToolDir}
}

func (service Service) RegisterTool(ctx context.Context, request RegisterToolRequest) (Tool, error) {
	tool := request.Tool()
	if err := tool.Validate(service.trustedToolDir); err != nil {
		return Tool{}, err
	}

	saved, err := service.store.saveTool(ctx, tool)
	if err != nil {
		return Tool{}, fmt.Errorf("save tool: %w", err)
	}

	return saved, nil
}

func (service Service) ListEnabledTools(ctx context.Context) ([]Tool, error) {
	tools, err := service.store.listEnabledTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled tools: %w", err)
	}

	return tools, nil
}

func (service Service) UpdateInstructions(ctx context.Context, request UpdateInstructionsRequest) (Instructions, error) {
	instructions, err := service.store.updateInstructions(ctx, Instructions{Content: strings.TrimSpace(request.Content)})
	if err != nil {
		return Instructions{}, fmt.Errorf("update instructions: %w", err)
	}

	return instructions, nil
}

func (service Service) GetInstructions(ctx context.Context) (Instructions, error) {
	instructions, err := service.store.getInstructions(ctx)
	if errors.Is(err, ErrInstructionsNotFound) {
		return Instructions{}, nil
	}
	if err != nil {
		return Instructions{}, fmt.Errorf("get instructions: %w", err)
	}

	return instructions, nil
}

func storeFromRepository(repository Repository) store {
	return store{
		saveTool:           repository.SaveTool,
		listEnabledTools:   repository.ListEnabledTools,
		updateInstructions: repository.UpdateInstructions,
		getInstructions:    repository.GetInstructions,
	}
}
