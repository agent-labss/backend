package postgres

import (
	"context"
	"testing"
)

func TestConnectRejectsInvalidDatabaseURL(t *testing.T) {
	pool, err := Connect(context.Background(), "not-a-postgres-url")

	if err == nil {
		t.Fatal("expected error for invalid database URL")
	}
	if pool != nil {
		t.Fatal("expected nil pool for invalid database URL")
	}
}
