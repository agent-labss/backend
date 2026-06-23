package app

import (
	"strings"
	"testing"

	"orderbuddy-ai/backend/internal/config"
)

func TestRunWrapsSQLiteConnectionErrors(t *testing.T) {
	err := Run(config.Config{
		AppEnv:         config.DefaultAppEnv,
		HTTPAddr:       config.DefaultHTTPAddr,
		DatabaseDriver: config.DefaultDatabaseDriver,
		DatabaseURL:    "/dev/null/orderbuddy_ai.db",
	})

	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "connect database: connect sqlite database:") {
		t.Fatalf("Run() error = %q, want sqlite context", err)
	}
}
