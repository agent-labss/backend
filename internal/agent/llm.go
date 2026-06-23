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

	"ai/backend/internal/toolcatalog"
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

type plannerTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type OpenAIPlanner struct {
	client openai.Client
	model  string
}

func NewOpenAIPlanner(apiKey string, model string, baseURL string) OpenAIPlanner {
	options := []option.RequestOption{option.WithAPIKey(apiKey)}
	if strings.TrimSpace(baseURL) != "" {
		options = append(options, option.WithBaseURL(strings.TrimSpace(baseURL)))
	}

	return OpenAIPlanner{
		client: openai.NewClient(options...),
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
		Text: plannerResponseTextConfig(),
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
		"instructions":    request.Instructions,
		"message":         request.Message,
		"tools":           plannerTools(request.Tools),
		"observations":    request.Observations,
		"output_contract": "Return exactly one JSON object for the next planner action. Do not include Markdown, prose, fenced JSON, refusals, or partial output.",
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

func plannerResponseTextConfig() responses.ResponseTextConfigParam {
	jsonObject := shared.NewResponseFormatJSONObjectParam()
	return responses.ResponseTextConfigParam{
		Format: responses.ResponseFormatTextConfigUnionParam{
			OfJSONObject: &jsonObject,
		},
	}
}

func plannerTools(tools []toolcatalog.Tool) []plannerTool {
	promptTools := make([]plannerTool, 0, len(tools))
	for _, tool := range tools {
		promptTools = append(promptTools, plannerTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}

	return promptTools
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
