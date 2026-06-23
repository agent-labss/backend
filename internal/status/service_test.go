package status

import (
	"context"
	"errors"
	"testing"
)

const testEnvironment = "test"

var errDatabaseUnavailable = errors.New("database unavailable")

type fakeDatabase struct {
	err   error
	calls int
}

func (database *fakeDatabase) Ping(context.Context) error {
	database.calls++
	return database.err
}

func TestServiceStatusReportsDatabaseOK(t *testing.T) {
	database := &fakeDatabase{}
	service := NewService(database)

	response := service.Status(context.Background(), testEnvironment)

	if response.Service != serviceName {
		t.Fatalf("Service = %q, want %q", response.Service, serviceName)
	}
	if response.Environment != testEnvironment {
		t.Fatalf("Environment = %q, want %q", response.Environment, testEnvironment)
	}
	if response.Database.Status != DependencyStatusOK {
		t.Fatalf("Database.Status = %q, want %q", response.Database.Status, DependencyStatusOK)
	}
	if database.calls != 1 {
		t.Fatalf("database calls = %d, want 1", database.calls)
	}
}

func TestServiceStatusReportsDatabaseError(t *testing.T) {
	database := &fakeDatabase{err: errDatabaseUnavailable}
	service := NewService(database)

	response := service.Status(context.Background(), testEnvironment)

	if response.Service != serviceName {
		t.Fatalf("Service = %q, want %q", response.Service, serviceName)
	}
	if response.Environment != testEnvironment {
		t.Fatalf("Environment = %q, want %q", response.Environment, testEnvironment)
	}
	if response.Database.Status != DependencyStatusError {
		t.Fatalf("Database.Status = %q, want %q", response.Database.Status, DependencyStatusError)
	}
	if database.calls != 1 {
		t.Fatalf("database calls = %d, want 1", database.calls)
	}
}
