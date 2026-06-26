package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai/backend/internal/toolcatalog"
)

const testPlannerToolName = "export_report"

func TestOpenAIPlannerUsesBaseURL(t *testing.T) {
	const (
		testAPIKey = "sk-test"
		testModel  = "third-party-model"
	)

	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordPlannerTestCall(called)
		if !assertPlannerRequest(t, w, r, testAPIKey, testModel) {
			return
		}
		writePlannerTestResponse(t, w, testModel)
	}))
	defer server.Close()

	planner := NewOpenAIPlanner(testAPIKey, testModel, server.URL+"/openai/v1")
	action, err := planner.NextAction(context.Background(), PlanRequest{
		Instructions: "answer directly",
		Message:      "hello",
	})
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	select {
	case <-called:
	default:
		t.Fatal("test server was not called")
	}
	if action.Type != ActionTypeFinalAnswer {
		t.Fatalf("action.Type = %q, want %q", action.Type, ActionTypeFinalAnswer)
	}
	if action.Answer != testRunAnswer {
		t.Fatalf("action.Answer = %q, want %s", action.Answer, testRunAnswer)
	}
}

func TestOpenAIPlannerSendsAttachmentContent(t *testing.T) {
	const (
		testAPIKey = "sk-test"
		testModel  = "third-party-model"
	)

	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordPlannerTestCall(called)
		payload, ok := decodePlannerRequestPayload(t, w, r, testAPIKey, testModel)
		if !ok {
			return
		}
		assertPlannerRequestIncludesContent(t, w, payload, "input_file", "input_image")
		writePlannerTestResponse(t, w, testModel)
	}))
	defer server.Close()

	planner := NewOpenAIPlanner(testAPIKey, testModel, server.URL+"/openai/v1")
	_, err := planner.NextAction(context.Background(), PlanRequest{
		Instructions: "read attachments",
		Message:      "update catalog",
		Attachments: []Attachment{
			{
				ID:       "att_pdf",
				Filename: "merchant_catalog.pdf",
				MIMEType: "application/pdf",
				Kind:     AttachmentKindPDF,
				Size:     8,
				Data:     base64.StdEncoding.EncodeToString([]byte("%PDF-1.7")),
			},
			{
				ID:       "att_img",
				Filename: "menu.png",
				MIMEType: "image/png",
				Kind:     AttachmentKindImage,
				Size:     7,
				Data:     base64.StdEncoding.EncodeToString([]byte("PNGDATA")),
			},
		},
	})
	if err != nil {
		t.Fatalf("NextAction() error = %v", err)
	}
	select {
	case <-called:
	default:
		t.Fatal("test server was not called")
	}
}

func recordPlannerTestCall(called chan<- struct{}) {
	select {
	case called <- struct{}{}:
	default:
	}
}

func assertPlannerRequest(t *testing.T, w http.ResponseWriter, r *http.Request, apiKey string, model string) bool {
	t.Helper()
	_, ok := decodePlannerRequestPayload(t, w, r, apiKey, model)
	return ok
}

func decodePlannerRequestPayload(t *testing.T, w http.ResponseWriter, r *http.Request, apiKey string, model string) (map[string]any, bool) {
	t.Helper()
	if !assertPlannerHTTPEnvelope(t, w, r, apiKey) {
		return nil, false
	}
	payload, raw, ok := decodePlannerPayloadBody(t, w, r)
	if !ok {
		return nil, false
	}
	if !assertPlannerPayloadFields(t, w, payload, model) {
		return nil, false
	}

	return raw, true
}

func assertPlannerHTTPEnvelope(t *testing.T, w http.ResponseWriter, r *http.Request, apiKey string) bool {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Errorf("method = %s, want %s", r.Method, http.MethodPost)
		http.Error(w, "wrong method", http.StatusMethodNotAllowed)
		return false
	}
	if r.URL.Path != "/openai/v1/responses" {
		t.Errorf("path = %s, want /openai/v1/responses", r.URL.Path)
		http.Error(w, "wrong path", http.StatusNotFound)
		return false
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
		t.Errorf("Authorization = %q, want bearer API key", got)
		http.Error(w, "wrong auth", http.StatusUnauthorized)
		return false
	}
	return true
}

type plannerPayloadFields struct {
	Model string `json:"model"`
	Text  struct {
		Format struct {
			Type string `json:"type"`
		} `json:"format"`
	} `json:"text"`
}

func decodePlannerPayloadBody(t *testing.T, w http.ResponseWriter, r *http.Request) (plannerPayloadFields, map[string]any, bool) {
	t.Helper()

	var payload plannerPayloadFields
	var raw map[string]any
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("ReadAll request body error = %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return payload, nil, false
	}
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		t.Errorf("Decode request body error = %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return payload, nil, false
	}
	if err := json.Unmarshal(rawBody, &raw); err != nil {
		t.Errorf("Unmarshal request payload error = %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return payload, nil, false
	}

	return payload, raw, true
}

func assertPlannerPayloadFields(t *testing.T, w http.ResponseWriter, payload plannerPayloadFields, model string) bool {
	t.Helper()
	if payload.Model != model {
		t.Errorf("model = %q, want %q", payload.Model, model)
		http.Error(w, "wrong model", http.StatusBadRequest)
		return false
	}
	if payload.Text.Format.Type != "json_object" {
		t.Errorf("text.format.type = %q, want json_object", payload.Text.Format.Type)
		http.Error(w, "missing structured output format", http.StatusBadRequest)
		return false
	}
	return true
}

func assertPlannerRequestIncludesContent(t *testing.T, w http.ResponseWriter, payload map[string]any, wantTypes ...string) {
	t.Helper()

	raw, err := json.Marshal(payload["input"])
	if err != nil {
		t.Errorf("Marshal input error = %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	input := string(raw)
	for _, wantType := range wantTypes {
		if !strings.Contains(input, `"type":"`+wantType+`"`) {
			t.Errorf("input = %s, want content type %s", input, wantType)
			http.Error(w, "missing input content", http.StatusBadRequest)
			return
		}
	}
	if !strings.Contains(input, `"filename":"merchant_catalog.pdf"`) {
		t.Errorf("input = %s, want PDF filename", input)
		http.Error(w, "missing file filename", http.StatusBadRequest)
		return
	}
	if !strings.Contains(input, `"file_data":"data:application/pdf;base64,`) {
		t.Errorf("input = %s, want PDF data URL", input)
		http.Error(w, "missing file data URL", http.StatusBadRequest)
		return
	}
	if !strings.Contains(input, `"image_url":"data:image/png;base64,`) {
		t.Errorf("input = %s, want image data URL", input)
		http.Error(w, "missing image data URL", http.StatusBadRequest)
		return
	}
}

func writePlannerTestResponse(t *testing.T, w http.ResponseWriter, model string) {
	t.Helper()

	action, err := json.Marshal(PlannerAction{
		Type:   ActionTypeFinalAnswer,
		Answer: testRunAnswer,
	})
	if err != nil {
		t.Errorf("Marshal planner action error = %v", err)
		http.Error(w, "bad response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"id":                 "resp_test",
		"created_at":         0,
		"error":              nil,
		"incomplete_details": nil,
		"instructions":       nil,
		"metadata":           map[string]any{},
		"model":              model,
		"object":             "response",
		"output": []map[string]any{{
			"id":     "msg_test",
			"type":   "message",
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]any{{
				"type":        "output_text",
				"text":        string(action),
				"annotations": []any{},
			}},
		}},
		"parallel_tool_calls": false,
		"temperature":         1,
		"tool_choice":         "auto",
		"tools":               []any{},
		"top_p":               1,
		"status":              "completed",
	}); err != nil {
		t.Errorf("Encode response error = %v", err)
	}
}

func TestParsePlannerActionAcceptsCallTool(t *testing.T) {
	action, err := ParsePlannerAction([]byte(`{"type":"call_tool","tool":"export_report","inputs":{"month":"2026-05"}}`))
	if err != nil {
		t.Fatalf("ParsePlannerAction() error = %v", err)
	}
	if action.Type != ActionTypeCallTool {
		t.Fatalf("action.Type = %q, want call_tool", action.Type)
	}
	if action.Tool != testPlannerToolName {
		t.Fatalf("action.Tool = %q, want %s", action.Tool, testPlannerToolName)
	}

	var inputs map[string]any
	if err := json.Unmarshal(action.Inputs, &inputs); err != nil {
		t.Fatalf("Unmarshal inputs error = %v", err)
	}
	if inputs["month"] != "2026-05" {
		t.Fatalf("month = %v, want 2026-05", inputs["month"])
	}
}

func TestParsePlannerActionAcceptsAskUser(t *testing.T) {
	action, err := ParsePlannerAction([]byte(`{"type":"ask_user","message":"Which account should I use?","payload":{"kind":"choice"}}`))
	if err != nil {
		t.Fatalf("ParsePlannerAction() error = %v", err)
	}
	if action.Type != ActionTypeAskUser {
		t.Fatalf("action.Type = %q, want %q", action.Type, ActionTypeAskUser)
	}
	if action.Message != "Which account should I use?" {
		t.Fatalf("Message = %q, want planner question", action.Message)
	}
}

func TestParsePlannerActionRejectsAskUserWithoutMessage(t *testing.T) {
	_, err := ParsePlannerAction([]byte(`{"type":"ask_user"}`))
	if err == nil {
		t.Fatal("ParsePlannerAction() error = nil, want error")
	}
	if !errors.Is(err, ErrInvalidPlannerAction) {
		t.Fatalf("ParsePlannerAction() error = %v, want ErrInvalidPlannerAction", err)
	}
}

func TestParsePlannerActionRejectsUnknownAction(t *testing.T) {
	_, err := ParsePlannerAction([]byte(`{"type":"run_shell"}`))

	if err == nil {
		t.Fatal("ParsePlannerAction() error = nil, want error")
	}
}

func TestParsePlannerActionRejectsNonJSONPlannerText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "prose", raw: `Here is the next action: {"type":"final_answer","answer":"done"}`},
		{name: "fenced", raw: "```json\n{\"type\":\"final_answer\",\"answer\":\"done\"}\n```"},
		{name: "partial", raw: `{"type":"final_answer","answer":"done"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParsePlannerAction([]byte(test.raw))
			if err == nil {
				t.Fatal("ParsePlannerAction() error = nil, want error")
			}
			if !errors.Is(err, ErrInvalidPlannerAction) {
				t.Fatalf("ParsePlannerAction() error = %v, want ErrInvalidPlannerAction", err)
			}
		})
	}
}

func TestBuildPlannerPromptAllowsAskUser(t *testing.T) {
	prompt, err := buildPlannerPrompt(PlanRequest{Message: "delete duplicate account"})
	if err != nil {
		t.Fatalf("buildPlannerPrompt() error = %v", err)
	}
	if !strings.Contains(string(prompt), string(ActionTypeAskUser)) {
		t.Fatalf("prompt = %s, want ask_user in allowed actions", prompt)
	}
}

func TestBuildPlannerPromptIncludesInterruption(t *testing.T) {
	prompt, err := buildPlannerPrompt(PlanRequest{
		Message: "ok",
		Interruption: &Interruption{
			ID:      "int_test",
			Type:    InterruptionTypeInputRequest,
			Message: "Delete the duplicate account?",
			Payload: json.RawMessage(`{"risk":"destructive"}`),
		},
	})
	if err != nil {
		t.Fatalf("buildPlannerPrompt() error = %v", err)
	}
	if !strings.Contains(string(prompt), "Delete the duplicate account?") {
		t.Fatalf("prompt = %s, want interruption message", prompt)
	}
	if !strings.Contains(string(prompt), "int_test") {
		t.Fatalf("prompt = %s, want interruption id", prompt)
	}
	if !strings.Contains(string(prompt), "interruption") {
		t.Fatalf("prompt = %s, want interruption key", prompt)
	}
}

func TestBuildPlannerPromptIncludesOnlyPlannerToolFields(t *testing.T) {
	prompt, err := buildPlannerPrompt(PlanRequest{
		Instructions: "use tools",
		Message:      "export",
		Tools: []toolcatalog.Tool{{
			ID:           "tool_1",
			Name:         testPlannerToolName,
			Description:  "Export report.",
			CommandPath:  "/trusted/export_report",
			InputSchema:  json.RawMessage(`{"type":"object","properties":{"month":{"type":"string"}}}`),
			OutputSchema: json.RawMessage(`{"type":"object"}`),
			TimeoutMS:    1000,
			Status:       toolcatalog.ToolStatusEnabled,
			CreatedAt:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			UpdatedAt:    time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC),
		}},
	})
	if err != nil {
		t.Fatalf("buildPlannerPrompt() error = %v", err)
	}

	tool := requirePlannerPromptTool(t, prompt)
	requirePlannerToolField(t, tool, "name", testPlannerToolName)
	requirePlannerToolField(t, tool, "description", "Export report.")
	requirePlannerToolIncludesField(t, tool, "input_schema")
	requirePlannerToolOmitsFields(t, tool, "id", "command_path", "output_schema", "timeout_ms", "status", "created_at", "updated_at")
}

func TestBuildPlannerPromptIncludesAttachmentMetadataWithoutData(t *testing.T) {
	rawData := base64.StdEncoding.EncodeToString([]byte("%PDF-1.7"))
	prompt, err := buildPlannerPrompt(PlanRequest{
		Instructions: "use attachments",
		Message:      "update catalog",
		Attachments: []Attachment{{
			ID:       "att_pdf",
			Filename: "merchant_catalog.pdf",
			MIMEType: "application/pdf",
			Kind:     AttachmentKindPDF,
			Size:     8,
			Data:     rawData,
		}},
	})
	if err != nil {
		t.Fatalf("buildPlannerPrompt() error = %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(prompt, &body); err != nil {
		t.Fatalf("Unmarshal prompt error = %v", err)
	}
	attachments, ok := body["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("attachments = %#v, want one attachment", body["attachments"])
	}
	attachment, ok := attachments[0].(map[string]any)
	if !ok {
		t.Fatalf("attachment = %T, want map[string]any", attachments[0])
	}
	if attachment["filename"] != "merchant_catalog.pdf" {
		t.Fatalf("filename = %v, want merchant_catalog.pdf", attachment["filename"])
	}
	if strings.Contains(string(prompt), rawData) {
		t.Fatalf("prompt leaked attachment data: %s", prompt)
	}
}

func requirePlannerPromptTool(t *testing.T, prompt []byte) map[string]any {
	t.Helper()

	var body map[string]any
	if err := json.Unmarshal(prompt, &body); err != nil {
		t.Fatalf("Unmarshal prompt error = %v", err)
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one tool", body["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tool = %T, want map[string]any", tools[0])
	}

	return tool
}

func requirePlannerToolField(t *testing.T, tool map[string]any, field string, want string) {
	t.Helper()

	if tool[field] != want {
		t.Fatalf("%s = %v, want %s", field, tool[field], want)
	}
}

func requirePlannerToolIncludesField(t *testing.T, tool map[string]any, field string) {
	t.Helper()

	if _, ok := tool[field]; !ok {
		t.Fatalf("%s missing from planner tool view", field)
	}
}

func requirePlannerToolOmitsFields(t *testing.T, tool map[string]any, fields ...string) {
	t.Helper()

	for _, field := range fields {
		if _, ok := tool[field]; ok {
			t.Fatalf("planner tool view includes %q: %#v", field, tool)
		}
	}
}
