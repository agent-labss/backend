package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"

	"orderbuddy-ai/backend/internal/agent"
	"orderbuddy-ai/backend/internal/status"
	"orderbuddy-ai/backend/internal/toolcatalog"
)

type fakeDatabase struct{}

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func (database *fakeDatabase) Ping(_ context.Context) error {
	return nil
}

type fakeToolHandler struct{}

func (handler fakeToolHandler) RegisterTool(c fiber.Ctx) error {
	return c.SendStatus(http.StatusCreated)
}

func (handler fakeToolHandler) ListTools(c fiber.Ctx) error {
	return c.SendStatus(http.StatusOK)
}

func (handler fakeToolHandler) UpdateInstructions(c fiber.Ctx) error {
	return c.SendStatus(http.StatusOK)
}

type fakeAgentHandler struct{}

func (handler fakeAgentHandler) CreateRun(c fiber.Ctx) error {
	return c.SendStatus(http.StatusOK)
}

func TestHealthzReturnsOK(t *testing.T) {
	app := NewRouter(RouterConfig{
		StatusHandler: status.NewHandler(status.NewService(&fakeDatabase{}), "test"),
	})

	req, err := http.NewRequest(http.MethodGet, HealthzPath, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer closeResponseBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if body[responseStatusField] != responseStatusOK {
		t.Fatalf("status = %q, want %q", body[responseStatusField], responseStatusOK)
	}
}

func TestOptionsRequestReturnsCORSHeaders(t *testing.T) {
	app := NewRouter(RouterConfig{
		StatusHandler: status.NewHandler(status.NewService(&fakeDatabase{}), "test"),
	})

	req, err := http.NewRequest(http.MethodOptions, StatusPath, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer closeResponseBody(t, resp)

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	if got := resp.Header.Get(headerAccessControlAllowOrigin); got != corsAllowedOrigin {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, corsAllowedOrigin)
	}
	if got := resp.Header.Get(headerAccessControlAllowMethods); got != corsAllowedMethods {
		t.Fatalf("Access-Control-Allow-Methods = %q, want %q", got, corsAllowedMethods)
	}
}

func TestToolRoutesAreRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodPost, toolcatalog.ToolsPath, http.StatusCreated)
}

func TestAgentRunRouteIsRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodPost, agent.AgentRunsPath, http.StatusOK)
}

func assertRouteStatus(t *testing.T, method string, path string, wantStatus int) {
	t.Helper()

	app := NewRouter(RouterConfig{
		StatusHandler: status.NewHandler(status.NewService(&fakeDatabase{}), "test"),
		ToolHandler:   fakeToolHandler{},
		AgentHandler:  fakeAgentHandler{},
	})

	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer closeResponseBody(t, resp)

	if resp.StatusCode != wantStatus {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, wantStatus)
	}
}
