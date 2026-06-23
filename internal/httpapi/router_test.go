package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"orderbuddy-ai/backend/internal/status"
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
}
