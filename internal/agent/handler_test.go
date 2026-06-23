package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type fakeRunService struct {
	response RunResponse
	err      error
	called   bool
}

func (service *fakeRunService) Run(_ context.Context, _ CreateRunRequest) (RunResponse, error) {
	service.called = true
	return service.response, service.err
}

func TestHandlerCreateRunReturnsResponse(t *testing.T) {
	service := &fakeRunService{response: RunResponse{RunID: testRunID, Status: RunStatusSucceeded, Answer: "done"}}
	handler := NewHandler(service)
	app := fiber.New()
	app.Post(AgentRunsPath, handler.CreateRun)

	req, err := http.NewRequest(http.MethodPost, AgentRunsPath, bytes.NewReader([]byte(`{"message":"export report"}`)))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("Body.Close() error = %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body RunResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body.RunID != testRunID {
		t.Fatalf("RunID = %q, want %s", body.RunID, testRunID)
	}
	if !service.called {
		t.Fatal("Run() was not called")
	}
}

func TestHandlerCreateRunRejectsBadJSON(t *testing.T) {
	service := &fakeRunService{}
	resp := testCreateRunRequest(t, service, []byte(`{`))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("Run() called for bad JSON, want no call")
	}
}

func TestHandlerCreateRunRejectsBlankMessage(t *testing.T) {
	service := &fakeRunService{}
	resp := testCreateRunRequest(t, service, []byte(`{"message":"   "}`))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("Run() called for blank message, want no call")
	}
}

func testCreateRunRequest(t *testing.T, service *fakeRunService, body []byte) *http.Response {
	t.Helper()

	handler := NewHandler(service)
	app := fiber.New()
	app.Post(AgentRunsPath, handler.CreateRun)

	req, err := http.NewRequest(http.MethodPost, AgentRunsPath, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}

	return resp
}

func closeAgentResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Body.Close() error = %v", err)
	}
}
