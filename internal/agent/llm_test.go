package agent

import (
	"encoding/json"
	"testing"
)

const testPlannerToolName = "export_report"

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
