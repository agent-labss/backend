package status

import (
	"context"
	"testing"
)

const testEnvironment = "test"

func TestServiceStatusReportsServiceMetadata(t *testing.T) {
	service := NewService()

	response := service.Status(context.Background(), testEnvironment)

	if response.Service != serviceName {
		t.Fatalf("Service = %q, want %q", response.Service, serviceName)
	}
	if response.Environment != testEnvironment {
		t.Fatalf("Environment = %q, want %q", response.Environment, testEnvironment)
	}
}
