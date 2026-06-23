package sqlite

import (
	"context"
	"strings"
	"testing"
)

func TestConnectRejectsInvalidDatabaseURL(t *testing.T) {
	db, err := Connect(context.Background(), "/dev/null/orderbuddy_ai.db")

	if err == nil {
		t.Fatal("expected error for invalid database URL")
	}
	if !strings.Contains(err.Error(), "open sqlite database:") {
		t.Fatalf("error = %q, want open sqlite context", err)
	}
	if db != nil {
		t.Fatal("expected nil db for invalid database URL")
	}
}
