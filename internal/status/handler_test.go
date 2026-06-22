package status

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestHandlerReadyzReturnsOKWhenDatabasePings(t *testing.T) {
	handler := NewHandler(NewService(&fakeDatabase{}), "test")
	app := fiber.New()
	app.Get("/readyz", handler.Readyz)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandlerReadyzReturnsServiceUnavailableWhenDatabaseFails(t *testing.T) {
	handler := NewHandler(NewService(&fakeDatabase{err: errors.New("database unavailable")}), "test")
	app := fiber.New()
	app.Get("/readyz", handler.Readyz)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandlerStatusReturnsDatabaseOK(t *testing.T) {
	handler := NewHandler(NewService(&fakeDatabase{}), "test")
	app := fiber.New()
	app.Get("/api/status", handler.Status)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer resp.Body.Close()

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
	if response.Environment != "test" {
		t.Fatalf("Environment = %q, want %q", response.Environment, "test")
	}
	if response.Database.Status != "ok" {
		t.Fatalf("Database.Status = %q, want %q", response.Database.Status, "ok")
	}
}

func TestHandlerStatusReturnsDatabaseErrorWithOKHTTPStatus(t *testing.T) {
	handler := NewHandler(NewService(&fakeDatabase{err: errors.New("database unavailable")}), "test")
	app := fiber.New()
	app.Get("/api/status", handler.Status)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var response Response
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Database.Status != "error" {
		t.Fatalf("Database.Status = %q, want %q", response.Database.Status, "error")
	}
}
