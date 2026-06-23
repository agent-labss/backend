package toolcatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	ToolsPath             = "/api/tools"
	AgentInstructionsPath = "/api/agent/instructions"
)

const (
	ToolStatusEnabled  ToolStatus = "enabled"
	ToolStatusDisabled ToolStatus = "disabled"
)

var (
	ErrInvalidTool          = errors.New("invalid tool")
	ErrToolNotFound         = errors.New("tool not found")
	ErrDuplicateToolName    = errors.New("duplicate tool name")
	ErrInstructionsNotFound = errors.New("agent instructions not found")
)

var toolNamePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

type ToolStatus string

type Tool struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	CommandPath  string          `json:"command_path"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	TimeoutMS    int             `json:"timeout_ms"`
	Status       ToolStatus      `json:"status"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type RegisterToolRequest struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	CommandPath  string          `json:"command_path"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	TimeoutMS    int             `json:"timeout_ms"`
}

type Instructions struct {
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpdateInstructionsRequest struct {
	Content string `json:"content"`
}

func (request RegisterToolRequest) Tool() Tool {
	return Tool{
		Name:         strings.TrimSpace(request.Name),
		Description:  strings.TrimSpace(request.Description),
		CommandPath:  strings.TrimSpace(request.CommandPath),
		InputSchema:  request.InputSchema,
		OutputSchema: request.OutputSchema,
		TimeoutMS:    request.TimeoutMS,
		Status:       ToolStatusEnabled,
	}
}

func (tool Tool) Validate(trustedToolDir string) error {
	validations := []func() error{
		func() error { return validateToolName(tool.Name) },
		func() error { return validateDescription(tool.Description) },
		func() error { return validateTimeout(tool.TimeoutMS) },
		func() error { return validateJSONObject(tool.InputSchema, "input_schema") },
		func() error { return validateJSONObject(tool.OutputSchema, "output_schema") },
		func() error { return validateToolStatus(tool.NormalizedStatus()) },
		func() error { return validateTrustedCommandPath(tool.CommandPath, trustedToolDir) },
	}

	for _, validate := range validations {
		if err := validate(); err != nil {
			return err
		}
	}

	return nil
}

func (tool Tool) NormalizedStatus() ToolStatus {
	if tool.Status == "" {
		return ToolStatusEnabled
	}

	return tool.Status
}

func validateToolName(name string) error {
	if !toolNamePattern.MatchString(name) {
		return fmt.Errorf("%w: name must use lowercase letters, numbers, and underscores", ErrInvalidTool)
	}

	return nil
}

func validateDescription(description string) error {
	if strings.TrimSpace(description) == "" {
		return fmt.Errorf("%w: description is required", ErrInvalidTool)
	}

	return nil
}

func validateTimeout(timeoutMS int) error {
	if timeoutMS <= 0 {
		return fmt.Errorf("%w: timeout_ms must be positive", ErrInvalidTool)
	}

	return nil
}

func validateJSONObject(raw json.RawMessage, field string) error {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%w: %s must be a JSON object", ErrInvalidTool, field)
	}
	if value == nil {
		return fmt.Errorf("%w: %s must be a JSON object", ErrInvalidTool, field)
	}

	return nil
}

func validateToolStatus(status ToolStatus) error {
	if status != ToolStatusEnabled && status != ToolStatusDisabled {
		return fmt.Errorf("%w: status is invalid", ErrInvalidTool)
	}

	return nil
}

func validateTrustedCommandPath(commandPath string, trustedToolDir string) error {
	if strings.TrimSpace(commandPath) == "" {
		return fmt.Errorf("%w: command_path is required", ErrInvalidTool)
	}
	if strings.TrimSpace(trustedToolDir) == "" {
		return fmt.Errorf("%w: trusted tool directory is required", ErrInvalidTool)
	}

	return validateCommandPathInsideTrustedDir(commandPath, trustedToolDir)
}

func validateCommandPathInsideTrustedDir(commandPath string, trustedToolDir string) error {
	absoluteTrustedDir, err := filepath.Abs(trustedToolDir)
	if err != nil {
		return fmt.Errorf("%w: resolve trusted tool directory: %w", ErrInvalidTool, err)
	}
	absoluteCommandPath, err := filepath.Abs(commandPath)
	if err != nil {
		return fmt.Errorf("%w: resolve command path: %w", ErrInvalidTool, err)
	}

	if err := validateRelativePath(absoluteTrustedDir, absoluteCommandPath); err != nil {
		return err
	}
	if err := validateCommandFile(absoluteCommandPath); err != nil {
		return err
	}

	return nil
}

func validateRelativePath(absoluteTrustedDir string, absoluteCommandPath string) error {
	relativePath, err := filepath.Rel(absoluteTrustedDir, absoluteCommandPath)
	if err != nil {
		return fmt.Errorf("%w: compare command path: %w", ErrInvalidTool, err)
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)) || filepath.IsAbs(relativePath) {
		return fmt.Errorf("%w: command_path must be inside trusted tool directory", ErrInvalidTool)
	}

	return nil
}

func validateCommandFile(absoluteCommandPath string) error {
	info, err := os.Stat(absoluteCommandPath)
	if err != nil {
		return fmt.Errorf("%w: command_path must exist", ErrInvalidTool)
	}
	if info.IsDir() {
		return fmt.Errorf("%w: command_path must be a file", ErrInvalidTool)
	}

	return nil
}
