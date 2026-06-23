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
}

func (service fakeRunService) Run(_ context.Context, _ CreateRunRequest) (RunResponse, error) {
	return service.response, service.err
}

func TestHandlerCreateRunReturnsResponse(t *testing.T) {
	handler := NewHandler(fakeRunService{response: RunResponse{RunID: testRunID, Status: RunStatusSucceeded, Answer: "done"}})
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
}

func TestHandlerCreateRunRejectsBadJSON(t *testing.T) {
	handler := NewHandler(fakeRunService{})
	app := fiber.New()
	app.Post(AgentRunsPath, handler.CreateRun)

	req, err := http.NewRequest(http.MethodPost, AgentRunsPath, bytes.NewReader([]byte(`{`)))
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

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}
