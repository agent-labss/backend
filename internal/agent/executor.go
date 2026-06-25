package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"ai/backend/internal/toolcatalog"
)

var ErrToolExecutionFailed = errors.New("tool execution failed")

type ExecuteRequest struct {
	ExecutionID      string
	StepID           string
	StepOrder        int
	Tool             toolcatalog.Tool
	Inputs           map[string]any
	ExecutionContext *ExecutionContext
}

type CLIExecutor struct{}

func NewCLIExecutor() CLIExecutor {
	return CLIExecutor{}
}

func (executor CLIExecutor) Execute(parent context.Context, request ExecuteRequest) (Observation, error) {
	executionContext := requestExecutionContext(request)
	stdout, err := executor.runCommand(parent, request, executionContext)
	if err != nil {
		return Observation{}, err
	}

	result, err := decodeToolResult(stdout)
	if err != nil {
		return Observation{}, err
	}

	return observationFromToolResult(request, executionContext, result)
}

func (executor CLIExecutor) runCommand(parent context.Context, request ExecuteRequest, executionContext *ExecutionContext) ([]byte, error) {
	timeout := time.Duration(request.Tool.TimeoutMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	stdin, err := json.Marshal(executor.inputEnvelope(request, executionContext))
	if err != nil {
		return nil, fmt.Errorf("%w: encode stdin: %w", ErrToolExecutionFailed, err)
	}

	cmd := exec.CommandContext(ctx, request.Tool.CommandPath)
	cmd.Env = minimalToolEnvironment()
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

func (executor CLIExecutor) inputEnvelope(request ExecuteRequest, executionContext *ExecutionContext) ToolInputEnvelope {
	envelope := ToolInputEnvelope{
		ExecutionID: request.ExecutionID,
		StepID:      request.StepID,
		Inputs:      resolveInputs(request.Inputs, executionContext),
		Context:     map[string]any{},
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

func observationFromToolResult(request ExecuteRequest, executionContext *ExecutionContext, result ToolResult) (Observation, error) {
	switch result.Status {
	case ToolResultStatusError:
		return failedObservation(request, result), nil
	case ToolResultStatusOK:
		return succeededObservation(request, executionContext, result), nil
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

func succeededObservation(request ExecuteRequest, executionContext *ExecutionContext, result ToolResult) Observation {
	return Observation{
		StepOrder: request.StepOrder,
		ToolName:  request.Tool.Name,
		Status:    StepStatusSucceeded,
		Outputs:   outputsFromToolResult(request, executionContext, result),
	}
}

func outputsFromToolResult(request ExecuteRequest, executionContext *ExecutionContext, result ToolResult) map[string]any {
	outputs := make(map[string]any, len(result.Outputs))
	for name, output := range result.Outputs {
		if output.Sensitive {
			outputs[name] = executionContext.Store(request.StepID, request.Tool.Name, name, output.Value)
			continue
		}
		outputs[name] = RedactJSONValue(output.Value)
	}

	return outputs
}

func resolveInputs(inputs map[string]any, executionContext *ExecutionContext) map[string]any {
	resolved := make(map[string]any, len(inputs))
	for key, value := range inputs {
		resolveInputValueInto(resolved, key, value, executionContext)
	}

	return resolved
}

func resolveInputValueInto(resolved map[string]any, key string, value any, executionContext *ExecutionContext) {
	switch typed := value.(type) {
	case string:
		contextValue, ok := executionContext.Resolve(typed)
		if !ok {
			resolved[key] = value
			return
		}
		resolved[key] = contextValue.Value
	case map[string]any:
		resolved[key] = resolveInputs(typed, executionContext)
	case []any:
		resolvedSlice := make([]any, 0, len(typed))
		for _, item := range typed {
			wrapped := make(map[string]any, 1)
			resolveInputValueInto(wrapped, key, item, executionContext)
			resolvedSlice = append(resolvedSlice, wrapped[key])
		}
		resolved[key] = resolvedSlice
	default:
		resolved[key] = value
	}
}

func requestExecutionContext(request ExecuteRequest) *ExecutionContext {
	if request.ExecutionContext != nil {
		return request.ExecutionContext
	}

	return NewExecutionContext()
}

func minimalToolEnvironment() []string {
	env := []string{}
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "TEMP", "TMP"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}

	return env
}
