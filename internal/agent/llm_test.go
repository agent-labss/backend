package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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

func recordPlannerTestCall(called chan<- struct{}) {
	select {
	case called <- struct{}{}:
	default:
	}
}

func assertPlannerRequest(t *testing.T, w http.ResponseWriter, r *http.Request, apiKey string, model string) bool {
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

	var payload struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Errorf("Decode request body error = %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	if payload.Model != model {
		t.Errorf("model = %q, want %q", payload.Model, model)
		http.Error(w, "wrong model", http.StatusBadRequest)
		return false
	}

	return true
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

func TestParsePlannerActionRejectsUnknownAction(t *testing.T) {
	_, err := ParsePlannerAction([]byte(`{"type":"run_shell"}`))

	if err == nil {
		t.Fatal("ParsePlannerAction() error = nil, want error")
	}
}
