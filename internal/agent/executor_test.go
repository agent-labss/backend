package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai/backend/internal/toolcatalog"
)

const (
	testRunID       = "run_1"
	testStepID      = "step_1"
	testSessionRef  = "ctx://login/session"
	testCookieValue = "cookie-value"
	testPartnerID   = "p_123"
	testSecretToken = "secret-token"
)

func TestCLIExecutorReturnsObservationAndStoresSensitiveOutput(t *testing.T) {
	commandPath := writeToolScript(t, `#!/usr/bin/env sh
cat >/dev/null
printf '%s\n' '{"status":"ok","outputs":{"session":{"sensitive":true,"value":"`+testCookieValue+`"},"partner_id":{"sensitive":false,"value":"`+testPartnerID+`"}},"summary":"done"}'
`)

	executor := NewCLIExecutor(ServiceAccount{Profile: "internal_report_service", Username: "svc", Password: "secret"})
	runContext := NewRunContext()
	observation, err := executor.Execute(context.Background(), ExecuteRequest{
		RunID:      testRunID,
		StepID:     testStepID,
		StepOrder:  1,
		Tool:       toolcatalog.Tool{Name: "login", CommandPath: commandPath, TimeoutMS: 1000, RequiresServiceAccount: true},
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

func TestCLIExecutorFailsOnInvalidJSON(t *testing.T) {
	commandPath := writeToolScript(t, "#!/usr/bin/env sh\nprintf 'not-json'\n")

	executor := NewCLIExecutor(ServiceAccount{})
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

	executor := NewCLIExecutor(ServiceAccount{})
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

	executor := NewCLIExecutor(ServiceAccount{})
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

	executor := NewCLIExecutor(ServiceAccount{})
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

func writeToolScript(t *testing.T, script string) string {
	t.Helper()

	commandPath := filepath.Join(t.TempDir(), "tool.sh")
	if err := os.WriteFile(commandPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return commandPath
}
