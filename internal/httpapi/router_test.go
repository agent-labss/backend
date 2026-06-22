package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"orderbuddy-ai/backend/internal/status"
)

type fakeDatabase struct{}

func (database *fakeDatabase) Ping(ctx context.Context) error {
	return nil
}

func TestHealthzReturnsOK(t *testing.T) {
	app := NewRouter(RouterConfig{
		StatusHandler: status.NewHandler(status.NewService(&fakeDatabase{}), "test"),
	})

	req, err := http.NewRequest(http.MethodGet, "/healthz", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if body["status"] != "ok" {
		t.Fatalf("status = %q, want %q", body["status"], "ok")
	}
}

func TestOptionsRequestReturnsCORSHeaders(t *testing.T) {
	app := NewRouter(RouterConfig{
		StatusHandler: status.NewHandler(status.NewService(&fakeDatabase{}), "test"),
	})

	req, err := http.NewRequest(http.MethodOptions, "/api/status", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}
