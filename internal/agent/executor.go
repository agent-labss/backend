package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"ai/backend/internal/toolcatalog"
)

var ErrToolExecutionFailed = errors.New("tool execution failed")

type ExecuteRequest struct {
	RunID      string
	StepID     string
	StepOrder  int
	Tool       toolcatalog.Tool
	Inputs     map[string]any
	RunContext *RunContext
}

type CLIExecutor struct{}

func NewCLIExecutor() CLIExecutor {
	return CLIExecutor{}
}

func (executor CLIExecutor) Execute(parent context.Context, request ExecuteRequest) (Observation, error) {
	runContext := requestRunContext(request)
	stdout, err := executor.runCommand(parent, request, runContext)
	if err != nil {
		return Observation{}, err
	}

	result, err := decodeToolResult(stdout)
	if err != nil {
		return Observation{}, err
	}

	return observationFromToolResult(request, runContext, result)
}

func (executor CLIExecutor) runCommand(parent context.Context, request ExecuteRequest, runContext *RunContext) ([]byte, error) {
	timeout := time.Duration(request.Tool.TimeoutMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	stdin, err := json.Marshal(executor.inputEnvelope(request, runContext))
	if err != nil {
		return nil, fmt.Errorf("%w: encode stdin: %w", ErrToolExecutionFailed, err)
	}

	cmd := exec.CommandContext(ctx, request.Tool.CommandPath)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, commandError(ctx, request.Tool.Name, stderr.String(), err)
	}

	return stdout.Bytes(), nil
}

func (executor CLIExecutor) inputEnvelope(request ExecuteRequest, runContext *RunContext) ToolInputEnvelope {
	envelope := ToolInputEnvelope{
		RunID:   request.RunID,
		StepID:  request.StepID,
		Inputs:  resolveInputs(request.Inputs, runContext),
		Context: map[string]any{},
	}

	return envelope
}

func commandError(ctx context.Context, toolName string, stderr string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w: tool %q timed out: %w", ErrToolExecutionFailed, toolName, ctx.Err())
	}

	return fmt.Errorf("%w: tool %q exited with error: %w: %s", ErrToolExecutionFailed, toolName, err, RedactText(stderr))
}

func decodeToolResult(stdout []byte) (ToolResult, error) {
	var result ToolResult
	if err := json.Unmarshal(stdout, &result); err != nil {
		return ToolResult{}, fmt.Errorf("%w: decode stdout JSON: %w", ErrToolExecutionFailed, err)
	}

	return result, nil
}

func observationFromToolResult(request ExecuteRequest, runContext *RunContext, result ToolResult) (Observation, error) {
	switch result.Status {
	case ToolResultStatusError:
		return failedObservation(request, result), nil
	case ToolResultStatusOK:
		return succeededObservation(request, runContext, result), nil
	default:
		return Observation{}, fmt.Errorf("%w: tool returned status %q", ErrToolExecutionFailed, result.Status)
	}
}

func failedObservation(request ExecuteRequest, result ToolResult) Observation {
	errorSummary := fmt.Sprintf("tool returned status %q", result.Status)
	if result.Error != nil {
		errorSummary = result.Error.Code + ": " + result.Error.Message
	}

	return Observation{
		StepOrder: request.StepOrder,
		ToolName:  request.Tool.Name,
		Status:    StepStatusFailed,
		Error:     RedactText(errorSummary),
	}
}

func succeededObservation(request ExecuteRequest, runContext *RunContext, result ToolResult) Observation {
	return Observation{
		StepOrder: request.StepOrder,
		ToolName:  request.Tool.Name,
		Status:    StepStatusSucceeded,
		Outputs:   outputsFromToolResult(request.Tool.Name, runContext, result),
	}
}

func outputsFromToolResult(toolName string, runContext *RunContext, result ToolResult) map[string]any {
	outputs := make(map[string]any, len(result.Outputs))
	for name, output := range result.Outputs {
		if output.Sensitive {
			outputs[name] = runContext.Store(toolName, name, output.Value)
			continue
		}
		outputs[name] = RedactJSONValue(output.Value)
	}

	return outputs
}

func resolveInputs(inputs map[string]any, runContext *RunContext) map[string]any {
	resolved := make(map[string]any, len(inputs))
	for key, value := range inputs {
		resolved[key] = resolveInputValue(value, runContext)
	}

	return resolved
}

func resolveInputValue(value any, runContext *RunContext) any {
	switch typed := value.(type) {
	case string:
		contextValue, ok := runContext.Resolve(typed)
		if !ok {
			return value
		}
		return contextValue.Value
	case map[string]any:
		return resolveInputs(typed, runContext)
	case []any:
		resolved := make([]any, 0, len(typed))
		for _, item := range typed {
			resolved = append(resolved, resolveInputValue(item, runContext))
		}
		return resolved
	default:
		return value
	}
}

func requestRunContext(request ExecuteRequest) *RunContext {
	if request.RunContext != nil {
		return request.RunContext
	}

	return NewRunContext()
}
