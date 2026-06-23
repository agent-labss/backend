package agent

import (
	"context"
	"strings"
	"testing"
)

func TestRepositoryCreateSchemaRequiresPool(t *testing.T) {
	repository := NewRepository(nil)

	err := repository.CreateSchema(context.Background())

	if err == nil {
		t.Fatal("CreateSchema() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "agent database is missing") {
		t.Fatalf("CreateSchema() error = %q, want missing database context", err)
	}
}

func TestRepositorySchemaContainsTables(t *testing.T) {
	schema := schemaSQL()

	for _, table := range []string{"agent_runs", "agent_run_steps"} {
		if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("schema missing table %q", table)
		}
	}
}
