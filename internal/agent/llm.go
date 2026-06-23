package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"orderbuddy-ai/backend/internal/toolcatalog"
)

var ErrInvalidPlannerAction = errors.New("invalid planner action")

type PlanRequest struct {
	Instructions string
	Message      string
	Tools        []toolcatalog.Tool
	Observations []Observation
}

type Planner interface {
	NextAction(ctx context.Context, request PlanRequest) (PlannerAction, error)
}

type OpenAIPlanner struct {
	client openai.Client
	model  string
}

func NewOpenAIPlanner(apiKey string, model string) OpenAIPlanner {
	return OpenAIPlanner{
		client: openai.NewClient(option.WithAPIKey(apiKey)),
		model:  model,
	}
}

func (planner OpenAIPlanner) NextAction(ctx context.Context, request PlanRequest) (PlannerAction, error) {
	prompt, err := buildPlannerPrompt(request)
	if err != nil {
		return PlannerAction{}, err
	}

	response, err := planner.client.Responses.New(ctx, responses.ResponseNewParams{
		Model: shared.ResponsesModel(planner.model),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(string(prompt)),
		},
	})
	if err != nil {
		return PlannerAction{}, fmt.Errorf("openai response: %w", err)
	}

	return ParsePlannerAction([]byte(response.OutputText()))
}

func ParsePlannerAction(raw []byte) (PlannerAction, error) {
	var action PlannerAction
	if err := json.Unmarshal(raw, &action); err != nil {
		return PlannerAction{}, fmt.Errorf("%w: decode JSON: %w", ErrInvalidPlannerAction, err)
	}

	if err := validatePlannerAction(action); err != nil {
		return PlannerAction{}, err
	}

	return action, nil
}

func buildPlannerPrompt(request PlanRequest) ([]byte, error) {
	prompt, err := json.Marshal(map[string]any{
		"instructions": request.Instructions,
		"message":      request.Message,
		"tools":        request.Tools,
		"observations": request.Observations,
		"allowed_actions": []string{
			string(ActionTypeCallTool),
			string(ActionTypeFinalAnswer),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build planner prompt: %w", err)
	}

	return prompt, nil
}

func validatePlannerAction(action PlannerAction) error {
	switch action.Type {
	case ActionTypeCallTool:
		return validateCallToolAction(action)
	case ActionTypeFinalAnswer:
		return validateFinalAnswerAction(action)
	default:
		return fmt.Errorf("%w: unknown action type %q", ErrInvalidPlannerAction, action.Type)
	}
}

func validateCallToolAction(action PlannerAction) error {
	if strings.TrimSpace(action.Tool) == "" {
		return fmt.Errorf("%w: tool is required", ErrInvalidPlannerAction)
	}
	if !isJSONObject(action.Inputs) {
		return fmt.Errorf("%w: inputs must be a JSON object", ErrInvalidPlannerAction)
	}

	return nil
}

func validateFinalAnswerAction(action PlannerAction) error {
	if strings.TrimSpace(action.Answer) == "" {
		return fmt.Errorf("%w: answer is required", ErrInvalidPlannerAction)
	}

	return nil
}

func isJSONObject(raw json.RawMessage) bool {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}

	return value != nil
}
