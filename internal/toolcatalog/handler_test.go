package toolcatalog

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v3"
)

const testToolName = "export_report"

func TestHandlerRegisterToolReturnsCreated(t *testing.T) {
	dir := t.TempDir()
	commandPath := writeTestCommand(t, dir, testToolName)
	handler := NewHandler(newService(storeFromMemoryRepository(&memoryRepository{}), dir))
	app := fiber.New()
	app.Post(ToolsPath, handler.RegisterTool)

	body := []byte(`{
		"name":"export_report",
		"description":"Export report.",
		"command_path":"` + commandPath + `",
		"input_schema":{"type":"object"},
		"output_schema":{"type":"object"},
		"timeout_ms":1000,
		"requires_service_account":true
	}`)
	resp := testJSONRequest(t, app, http.MethodPost, ToolsPath, body, http.StatusCreated)
	defer closeResponseBody(t, resp)

	var tool Tool
	if err := json.NewDecoder(resp.Body).Decode(&tool); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if tool.Name != testToolName {
		t.Fatalf("tool.Name = %q, want %s", tool.Name, testToolName)
	}
}

func TestHandlerRegisterToolRejectsBadJSON(t *testing.T) {
	handler := NewHandler(newService(storeFromMemoryRepository(&memoryRepository{}), t.TempDir()))
	app := fiber.New()
	app.Post(ToolsPath, handler.RegisterTool)

	testJSONRequestStatus(t, app, http.MethodPost, ToolsPath, []byte(`{`), http.StatusBadRequest)
}

func TestHandlerListToolsReturnsTools(t *testing.T) {
	repository := &memoryRepository{}
	repository.tools = []Tool{{Name: testToolName, Description: "Export.", Status: ToolStatusEnabled}}
	handler := NewHandler(newService(storeFromMemoryRepository(repository), t.TempDir()))
	app := fiber.New()
	app.Get(ToolsPath, handler.ListTools)

	testRequestStatus(t, app, http.MethodGet, ToolsPath, nil, http.StatusOK)
}

func TestHandlerUpdateInstructionsReturnsUpdatedInstructions(t *testing.T) {
	handler := NewHandler(newService(storeFromMemoryRepository(&memoryRepository{}), t.TempDir()))
	app := fiber.New()
	app.Put(AgentInstructionsPath, handler.UpdateInstructions)

	testJSONRequestStatus(t, app, http.MethodPut, AgentInstructionsPath, []byte(`{"content":"Use report tools."}`), http.StatusOK)
}

func writeTestCommand(t *testing.T, dir string, name string) string {
	t.Helper()

	commandPath := filepath.Join(dir, name)
	if err := os.WriteFile(commandPath, []byte("#!/usr/bin/env sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return commandPath
}

func testJSONRequest(t *testing.T, app *fiber.App, method string, path string, body []byte, wantStatus int) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	return testAppRequest(t, app, req, wantStatus)
}

func testRequest(t *testing.T, app *fiber.App, method string, path string, body []byte, wantStatus int) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	return testAppRequest(t, app, req, wantStatus)
}

func testJSONRequestStatus(t *testing.T, app *fiber.App, method string, path string, body []byte, wantStatus int) {
	t.Helper()

	resp := testJSONRequest(t, app, method, path, body, wantStatus)
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Body.Close() error = %v", err)
	}
}

func testRequestStatus(t *testing.T, app *fiber.App, method string, path string, body []byte, wantStatus int) {
	t.Helper()

	resp := testRequest(t, app, method, path, body, wantStatus)
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Body.Close() error = %v", err)
	}
}

func testAppRequest(t *testing.T, app *fiber.App, req *http.Request, wantStatus int) *http.Response {
	t.Helper()

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if resp.StatusCode != wantStatus {
		closeResponseBody(t, resp)
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, wantStatus)
	}

	return resp
}

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Body.Close() error = %v", err)
	}
}
