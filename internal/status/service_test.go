package status

import (
	"context"
	"errors"
	"testing"
)

type fakeDatabase struct {
	err   error
	calls int
}

func (database *fakeDatabase) Ping(context.Context) error {
	database.calls++
	return database.err
}

func TestServiceReadyReturnsNilWhenDatabasePings(t *testing.T) {
	database := &fakeDatabase{}
	service := NewService(database)

	err := service.Ready(context.Background())

	if err != nil {
		t.Fatalf("Ready() error = %v, want nil", err)
	}
	if database.calls != 1 {
		t.Fatalf("database calls = %d, want 1", database.calls)
	}
}

func TestServiceReadyReturnsErrorWhenDatabaseFails(t *testing.T) {
	expectedErr := errors.New("database unavailable")
	database := &fakeDatabase{err: expectedErr}
	service := NewService(database)

	err := service.Ready(context.Background())

	if !errors.Is(err, expectedErr) {
		t.Fatalf("Ready() error = %v, want %v", err, expectedErr)
	}
	if database.calls != 1 {
		t.Fatalf("database calls = %d, want 1", database.calls)
	}
}

func TestServiceReadyReturnsErrorWhenDatabaseMissing(t *testing.T) {
	service := NewService(nil)

	err := service.Ready(context.Background())

	if err == nil {
		t.Fatal("Ready() error = nil, want error")
	}
}

func TestServiceStatusReportsDatabaseOK(t *testing.T) {
	database := &fakeDatabase{}
	service := NewService(database)

	response := service.Status(context.Background(), "test")

	if response.Service != serviceName {
		t.Fatalf("Service = %q, want %q", response.Service, serviceName)
	}
	if response.Environment != "test" {
		t.Fatalf("Environment = %q, want %q", response.Environment, "test")
	}
	if response.Database.Status != "ok" {
		t.Fatalf("Database.Status = %q, want %q", response.Database.Status, "ok")
	}
	if database.calls != 1 {
		t.Fatalf("database calls = %d, want 1", database.calls)
	}
}

func TestServiceStatusReportsDatabaseError(t *testing.T) {
	database := &fakeDatabase{err: errors.New("database unavailable")}
	service := NewService(database)

	response := service.Status(context.Background(), "test")

	if response.Service != serviceName {
		t.Fatalf("Service = %q, want %q", response.Service, serviceName)
	}
	if response.Environment != "test" {
		t.Fatalf("Environment = %q, want %q", response.Environment, "test")
	}
	if response.Database.Status != "error" {
		t.Fatalf("Database.Status = %q, want %q", response.Database.Status, "error")
	}
	if database.calls != 1 {
		t.Fatalf("database calls = %d, want 1", database.calls)
	}
}
