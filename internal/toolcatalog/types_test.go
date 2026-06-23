package toolcatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestToolValidateAcceptsTrustedCommand(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "login_internal_site")
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := Tool{
		Name:         "login_internal_site",
		Description:  "Login to internal site.",
		CommandPath:  commandPath,
		InputSchema:  json.RawMessage(`{"type":"object","properties":{}}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		TimeoutMS:    10000,
		Status:       ToolStatusEnabled,
	}

	if err := tool.Validate(dir); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestToolValidateRejectsInvalidName(t *testing.T) {
	tool := Tool{
		Name:         "Login Tool",
		Description:  "Login.",
		CommandPath:  "/tmp/login",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		TimeoutMS:    1000,
	}

	if err := tool.Validate("/tmp"); err == nil {
		t.Fatal("Validate() error = nil, want invalid name error")
	}
}

func TestToolValidateRejectsPathOutsideTrustedDir(t *testing.T) {
	trustedDir := t.TempDir()
	outsideDir := t.TempDir()
	commandPath := filepath.Join(outsideDir, "tool")
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := Tool{
		Name:         "export_report",
		Description:  "Export report.",
		CommandPath:  commandPath,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		TimeoutMS:    1000,
	}

	if err := tool.Validate(trustedDir); err == nil {
		t.Fatal("Validate() error = nil, want trusted directory error")
	}
}

func TestToolValidateRejectsNonObjectSchema(t *testing.T) {
	tool := Tool{
		Name:         "export_report",
		Description:  "Export report.",
		CommandPath:  "/tmp/export_report",
		InputSchema:  json.RawMessage(`[]`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		TimeoutMS:    1000,
	}

	if err := tool.Validate("/tmp"); err == nil {
		t.Fatal("Validate() error = nil, want schema error")
	}
}

func TestToolValidateDefaultsStatusToEnabled(t *testing.T) {
	dir := t.TempDir()
	commandPath := filepath.Join(dir, "export_report")
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tool := Tool{
		Name:         "export_report",
		Description:  "Export report.",
		CommandPath:  commandPath,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		TimeoutMS:    1000,
	}

	if err := tool.Validate(dir); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if tool.NormalizedStatus() != ToolStatusEnabled {
		t.Fatalf("NormalizedStatus() = %q, want %q", tool.NormalizedStatus(), ToolStatusEnabled)
	}
}
