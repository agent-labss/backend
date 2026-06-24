package httpapi

import (
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"

	"ai/backend/internal/agent"
	"ai/backend/internal/status"
	"ai/backend/internal/toolcatalog"
)

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
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

func TestOptionsRequestReturnsCORSHeaders(t *testing.T) {
	app := NewRouter(RouterConfig{
		StatusHandler: status.NewHandler(status.NewService(), "test"),
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

func TestStatusRouteIsRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodGet, StatusPath, http.StatusOK)
}

func TestToolRoutesAreRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodPost, toolcatalog.ToolsPath, http.StatusCreated)
}

func TestAgentRunRouteIsRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodPost, agent.AgentRunsPath, http.StatusOK)
}

func TestNewRouterUsesUploadBodyLimit(t *testing.T) {
	app := NewRouter(RouterConfig{
		StatusHandler: status.NewHandler(status.NewService(), "test"),
	})

	if app.Config().BodyLimit != uploadBodyLimit {
		t.Fatalf("BodyLimit = %d, want %d", app.Config().BodyLimit, uploadBodyLimit)
	}
}

func assertRouteStatus(t *testing.T, method string, path string, wantStatus int) {
	t.Helper()

	app := NewRouter(RouterConfig{
		StatusHandler: status.NewHandler(status.NewService(), "test"),
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
