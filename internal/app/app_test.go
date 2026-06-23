package app

import (
	"strings"
	"testing"

	"orderbuddy-ai/backend/internal/config"
)

func TestRunWrapsPostgresConnectionErrors(t *testing.T) {
	err := Run(config.Config{
		AppEnv:      config.DefaultAppEnv,
		HTTPAddr:    config.DefaultHTTPAddr,
		DatabaseURL: "not-a-postgres-url",
	})

	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "connect postgres:") {
		t.Fatalf("Run() error = %q, want postgres context", err)
	}
}
