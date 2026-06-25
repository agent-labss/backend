package httpapi

import (
	"net/http"
	"reflect"
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

type fakeChatHandler struct{}

func (handler fakeChatHandler) CreateChat(c fiber.Ctx) error {
	return c.SendStatus(http.StatusCreated)
}

func (handler fakeChatHandler) GetChat(c fiber.Ctx) error {
	return c.SendStatus(http.StatusOK)
}

func (handler fakeChatHandler) ListChatMessages(c fiber.Ctx) error {
	return c.SendStatus(http.StatusOK)
}

func (handler fakeChatHandler) CreateChatMessage(c fiber.Ctx) error {
	return c.SendStatus(http.StatusOK)
}

func (handler fakeChatHandler) SubscribeChatEvents(c fiber.Ctx) error {
	return c.SendStatus(http.StatusNoContent)
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

func TestLegacyAgentRunRoutesAreNotRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodPost, "/api/agent/runs", http.StatusNotFound)
	assertRouteStatus(t, http.MethodGet, "/api/agent/runs/run_test", http.StatusNotFound)
	assertRouteStatus(t, http.MethodPost, "/api/agent/runs/run_test/turns", http.StatusNotFound)
}

func TestRouterConfigDoesNotExposeLegacyAgentHandler(t *testing.T) {
	if _, ok := reflect.TypeOf(RouterConfig{}).FieldByName("AgentHandler"); ok {
		t.Fatal("RouterConfig exposes legacy AgentHandler, want chat handlers only")
	}
}

func TestChatRouteIsRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodPost, agent.ChatSessionsPath, http.StatusCreated)
}

func TestChatGetRouteIsRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodGet, agent.ChatSessionsPath+"/chat_test", http.StatusOK)
}

func TestChatMessagesGetRouteIsRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodGet, agent.ChatSessionsPath+"/chat_test/messages", http.StatusOK)
}

func TestChatMessageRouteIsRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodPost, agent.ChatSessionsPath+"/chat_test/messages", http.StatusOK)
}

func TestChatEventsRouteIsRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodGet, agent.ChatSessionsPath+"/chat_test/events", http.StatusNoContent)
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
		StatusHandler:      status.NewHandler(status.NewService(), "test"),
		ToolHandler:        fakeToolHandler{},
		ChatSessionHandler: fakeChatHandler{},
		ChatMessageHandler: fakeChatHandler{},
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
