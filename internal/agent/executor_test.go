package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai/backend/internal/toolcatalog"
)

const (
	testRunID       = "run_1"
	testStepID      = "step_1"
	testSessionRef  = "ctx://step_1/login/session"
	testCookieValue = "cookie-value"
	testPartnerID   = "p_123"
	testSecretToken = "secret-token"
)

func TestCLIExecutorReturnsObservationAndStoresSensitiveOutput(t *testing.T) {
	commandPath := writeToolScript(t, `#!/usr/bin/env sh
cat >/dev/null
printf '%s\n' '{"status":"ok","outputs":{"session":{"sensitive":true,"value":"`+testCookieValue+`"},"partner_id":{"sensitive":false,"value":"`+testPartnerID+`"}},"summary":"done"}'
`)

	executor := NewCLIExecutor()
	runContext := NewRunContext()
	observation, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      testRunID,
		StepID:     testStepID,
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "login", CommandPath: commandPath, TimeoutMS: 1000},
		Inputs:     map[string]any{},
		RunContext: runContext,
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if observation.Outputs["session"] != testSessionRef {
		t.Fatalf("session output = %v, want ctx ref", observation.Outputs["session"])
	}
	if observation.Outputs["partner_id"] != testPartnerID {
		t.Fatalf("partner_id output = %v, want %s", observation.Outputs["partner_id"], testPartnerID)
	}
	resolved, ok := runContext.Resolve(testSessionRef)
	if !ok || resolved.Value != testCookieValue {
		t.Fatalf("resolved session = %v, %v; want %s, true", resolved.Value, ok, testCookieValue)
	}
}

func TestCLIExecutorResolvesNestedContextReferences(t *testing.T) {
	commandPath := writeToolScript(t, `#!/usr/bin/env sh
python3 -c '
import json
import sys

envelope = json.load(sys.stdin)
print(json.dumps({"status":"ok","outputs":{"received":{"sensitive":False,"value":envelope["inputs"]}}}))
'
`)

	executor := NewCLIExecutor()
	runContext := NewRunContext()
	runContext.Store(testStepID, "login", "session", map[string]any{
		"access_token": testSecretToken,
		"user": map[string]any{
			"id": "u_123",
		},
	})

	observation, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:     testRunID,
		StepID:    testStepID,
		StepOrder: 1,
		Tool:      toolcatalog.Tool{Name: "nested", CommandPath: commandPath, TimeoutMS: 1000},
		Inputs: map[string]any{
			"auth": map[string]any{
				"session": testSessionRef,
			},
			"requests": []any{
				map[string]any{"session": testSessionRef},
			},
		},
		RunContext: runContext,
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	received, ok := observation.Outputs["received"].(map[string]any)
	if !ok {
		t.Fatalf("received output = %T, want map[string]any", observation.Outputs["received"])
	}
	assertNestedSessionResolved(t, received["auth"], []string{"session"})
	requests, ok := received["requests"].([]any)
	if !ok || len(requests) != 1 {
		t.Fatalf("requests = %#v, want one-element array", received["requests"])
	}
	assertNestedSessionResolved(t, requests[0], []string{"session"})
}

func TestCLIExecutorSensitiveOutputReferencesDoNotCollideForRepeatedTool(t *testing.T) {
	commandPath := writeToolScript(t, `#!/usr/bin/env sh
python3 -c '
import json
import sys

envelope = json.load(sys.stdin)
value = "cookie-" + envelope["step_id"]
print(json.dumps({"status":"ok","outputs":{"session":{"sensitive":True,"value":value}}}))
'
`)

	executor := NewCLIExecutor()
	runContext := NewRunContext()

	first, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      testRunID,
		StepID:     "step_1",
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "login", CommandPath: commandPath, TimeoutMS: 1000},
		RunContext: runContext,
	})
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	second, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      testRunID,
		StepID:     "step_2",
		StepOrder:  2,
		Tool:       toolcatalog.Tool{Name: "login", CommandPath: commandPath, TimeoutMS: 1000},
		RunContext: runContext,
	})
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}

	firstRef := requireStringOutput(t, first, "session")
	secondRef := requireStringOutput(t, second, "session")
	if firstRef == secondRef {
		t.Fatalf("session refs both = %q, want distinct refs", firstRef)
	}
	requireResolvedContextValue(t, runContext, firstRef, "cookie-step_1")
	requireResolvedContextValue(t, runContext, secondRef, "cookie-step_2")
}

func TestCLIExecutorDoesNotExposeBackendEnvironmentToTools(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", testSecretToken)
	t.Setenv("DATABASE_URL", "sqlite.db")

	commandPath := writeToolScript(t, `#!/usr/bin/env sh
python3 -c '
import json
import os

print(json.dumps({"status":"ok","outputs":{"env":{"sensitive":False,"value":{
  "openai": os.environ.get("OPENAI_API_KEY"),
  "database": os.environ.get("DATABASE_URL"),
  "path": os.environ.get("PATH")
}}}}))
'
`)

	executor := NewCLIExecutor()
	observation, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      testRunID,
		StepID:     testStepID,
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "env", CommandPath: commandPath, TimeoutMS: 1000},
		RunContext: NewRunContext(),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	env, ok := observation.Outputs["env"].(map[string]any)
	if !ok {
		t.Fatalf("env output = %T, want map[string]any", observation.Outputs["env"])
	}
	if env["openai"] != nil {
		t.Fatalf("OPENAI_API_KEY = %v, want nil", env["openai"])
	}
	if env["database"] != nil {
		t.Fatalf("DATABASE_URL = %v, want nil", env["database"])
	}
	path, ok := env["path"].(string)
	if !ok {
		t.Fatalf("PATH = %T, want string", env["path"])
	}
	if strings.TrimSpace(path) == "" {
		t.Fatal("PATH is empty, want minimal executable path")
	}
}

func TestCLIExecutorFailsOnInvalidJSON(t *testing.T) {
	commandPath := writeToolScript(t, "#!/usr/bin/env sh\nprintf 'not-json'\n")

	executor := NewCLIExecutor()
	_, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      testRunID,
		StepID:     testStepID,
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "bad", CommandPath: commandPath, TimeoutMS: 1000},
		RunContext: NewRunContext(),
	})

	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
}

func TestCLIExecutorReturnsFailedObservationForToolBusinessError(t *testing.T) {
	commandPath := writeToolScript(t, `#!/usr/bin/env sh
printf '%s\n' '{"status":"error","error":{"code":"partner_not_found","message":"No partner matched token abc.def.ghi"}}'
`)

	executor := NewCLIExecutor()
	observation, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      testRunID,
		StepID:     testStepID,
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "find_partner", CommandPath: commandPath, TimeoutMS: 1000},
		RunContext: NewRunContext(),
	})

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if observation.Status != StepStatusFailed {
		t.Fatalf("Status = %q, want failed", observation.Status)
	}
	if strings.Contains(observation.Error, "abc.def.ghi") {
		t.Fatalf("observation.Error = %q, want redacted token", observation.Error)
	}
}

func TestCLIExecutorRedactsStderrOnFailure(t *testing.T) {
	commandPath := writeToolScript(t, "#!/usr/bin/env sh\necho 'Authorization: Bearer "+testSecretToken+"' >&2\nexit 2\n")

	executor := NewCLIExecutor()
	_, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      testRunID,
		StepID:     testStepID,
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "fail", CommandPath: commandPath, TimeoutMS: 1000},
		RunContext: NewRunContext(),
	})

	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
	if strings.Contains(err.Error(), testSecretToken) {
		t.Fatalf("Execute() error = %q, want redacted token", err)
	}
}

func TestCLIExecutorTimesOut(t *testing.T) {
	commandPath := writeToolScript(t, "#!/usr/bin/env sh\nsleep 2\n")

	executor := NewCLIExecutor()
	_, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      testRunID,
		StepID:     testStepID,
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "slow", CommandPath: commandPath, TimeoutMS: 10},
		RunContext: NewRunContext(),
	})

	if err == nil {
		t.Fatal("Execute() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Execute() error = %q, want timeout context", err)
	}
}

func assertNestedSessionResolved(t *testing.T, value any, path []string) {
	t.Helper()

	current := value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("path %v reached %T, want map[string]any", path, current)
		}
		current = object[key]
	}

	session, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("session = %T, want resolved object", current)
	}
	if session["access_token"] != redactedValue {
		encoded, err := json.Marshal(session)
		if err != nil {
			t.Fatalf("Marshal session error = %v", err)
		}
		t.Fatalf("session = %s, want redacted access_token", encoded)
	}
	user, ok := session["user"].(map[string]any)
	if !ok || user["id"] != "u_123" {
		t.Fatalf("session.user = %#v, want resolved user", session["user"])
	}
}

func requireStringOutput(t *testing.T, observation Observation, name string) string {
	t.Helper()

	value, ok := observation.Outputs[name].(string)
	if !ok {
		t.Fatalf("%s output = %T, want string", name, observation.Outputs[name])
	}

	return value
}

func requireResolvedContextValue(t *testing.T, runContext *RunContext, ref string, want string) {
	t.Helper()

	resolved, ok := runContext.Resolve(ref)
	if !ok || resolved.Value != want {
		t.Fatalf("resolved %s = %v, %v; want %s, true", ref, resolved.Value, ok, want)
	}
}

func writeToolScript(t *testing.T, script string) string {
	t.Helper()

	commandPath := filepath.Join(t.TempDir(), "tool.sh")
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return commandPath
}
