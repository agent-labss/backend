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
	Attachments  []Attachment
	Interaction  *Interaction
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
		Input: plannerResponseInput(prompt, request.Attachments),
		Text:  plannerResponseTextConfig(),
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
		"attachments":     attachmentPromptViews(request.Attachments),
		"interaction":     request.Interaction,
		"tools":           plannerTools(request.Tools),
		"observations":    request.Observations,
		"output_contract": "Return exactly one JSON object for the next planner action. Do not include Markdown, prose, fenced JSON, refusals, or partial output.",
		"allowed_actions": []string{
			string(ActionTypeCallTool),
			string(ActionTypeAskUser),
			string(ActionTypeFinalAnswer),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build planner prompt: %w", err)
	}

	return prompt, nil
}

func plannerResponseInput(prompt []byte, attachments []Attachment) responses.ResponseNewParamsInputUnion {
	if len(attachments) == 0 {
		return responses.ResponseNewParamsInputUnion{OfString: openai.String(string(prompt))}
	}

	content := responses.ResponseInputMessageContentListParam{
		responses.ResponseInputContentParamOfInputText(string(prompt)),
	}
	for _, attachment := range attachments {
		content = append(content, attachmentInputContent(attachment))
	}

	return responses.ResponseNewParamsInputUnion{
		OfInputItemList: responses.ResponseInputParam{
			responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser),
		},
	}
}

func attachmentInputContent(attachment Attachment) responses.ResponseInputContentUnionParam {
	if attachment.Kind == AttachmentKindImage {
		image := responses.ResponseInputImageParam{
			Detail: responses.ResponseInputImageDetailAuto,
		}
		if strings.TrimSpace(attachment.Data) != "" {
			image.ImageURL = openai.String("data:" + attachment.MIMEType + ";base64," + attachment.Data)
		}
		if strings.TrimSpace(attachment.FileID) != "" {
			image.FileID = openai.String(attachment.FileID)
		}
		return responses.ResponseInputContentUnionParam{OfInputImage: &image}
	}

	file := responses.ResponseInputFileParam{
		Filename: openai.String(attachment.Filename),
		Detail:   responses.ResponseInputFileDetailLow,
	}
	if strings.TrimSpace(attachment.Data) != "" {
		file.FileData = openai.String("data:" + strings.TrimSpace(attachment.MIMEType) + ";base64," + attachment.Data)
	}
	if strings.TrimSpace(attachment.FileID) != "" {
		file.FileID = openai.String(attachment.FileID)
	}
	return responses.ResponseInputContentUnionParam{OfInputFile: &file}
}

func attachmentPromptViews(attachments []Attachment) []map[string]any {
	views := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		views = append(views, attachmentPromptView(attachment))
	}
	return views
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
	case ActionTypeAskUser:
		return validateAskUserAction(action)
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

func validateAskUserAction(action PlannerAction) error {
	if strings.TrimSpace(action.Message) == "" {
		return fmt.Errorf("%w: message is required", ErrInvalidPlannerAction)
	}
	if len(action.Payload) > 0 && !isJSONObject(action.Payload) {
		return fmt.Errorf("%w: payload must be a JSON object", ErrInvalidPlannerAction)
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
