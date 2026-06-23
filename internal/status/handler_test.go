package status

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

const (
	statusPath = "/api/status"
)

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestHandlerStatusReturnsServiceMetadata(t *testing.T) {
	handler := NewHandler(NewService(), testEnvironment)
	app := fiber.New()
	app.Get(statusPath, handler.Status)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, statusPath, nil))
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer closeResponseBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var response Response
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Service != "ai-backend" {
		t.Fatalf("Service = %q, want %q", response.Service, "ai-backend")
	}
	if response.Environment != testEnvironment {
		t.Fatalf("Environment = %q, want %q", response.Environment, testEnvironment)
	}
}
